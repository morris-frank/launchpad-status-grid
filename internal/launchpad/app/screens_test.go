package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

type sessionSource struct {
	sessions []app.Session
	err      error
}

// full is the rectangle every control in these tests is given: the whole grid,
// which is the size the composition root places them at today.
var full = domain.FullGrid().Size()

func (s sessionSource) Sessions(context.Context) ([]app.Session, error) { return s.sessions, s.err }

type repoSource struct {
	repos []app.Repo
	err   error
}

func (r repoSource) Repos(context.Context) ([]app.Repo, error) { return r.repos, r.err }

type focusRecorder struct {
	got []string
	err error
}

func (f *focusRecorder) Focus(_ context.Context, s app.Session) error {
	f.got = append(f.got, s.ID)
	return f.err
}

func lit(f domain.Frame) int {
	n := 0
	domain.AllPads(func(p domain.Pad) {
		if !f.At(p).Dark() {
			n++
		}
	})
	return n
}

// The three ways a grid can be empty are not the same fact, and only one of
// them is good news. A board that drew all three as darkness would report a
// crashed source as a quiet morning.
func TestAnEmptyGridSaysWhichKindOfEmptyItIs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	unread := app.NewSessionsControl(sessionSource{}, nil)
	before := unread.Render(now, full)

	empty := app.NewSessionsControl(sessionSource{}, nil)
	if err := empty.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	after := empty.Render(now, full)

	broken := app.NewSessionsControl(sessionSource{err: errors.New("no such directory")}, nil)
	_ = broken.Refresh(ctx)
	failed := broken.Render(now, full)

	for name, f := range map[string]domain.Frame{"unread": before, "empty": after, "failed": failed} {
		if lit(f) == 0 {
			t.Errorf("%s: the grid is entirely dark, which says nothing at all", name)
		}
	}

	corner, _ := domain.Grid(0, 0)
	if before.At(corner).Level >= after.At(corner).Level {
		t.Error("an unread grid is not dimmer than a read-and-empty one")
	}
	if failed.At(corner).Palette != domain.Red.Palette {
		t.Errorf("a failed read draws palette %d, want red", failed.At(corner).Palette)
	}
}

// The pad a person touched has to be the item the screen acts on. Render and
// Press derive the mapping independently, so this is the check that they cannot
// drift — the bug class where a board acts on something other than what you hit.
func TestAPressActsOnThePadItWasDrawnOn(t *testing.T) {
	t.Parallel()
	sessions := make([]app.Session, 0, 12)
	for i := range 12 {
		sessions = append(sessions, app.Session{Tool: "claude", ID: string(rune('a' + i)), Dir: "/w/repo", LastWrite: now})
	}
	focus := &focusRecorder{}
	screen := app.NewSessionsControl(sessionSource{sessions: sessions}, focus)
	if err := screen.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The eleventh session is on the second row, which is where a row-major
	// mapping and a column-major one part company.
	if _, err := screen.Press(context.Background(), app.Slot{Row: 1, Col: 2, Size: full}); err != nil {
		t.Fatal(err)
	}
	if len(focus.got) != 1 || focus.got[0] != "k" {
		t.Errorf("pressing (1,2) focused %v, want the eleventh session", focus.got)
	}

	// And a pad past the end of the list does nothing rather than the wrong
	// thing.
	if _, err := screen.Press(context.Background(), app.Slot{Row: 7, Col: 7, Size: full}); err != nil {
		t.Fatal(err)
	}
	if len(focus.got) != 1 {
		t.Errorf("an empty pad focused something: %v", focus.got)
	}
}

// One pad, several facts, so the colours are a priority order rather than a set
// of flags. The order is the design: a repository mario could not read must
// never look clean, and uncommitted work outranks divergence because it is the
// state that gets lost.
func TestARepositoryPadShowsItsMostUrgentState(t *testing.T) {
	t.Parallel()
	repos := []app.Repo{
		{Name: "broken", Unreadable: true, Dirty: true, Ahead: 3},
		{Name: "dirty", Dirty: true, Ahead: 2, Behind: 1},
		{Name: "ahead", Ahead: 1},
		{Name: "behind", Behind: 1},
		{Name: "clean"},
	}
	screen := app.NewReposControl(repoSource{repos: repos})
	if err := screen.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := screen.Render(now, full)

	want := []domain.Colour{domain.Red, domain.Amber, domain.Cyan, domain.Yellow, domain.Green}
	for i, colour := range want {
		pad, _ := domain.Grid(0, i)
		if got := f.At(pad).Palette; got != colour.Palette {
			t.Errorf("%s is palette %d, want %d", repos[i].Name, got, colour.Palette)
		}
	}

	pad, _ := domain.Grid(0, 0)
	if f.At(pad).Behaviour.Kind != domain.Flash {
		t.Error("an unreadable repository does not flash, so it can be mistaken for a state")
	}
}

