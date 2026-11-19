package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// Each notification covers the whole board, chrome included, in its own hue,
// and is gone once it has run: a flash that left a lamp lit would be a
// persistent state nobody asked the board to show.
func TestEachFlashCoversTheBoardAndReturnsItToThePriorFrame(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		flash app.Flash
		hue   domain.Colour
		name  string
	}{
		{app.FlashStarted, domain.Blue, "started"},
		{app.FlashEnded, domain.Mint, "ended"},
		{app.FlashSucceeded, domain.Green, "succeeded"},
		{app.FlashFailed, domain.Red, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			one, other := two()
			b := board(t, one, other)
			before := b.Render(now)

			b.Flash(tc.flash, now)
			during := b.Render(now)
			for p := domain.Pad(0); p < domain.PadCount; p++ {
				if c := during.At(p); c.Palette != tc.hue.Palette {
					t.Fatalf("pad %d shows palette %d during the flash, want %d", p, c.Palette, tc.hue.Palette)
				}
			}
			if got := b.Describe(now, domain.Logo); !strings.Contains(got, tc.name) {
				t.Errorf("during the flash the logo says %q, want it to name the flash %q", got, tc.name)
			}

			later := now.Add(time.Second)
			if got := b.Render(later); !got.Equal(before) {
				t.Error("the board did not return to its prior frame once the flash ran out")
			}
			if b.Animating() {
				t.Error("a spent flash is still animating")
			}
			if got := b.Describe(later, domain.Logo); strings.Contains(got, "flash") {
				t.Errorf("after the flash the logo still says %q", got)
			}
		})
	}
}

// Notify is called from whatever goroutine saw the event, and the run loop is
// the one that draws it, so a notification reaches the device without anyone
// pressing anything.
func TestANotificationReachesTheDeviceThroughTheRunLoop(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)
	dev := &streamed{frames: make(chan domain.Frame, 16), presses: make(chan app.Press)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = app.Run(ctx, b, dev, nil) }()
	<-dev.frames

	b.Notify(app.FlashFailed)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f := <-dev.frames:
			if f.At(domain.Logo).Palette == domain.Red.Palette {
				return
			}
		case <-deadline:
			t.Fatal("a failure notification never reached the device")
		}
	}
}

type streamed struct {
	frames  chan domain.Frame
	presses chan app.Press
}

func (r *streamed) Render(f domain.Frame) error {
	select {
	case r.frames <- f:
	default:
	}
	return nil
}
func (r *streamed) Presses() <-chan app.Press { return r.presses }
func (r *streamed) Close() error              { return nil }
