//go:build darwin && cgo

// The cgo half: a Core Audio process tap for everything playing, and an
// AudioQueue for the microphone, each writing mono float samples into a ring
// that Go copies from when a frame is drawn.
//
// # Why this and not the alternatives
//
// A process tap (AudioHardwareCreateProcessTap, macOS 14.2) is the platform's
// own API for hearing what the machine plays. A loopback driver is a kernel
// extension somebody has to install, and ScreenCaptureKit is a screen-capture
// framework that asks for the screen to reach the audio beside it. The tap is
// C plus one Objective-C class (CATapDescription), so cgo reaches it directly,
// as micaudio reaches AudioQueue and coremidi reaches CoreMIDI — no Go audio
// library, which would be PortAudio or miniaudio and a native dependency.
//
// The microphone is a second AudioQueue rather than micaudio's recorder:
// micaudio is internal/transform/dictate's adapter and writes a file, and a
// context never imports another's adapter. What they share is the framework.
//
// # Nothing on the audio thread enters Go
//
// The callbacks run on Core Audio's real-time threads. They mix to mono into
// a fixed ring and bump an atomic counter; Go reads the ring when it draws. A
// torn read costs one frame a glitch, and nothing here waits on the collector.
//
// # What that costs everywhere else
//
// darwin-and-cgo only, so CI never type-checks this file; see unsupported.go.
// CoreAudio is weak-linked by availability, so a binary built here still
// launches on a macOS older than the tap, where System fails to start and the
// board draws its fallback.
package liveaudio

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework CoreAudio -framework AudioToolbox -framework Foundation
#import <Foundation/Foundation.h>
#import <CoreAudio/CoreAudio.h>
#import <CoreAudio/AudioHardwareTapping.h>
#import <CoreAudio/CATapDescription.h>
#import <AudioToolbox/AudioToolbox.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define RING 16384

typedef struct {
	float ring[RING];
	_Atomic uint64_t written;
	double rate;
	AudioObjectID tap, agg;
	AudioDeviceIOProcID proc;
	AudioQueueRef queue;
} capture;

// Every buffer is float32, interleaved or not: mix each frame's channels.
static void push(capture *c, const AudioBufferList *in) {
	if (in == NULL || in->mNumberBuffers == 0 || in->mBuffers[0].mNumberChannels == 0) return;
	UInt32 frames = in->mBuffers[0].mDataByteSize / sizeof(float) / in->mBuffers[0].mNumberChannels;
	uint64_t w = atomic_load_explicit(&c->written, memory_order_relaxed);
	for (UInt32 f = 0; f < frames; f++) {
		float s = 0; int n = 0;
		for (UInt32 b = 0; b < in->mNumberBuffers; b++) {
			const AudioBuffer *buf = &in->mBuffers[b];
			UInt32 ch = buf->mNumberChannels;
			if (buf->mData == NULL || (f+1)*ch*sizeof(float) > buf->mDataByteSize) continue;
			for (UInt32 k = 0; k < ch; k++) { s += ((const float *)buf->mData)[f*ch+k]; n++; }
		}
		c->ring[(w+f) % RING] = n ? s/n : 0;
	}
	atomic_store_explicit(&c->written, w+frames, memory_order_release);
}

static OSStatus cTapIO(AudioObjectID dev, const AudioTimeStamp *now, const AudioBufferList *in,
                       const AudioTimeStamp *inTime, AudioBufferList *out, const AudioTimeStamp *outTime, void *ud) {
	push((capture *)ud, in);
	return noErr;
}

static void cMicIO(void *ud, AudioQueueRef q, AudioQueueBufferRef buf, const AudioTimeStamp *ts,
                   UInt32 n, const AudioStreamPacketDescription *d) {
	AudioBufferList l;
	l.mNumberBuffers = 1;
	l.mBuffers[0].mNumberChannels = 1;
	l.mBuffers[0].mDataByteSize = buf->mAudioDataByteSize;
	l.mBuffers[0].mData = buf->mAudioData;
	push((capture *)ud, &l);
	AudioQueueEnqueueBuffer(q, buf, 0, NULL);
}

