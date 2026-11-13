// Package tui draws a Launchpad in the terminal and lets you press it.
//
// It is a second implementation of app.Device, and that is the entire point:
// the board it drives is the same board the hardware drives, running the same
// screens over the same frames. A screen can therefore be built, watched and
// changed with no Launchpad plugged in, and what you see here is what the
// device will do — not a mock of it, the actual composition. It is the only
// exercise the board gets on CI, since the darwin-and-cgo half is never built
// on the Linux runner, so a change this preview cannot render is one nothing
// in `mise run check` sees.
//
// It is also the answer to "what is this board even showing". Eighty-one
// lights carry a lot once you know the colours and nothing at all before that,
// so this surface prints the screen names, the tab row and the last press
// beside the picture. Configuration in any persisted sense is not built: the
// screens a board runs are chosen at the composition root, and this shows you
// what that choice produced.
package tui

import (
	"errors"
	"sync"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// Device is a Launchpad rendered in a terminal.
type Device struct {
	// frames carries the newest frame to the model. Depth one and
	// latest-wins: a run loop rendering at 30 fps must never be slowed by a
	// terminal, and an intermediate frame nobody drew is worth nothing.
	frames  chan domain.Frame
	presses chan app.Press

	closeOnce sync.Once
	closed    chan struct{}
}

// ErrClosed is returned by Render after the surface has gone.
var ErrClosed = errors.New("launchpad: the terminal surface is closed")

// NewDevice builds a terminal Launchpad.
func NewDevice() *Device {
	return &Device{
		frames:  make(chan domain.Frame, 1),
		presses: make(chan app.Press, 32),
		closed:  make(chan struct{}),
	}
}

// Render implements app.Device.
func (d *Device) Render(f domain.Frame) error {
	select {
	case <-d.closed:
		return ErrClosed
	default:
	}

	// Drop whatever is queued and post the new frame. Blocking here would let
	// a terminal that is not being redrawn — a scrolled-back pane, a suspended
	// job — hold up the board that hardware is also driving.
	select {
	case <-d.frames:
	default:
	}
	select {
	case d.frames <- f:
	default:
	}
	return nil
}

// Presses implements app.Device.
func (d *Device) Presses() <-chan app.Press { return d.presses }

// Close implements app.Device.
func (d *Device) Close() error {
	d.closeOnce.Do(func() { close(d.closed) })
	return nil
}

// Frames yields the newest frame the board rendered, latest-wins, which is
// what the preview draws — and what a test reads to see what the device would.
func (d *Device) Frames() <-chan domain.Frame { return d.frames }

// Press posts a press and its release, as one keystroke does, or drops it if
// the board is not keeping up. Dropping a keystroke is better than a terminal
// that stops accepting them.
func (d *Device) Press(pad domain.Pad, at time.Time) {
	for _, down := range []bool{true, false} {
		select {
		case d.presses <- app.Press{Pad: pad, Down: down, At: at}:
		default:
		}
	}
}
