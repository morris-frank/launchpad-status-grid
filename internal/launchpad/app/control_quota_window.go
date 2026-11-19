package app

import (
	"context"
	"fmt"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// QuotaWindow is which of a subscription's windows a QuotaWindowControl draws.
type QuotaWindow int

const (
	FiveHour QuotaWindow = iota
	SevenDay
)

func (w QuotaWindow) String() string {
	if w == SevenDay {
		return "seven-day"
	}
	return "five-hour"
}

func (w QuotaWindow) of(sub SubscriptionReading) WindowReading {
	if w == SevenDay {
		return sub.SevenDay
	}
	return sub.FiveHour
}

// QuotaWindowControl is one column per subscription, in the order the
// subscription pads sit, filled in the colour of how much of one window is
// left and blinking once it is near the limit. The colour is the reading; a
// bar would spend the column's height on a precision nobody reads at arm's
// length.
//
// A window nothing measured is grey, which is no hue on the Ramp.
type QuotaWindowControl struct {
	feed   *QuotaFeed
	window QuotaWindow
}

// NewQuotaWindowControl draws window from feed.
func NewQuotaWindowControl(feed *QuotaFeed, window QuotaWindow) *QuotaWindowControl {
	return &QuotaWindowControl{feed: feed, window: window}
}

// Refresh implements Refresher.
func (c *QuotaWindowControl) Refresh(ctx context.Context) error { return c.feed.Refresh(ctx) }

// Render implements Control.
func (c *QuotaWindowControl) Render(_ time.Time, size domain.Size) domain.Frame {
	status, loaded := c.feed.Value()
	if !loaded {
		return unread(size)
	}
	f := domain.Blank()
	for col, sub := range status.Subscriptions {
		if col >= size.Cols {
			break
		}
		column := domain.Region{Col: col, Rows: size.Rows, Cols: 1}
		w := c.window.of(sub)
		if !sub.Measured || !w.Known {
			f = f.Fill(column, domain.Grey.Cell(domain.LevelDim))
			continue
		}
		f = f.Fill(column, domain.Ramp(w.Remaining).Cell(domain.LevelFull))
		if w.Remaining <= nearLimit {
			f = f.Blink(column, 0)
		}
	}
	return f
}

// Press implements Control.
func (c *QuotaWindowControl) Press(_ context.Context, at Slot) (Outcome, error) {
	return Outcome{Note: c.Describe(time.Time{}, at)}, nil
}

// Describe implements Describer.
func (c *QuotaWindowControl) Describe(_ time.Time, at Slot) string {
	status, loaded := c.feed.Value()
	if !loaded {
		return c.window.String() + " quota · not read yet"
	}
	if at.Col >= len(status.Subscriptions) {
		return "no subscription in this column"
	}
	sub := status.Subscriptions[at.Col]
	w := c.window.of(sub)
	switch {
	case !sub.Measured || !w.Known:
		return fmt.Sprintf("%s · %s window unmeasured", sub.Name, c.window)
	case w.Remaining <= nearLimit:
		return fmt.Sprintf("%s · %s of its %s window left, near the limit", sub.Name, percent(int(w.Remaining*100)), c.window)
	default:
		return fmt.Sprintf("%s · %s of its %s window left", sub.Name, percent(int(w.Remaining*100)), c.window)
	}
}
