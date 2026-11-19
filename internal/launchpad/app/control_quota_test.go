package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

type quotaSource struct {
	status app.QuotaStatus
	reads  *int
}

func (q quotaSource) Quota(context.Context) (app.QuotaStatus, error) {
	if q.reads != nil {
		*q.reads++
	}
	return q.status, nil
}

func quotaFeed(t *testing.T, status app.QuotaStatus) *app.QuotaFeed {
	t.Helper()
	feed := app.NewQuotaFeed(quotaSource{status: status})
	if err := feed.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return feed
}

var (
	subsSize   = domain.Size{Rows: 1, Cols: 4}
	windowSize = domain.Size{Rows: 2, Cols: 4}
)

func window(remaining float64) app.WindowReading {
	return app.WindowReading{Known: true, Remaining: remaining}
}

// Four controls read one feed, and a vendor endpoint is asked once for all of
// them rather than once each.
func TestEveryQuotaControlSharesOneRead(t *testing.T) {
	t.Parallel()
	reads := 0
	feed := app.NewQuotaFeed(quotaSource{reads: &reads})
	for _, c := range []app.Refresher{
		app.NewSubscriptionsControl(feed), app.NewBurnControl(feed),
		app.NewQuotaWindowControl(feed, app.FiveHour), app.NewQuotaWindowControl(feed, app.SevenDay),
	} {
		if err := c.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 1 {
		t.Errorf("four controls read the quota %d times, want once", reads)
	}
}

// Usable is green and resting is red; unmetered is neither, because the
// rotation still launches on it and a green pad would claim a reading nobody
// took.
func TestASubscriptionPadIsGreenUsableRedRestingAndGreyUnmetered(t *testing.T) {
	t.Parallel()
	c := app.NewSubscriptionsControl(quotaFeed(t, app.QuotaStatus{Subscriptions: []app.SubscriptionReading{
		{Name: "max", Measured: true},
		{Name: "pro", Measured: true, Spent: true},
		{Name: "team"},
	}}))
	f := c.Render(now, subsSize)
	for col, want := range []domain.Colour{domain.Green, domain.Red, domain.Grey} {
		if got := f.At(domain.Pad(col)).Palette; got != want.Palette {
			t.Errorf("subscription %d is palette %d, want %d", col, got, want.Palette)
		}
	}
	if !f.At(domain.Pad(3)).Dark() {
		t.Error("a pad with no subscription is lit")
	}
	if got := c.Describe(now, app.Slot{Col: 1, Size: subsSize}); got != "pro · resting: a window is spent" {
		t.Errorf("the resting pad says %q", got)
	}
}

// Each subscription's column is the colour of what is left, and blinks only
// near the limit; an unmeasured window is grey and still.
func TestAWindowIsItsColourAndBlinksNearTheLimit(t *testing.T) {
	t.Parallel()
	c := app.NewQuotaWindowControl(quotaFeed(t, app.QuotaStatus{Subscriptions: []app.SubscriptionReading{
		{Name: "max", Measured: true, FiveHour: window(0.8), SevenDay: window(0.05)},
		{Name: "pro", Measured: true, FiveHour: window(0.05)},
		{Name: "team"},
	}}), app.FiveHour)
	f := c.Render(now, windowSize)
	for col, want := range []struct {
		hue   domain.Colour
		blink bool
	}{{domain.Green, false}, {domain.Red, true}, {domain.Grey, false}} {
		for row := range windowSize.Rows {
			pad, _ := domain.Grid(row, col)
			cell := f.At(pad)
			if cell.Palette != want.hue.Palette || (cell.Behaviour.Kind == domain.Flash) != want.blink {
				t.Errorf("row %d col %d is %+v, want palette %d blinking %v", row, col, cell, want.hue.Palette, want.blink)
			}
		}
	}
	if got := c.Describe(now, app.Slot{Col: 1, Size: windowSize}); !strings.Contains(got, "near the limit") {
		t.Errorf("the near-limit column says %q", got)
	}
	if got := c.Describe(now, app.Slot{Col: 2, Size: windowSize}); !strings.Contains(got, "unmeasured") {
		t.Errorf("the unmeasured column says %q", got)
	}
}

// An unread burn is unknown across the whole bar, and never an empty one: zero
// units would read as an idle desk.
func TestAnUnreadBurnIsUnknownRatherThanZero(t *testing.T) {
	t.Parallel()
	c := app.NewBurnControl(quotaFeed(t, app.QuotaStatus{}))
	f := c.Render(now, subsSize)
	for col := range subsSize.Cols {
		if cell := f.At(domain.Pad(col)); cell.Dark() || cell.Palette != domain.Grey.Palette {
			t.Errorf("pad %d of an unread burn is %+v, want lit grey", col, cell)
		}
	}
	if got := c.Describe(now, app.Slot{Size: subsSize}); !strings.Contains(got, "unknown") {
		t.Errorf("the unread burn says %q", got)
	}
}

// The median sits at half length, so a machine spending at its usual rate
// lights half the bar.
func TestTheBurnBarIsTheRatioAgainstTheMedian(t *testing.T) {
	t.Parallel()
	c := app.NewBurnControl(quotaFeed(t, app.QuotaStatus{Burn: app.BurnReading{
		Known: true, LastFiveHours: 100, MedianFiveHours: 100, Ratio: 1,
	}}))
	f := c.Render(now, subsSize)
	if f.At(domain.Pad(1)).Level != domain.LevelFull || f.At(domain.Pad(2)).Level == domain.LevelFull {
		t.Error("a burn at the median did not light exactly the left half of its bar")
	}
	if got, want := c.Describe(now, app.Slot{Col: 3, Size: subsSize}), "burn · 1.00× this machine's median five hours"; got != want {
		t.Errorf("the burn says %q, want %q", got, want)
	}
}

// A burn with transcripts and no comparable stretch has counts and no ratio.
func TestABurnWithNoMedianIsNotDrawnAsAZeroRatio(t *testing.T) {
	t.Parallel()
	c := app.NewBurnControl(quotaFeed(t, app.QuotaStatus{Burn: app.BurnReading{Known: true, LastFiveHours: 40}}))
	if got := c.Render(now, subsSize).At(domain.Pad(0)); got.Palette != domain.Grey.Palette || got.Level != domain.LevelDim {
		t.Errorf("a burn with no median is %+v, want dim grey", got)
	}
}
