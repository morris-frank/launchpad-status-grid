package app

import (
	"context"
	"sync"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// QuotaFeed is the worker's subscriptions and burn readings (rosalina account show), read once and
// shared by every control that draws a part of them: the subscription pads,
// the burn, and the two window blocks. One feed rather than one read per
// control, because a reading is a request per stored subscription to its
// vendor's usage endpoint and a week of transcripts.
type QuotaFeed struct {
	data *Data[QuotaStatus]

	mu   sync.Mutex
	last time.Time
}

// quotaRefreshEvery bounds how often the feed re-reads. The board refreshes
// every control every RefreshRate, and each control over this feed passes the
// refresh on, so the gate is also what makes four controls one read.
const quotaRefreshEvery = time.Minute

// nearLimit is the share left at which a window blinks: the point Ramp turns
// red, so the colour and the motion agree about when to worry.
const nearLimit = 0.10

// WindowReading is one of a subscription's windows.
type WindowReading struct {
	// Known is whether the reading carried this window. Remaining means
	// nothing when it is false.
	Known bool
	// Remaining is the share of the window still unspent, 0..1.
	Remaining float64
}

// SubscriptionReading is one stored subscription as a pad sees it. It has no
// field a credential or a vendor's error text fits in, which is the whole of
// why neither can reach a frame or a description.
type SubscriptionReading struct {
	// Name is the slot the subscription is stored under.
	Name string
	// Owner is a stored token's owner, empty on a profile directory's entry:
	// a desk finds its caller's own seat by it, and no pad reads it.
	Owner string
	// Agent is "codex" for a codex home, "" for a Claude account; State is
	// rotationLines' word where the entry is no room — "unmeasured", or why
	// the rotation skips it — and empty otherwise. A word, never the reason's
	// text. A desk's menu draws both, and no pad reads either.
	Agent, State string
	// Measured is whether its usage endpoint answered. Everything below
	// means nothing when it is false.
	Measured bool
	// Spent is whether any window is out, which is what rests the slot in the
	// rotation until the vendor's stated refill.
	Spent bool
	// FiveHour is the session window; SevenDay the fullest weekly one.
	FiveHour, SevenDay WindowReading
}

// BurnReading is how fast this machine has been spending, in weighted units.
type BurnReading struct {
	// Known is whether any transcript was read. A false one is absent, never
	// zero.
	Known bool
	// LastFiveHours and MedianFiveHours are units; Ratio is the first over the
	// second, and zero where there is no median to compare against.
	LastFiveHours, MedianFiveHours int64
	Ratio                          float64
}

// QuotaStatus is what the feed reads.
type QuotaStatus struct {
	Subscriptions []SubscriptionReading
	Burn          BurnReading
}

// QuotaReadings reports the stored subscriptions and the burn.
type QuotaReadings interface {
	Quota(ctx context.Context) (QuotaStatus, error)
}

// NewQuotaFeed reads from src.
func NewQuotaFeed(src QuotaReadings) *QuotaFeed {
	return &QuotaFeed{data: NewData(src.Quota)}
}

// Refresh reads at most once per quotaRefreshEvery, however many controls ask.
func (q *QuotaFeed) Refresh(ctx context.Context) error {
	q.mu.Lock()
	if !q.last.IsZero() && time.Since(q.last) < quotaRefreshEvery {
		q.mu.Unlock()
		return nil
	}
	q.last = time.Now()
	q.mu.Unlock()
	return q.data.Refresh(ctx)
}

// Value is the last good reading, and whether there has been one.
func (q *QuotaFeed) Value() (QuotaStatus, bool) {
	status, loaded, _ := q.data.Value()
	return status, loaded
}

// unread is the picture of a quota control with nothing read yet: faint grey,
// which says the control is alive and knows nothing, and is no quantity.
func unread(size domain.Size) domain.Frame {
	return domain.Blank().Fill(size.Region(), domain.Grey.Cell(domain.LevelFaint))
}
