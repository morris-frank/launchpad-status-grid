// Package liveaudio is the visualiser's live Levels sources: everything the
// machine is playing, through a Core Audio process tap, and the desk's
// microphone. Both hand samples to the same spectrum.Analyser the WAV source
// uses, so a sound draws the same picture however it arrived.
//
// This file is the half that is pure Go and runs everywhere: reading the
// capture's latest samples, and deciding when a capture that is running is in
// fact refused. The cgo half is capture_darwin.go.
//
// # A refused capture is not a quiet room
//
// macOS does not fail a capture it has not been granted. A process tap nobody
// allowed to record system audio, and a microphone nobody allowed to listen,
// both run and hand over digital silence — every sample exactly zero. Drawn,
// that is a dark grid, which reads as "nothing is playing" and is a claim
// nothing measured. So a source that has never delivered one non-zero sample
// is asked, after proofWindow, whether zeros can be a real room: a
// microphone's never can, because a real one has a noise floor; the system's
// can only while no process is running output. When the answer is no, the
// source says so once and hands every later reading to its fallback — the
// synthetic source, which names itself in every reading.
//
// One non-zero sample proves the capture for good. A tap that has worked
// and then goes quiet is a room that went quiet.
package liveaudio

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/adapter/spectrum"
	launchpadapp "github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
)

// Source names a live input.
type Source string

// The two live inputs.
const (
	System     Source = "system"
	Microphone Source = "microphone"
)

// proofWindow is how long a capture may hand over only zeros before it is
// asked whether zeros could be real. Long enough for a device to start and a
// first buffer to land; short enough that a refused source is replaced before
// anybody has stared at a dark grid wondering.
const proofWindow = 2 * time.Second

// deliveryWindow is how long a capture may deliver no buffer at all. Longer
// than proofWindow because it was measured: the first tap this machine ever
// opened blocked three seconds in its start and delivered nothing for four
// more, and every tap after it delivered from the first second.
const deliveryWindow = 10 * time.Second

// askEvery bounds how often the system is asked whether anything is playing:
// it is a walk over every audio process, and Level runs thirty times a second.
const askEvery = time.Second

// grant is where macOS keeps each input's permission.
var grant = map[Source]string{System: "Screen & System Audio Recording", Microphone: "Microphone"}

// ErrRefused is a capture that runs and hears nothing it could have heard.
var ErrRefused = errors.New("liveaudio: the capture is running and handing over only silence")

// capture is what the cgo half provides: the latest samples, mono and -1..1,
// and whether exact zeros can be what this input really hears.
type capture interface {
	// latest copies the newest len(dst) samples into dst and reports the
	// rate and how many samples have ever been delivered.
	latest(dst []float64) (rate int, delivered uint64)
	// zerosAreReal reports whether a stream of exact zeros can, right now, be
	// what this input hears rather than a refusal.
	zerosAreReal() bool
	close()
}

// Levels implements app.Levels over a live capture.
type Levels struct {
	name     Source
	fallback launchpadapp.Levels
	refused  func(error)

	mu        sync.Mutex
	cap       capture
	opened    time.Time
	proven    bool
	asked     time.Time // when zerosAreReal last answered
	zeroSince time.Time // when zeros first stopped being explainable; zero while they are
	buf       []float64
	analyser  spectrum.Analyser
}

// Open starts src. It fails when the capture cannot start at all — no cgo, a
// macOS older than the tap, no device — and a caller draws its fallback
// instead. A capture that starts and turns out to be refused is Levels' own
// discovery: refused is told once and fallback draws from then on.
func Open(src Source, now time.Time, fallback launchpadapp.Levels, refused func(error)) (*Levels, error) {
	c, err := start(src)
	if err != nil {
		return nil, err
	}
	return newLevels(src, c, now, fallback, refused), nil
}

func newLevels(name Source, c capture, now time.Time, fallback launchpadapp.Levels, refused func(error)) *Levels {
	if refused == nil {
		refused = func(error) {}
	}
	return &Levels{name: name, cap: c, opened: now, fallback: fallback, refused: refused, buf: make([]float64, spectrum.Window)}
}

// Level implements app.Levels.
func (l *Levels) Level(now time.Time) launchpadapp.Loudness {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cap == nil {
		return l.fallback.Level(now)
	}
	rate, delivered := l.cap.latest(l.buf)
	if !l.proven {
		if err := l.prove(now, delivered); err != nil {
			l.cap.close()
			l.cap = nil
			l.refused(err)
			return l.fallback.Level(now)
		}
	}
	if rate <= 0 {
		rate = 48000
	}
	return l.analyser.Read(l.buf, rate, string(l.name))
}

// prove latches the capture as working on its first non-zero sample, and
// returns an error once zeros have stopped being explainable.
func (l *Levels) prove(now time.Time, delivered uint64) error {
	for _, s := range l.buf {
		if s != 0 {
			l.proven = true
			return nil
		}
	}
	if delivered == 0 {
		if now.Sub(l.opened) < deliveryWindow {
			return nil
		}
		return errors.New("liveaudio: " + string(l.name) + " capture started and delivered no audio at all")
	}
	if now.Sub(l.opened) < proofWindow || now.Sub(l.asked) < askEvery {
		return nil
	}
	l.asked = now
	if l.cap.zerosAreReal() {
		l.zeroSince = time.Time{}
		return nil
	}
	// Twice, a second apart, so a sound that started between one buffer and
	// the next is not mistaken for a refusal.
	if l.zeroSince.IsZero() {
		l.zeroSince = now
		return nil
	}
	return fmt.Errorf("%w: %s — System Settings › Privacy & Security › %s", ErrRefused, l.name, grant[l.name])
}

// Close stops the capture. Readings after it are the fallback's.
func (l *Levels) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cap != nil {
		l.cap.close()
		l.cap = nil
	}
}