static void cClose(capture *c) {
	if (c == NULL) return;
	if (c->queue) { AudioQueueStop(c->queue, true); AudioQueueDispose(c->queue, true); }
	if (@available(macOS 14.2, *)) {
		if (c->agg && c->proc) { AudioDeviceStop(c->agg, c->proc); AudioDeviceDestroyIOProcID(c->agg, c->proc); }
		if (c->agg) AudioHardwareDestroyAggregateDevice(c->agg);
		if (c->tap) AudioHardwareDestroyProcessTap(c->tap);
	}
	free(c);
}

static OSStatus deviceUID(AudioObjectID dev, CFStringRef *uid) {
	AudioObjectPropertyAddress a = {kAudioDevicePropertyDeviceUID, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	UInt32 sz = sizeof(*uid);
	return AudioObjectGetPropertyData(dev, &a, 0, NULL, &sz, uid);
}

// The tap is global and excludes nobody: this binary plays nothing. It is private,
// so no other application sees it, and unmuted, so hearing is all it does.
// The aggregate device is what an IOProc can run on; the default output is
// its clock, as a tap alone has none to follow.
static OSStatus startSystem(capture **out) API_AVAILABLE(macos(14.2)) {
	capture *c = calloc(1, sizeof(capture));
	OSStatus err = noErr;
	@autoreleasepool {
		CATapDescription *d = [[CATapDescription alloc] initStereoGlobalTapButExcludeProcesses:@[]];
		d.privateTap = YES;
		d.muteBehavior = CATapUnmuted;
		err = AudioHardwareCreateProcessTap(d, &c->tap);
		if (err != noErr) { c->tap = 0; cClose(c); return err; }

		AudioObjectPropertyAddress fa = {kAudioTapPropertyFormat, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
		AudioStreamBasicDescription f;
		UInt32 sz = sizeof(f);
		err = AudioObjectGetPropertyData(c->tap, &fa, 0, NULL, &sz, &f);
		if (err != noErr) { cClose(c); return err; }
		if (f.mFormatID != kAudioFormatLinearPCM || !(f.mFormatFlags & kAudioFormatFlagIsFloat) || f.mBitsPerChannel != 32) {
			cClose(c); return kAudioHardwareUnsupportedOperationError;
		}
		c->rate = f.mSampleRate;

		AudioObjectPropertyAddress oa = {kAudioHardwarePropertyDefaultSystemOutputDevice, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
		AudioObjectID outDev = 0;
		sz = sizeof(outDev);
		err = AudioObjectGetPropertyData(kAudioObjectSystemObject, &oa, 0, NULL, &sz, &outDev);
		CFStringRef outUID = NULL;
		if (err == noErr) err = deviceUID(outDev, &outUID);
		if (err != noErr) { cClose(c); return err; }

		NSString *main = (__bridge_transfer NSString *)outUID;
		NSDictionary *desc = @{
			@kAudioAggregateDeviceNameKey: @"launchpad-status-grid visualiser tap",
			@kAudioAggregateDeviceUIDKey: [[NSUUID UUID] UUIDString],
			@kAudioAggregateDeviceMainSubDeviceKey: main,
			@kAudioAggregateDeviceIsPrivateKey: @YES,
			@kAudioAggregateDeviceIsStackedKey: @NO,
			@kAudioAggregateDeviceTapAutoStartKey: @YES,
			@kAudioAggregateDeviceSubDeviceListKey: @[ @{ @kAudioSubDeviceUIDKey: main } ],
			@kAudioAggregateDeviceTapListKey: @[ @{ @kAudioSubTapUIDKey: d.UUID.UUIDString, @kAudioSubTapDriftCompensationKey: @YES } ],
		};
		err = AudioHardwareCreateAggregateDevice((__bridge CFDictionaryRef)desc, &c->agg);
		if (err != noErr) { c->agg = 0; cClose(c); return err; }
		err = AudioDeviceCreateIOProcID(c->agg, cTapIO, c, &c->proc);
		if (err != noErr) { c->proc = NULL; cClose(c); return err; }
		err = AudioDeviceStart(c->agg, c->proc);
		if (err != noErr) { cClose(c); return err; }
	}
	*out = c;
	return noErr;
}

static OSStatus cStartSystem(capture **out) {
	if (@available(macOS 14.2, *)) return startSystem(out);
	return kAudioHardwareUnsupportedOperationError;
}

// 48 kHz mono float: the rate the tap runs at, so the two draw alike.
static OSStatus cStartMic(capture **out) {
	capture *c = calloc(1, sizeof(capture));
	AudioStreamBasicDescription f;
	memset(&f, 0, sizeof(f));
	f.mSampleRate       = 48000.0;
	f.mFormatID         = kAudioFormatLinearPCM;
	f.mFormatFlags      = kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked;
	f.mChannelsPerFrame = 1;
	f.mBitsPerChannel   = 32;
	f.mFramesPerPacket  = 1;
	f.mBytesPerFrame    = 4;
	f.mBytesPerPacket   = 4;
	c->rate = f.mSampleRate;
	OSStatus err = AudioQueueNewInput(&f, cMicIO, c, NULL, NULL, 0, &c->queue);
	if (err != noErr) { c->queue = NULL; cClose(c); return err; }
	for (int i = 0; i < 3 && err == noErr; i++) {
		AudioQueueBufferRef b;
		err = AudioQueueAllocateBuffer(c->queue, 4096, &b);
		if (err == noErr) err = AudioQueueEnqueueBuffer(c->queue, b, 0, NULL);
	}
	if (err == noErr) err = AudioQueueStart(c->queue, NULL);
	if (err != noErr) { cClose(c); return err; }
	*out = c;
	return noErr;
}

static uint64_t cLatest(capture *c, double *dst, int n) {
	uint64_t w = atomic_load_explicit(&c->written, memory_order_acquire);
	for (int i = 0; i < n; i++) {
		int64_t j = (int64_t)w - n + i;
		dst[i] = j < 0 ? 0 : c->ring[j % RING];
	}
	return w;
}

// Whether any process but this one is playing, which is when a tap's zeros
// stop being explainable as silence.
static int othersPlaying(void) API_AVAILABLE(macos(14.2)) {
	AudioObjectPropertyAddress a = {kAudioHardwarePropertyProcessObjectList, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
	UInt32 sz = 0;
	if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &a, 0, NULL, &sz) != noErr || sz == 0) return 0;
	AudioObjectID *ids = malloc(sz);
	int playing = 0;
	if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &a, 0, NULL, &sz, ids) == noErr) {
		pid_t self = getpid();
		for (UInt32 i = 0; i < sz/sizeof(AudioObjectID) && !playing; i++) {
			AudioObjectPropertyAddress ra = {kAudioProcessPropertyIsRunningOutput, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
			AudioObjectPropertyAddress pa = {kAudioProcessPropertyPID, kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain};
			UInt32 running = 0, rs = sizeof(running);
			pid_t pid = 0; UInt32 ps = sizeof(pid);
			if (AudioObjectGetPropertyData(ids[i], &ra, 0, NULL, &rs, &running) != noErr || !running) continue;
			if (AudioObjectGetPropertyData(ids[i], &pa, 0, NULL, &ps, &pid) == noErr && pid == self) continue;
			playing = 1;
		}
	}
	free(ids);
	return playing;
}

// Before the tap there is no process list, and no tap to be refused.
static int cOthersPlaying(void) {
	if (@available(macOS 14.2, *)) return othersPlaying();
	return 1;
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"
)

type darwinCapture struct {
	src  Source
	once sync.Once
	c    *C.capture
}

func start(src Source) (capture, error) {
	var c *C.capture
	var status C.OSStatus
	switch src {
	case System:
		status = C.cStartSystem(&c)
	case Microphone:
		status = C.cStartMic(&c)
	default:
		return nil, fmt.Errorf("liveaudio: no source %q", src)
	}
	if status != 0 {
		return nil, fmt.Errorf("liveaudio: %s capture would not start (OSStatus %d)", src, int(status))
	}
	return &darwinCapture{src: src, c: c}, nil
}

func (d *darwinCapture) latest(dst []float64) (int, uint64) {
	if len(dst) == 0 {
		return int(d.c.rate), 0
	}
	w := C.cLatest(d.c, (*C.double)(unsafe.Pointer(&dst[0])), C.int(len(dst)))
	return int(d.c.rate), uint64(w)
}

// A real microphone has a noise floor, so its exact zeros are never a room.
func (d *darwinCapture) zerosAreReal() bool {
	return d.src == System && C.cOthersPlaying() == 0
}

func (d *darwinCapture) close() { d.once.Do(func() { C.cClose(d.c) }) }
