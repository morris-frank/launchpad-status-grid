//go:build darwin && cgo

// Package coremidi is the Launchpad's device adapter: CoreMIDI through cgo,
// with no Go MIDI library between this binary and the framework.
//
// # Why cgo, and why no library
//
// Golden rule 7 asks that a native dependency be justified rather than
// assumed. There is no pure-Go CoreMIDI, and the one maintained macOS driver —
// gomidi's rtmididrv, wrapping RtMidi — has no device add/remove notification
// in its API, so a daemon built on it degrades hotplug into a port-list poll.
// That is a fact about RtMidi rather than about Go: MIDIClientCreate takes a
// plain C MIDINotifyProc, which cgo supplies directly, and a probe against the
// physical unit on 2026-09-07 confirmed hotplug, endpoint discovery by name,
// Programmer mode and LED writes all work with no library at all.
//
// The measurement that could have disqualified the approach was how long the
// Go runtime holds CoreMIDI's receive thread. Against the unit: 28 packets from
// hand-played pads, none dropped. Against a synthetic source at volume: p50
// 3.2 µs, p99.9 27 µs with no collector running, and a few hundred µs under
// realistic allocation. Most of that tail is macOS preempting the receive
// thread, which Swift pays too. A thread inside C is not stopped by Go's
// collector; it blocks only if it re-enters Go during a stop-the-world.
//
// # What that costs everywhere else
//
// This file is darwin-only and cgo-only, so `GOOS=linux go build ./...` never
// compiles it — see unsupported.go, which is what the rest of the world links
// and which fails with a sentence rather than a link error. CI runs on Linux,
// which means **nothing in CI ever type-checks this file**. Anything here that
// can be tested without the framework belongs in domain or app instead, and
// almost all of it already does: this package resolves endpoints, encodes, and
// diffs, and the encoding and diffing are the parts with a test.
package coremidi

