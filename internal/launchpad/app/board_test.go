package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// fake is a screen that records what it was asked and paints whatever it is
// told to, so a test can talk about the board rather than about a screen.
type fake struct {
	name    string
	colour  domain.Colour
	fill    domain.Cell
	pressed []domain.Pad
	out     app.Outcome
	err     error
	// notify, when set, receives each press before Press returns, so a test
	// driving the run loop can wait for the press to land instead of polling
	// a field the loop's own goroutine is writing.
	notify chan domain.Pad
}

func (f *fake) Name() string          { return f.name }
func (f *fake) Colour() domain.Colour { return f.colour }

func (f *fake) Render(time.Time) domain.Frame {
	return domain.Blank().Fill(domain.FullGrid(), f.fill)
}

func (f *fake) Press(_ context.Context, p domain.Pad) (app.Outcome, error) {
	f.pressed = append(f.pressed, p)
	if f.notify != nil {
		f.notify <- p
	}
	return f.out, f.err
}

func board(t *testing.T, screens ...app.Screen) *app.Board {
	t.Helper()
	b, err := app.New(screens...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func two() (*fake, *fake) {
	return &fake{name: "one", colour: domain.Green, fill: domain.Green.Cell(domain.LevelFull)},
		&fake{name: "two", colour: domain.Cyan, fill: domain.Cyan.Cell(domain.LevelFull)}
}

var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// Navigation must survive a screen that uses everything it is given. A screen
// is told it owns the whole grid, so the only thing that can protect the arrows
// is the board drawing chrome over the top — and if that ever inverts, the
// symptom is a board you cannot navigate off whichever screen paints brightest.
func TestChromeIsNeverOccludedByAScreen(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)
	f := b.Render(now)

	// Two screens, so both arrows lead to the other one and wear its hue.
	for _, n := range []int{app.PrevTab, app.NextTab} {
		pad, _ := domain.Top(n)
		if got := f.At(pad).Palette; got != other.colour.Palette {
			t.Errorf("the arrow at %d shows palette %d, want the screen it leads to, %d", n, got, other.colour.Palette)
		}
	}
	if got := f.At(domain.Logo).Palette; got != one.colour.Palette {
		t.Errorf("the logo shows palette %d, want the active screen's %d", got, one.colour.Palette)
	}
}

// An arrow says where it goes, which is the whole of what replaced a tab per
// screen: the top row no longer counts the screens, so it has to aim.
func TestEachArrowWearsTheHueOfTheScreenItLeadsTo(t *testing.T) {
	t.Parallel()
	one, other := two()
	third := &fake{name: "third", colour: domain.Red}
	b := board(t, one, other, third)
	f := b.Render(now)

	prev, _ := domain.Top(app.PrevTab)
	next, _ := domain.Top(app.NextTab)
	if got := f.At(prev).Palette; got != third.colour.Palette {
		t.Errorf("the back arrow shows palette %d, want the last screen's %d", got, third.colour.Palette)
	}
	if got := f.At(next).Palette; got != other.colour.Palette {
		t.Errorf("the forward arrow shows palette %d, want the next screen's %d", got, other.colour.Palette)
	}
}

// The cycle wraps. An arrow that stopped at the end of the row would look
// exactly like a board that had stopped responding, which is the failure this
// whole row was rebuilt to prevent.
func TestTheArrowsCycleAndWrapInBothDirections(t *testing.T) {
	t.Parallel()
	one, other := two()
	third := &fake{name: "third", colour: domain.Red}
	b := board(t, one, other, third)

	press := func(n int) {
		t.Helper()
		pad, _ := domain.Top(n)
		if err := b.Handle(context.Background(), app.Press{Pad: pad, Down: true, At: now}); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}

	for i, want := range []int{1, 2, 0} {
		press(app.NextTab)
		if b.ActiveIndex() != want {
			t.Fatalf("forward press %d landed on screen %d, want %d", i+1, b.ActiveIndex(), want)
		}
	}
	for i, want := range []int{2, 1, 0} {
		press(app.PrevTab)
		if b.ActiveIndex() != want {
			t.Fatalf("back press %d landed on screen %d, want %d", i+1, b.ActiveIndex(), want)
		}
	}
}

// A press on a button nothing is bound to has to be visible. Returning silently
// is what made a working board read as dead: the buttons a person reaches for
// first are the ones the device labels, and none of those are arrows.
func TestAnUnboundChromePressIsAcknowledged(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)

	// The Session button: in the top row, bound to nothing.
	pad, _ := domain.Top(app.NextTab + 1)
	if err := b.Handle(context.Background(), app.Press{Pad: pad, Down: true, At: now}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if b.ActiveIndex() != 0 {
		t.Errorf("an unbound press changed the screen to %d", b.ActiveIndex())
	}
	if len(one.pressed)+len(other.pressed) != 0 {
		t.Error("an unbound chrome press reached a screen")
	}

	cell := b.Render(now).At(pad)
	if cell.Behaviour.Kind != domain.Flash {
		t.Error("an unbound press left the button static, and a dark static button is what a dead board looks like")
	}
}

// A tab press is navigation and nothing else. A screen that also saw it would
// act every time somebody moved away from it.
func TestATabPressSwitchesScreensAndNeverReachesOne(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)

	pad, _ := domain.Top(app.NextTab)
	if err := b.Handle(context.Background(), app.Press{Pad: pad, Down: true, At: now}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if b.ActiveIndex() != 1 {
		t.Errorf("active screen is %d, want 1", b.ActiveIndex())
	}
	if len(one.pressed)+len(other.pressed) != 0 {
		t.Errorf("a tab press reached a screen: %v %v", one.pressed, other.pressed)
	}
}

// Acting on release as well as on press would fire twice for every press; the
// board deliberately only ever sees a pad go down.
func TestAReleaseDoesNothing(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)

	pad, _ := domain.Grid(3, 3)
	if err := b.Handle(context.Background(), app.Press{Pad: pad, Down: false, At: now}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(one.pressed) != 0 {
		t.Errorf("a release reached the screen: %v", one.pressed)
	}
	if b.Animating() {
		t.Error("a release armed an effect")
	}
}

// A press the person has to resolve marks its own pad, so the board says which
// press failed rather than only that something did. It clears on the next
// press, because an attention marker nobody can dismiss becomes decoration.
func TestAFailedPressFlagsItsOwnPadUntilTheNextPress(t *testing.T) {
	t.Parallel()
	one, other := two()
	one.err = errors.New("no terminal")
	b := board(t, one, other)

	pad, _ := domain.Grid(2, 5)
	if err := b.Handle(context.Background(), app.Press{Pad: pad, Down: true, At: now}); err == nil {
		t.Fatal("Handle swallowed the screen's error")
	}

	f := b.Render(now.Add(time.Second)) // past the ripple, so only attention is left
	if got := f.At(pad).Behaviour.Kind; got != domain.Flash {
		t.Errorf("the failed pad is %v, want it flashing", got)
	}

	elsewhere, _ := domain.Grid(6, 6)
	one.err = nil
	if err := b.Handle(context.Background(), app.Press{Pad: elsewhere, Down: true, At: now}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := b.Render(now.Add(2 * time.Second)).At(pad).Behaviour.Kind; got == domain.Flash {
		t.Error("the attention marker survived the next press")
	}
}

// The two-rate tick is the whole reason a board left showing is free, and it
// hangs off this one boolean: an effect that never reported itself finished
// would pin the driver at 30 fps forever.
func TestAnEffectStopsAnimatingOnceItIsSpent(t *testing.T) {
	t.Parallel()
	one, other := two()
	b := board(t, one, other)

	if b.Animating() {
		t.Error("a fresh board is animating")
	}

	pad, _ := domain.Grid(1, 1)
	if err := b.Handle(context.Background(), app.Press{Pad: pad, Down: true, At: now}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !b.Animating() {
		t.Error("a grid press armed no effect")
	}

	b.Render(now.Add(time.Second))
	if b.Animating() {
		t.Error("the effect is still in flight a second later")
	}
}

func TestABoardRefusesAShapeItCannotShow(t *testing.T) {
	t.Parallel()
	if _, err := app.New(); !errors.Is(err, app.ErrNoScreens) {
		t.Errorf("New() with no screens: %v, want ErrNoScreens", err)
	}

	one, other := two()
	other.name = one.name
	if _, err := app.New(one, other); err == nil {
		t.Error("New accepted two screens with the same name")
	}

	many := make([]app.Screen, 0, app.MaxScreens+1)
	for i := 0; i <= app.MaxScreens; i++ {
		many = append(many, &fake{name: string(rune('a' + i)), colour: domain.Green})
	}
	if _, err := app.New(many...); err == nil {
		t.Errorf("New accepted %d screens, over the cap of %d", len(many), app.MaxScreens)
	}
}
