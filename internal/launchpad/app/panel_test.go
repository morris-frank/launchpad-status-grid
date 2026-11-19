package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// block is a control that floods whatever it is given and records where it was
// pressed, in its own coordinates.
type block struct {
	hue      domain.Colour
	pressed  []app.Slot
	refreshs int
	err      error
}

func (b *block) Render(_ time.Time, size domain.Size) domain.Frame {
	return domain.Blank().Fill(size.Region(), b.hue.Cell(domain.LevelFull))
}

func (b *block) Press(_ context.Context, at app.Slot) (app.Outcome, error) {
	b.pressed = append(b.pressed, at)
	return app.Outcome{}, nil
}

func (b *block) Refresh(context.Context) error {
	b.refreshs++
	return b.err
}

// The point of placing by rectangle: two controls drawn from the same code sit
// in different halves of the grid and neither of them knows it.
func TestAPanelDrawsEachControlInsideItsOwnRectangle(t *testing.T) {
	t.Parallel()
	top, bottom := &block{hue: domain.Green}, &block{hue: domain.Red}
	p := app.NewPanel("split", domain.Cyan,
		app.At(domain.Rows(0, 3), top),
		app.At(domain.Rows(3, 5), bottom),
	)

	f := p.Render(time.Now())
	for row := range domain.GridSize {
		want := domain.Green.Palette
		if row >= 3 {
			want = domain.Red.Palette
		}
		pad, _ := domain.Grid(row, 4)
		if got := f.At(pad).Palette; got != want {
			t.Errorf("row %d is palette %d, want %d", row, got, want)
		}
	}
}

// A control is told where it was pressed in ITS coordinates, not the board's.
// Getting this wrong acts on the right control and the wrong item, which is the
// failure that never looks like a bug in the router.
func TestAPressReachesTheControlInItsOwnCoordinates(t *testing.T) {
	t.Parallel()
	left, right := &block{hue: domain.Green}, &block{hue: domain.Red}
	p := app.NewPanel("split", domain.Cyan,
		app.At(domain.Region{Col: 0, Rows: domain.GridSize, Cols: 4}, left),
		app.At(domain.Region{Col: 4, Rows: domain.GridSize, Cols: 4}, right),
	)

	// Board column 5 is the right-hand control's own column 1.
	pad, _ := domain.Grid(2, 5)
	if _, err := p.Press(context.Background(), pad); err != nil {
		t.Fatal(err)
	}
	if len(left.pressed) != 0 {
		t.Fatalf("the press reached the wrong control: %v", left.pressed)
	}
	if len(right.pressed) != 1 {
		t.Fatalf("the press reached %d controls, want 1", len(right.pressed))
	}
	got := right.pressed[0]
	if got.Row != 2 || got.Col != 1 {
		t.Errorf("the control was told (%d,%d), want (2,1)", got.Row, got.Col)
	}
	if got.Size != (domain.Size{Rows: 8, Cols: 4}) {
		t.Errorf("the control was told it is %v, want 8x4", got.Size)
	}
	if got.Index() != 2*4+1 {
		t.Errorf("Index is %d, want it computed against the control's own width", got.Index())
	}
}

// A pad no control covers is the same problem as an unbound chrome button:
// silence reads as a board that has stopped listening.
func TestAPressOnBarePanelAsksForAttention(t *testing.T) {
	t.Parallel()
	p := app.NewPanel("sparse", domain.Cyan, app.At(domain.Rows(0, 1), &block{hue: domain.Green}))

	pad, _ := domain.Grid(5, 5)
	out, err := p.Press(context.Background(), pad)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Attention {
		t.Error("a press on a pad no control covers said nothing")
	}
}

// Controls are independent sources. A panel that stopped at the first failure
// would blank the controls that were working.
func TestRefreshReachesEveryControlEvenAfterOneFails(t *testing.T) {
	t.Parallel()
	bad := &block{hue: domain.Red, err: errors.New("source is down")}
	good := &block{hue: domain.Green}
	p := app.NewPanel("two", domain.Cyan,
		app.At(domain.Rows(0, 4), bad),
		app.At(domain.Rows(4, 4), good),
	)

	if err := p.Refresh(context.Background()); err == nil {
		t.Error("Refresh hid a failing source")
	}
	if good.refreshs != 1 {
		t.Errorf("the working control refreshed %d times, want 1", good.refreshs)
	}
}

// A control placed down the side column draws and is pressed there as the
// one-column rectangle it believes it is, and owns no grid pad beside it.
func TestAControlPlacedOnTheSideColumnDrawsAndIsPressedThere(t *testing.T) {
	t.Parallel()
	b := &block{hue: domain.Red}
	p := app.NewPanel("side", domain.Cyan, app.AtSide(6, 2, b))

	f := p.Render(time.Now())
	for n := 1; n <= domain.GridSize; n++ {
		pad, _ := domain.Side(n)
		if lit := !f.At(pad).Dark(); lit != (n == 7 || n == 8) {
			t.Errorf("side button %d lit = %v, want only 7 and 8", n, lit)
		}
	}
	eighth, _ := domain.Side(8)
	if !p.OwnsChrome(eighth) {
		t.Fatal("the panel does not own the side button it placed")
	}
	if _, err := p.Press(context.Background(), eighth); err != nil {
		t.Fatal(err)
	}
	beside, _ := domain.Grid(7, 0)
	if _, err := p.Press(context.Background(), beside); err != nil {
		t.Fatal(err)
	}
	if len(b.pressed) != 1 || b.pressed[0] != (app.Slot{Row: 1, Size: domain.Size{Rows: 2, Cols: 1}}) {
		t.Errorf("pressed at %+v, want the second slot of a one-column control, once", b.pressed)
	}
}
