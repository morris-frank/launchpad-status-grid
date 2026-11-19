package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// named is a control that says the same sentence about every pad, so a test can
// look for it in the legend without caring which pad it landed on.
type named struct{}

func (named) Render(time.Time, domain.Size) domain.Frame { return domain.Blank() }

func (named) Press(context.Context, app.Slot) (app.Outcome, error) { return app.Outcome{}, nil }

func (named) Describe(time.Time, app.Slot) string { return "the session you just pressed" }

// The twin's whole purpose. A press on the hardware moves the cursor here and
// the line under the picture names what was pressed -- which is the only way
// the device ever answers "what is this one" short of pressing it and seeing.
func TestTheCursorFollowsAPressMadeOnTheOtherSurface(t *testing.T) {
	b, err := app.New(app.NewPanel("sessions", domain.Amber, app.At(domain.FullGrid(), named{})))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := New(NewDevice(), b, WithSize(90, 30), WithClock(func() time.Time { return at }))

	pad, _ := domain.Grid(3, 5)
	if err := b.Handle(context.Background(), app.Press{Pad: pad, Down: true, At: at}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	b.Render(at) // the board publishes its status on render, as the run loop makes it

	m.Update(animateMsg(at))

	// Row 3 of the grid is row 4 of the drawn surface: the round buttons are
	// drawn above it.
	if m.row != 4 || m.col != 5 {
		t.Errorf("the cursor is at %d,%d, want 4,5 -- the pad that was pressed", m.row, m.col)
	}
	if out := m.Render(); !strings.Contains(out, "the session you just pressed") {
		t.Error("the preview does not say what the pressed pad is")
	}
}

// A cursor that jumped back on every tick would be unusable next to a board
// that is being pressed, so it follows a new press and then leaves you alone.
func TestTheCursorIsNotDraggedBackToAPressAlreadyFollowed(t *testing.T) {
	b, err := app.New(app.NewPanel("sessions", domain.Amber, app.At(domain.FullGrid(), named{})))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := New(NewDevice(), b, WithSize(90, 30), WithClock(func() time.Time { return at }))

	pad, _ := domain.Grid(0, 0)
	if err := b.Handle(context.Background(), app.Press{Pad: pad, Down: true, At: at}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	b.Render(at)
	m.Update(animateMsg(at))

	m.row, m.col = 6, 6
	m.Update(animateMsg(at))

	if m.row != 6 || m.col != 6 {
		t.Errorf("the cursor was dragged to %d,%d, want it left where it was put", m.row, m.col)
	}
}

// Esc never ends the preview, and each of the three control keys does.
func TestTheControlKeysQuitThePreviewAndEscDoesNot(t *testing.T) {
	b, err := app.New(app.NewPanel("sessions", domain.Amber, app.At(domain.FullGrid(), named{})))
	if err != nil {
		t.Fatal(err)
	}
	quits := func(k tea.KeyPressMsg) bool {
		_, cmd := New(NewDevice(), b, WithSize(90, 30)).Update(k)
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	if quits(tea.KeyPressMsg{Code: tea.KeyEscape}) {
		t.Error("esc quit the preview")
	}
	for _, r := range "cqd" {
		if k := (tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}); !quits(k) {
			t.Errorf("%s did not quit the preview", k)
		}
	}
}
