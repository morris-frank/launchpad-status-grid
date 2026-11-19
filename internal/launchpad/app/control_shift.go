package app

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// Shift is the board's one modifier, shared at the composition root between
// the shift pad and every control with a second meaning.
//
// It latches rather than being held: a press arms it, and the next shifted
// press — or a second press on shift — spends it. A board only ever acts on a
// pad going down, and the terminal twin presses and releases in one keystroke,
// so a held modifier would be one surface's behaviour and not the other's.
type Shift struct{ armed atomic.Bool }

// Take reports whether shift was armed, and disarms it.
func (s *Shift) Take() bool { return s.armed.Swap(false) }

// Armed reports whether the next shifted press will be shifted.
func (s *Shift) Armed() bool { return s.armed.Load() }

// ShiftControl is the pad that arms Shift: dim while idle, bright and pulsing
// while armed, so a latch left on is visible from across the desk.
type ShiftControl struct{ shift *Shift }

// NewShiftControl arms s.
func NewShiftControl(s *Shift) *ShiftControl { return &ShiftControl{shift: s} }

// Render implements Control.
func (c *ShiftControl) Render(_ time.Time, _ domain.Size) domain.Frame {
	cell := domain.White.Cell(domain.LevelDim)
	if c.shift.Armed() {
		cell = domain.White.Cell(domain.LevelFull).With(domain.BPulse)
	}
	return domain.Blank().SetGrid(0, 0, cell)
}

// Press implements Control.
func (c *ShiftControl) Press(_ context.Context, at Slot) (Outcome, error) {
	if at.Index() != 0 {
		return Outcome{}, nil
	}
	if c.shift.Take() {
		return Outcome{Note: "shift released"}, nil
	}
	c.shift.armed.Store(true)
	return Outcome{Note: "shift armed for the next press"}, nil
}

// Describe implements Describer.
func (c *ShiftControl) Describe(_ time.Time, at Slot) string {
	switch {
	case at.Index() != 0:
		return ""
	case c.shift.Armed():
		return "shift · armed: the next shifted pad takes its second meaning"
	default:
		return "shift · press, then a pad, for that pad's second meaning"
	}
}
