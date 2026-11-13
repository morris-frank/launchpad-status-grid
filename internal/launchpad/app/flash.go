package app

import (
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// Flash is a notification the board draws: the whole surface, chrome
// included, lit in one hue for a moment and then handed back to whatever was
// showing. It is an effect like the wipe, so it never leaves a lamp behind —
// a board that kept a notification lit would be showing a state, and the
// screens are what show state.
type Flash int

const (
	// FlashStarted is an agent run starting: the longest and the calmest,
	// because it is news and not a verdict.
	FlashStarted Flash = iota + 1
	// FlashEnded is an agent run ending cleanly: short, and not green,
	// because a session that ended has not necessarily succeeded at anything.
	FlashEnded
	// FlashSucceeded and FlashFailed are an event's verdict. Red is the
	// palette's attention colour, which is exactly what a failure is.
	FlashSucceeded
	FlashFailed
)

var flashes = map[Flash]struct {
	name string
	hue  domain.Colour
	over time.Duration
}{
	FlashStarted:   {"agent run started", domain.Blue, 600 * time.Millisecond},
	FlashEnded:     {"agent run ended", domain.Mint, 250 * time.Millisecond},
	FlashSucceeded: {"succeeded", domain.Green, 400 * time.Millisecond},
	FlashFailed:    {"failed", domain.Red, 400 * time.Millisecond},
}

// flashing is the flash in flight, as Describe reads it from another goroutine.
type flashing struct {
	name  string
	until time.Time
}

// Notify asks the board to draw f, from any goroutine: the run loop draws it on
// its next turn. A notification arriving while the queue is full is dropped,
// because a flash late enough to queue behind three others is news nobody
// would connect to its event.
func (b *Board) Notify(f Flash) {
	select {
	case b.flashes <- f:
	default:
	}
}

// Flash arms f as the effect in flight from now. It belongs to the goroutine
// that renders, as Show does; anything else calls Notify.
func (b *Board) Flash(f Flash, now time.Time) {
	spec, ok := flashes[f]
	if !ok {
		return
	}
	cell := spec.hue.Cell(domain.LevelBright)
	b.effect = &effect{
		frame: func(float64) domain.Frame {
			return domain.Blank().Map(func(domain.Pad, domain.Cell) domain.Cell { return cell })
		},
		start: now,
		over:  spec.over,
	}
	b.flashing.Store(&flashing{name: spec.name, until: now.Add(spec.over)})
}

// flashName is the flash showing at now, if one is.
func (b *Board) flashName(now time.Time) (string, bool) {
	if f := b.flashing.Load(); f != nil && now.Before(f.until) {
		return f.name, true
	}
	return "", false
}
