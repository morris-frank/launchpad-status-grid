package app

import (
	"context"
	"fmt"
	"time"
)

// Rates are the two speeds a board runs at.
//
// Settled, it redraws slowly, because the only thing changing is the data
// behind a screen and no eye needs that at 30 Hz. While an effect is in flight
// it redraws at the frame rate, and stops again the moment the effect is spent.
// That is the whole reason a board left showing costs nothing: the device
// renders flash and pulse from its own clock, the host sends no bytes, and
// there is no timer armed.
const (
	// IdleRate is how often a settled board recomputes. Fast enough that a
	// quota bar visibly moves, slow enough to be free.
	IdleRate = 250 * time.Millisecond
	// FrameRate caps host-animated effects. 30 fps: CoreMIDI accepts frames far
	// faster than this, which is the problem — an unpaced writer builds a
	// latency backlog you can see, and a dropped frame self-corrects.
	FrameRate = 33 * time.Millisecond
	// RefreshRate is how often the screens re-read their sources. Slower than
	// the redraw by an order of magnitude, because a directory walk is not a
	// frame: the board redraws to move an animation, and refreshes to learn
	// something new.
	RefreshRate = 3 * time.Second
)

// Refresher is a screen with a source to re-read. It is optional — a screen
// computed entirely from the moment it is handed does not implement it.
type Refresher interface {
	Refresh(ctx context.Context) error
}

// Refresh re-reads every screen's source, not just the showing one.
//
// Refreshing all of them is what lets the refresh run on its own goroutine
// without a lock: the screen list never changes, so nothing here touches the
// state the run loop is mutating. It costs nothing extra in practice because
// screens over the same source share it — two screens reading agent sessions
// are given one cached reader at the composition root, and the second refresh
// is a map lookup.
//
// Errors are deliberately dropped. A screen that could not read draws that
// itself, which is more use than a run loop that stopped over it.
func (b *Board) Refresh(ctx context.Context) {
	for _, s := range b.screens {
		if r, ok := s.(Refresher); ok {
			_ = r.Refresh(ctx)
		}
	}
}

// Clock is the board's view of time, so a test can drive a run loop without
// sleeping. The zero value of Real is the production one.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

// Real is the wall clock.
type Real struct{}

func (Real) Now() time.Time                         { return time.Now() }
func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Run drives board on dev until ctx is cancelled or the device stops.
//
// It owns exactly one thing: when to redraw. Everything else is a decision the
// board or a screen already made, which is what keeps this function short
// enough to be obviously correct — a run loop that also knew about screens is
// where the two-rate tick would quietly become one rate.
func Run(ctx context.Context, board *Board, dev Device, clock Clock) error {
	if clock == nil {
		clock = Real{}
	}
	presses := dev.Presses()

	// Sources are read on their own goroutine so that a slow directory walk
	// cannot hold up a frame, and the first read happens there too — the board
	// is showing immediately with whatever it has, which is an outline saying
	// "not read yet", and fills in a moment later.
	go func() {
		board.Refresh(ctx)
		ticker := time.NewTicker(RefreshRate)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				board.Refresh(ctx)
			}
		}
	}()

	// Draw once before waiting for anything, so a board is showing the instant
	// it starts rather than one tick later.
	if err := dev.Render(board.Render(clock.Now())); err != nil {
		return fmt.Errorf("launchpad: first render: %w", err)
	}

	for {
		rate := IdleRate
		if board.Animating() {
			rate = FrameRate
		}

		select {
		case <-ctx.Done():
			return ctx.Err()

		case p, ok := <-presses:
			if !ok {
				return nil
			}
			// A press failing is the screen's problem to report, not the run
			// loop's to die of: a board that exited because one action could
			// not find a terminal would be worse than useless.
			if err := board.Handle(ctx, p); err != nil {
				board.lastNote = err.Error()
			}

		case f := <-board.flashes:
			board.Flash(f, clock.Now())

		case <-clock.After(rate):
		}

		if err := dev.Render(board.Render(clock.Now())); err != nil {
			return fmt.Errorf("launchpad: render: %w", err)
		}
	}
}
