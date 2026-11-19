package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// talker is a control that says where it was asked about, so a test can prove
// the question reached the right control in the right coordinates.
type talker struct{ block }

func (t *talker) Describe(_ time.Time, at app.Slot) string {
	return "slot " + itoa(at.Row) + "," + itoa(at.Col)
}

func itoa(n int) string { return string(rune('0' + n)) }

// The board answers for its own chrome on every screen, because chrome belongs
// to the board and not to whatever is showing.
func TestTheBoardNamesTheScreenAnArrowLeadsTo(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)

	prev, _ := domain.Top(app.PrevTab)
	next, _ := domain.Top(app.NextTab)
	if got := b.Describe(now, prev); !strings.Contains(got, other.name) {
		t.Errorf("the previous arrow says %q, want it to name %q", got, other.name)
	}
	if got := b.Describe(now, next); !strings.Contains(got, other.name) {
		t.Errorf("the next arrow says %q, want it to name %q", got, other.name)
	}
	if got := b.Describe(now, domain.Logo); !strings.Contains(got, one.name) {
		t.Errorf("the logo says %q, want it to name the showing screen %q", got, one.name)
	}
}

// The unbound buttons are the ones a hand reaches for first, and a board that
// said nothing about them was read as a board that had stopped listening. The
// words are the other half of the flash they already answer with.
func TestAnUnboundButtonSaysThatItIsUnbound(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)

	pad, _ := domain.Top(8)
	if got := b.Describe(now, pad); got == "" {
		t.Error("an unbound button described itself as nothing at all")
	}
}

// A screen that cannot say what a pad is is not a broken screen: the words are
// an optional interface, and the surface prints nothing rather than inventing.
func TestAScreenWithNothingToSaySaysNothing(t *testing.T) {
	t.Parallel()
	one, other := two()
	pad, _ := domain.Grid(0, 0)

	if got := board(t, one, other).Describe(now, pad); got != "" {
		t.Errorf("a screen that describes nothing produced %q", got)
	}
}

// The description must come from the control that owns the pad, in that
// control's own coordinates -- the same walk Press does, so that what a pad
// says it is and what it does can never disagree.
func TestAPadIsDescribedByTheControlThatOwnsIt(t *testing.T) {
	t.Parallel()
	top, bottom := &talker{}, &talker{}
	p := app.NewPanel("split", domain.Cyan,
		app.At(domain.Rows(0, 3), top),
		app.At(domain.Rows(3, 5), bottom),
	)

	pad, _ := domain.Grid(4, 2)
	if got, want := p.Describe(now, pad), "slot 1,2"; got != want {
		t.Errorf("pad at row 4 says %q, want %q -- its own coordinates in the lower control", got, want)
	}
}

// A pad no control covers is empty, and saying so is the point: an unexplained
// dark pad and a pad with nothing on it look identical on the hardware.
func TestAPadNoControlCoversSaysSo(t *testing.T) {
	t.Parallel()
	p := app.NewPanel("corner", domain.Cyan, app.At(domain.Rows(0, 1), &talker{}))

	pad, _ := domain.Grid(5, 5)
	if got := p.Describe(now, pad); got == "" {
		t.Error("an uncovered pad described itself as nothing at all")
	}
}
