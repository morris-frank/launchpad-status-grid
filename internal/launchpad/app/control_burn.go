package app

import (
	"context"
	"fmt"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// BurnControl is the five-hour burn against this machine's own median, as a
// bar growing left to right: the median sits at half length, so a bar past
// halfway is spending faster than usual.
//
// An unread burn is faint grey across the whole bar — unknown — and never an
// empty bar, because zero units would read as an idle desk, which an
// unconfigured profile directory is not. A burn with no comparable stretch yet
// is dim grey: counted, with nothing to hold it against.
type BurnControl struct{ feed *QuotaFeed }

// burnCeiling is the ratio a full bar stands for.
const burnCeiling = 2.0

// NewBurnControl draws feed's burn.
func NewBurnControl(feed *QuotaFeed) *BurnControl { return &BurnControl{feed: feed} }

// Refresh implements Refresher.
func (c *BurnControl) Refresh(ctx context.Context) error { return c.feed.Refresh(ctx) }

// Render implements Control.
func (c *BurnControl) Render(_ time.Time, size domain.Size) domain.Frame {
	status, loaded := c.feed.Value()
	burn := status.Burn
	switch {
	case !loaded || !burn.Known:
		return unread(size)
	case burn.MedianFiveHours <= 0:
		return domain.Blank().Fill(size.Region(), domain.Grey.Cell(domain.LevelDim))
	}
	share := burn.Ratio / burnCeiling
	return domain.Blank().BarAlong(size.Region(), share, domain.Ramp(1-share), domain.Right)
}

// Press implements Control.
func (c *BurnControl) Press(_ context.Context, at Slot) (Outcome, error) {
	return Outcome{Note: c.Describe(time.Time{}, at)}, nil
}

// Describe implements Describer. Every pad of the bar says the same thing: it
// is one reading drawn wide.
func (c *BurnControl) Describe(time.Time, Slot) string {
	status, loaded := c.feed.Value()
	burn := status.Burn
	switch {
	case !loaded || !burn.Known:
		return "burn · unknown: no transcripts read, so how fast this machine is spending is not known"
	case burn.MedianFiveHours <= 0:
		return fmt.Sprintf("burn · %d units in five hours, no comparable stretch yet", burn.LastFiveHours)
	default:
		return fmt.Sprintf("burn · %.2f× this machine's median five hours", burn.Ratio)
	}
}
