package app_test

import (
	"errors"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// surface is a Device that records what it was drawn and can be made to fail.
type surface struct {
	presses chan app.Press
	frames  []domain.Frame
	fail    error
	closed  bool
	shut    error
}

func (s *surface) Render(f domain.Frame) error { s.frames = append(s.frames, f); return s.fail }
func (s *surface) Presses() <-chan app.Press   { return s.presses }
func (s *surface) Close() error                { s.closed = true; return s.shut }

// The whole reason a mirror exists: one board, and the terminal shows what the
// hardware shows rather than instead of it.
func TestEveryFrameReachesEverySurface(t *testing.T) {
	t.Parallel()
	one := &surface{presses: make(chan app.Press)}
	other := &surface{presses: make(chan app.Press)}
	m := app.NewMirror(one, other)

	pad, _ := domain.Grid(2, 3)
	f := domain.Blank().Set(pad, domain.Green.Cell(domain.LevelFull))
	if err := m.Render(f); err != nil {
		t.Fatalf("Render: %v", err)
	}

	for name, s := range map[string]*surface{"one": one, "other": other} {
		if len(s.frames) != 1 || s.frames[0].At(pad) != f.At(pad) {
			t.Errorf("surface %s was drawn %v, want the frame", name, s.frames)
		}
	}
}

// A press from either surface is a press. Which one it came from is not
// carried, because a board that could tell a thumb from a keystroke would
// eventually behave differently for the two.
func TestAPressFromAnySurfaceReachesTheBoard(t *testing.T) {
	t.Parallel()
	one := &surface{presses: make(chan app.Press, 1)}
	other := &surface{presses: make(chan app.Press, 1)}
	m := app.NewMirror(one, other)

	first, _ := domain.Grid(0, 0)
	second, _ := domain.Grid(7, 7)
	one.presses <- app.Press{Pad: first, Down: true}
	other.presses <- app.Press{Pad: second, Down: true}

	seen := map[domain.Pad]bool{}
	for range 2 {
		select {
		case p := <-m.Presses():
			seen[p.Pad] = true
		case <-time.After(time.Second):
			t.Fatal("a press never arrived through the mirror")
		}
	}
	if !seen[first] || !seen[second] {
		t.Errorf("the mirror carried %v, want both presses", seen)
	}
}

// Pull the cable and the terminal you were reading must not exit with it. The
// Device contract already says an unplugged Launchpad is not the board being
// over, and a mirror that propagated the first error would break exactly that.
func TestOneSurfaceFailingDoesNotFailTheFrame(t *testing.T) {
	t.Parallel()
	gone := &surface{presses: make(chan app.Press), fail: errors.New("device gone")}
	here := &surface{presses: make(chan app.Press)}
	m := app.NewMirror(gone, here)

	if err := m.Render(domain.Blank()); err != nil {
		t.Fatalf("Render: %v, want the surviving surface to carry the board", err)
	}
	if len(here.frames) != 1 {
		t.Error("the surviving surface was not drawn to")
	}
}

// With nothing left that can take a frame there is no board, and the loop
// should stop rather than spin drawing into the dark.
func TestAFrameWithNoSurfaceLeftIsAnError(t *testing.T) {
	t.Parallel()
	one := &surface{presses: make(chan app.Press), fail: errors.New("gone")}
	other := &surface{presses: make(chan app.Press), fail: errors.New("also gone")}

	if err := app.NewMirror(one, other).Render(domain.Blank()); err == nil {
		t.Fatal("Render reported success with no surface left")
	}
}

// One surface refusing to shut down must not leave the others lit.
func TestClosingReachesEverySurfaceEvenAfterOneFails(t *testing.T) {
	t.Parallel()
	one := &surface{presses: make(chan app.Press), shut: errors.New("stuck")}
	other := &surface{presses: make(chan app.Press)}

	if err := app.NewMirror(one, other).Close(); err == nil {
		t.Error("Close swallowed a surface that failed to shut down")
	}
	if !one.closed || !other.closed {
		t.Errorf("closed one=%v other=%v, want both", one.closed, other.closed)
	}
}
