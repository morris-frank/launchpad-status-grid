package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// held is a clock whose tick never comes, so a test drives the loop by presses
// alone and never races a timer.
type held struct{ at time.Time }

func (h held) Now() time.Time                     { return h.at }
func (held) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

type recorder struct {
	presses chan app.Press
	frames  []domain.Frame
	fail    error
}

func (r *recorder) Render(f domain.Frame) error { r.frames = append(r.frames, f); return r.fail }
func (r *recorder) Presses() <-chan app.Press   { return r.presses }
func (r *recorder) Close() error                { return nil }

// A board that only drew on its first tick would be dark for a quarter second
// after it started, which reads as "it did not work" on a surface with no other
// feedback.
func TestTheBoardIsShowingBeforeTheFirstTick(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)
	dev := &recorder{presses: make(chan app.Press)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Run(ctx, b, dev, held{at: now}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v, want context.Canceled", err)
	}
	if len(dev.frames) == 0 {
		t.Fatal("Run returned having drawn nothing")
	}
}

// A press that fails is a screen's problem to report, not the loop's to die of.
// A board that exited because one action could not find a terminal would be
// worse than useless: the surface goes dark and takes the other seven screens
// with it.
func TestAFailedPressDoesNotEndTheLoop(t *testing.T) {
	t.Parallel()
	one, other := two()
	one.err = errors.New("no terminal")
	one.notify = make(chan domain.Pad)
	b := board(t, one, other)

	presses := make(chan app.Press, 1)
	pad, _ := domain.Grid(0, 0)
	presses <- app.Press{Pad: pad, Down: true, At: now}
	dev := &recorder{presses: presses}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- app.Run(ctx, b, dev, held{at: now}) }()

	<-one.notify // the press reached the screen, which is about to fail it
	cancel()

	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v, want the loop to have survived the press", err)
	}
	if b.LastNote() != "no terminal" {
		t.Errorf("LastNote is %q, want the failure reported", b.LastNote())
	}
}

// A device that cannot be written to is not recoverable from in here — the
// surface is gone and every subsequent frame would fail the same way.
func TestARenderFailureStopsTheLoop(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)
	dev := &recorder{presses: make(chan app.Press), fail: errors.New("device gone")}

	if err := app.Run(context.Background(), b, dev, held{at: now}); err == nil {
		t.Fatal("Run ignored a device that could not be drawn to")
	}
}