/*
#cgo LDFLAGS: -framework CoreMIDI -framework CoreFoundation
#include <CoreMIDI/CoreMIDI.h>
#include <CoreFoundation/CoreFoundation.h>
#include <string.h>

extern void goLaunchpadPacket(unsigned char *data, int len);
extern void goLaunchpadNotify(int msgID);

// The read callback. CoreMIDI owns and schedules this thread; everything this
// function does is time the thread is not receiving MIDI, so it does exactly
// one thing -- hand the bytes to Go, which copies them into a buffered channel
// and returns.
static void cRead(const MIDIPacketList *pl, void *a, void *b) {
	const MIDIPacket *p = &pl->packet[0];
	for (UInt32 i = 0; i < pl->numPackets; i++) {
		goLaunchpadPacket((unsigned char *)p->data, (int)p->length);
		p = MIDIPacketNext(p);
	}
}

// Hotplug. This is the facility a Go MIDI library cannot offer and cgo can:
// MIDIClientCreate takes a plain C function pointer.
static void cNotify(const MIDINotification *m, void *r) { goLaunchpadNotify((int)m->messageID); }

static OSStatus mkClient(MIDIClientRef *out)               { return MIDIClientCreate(CFSTR("launchpad-status-grid"), cNotify, NULL, out); }
static OSStatus mkInput(MIDIClientRef c, MIDIPortRef *out) { return MIDIInputPortCreate(c, CFSTR("launchpad-status-grid-in"), cRead, NULL, out); }
static OSStatus mkOutput(MIDIClientRef c, MIDIPortRef *out){ return MIDIOutputPortCreate(c, CFSTR("launchpad-status-grid-out"), out); }

static void endpointName(MIDIEndpointRef e, char *buf, int cap) {
	CFStringRef s = NULL;
	buf[0] = 0;
	if (MIDIObjectGetStringProperty(e, kMIDIPropertyName, &s) == noErr && s) {
		CFStringGetCString(s, buf, cap, kCFStringEncodingUTF8);
		CFRelease(s);
	}
}

// A full-board SysEx is 81 specs of 5 bytes plus a header, so the buffer is
// sized for the worst case rather than the common one.
static OSStatus sendBytes(MIDIPortRef port, MIDIEndpointRef dest, unsigned char *bytes, int len) {
	unsigned char storage[2048];
	MIDIPacketList *pl = (MIDIPacketList *)storage;
	MIDIPacket *cur = MIDIPacketListInit(pl);
	cur = MIDIPacketListAdd(pl, sizeof(storage), cur, 0, len, bytes);
	if (!cur) return -1;
	return MIDISend(port, dest, pl);
}

static void stopRunLoop(CFRunLoopRef rl) { if (rl) CFRunLoopStop(rl); }
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// The exact CoreMIDI port names, measured against the unit rather than read
// from documentation. The DAW pair is the DAW-control surface protocol and
// speaks about transport rather than about the grid; binding it looks like a
// working connection and lights nothing.
const (
	SourceName = "LPMiniMK3 MIDI Out"
	DestName   = "LPMiniMK3 MIDI In"
)

// Programmer mode is what makes the grid addressable at all. The device echoes
// the same message back on its MIDI Out, and that echo is the handshake.
var (
	programmerOn  = []byte{0xF0, 0x00, 0x20, 0x29, 0x02, 0x0D, 0x0E, 0x01, 0xF7}
	programmerOff = []byte{0xF0, 0x00, 0x20, 0x29, 0x02, 0x0D, 0x0E, 0x00, 0xF7}
)

// ErrNoDevice is returned by Open when no Launchpad is attached. It is not a
// failure of this package and callers should say so: the fix is a USB cable.
var ErrNoDevice = errors.New("launchpad: no Launchpad Mini MK3 found")

// Device is a connected Launchpad. It implements app.Device.
type Device struct {
	client  C.MIDIClientRef
	in, out C.MIDIPortRef
	source  C.MIDIEndpointRef
	dest    C.MIDIEndpointRef

	presses  chan app.Press
	prev     surface
	havePrev bool

	runLoop C.CFRunLoopRef
	closing atomic.Bool
	once    sync.Once
}

// CoreMIDI's read callback is a C function pointer with no room for a Go
// closure, so the running device is package state. One Launchpad, one process:
// a second Open while a device is live is refused rather than silently
// stealing the first one's callback.
var (
	liveMu sync.Mutex
	live   *Device
)

// ErrAlreadyOpen is returned when a device is already running in this process.
var ErrAlreadyOpen = errors.New("launchpad: a device is already open in this process")

// Open connects to the Launchpad and puts it into Programmer mode.
//
// It must be called from a goroutine locked to an OS thread that will run a
// CFRunLoop, because CoreMIDI delivers notifications to the run loop current
// when the client was created. Run does that locking; a caller that wires this
// up itself has to.
func Open() (*Device, error) {
	liveMu.Lock()
	defer liveMu.Unlock()
	if live != nil {
		return nil, ErrAlreadyOpen
	}

	d := &Device{presses: make(chan app.Press, 256)}
	if st := C.mkClient(&d.client); st != 0 {
		return nil, fmt.Errorf("launchpad: MIDIClientCreate: OSStatus %d", int(st))
	}
	if st := C.mkInput(d.client, &d.in); st != 0 {
		return nil, fmt.Errorf("launchpad: MIDIInputPortCreate: OSStatus %d", int(st))
	}
	if st := C.mkOutput(d.client, &d.out); st != 0 {
		return nil, fmt.Errorf("launchpad: MIDIOutputPortCreate: OSStatus %d", int(st))
	}

	live = d
	if err := d.resolve(); err != nil {
		live = nil
		return nil, err
	}
	if d.source == 0 {
		live = nil
		return nil, ErrNoDevice
	}
	return d, nil
}

// Presses implements app.Device.
func (d *Device) Presses() <-chan app.Press { return d.presses }

// Render implements app.Device. It diffs against the last frame sent and
// writes one batched SysEx carrying only the lights that changed, so a settled
// board sends nothing at all.
//
// The diff is on the ENCODED bytes rather than on the Frame, which matters:
// two different cells that light identically — a solid green at level zero and
// an off cell — produce no traffic, and the board's own layering routinely
// creates exactly that.
func (d *Device) Render(f domain.Frame) error {
	next := encode(f)
	if d.havePrev && next == d.prev {
		return nil
	}

	msg := sysexFor(next, d.prev, d.havePrev)
	d.prev, d.havePrev = next, true
	if len(msg) == 0 {
		return nil
	}
	return d.send(msg)
}

// Close returns the device to Live mode and tears the client down. Leaving a
// board lit after this binary exits would be a lie about what is running.
func (d *Device) Close() error {
	var err error
	d.once.Do(func() {
		d.closing.Store(true)
		if d.dest != 0 {
			err = d.send(programmerOff)
		}
		C.stopRunLoop(d.runLoop)
		if d.client != 0 {
			C.MIDIClientDispose(d.client)
		}
		liveMu.Lock()
		if live == d {
			live = nil
		}
		liveMu.Unlock()
		// presses is deliberately not closed. The only writer is CoreMIDI's
		// receive thread, which this goroutine cannot synchronise with, so a
		// close here races a send on a closed channel. The run loop ends on
		// its context instead, and an unplugged Launchpad is not the end of
		// the device anyway -- resolve() picks it back up on replug.
	})
	return err
}

// Run drives the CFRunLoop the notification and read callbacks are delivered
// on, and returns when Close stops it. It locks its goroutine to an OS thread
// for the run loop's whole life.
func (d *Device) Run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	d.runLoop = C.CFRunLoopGetCurrent()
	C.CFRunLoopRun()
}

func (d *Device) send(b []byte) error {
	if d.dest == 0 || len(b) == 0 {
		return nil
	}
	st := C.sendBytes(d.out, d.dest, (*C.uchar)(unsafe.Pointer(&b[0])), C.int(len(b)))
	if st != 0 {
		return fmt.Errorf("launchpad: MIDISend: OSStatus %d", int(st))
	}
	return nil
}

// resolve re-scans the endpoint list from scratch rather than tracking
// add/remove deltas. Stateless re-resolution is what makes unplugging and
// replugging the device a no-op instead of a state machine.
//
// It returns an error because the two calls that can fail here fail silently
// in exactly the shape of the bug this package is easiest to misdiagnose as:
// a device that resolves, lights, and never delivers a press reads as dead
// hardware, and a discarded OSStatus is the one witness that it was not.
func (d *Device) resolve() error {
	var src, dst C.MIDIEndpointRef
	buf := make([]C.char, 256)

	for i := C.ItemCount(0); i < C.MIDIGetNumberOfSources(); i++ {
		e := C.MIDIGetSource(i)
		C.endpointName(e, &buf[0], C.int(len(buf)))
		if C.GoString(&buf[0]) == SourceName {
			src = e
		}
	}
	for i := C.ItemCount(0); i < C.MIDIGetNumberOfDestinations(); i++ {
		e := C.MIDIGetDestination(i)
		C.endpointName(e, &buf[0], C.int(len(buf)))
		if C.GoString(&buf[0]) == DestName {
			dst = e
		}
	}

	if src == d.source && dst == d.dest {
		return nil
	}
	if d.source != 0 {
		C.MIDIPortDisconnectSource(d.in, d.source)
	}
	d.source, d.dest = src, dst
	if src == 0 {
		return nil
	}
	if st := C.MIDIPortConnectSource(d.in, src, nil); st != 0 {
		// Give the source back rather than keeping it. Holding it would make
		// the equality check above believe this port is already connected, so
		// every later notification would return early and the device would
		// stay deaf until the process restarted -- a permanent failure built
		// out of a transient one.
		d.source = 0
		return fmt.Errorf("launchpad: MIDIPortConnectSource: OSStatus %d", int(st))
	}
	// The board on the other side of a reconnect is dark, and the diff does
	// not know that. Forgetting the previous frame is what makes the next
	// render a full repaint rather than a repaint of nothing.
	d.havePrev = false
	return d.send(programmerOn)
}

//export goLaunchpadPacket
func goLaunchpadPacket(data *C.uchar, length C.int) {
	d := live
	if d == nil || d.closing.Load() || length < 3 {
		return
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(data)), int(length))

	status, num, velocity := b[0]&0xF0, b[1], b[2]
	var isNote bool
	switch status {
	case 0x90:
		isNote = true
	case 0xB0:
		isNote = false
	default:
		return // clock, aftertouch, the Programmer-mode echo: not a press
	}

	pad, ok := domain.FromDevice(num, isNote)
	if !ok || !pad.Pressable() {
		return
	}

	// Sent without blocking. If the board is behind, the press is dropped
	// rather than the device stalled -- the callback is on CoreMIDI's own
	// receive thread and must never wait for a consumer.
	select {
	case d.presses <- app.Press{Pad: pad, Down: velocity > 0, At: time.Now()}:
	default:
	}
}

//export goLaunchpadNotify
func goLaunchpadNotify(C.int) {
	if d := live; d != nil && !d.closing.Load() {
		// A C callback has nobody to return an error to. Dropping it is safe
		// only because a failed resolve leaves the source unclaimed, which
		// makes the next notification the retry.
		_ = d.resolve()
	}
}
