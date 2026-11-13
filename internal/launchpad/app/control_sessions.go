package app

import (
	"context"
	"fmt"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// How recently a session has to have written to be drawn as live.
//
// Two thresholds rather than a continuum, because the grid has three states to
// spend and a person reading a board at arm's length distinguishes "moving",
// "today" and "old" — not a gradient. Live is generous enough to cover an agent
// thinking between writes; recent is the working day.
const (
	sessionLive   = 2 * time.Minute
	sessionRecent = time.Hour
)

// SessionsControl shows every coding-agent session as a pad, and brings one
// forward when it is pressed.
//
// The whole reason this is on hardware rather than in a terminal: the sessions
// you are running are the thing you look away from your editor to check, and a
// board that answers it without stealing the screen is answering it for free.
type SessionsControl struct {
	data  *Data[[]Session]
	focus Focus
}

// NewSessionsControl reads from src. focus may be nil, in which case a press
// says so rather than silently doing nothing.
func NewSessionsControl(src Sessions, focus Focus) *SessionsControl {
	return &SessionsControl{
		data:  NewData(func(ctx context.Context) ([]Session, error) { return src.Sessions(ctx) }),
		focus: focus,
	}
}

// Refresh implements Refresher.
func (s *SessionsControl) Refresh(ctx context.Context) error { return s.data.Refresh(ctx) }

// Render implements Control. Sessions fill the rectangle row-major from the
// top-left, so the newest are nearest the thumb whatever size it is given.
func (s *SessionsControl) Render(now time.Time, size domain.Size) domain.Frame {
	sessions, loaded, err := s.data.Value()
	if f, empty := emptyGrid(loaded, err, len(sessions), size); empty {
		return f
	}

	f := domain.Blank()
	for i, session := range sessions {
		if i >= size.Count() {
			break // more sessions than cells: the control shows the newest it fits
		}
		f = f.SetGrid(i/size.Cols, i%size.Cols, sessionCell(session, now))
	}
	return f
}

// sessionCell is the whole visual language of this screen in one function: hue
// is which agent, and behaviour is how alive it is.
//
// A live session pulses, which the device renders from its own clock — so a
// board full of working agents costs the host nothing per frame. That is not an
// optimisation detail, it is why the liveness signal can be motion at all.
func sessionCell(s Session, now time.Time) domain.Cell {
	hue := domain.Cyan
	if s.Tool == "claude" {
		hue = domain.Amber
	}

	switch age := now.Sub(s.LastWrite); {
	case age <= sessionLive:
		return hue.Cell(domain.LevelFull).With(domain.BPulse)
	case age <= sessionRecent:
		return hue.Cell(domain.LevelBright)
	default:
		return hue.Cell(domain.LevelDim)
	}
}

// Press implements Control.
func (s *SessionsControl) Press(ctx context.Context, at Slot) (Outcome, error) {
	sessions, _, _ := s.data.Value()
	i := at.Index()
	if i >= len(sessions) {
		return Outcome{Attention: true}, nil
	}
	session := sessions[i]

	if s.focus == nil {
		// No focus action configured. The press still answers the question the
		// pad raised — which session is this — because a board that says
		// nothing on a press is indistinguishable from a board that did not
		// register it.
		return Outcome{Note: fmt.Sprintf("%s in %s (%s) — nothing configured to act on a press",
			session.Tool, session.Label(), session.ID), Attention: true}, nil
	}
	if err := s.focus.Focus(ctx, session); err != nil {
		return Outcome{Note: fmt.Sprintf("could not reach %s in %s", session.Tool, session.Label())},
			fmt.Errorf("launchpad: focus %s: %w", session.ID, err)
	}
	note := fmt.Sprintf("brought %s in %s forward", session.Tool, session.Label())
	if remaining, known := session.ContextRemaining(); known {
		note += fmt.Sprintf(" — %d%% of its window left", int(remaining*100))
	}
	return Outcome{Note: note}, nil
}

// emptyGrid draws the three states that are not content, and reports whether it
// drew one.
//
// A dark grid is the ambiguity worth spending code on: "nothing is running",
// "not read yet" and "the source failed" all look identical, and only one of
// them is good news. An outline says the screen is alive and the grid is
// genuinely empty; red says the last read failed.
func emptyGrid(loaded bool, err error, n int, size domain.Size) (domain.Frame, bool) {
	switch {
	case err != nil && !loaded:
		return domain.Blank().Outline(size.Region(), domain.Red.Cell(domain.LevelDim)), true
	case !loaded:
		return domain.Blank().Outline(size.Region(), domain.Grey.Cell(domain.LevelFaint)), true
	case n == 0:
		return domain.Blank().Outline(size.Region(), domain.Grey.Cell(domain.LevelDim)), true
	}
	return domain.Frame{}, false
}

// Describe implements Describer: the pad in words, in the order you would ask
// for them — which agent, where, how long since it wrote, how much room it has
// left. It is the same four facts the pad encodes as hue, position, behaviour
// and nothing, which is the point: three of them are on the light and the
// fourth is on the next screen along, and here they are in one line.
func (s *SessionsControl) Describe(now time.Time, at Slot) string {
	sessions, loaded, err := s.data.Value()
	if note, pending := pending(loaded, err); pending {
		return note
	}
	i := at.Index()
	if i >= len(sessions) {
		return "no session on this pad"
	}

	session := sessions[i]
	line := fmt.Sprintf("%s in %s · wrote %s ago", session.Tool, session.Label(), since(now, session.LastWrite))
	if remaining, known := session.ContextRemaining(); known {
		return line + fmt.Sprintf(" · %s of its window left", percent(int(remaining*100)))
	}
	return line + " · window unknown"
}

// pending is emptyGrid's two not-yet states in words, for a surface that has
// room for them. It is separate from emptyGrid rather than folded into it
// because the third state — read fine, nothing to show — has a different
// answer per control: an empty grid is one picture and "no session on this
// pad" is not the same sentence as "no repository on this pad".
func pending(loaded bool, err error) (string, bool) {
	switch {
	case err != nil && !loaded:
		return "the source could not be read", true
	case !loaded:
		return "not read yet", true
	}
	return "", false
}

// since is an age at the resolution a person reading a board cares about. No
// seconds: a pad that says "wrote 43s ago" invites you to watch it count, and
// the question this answers is only ever "is that one still going".
func since(now, then time.Time) string {
	switch d := now.Sub(then); {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