// A failed refresh keeps the last good picture. Blanking would flicker the grid
// every time a directory was busy, and a blank board reads as "nothing is
// running" — the one wrong answer available here.
func TestAFailedRefreshKeepsTheLastGoodPicture(t *testing.T) {
	t.Parallel()
	src := &flaky{repos: []app.Repo{{Name: "one"}, {Name: "two"}}}
	screen := app.NewReposControl(src)
	if err := screen.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := lit(screen.Render(now, full))

	src.err = errors.New("workspace unmounted")
	if err := screen.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh hid the failure")
	}
	if got := lit(screen.Render(now, full)); got != good {
		t.Errorf("after a failed refresh %d pads are lit, want the last good %d", got, good)
	}
}

type flaky struct {
	repos []app.Repo
	err   error
}

func (f *flaky) Repos(context.Context) ([]app.Repo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.repos, nil
}

// Liveness is drawn as motion the device renders itself, which is the whole
// reason a board of working agents costs no host frames. A screen that animated
// this from the host would pin the run loop at 30 fps for as long as anything
// was running.
func TestALiveSessionPulsesRatherThanBeingAnimated(t *testing.T) {
	t.Parallel()
	screen := app.NewSessionsControl(sessionSource{sessions: []app.Session{
		{Tool: "claude", ID: "live", Dir: "/w/a", LastWrite: now},
		{Tool: "codex", ID: "today", Dir: "/w/b", LastWrite: now.Add(-30 * time.Minute)},
		{Tool: "claude", ID: "old", Dir: "/w/c", LastWrite: now.Add(-48 * time.Hour)},
	}}, nil)
	if err := screen.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := screen.Render(now, full)

	live, _ := domain.Grid(0, 0)
	today, _ := domain.Grid(0, 1)
	old, _ := domain.Grid(0, 2)

	if got := f.At(live).Behaviour.Kind; got != domain.Pulse {
		t.Errorf("a live session is %v, want it pulsing", got)
	}
	if got := f.At(today).Behaviour.Kind; got != domain.Solid {
		t.Errorf("a session from this morning is %v, want it steady", got)
	}
	if f.At(old).Level >= f.At(today).Level {
		t.Error("an old session is not dimmer than a recent one")
	}
	if f.At(today).Palette != domain.Cyan.Palette || f.At(live).Palette != domain.Amber.Palette {
		t.Error("the two agents are not distinguishable by hue")
	}
}

// The pad carries hue, position and behaviour; the words carry the same facts
// plus the one that lives on the next screen along. A person who has not learnt
// the palette gets the whole pad in one line.
func TestASessionPadCanSayWhatItIs(t *testing.T) {
	t.Parallel()
	src := sessionSource{sessions: []app.Session{{
		Tool: "claude", ID: "abc", Dir: "/w/soilytix/mario",
		LastWrite: now.Add(-4 * time.Minute), ContextTokens: 80_000, ContextLimit: 200_000,
	}}}
	s := app.NewSessionsControl(src, nil)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got := s.Describe(now, app.Slot{Size: full})
	for _, want := range []string{"claude", "mario", "4m ago", "60%"} {
		if !strings.Contains(got, want) {
			t.Errorf("the pad says %q, want it to mention %q", got, want)
		}
	}

	// A pad past the end of the list is empty, and saying so is the point: an
	// unexplained dark pad and a pad with nothing on it look identical.
	if got := s.Describe(now, app.Slot{Col: 4, Size: full}); !strings.Contains(got, "no session") {
		t.Errorf("an empty pad says %q, want it to say there is no session on it", got)
	}
}

// An unread source and an empty one are not the same fact, and the words must
// not flatten what the picture is careful to distinguish.
func TestAnUnreadSourceSaysSoRatherThanLookingEmpty(t *testing.T) {
	t.Parallel()
	s := app.NewSessionsControl(sessionSource{}, nil)
	if got := s.Describe(now, app.Slot{Size: full}); !strings.Contains(got, "not read yet") {
		t.Errorf("an unread control says %q, want it to say it has not read yet", got)
	}
}
