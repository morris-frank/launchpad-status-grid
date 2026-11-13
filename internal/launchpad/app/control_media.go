package app

import (
	"context"
	"fmt"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// MediaKey is one of the system media keys a pad can press.
type MediaKey int

const (
	PlayPause MediaKey = iota
	NextTrack
)

func (k MediaKey) String() string {
	if k == NextTrack {
		return "next track"
	}
	return "play/pause"
}

// Media presses a system media key, exactly as the keyboard's own would. It
// reads nothing back: whatever player answers the key is the system's choice,
// and what is playing is deliberately not the board's to show.
type Media interface {
	Send(ctx context.Context, k MediaKey) error
}

// MediaControl is one pad: it plays or pauses, and with Shift armed skips to
// the next track. It wears the colour of what a press would send, so an armed
// shift is visible on the pad it changes.
type MediaControl struct {
	media Media
	shift *Shift
}

// NewMediaControl presses keys through m, reading shift for the second key.
func NewMediaControl(m Media, shift *Shift) *MediaControl {
	return &MediaControl{media: m, shift: shift}
}

// next is the key a press would send now, without spending the shift.
func (c *MediaControl) next() MediaKey {
	if c.shift.Armed() {
		return NextTrack
	}
	return PlayPause
}

func (k MediaKey) hue() domain.Colour {
	if k == NextTrack {
		return domain.Cyan
	}
	return domain.Green
}

// Render implements Control.
func (c *MediaControl) Render(_ time.Time, _ domain.Size) domain.Frame {
	return domain.Blank().SetGrid(0, 0, c.next().hue().Cell(domain.LevelBright))
}

// Press implements Control.
func (c *MediaControl) Press(ctx context.Context, at Slot) (Outcome, error) {
	if at.Index() != 0 {
		return Outcome{}, nil
	}
	key := PlayPause
	if c.shift.Take() {
		key = NextTrack
	}
	if err := c.media.Send(ctx, key); err != nil {
		return Outcome{Note: "could not press " + key.String(), Attention: true},
			fmt.Errorf("launchpad: media %s: %w", key, err)
	}
	return Outcome{Note: "pressed " + key.String()}, nil
}

// Describe implements Describer.
func (c *MediaControl) Describe(_ time.Time, at Slot) string {
	if at.Index() != 0 {
		return "no media key on this pad"
	}
	if c.shift.Armed() {
		return "next track · shift is armed"
	}
	return "play/pause · shift: next track"
}
