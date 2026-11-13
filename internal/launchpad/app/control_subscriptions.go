package app

import (
	"context"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// SubscriptionsControl is one pad per stored subscription, row-major in the
// order the store lists them: green while the rotation can launch on it, red
// while a reading says a window is out and the slot is resting.
//
// A subscription nothing measured is grey rather than green: the rotation will
// still launch on it, but a green pad would claim a reading that was never
// taken. A slot resting on a phrase match alone lives in the chat daemon's
// memory and not in any reading, so it draws as whatever its reading says.
type SubscriptionsControl struct{ feed *QuotaFeed }

// NewSubscriptionsControl draws feed's subscriptions.
func NewSubscriptionsControl(feed *QuotaFeed) *SubscriptionsControl {
	return &SubscriptionsControl{feed: feed}
}

// Refresh implements Refresher.
func (c *SubscriptionsControl) Refresh(ctx context.Context) error { return c.feed.Refresh(ctx) }

// Render implements Control.
func (c *SubscriptionsControl) Render(_ time.Time, size domain.Size) domain.Frame {
	status, loaded := c.feed.Value()
	if !loaded {
		return unread(size)
	}
	f := domain.Blank()
	for i, sub := range status.Subscriptions {
		if i >= size.Count() {
			break
		}
		f = f.SetGrid(i/size.Cols, i%size.Cols, subscriptionCell(sub))
	}
	return f
}

func subscriptionCell(sub SubscriptionReading) domain.Cell {
	switch {
	case !sub.Measured:
		return domain.Grey.Cell(domain.LevelDim)
	case sub.Spent:
		return domain.Red.Cell(domain.LevelFull)
	default:
		return domain.Green.Cell(domain.LevelBright)
	}
}

// Press implements Control.
func (c *SubscriptionsControl) Press(_ context.Context, at Slot) (Outcome, error) {
	return Outcome{Note: c.Describe(time.Time{}, at)}, nil
}

// Describe implements Describer.
func (c *SubscriptionsControl) Describe(_ time.Time, at Slot) string {
	status, loaded := c.feed.Value()
	if !loaded {
		return "subscriptions · not read yet"
	}
	i := at.Index()
	if i >= len(status.Subscriptions) {
		return "no subscription on this pad"
	}
	sub := status.Subscriptions[i]
	switch {
	case !sub.Measured:
		return sub.Name + " · configured, unmetered: nothing measured its quota"
	case sub.Spent:
		return sub.Name + " · resting: a window is spent"
	default:
		return sub.Name + " · usable"
	}
}
