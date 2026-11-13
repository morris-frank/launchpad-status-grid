// Package app is the Launchpad as a running board: a set of screens, the one
// that is showing, and the loop that turns state into a frame and a press into
// an intent.
//
// The split from domain is that domain draws and this decides what to draw.
// Nothing here knows how a frame reaches hardware; a Device is a port and its
// implementations are adapters, one of which is a terminal.
//
// The board is deliberately small. A screen is an interface with two methods,
// so adding one is a file and removing one is deleting that file — which is the
// point of the whole exercise. Anything a screen needs that is not on the
// screen's own data goes in through its constructor at the composition root,
// never by reaching sideways from here. Nothing in the context knows how many
// screens there are or what they are, so a wild component is added, lived
// with and removed without touching anything but its own file and one line at
// the composition root.
//
// A screen returns a picture and never writes to anything, and the only code
// that knows what MIDI is lives in one adapter.
package app

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// Screen is one page of the board.
//
// Render is called on every tick and must be pure with respect to the board: it
// gets the moment and returns a picture, and anything it needs to know it
// captured when it was constructed. Press is called with a pad the person
// actually pressed and returns what should happen next.
//
// The two methods are separate because a screen that acts during its render is
// a screen that acts sixty times a second.
type Screen interface {
	// Name is what the screen is called, for the tab row's tooltip, the TUI's
	// header, and log lines. Stable: it identifies the screen.
	Name() string
	// Colour is the screen's identity hue. It carries the tab, the logo LED
	// while the screen is showing, and the wipe that announces it. One hue per
	// screen, never reused, is most of what makes the board navigable.
	Colour() domain.Colour
	// Render draws the screen's content into the grid. Chrome is drawn over it
	// by the board, so a screen may use the whole grid and should not try to
	// leave room for the tab row.
	Render(now time.Time) domain.Frame
	// Press handles a press on a grid pad, or on a round button the screen
	// owns through ChromeOwner. No other chrome press reaches a screen.
	Press(ctx context.Context, pad domain.Pad) (Outcome, error)
}

// ChromeOwner is a screen that binds round buttons the board has not bound
// itself — media keys on the top row. Optional: a screen that does not
// implement it gets grid presses alone, and the board's own arrows and logo
// outrank anything a screen claims.
type ChromeOwner interface {
	OwnsChrome(pad domain.Pad) bool
}

// ownsChrome reports whether the active screen answers pad.
func (b *Board) ownsChrome(pad domain.Pad) bool {
	o, ok := b.Active().(ChromeOwner)
	return ok && pad.Pressable() && o.OwnsChrome(pad)
}

// Outcome is what a press did, in the only terms the board cares about.
type Outcome struct {
	// Note is a short line for the run log and the TUI, in the imperative past:
	// "opened claude in soilytix/mario". Empty means the press was understood
	// and had nothing to say.
	Note string
	// Attention marks the pad as wanting a look — it flashes until the next
	// press. For a press that failed in a way the person has to resolve.
	Attention bool
}

// Device is the surface a board drives. Implemented by the CoreMIDI adapter and
// by the terminal preview, which is why every screen can be built and watched
// before any hardware is involved.
//
// Render is called with a complete frame every tick and is expected to do its
// own diffing: the board has no idea what the surface currently looks like, and
// deliberately does not, because that is the state that makes "save the board,
// draw over it, restore it" possible and therefore inevitable.
//
// The port has no device-shaped method, which is what keeps MIDI in one
// adapter.
type Device interface {
	Render(domain.Frame) error
	// Presses yields every press. Closed when the device is finished; a device
	// that has lost its hardware blocks rather than closing, because a
	// Launchpad being unplugged is not the board being over.
	Presses() <-chan Press
	Close() error
}

// Press is one pad going down or coming up.
type Press struct {
	Pad  domain.Pad
	Down bool
	At   time.Time
}

// Board holds the screens and the one that is showing.
type Board struct {
	screens []Screen
	active  int

	// effect is the overlay currently in flight, or nil. There is at most one:
	// two effects at once is two things asking for the same attention, and the
	// second one is always the one that matters.
	effect *effect

	// flashes carries Notify's notifications to the run loop, and flashing
	// is the one in flight, published for Describe.
	flashes  chan Flash
	flashing atomic.Pointer[flashing]

	// attention is the pad flashing until the next press, if any.
	attention    domain.Pad
	hasAttention bool

	lastNote string

	// lastPad is the pad the last press landed on, published in Status.
	lastPad    domain.Pad
	hasLastPad bool

	// status is the board in words, published for a surface that has room for
	// them. It is an atomic rather than a plain field because the terminal
	// preview reads it from its own goroutine while the run loop writes it —
	// the board is single-writer, so this is the whole of the synchronisation
	// it needs.
	status atomic.Pointer[Status]
}

// Status is what a board would say about itself if it could use words: which
// screen is showing, where it sits in the tab row, and what the last press did.
// The 81 lights say all of this already; a terminal has room to say it again in
// a form you can read without having learned the colours.
type Status struct {
	Screen string
	Index  int
	Note   string
	// Pad is the last pad pressed, on any surface, and Pressed is false until
	// one has been. It is published so that a surface showing the board can
	// follow a thumb it cannot see: with the terminal preview mirroring the
	// hardware, this is what lets the preview point at the pad you just
	// pressed and name it.
	Pad     domain.Pad
	Pressed bool
}

type effect struct {
	frame func(phase float64) domain.Frame
	start time.Time
	over  time.Duration
}

// PrevTab and NextTab are the two round buttons that move between screens: the
// left and right arrows, in the top row because it is the only run of pads a
// thumb reaches without covering the grid, and reserved on every screen so that
// "how do I get back" never depends on which screen you are lost on.
//
// Two arrows rather than one tab per screen, which is what this was. A tab per
// screen lights only as many buttons as there are screens, so five of the eight
// sat dark and inert — and the four that read most like menu buttons, the ones
// the device labels Session, Drums, Keys and User, were among them. A board
// whose obvious controls do nothing is indistinguishable from a board that has
// stopped listening, and it was read exactly that way.
//
// What it costs: the top row no longer says how many screens there are. The
// logo carries the active screen's hue and each arrow carries the hue of the
// screen it leads to, so where you are and where a press goes are both still
// visible — how many places there are in total is not, and that is the trade.
const (
	PrevTab = 3
	NextTab = 4
)

// MaxScreens caps the board. Cycling imposes no structural limit the way a tab
// per screen did, but a cycle you press through eight times to get back where
// you started has stopped being navigation.
const MaxScreens = domain.GridSize

// ErrNoScreens is returned by New when it is given nothing to show.
var ErrNoScreens = errors.New("launchpad: a board needs at least one screen")

// New builds a board over screens, the first of which is showing. It takes
// them variadically, as NewPanel takes placements, so there is no registry to
// forget to unregister from.
func New(screens ...Screen) (*Board, error) {
	if len(screens) == 0 {
		return nil, ErrNoScreens
	}
	if len(screens) > MaxScreens {
		return nil, fmt.Errorf("launchpad: %d screens, but a board holds at most %d", len(screens), MaxScreens)
	}
	seen := map[string]bool{}
	for _, s := range screens {
		if seen[s.Name()] {
			return nil, fmt.Errorf("launchpad: two screens are both called %q", s.Name())
		}
		seen[s.Name()] = true
	}
	return &Board{screens: screens, flashes: make(chan Flash, 4)}, nil
}

// Screens is the board's screens, in tab order.
func (b *Board) Screens() []Screen { return b.screens }

// Active is the screen currently showing.
func (b *Board) Active() Screen { return b.screens[b.active] }

// ActiveIndex is the showing screen's position in the tab row.
func (b *Board) ActiveIndex() int { return b.active }

// LastNote is the most recent press outcome's note, for a status line.
func (b *Board) LastNote() string { return b.lastNote }

// Render composes the whole surface: the active screen, then chrome over it,
// then whatever effect is in flight over that.
//
// The order is the argument. Chrome goes over the screen because navigation
// must never be hidden by content, and the effect goes over chrome because an
// effect that a tab could occlude would announce a screen change you cannot see.
func (b *Board) Render(now time.Time) domain.Frame {
	b.status.Store(&Status{
		Screen:  b.Active().Name(),
		Index:   b.active,
		Note:    b.lastNote,
		Pad:     b.lastPad,
		Pressed: b.hasLastPad,
	})

	f := b.Active().Render(now)
	f = f.Over(b.chrome(now))

	if b.hasAttention {
		if c := f.At(b.attention); !c.Dark() {
			f = f.Set(b.attention, c.With(domain.BFlash(domain.Red.Palette)))
		} else {
			f = f.Set(b.attention, domain.Red.Cell(domain.LevelFull).With(domain.BFlash(0)))
		}
	}

	if e := b.effect; e != nil {
		phase := float64(now.Sub(e.start)) / float64(e.over)
		if phase >= 1 {
			b.effect = nil
		} else {
			f = f.Over(e.frame(phase))
		}
	}
	return f
}

// chrome is the two arrows plus the logo, drawn identically on every screen.
//
// Each arrow wears the hue of the screen it leads to, so a press is aimed
// rather than guessed: the board answers "what is that way" before you go. The
// logo carries the active screen's hue, which is the one light that tells you
// where you are without you looking away from what you were doing. Arrows sit
// at dim and the logo at bright, so the eye goes to the content first.
//
// A board with one screen draws no arrows at all, because there is nowhere for
// them to lead and a lit button that does nothing is the thing this row exists
// to stop being.
func (b *Board) chrome(time.Time) domain.Frame {
	f := domain.Blank()
	if len(b.screens) > 1 {
		if pad, ok := domain.Top(PrevTab); ok {
			f = f.Set(pad, b.screens[b.step(-1)].Colour().Cell(domain.LevelDim))
		}
		if pad, ok := domain.Top(NextTab); ok {
			f = f.Set(pad, b.screens[b.step(1)].Colour().Cell(domain.LevelDim))
		}
	}
	return f.Set(domain.Logo, b.Active().Colour().Cell(domain.LevelBright))
}

// Handle routes a press. A press on the tab row switches screens and never
// reaches a screen; anything else is the active screen's, and only on the way
// down — a board that acted on release would fire twice for every press, and a
// board that acted on both would fire twice for half of them.
func (b *Board) Handle(ctx context.Context, p Press) error {
	if !p.Down {
		return nil
	}
	b.hasAttention = false
	b.lastPad, b.hasLastPad = p.Pad, true

	if delta, ok := b.tabStep(p.Pad); ok {
		b.Show(b.step(delta), p.At)
		return nil
	}
	if !p.Pad.IsGrid() && !b.ownsChrome(p.Pad) {
		// A chrome pad with nothing bound to it still owes an answer. Silence
		// is indistinguishable from a board that has stopped listening, and
		// that is not a hypothetical: a working board was reported dead
		// because the buttons somebody pressed first were the unbound ones.
		b.attention, b.hasAttention = p.Pad, true
		return nil
	}

	b.effect = &effect{
		frame: func(phase float64) domain.Frame {
			return domain.Ripple(p.Pad, phase, b.Active().Colour())
		},
		start: p.At,
		over:  180 * time.Millisecond,
	}

	out, err := b.Active().Press(ctx, p.Pad)
	b.lastNote = out.Note
	if out.Attention || err != nil {
		b.attention, b.hasAttention = p.Pad, true
	}
	return err
}

// tabStep is how far a chrome pad moves the board, if it moves it. With one
// screen neither arrow is bound, so both fall through to the acknowledgement a
// press with nowhere to go deserves rather than silently doing nothing.
func (b *Board) tabStep(p domain.Pad) (int, bool) {
	if len(b.screens) < 2 {
		return 0, false
	}
	if pad, ok := domain.Top(PrevTab); ok && pad == p {
		return -1, true
	}
	if pad, ok := domain.Top(NextTab); ok && pad == p {
		return 1, true
	}
	return 0, false
}

// step is the screen delta places along the cycle. Screens wrap because with
// no per-screen tab to aim at, an arrow that refused to move at the end of the
// row would look like the board had stopped responding.
func (b *Board) step(delta int) int {
	n := len(b.screens)
	return ((b.active+delta)%n + n) % n
}

// Show switches to screen i and arms the wipe that announces it.
//
// The wipe is not decoration. A screen change with no transition is
// indistinguishable from the data changing under you, and on a board with no
// labels that ambiguity is the whole navigation problem. Its direction carries
// which way you moved through the cycle, so the board tells you where you came
// from as well as where you are.
func (b *Board) Show(i int, now time.Time) {
	if i < 0 || i >= len(b.screens) || i == b.active {
		return
	}
	axis := b.axisTo(i)
	hue := b.screens[i].Colour()

	b.active = i
	b.hasAttention = false
	b.effect = &effect{
		frame: func(phase float64) domain.Frame { return domain.Wipe(axis, phase, hue) },
		start: now,
		over:  220 * time.Millisecond,
	}
}

// axisTo is the direction the wipe travels to reach screen i: whichever way
// round the cycle is shorter, which is the way a person pressing an arrow
// actually moved. Deriving it rather than passing it in is what keeps the wipe
// honest for the wrap, where the next screen has the lower index.
func (b *Board) axisTo(i int) domain.Axis {
	n := len(b.screens)
	if forward := ((i-b.active)%n + n) % n; forward*2 <= n {
		return domain.Right
	}
	return domain.Left
}

// Status is the board in words, safe to read from another goroutine. It is
// empty until the first Render.
func (b *Board) Status() Status {
	if s := b.status.Load(); s != nil {
		return *s
	}
	return Status{}
}

// Animating reports whether an effect is in flight. A driver ticks fast while
// this is true and slowly when it is not, which is what keeps a settled board
// at zero host frames.
func (b *Board) Animating() bool { return b.effect != nil }

// Inspector is a screen that can say in words what one of its pads is showing.
// Optional, exactly as Describer is on a control: a screen that cannot say is
// not a broken screen.
type Inspector interface {
	Describe(now time.Time, pad domain.Pad) string
}

// Describe answers "what is this pad" for any pad on the board.
//
// This is the one question the hardware cannot answer about itself. A lit pad
// carries a hue, a level and a behaviour, and all three are learnable — but
// nobody learns them by staring, and until you have, a board of colours is a
// board of colours. So the board can say what it means, and a surface with
// room for a sentence prints it.
//
// The board answers for its own chrome and delegates the grid, which is the
// same split as Handle: chrome belongs to the board on every screen and the
// grid belongs to whatever is showing. The grid goes through the same walk
// Press does, so what a pad says it is and what it does cannot disagree.
func (b *Board) Describe(now time.Time, pad domain.Pad) string {
	if name, ok := b.flashName(now); ok {
		return "flash — " + name
	}
	if pad == domain.Logo {
		return fmt.Sprintf("logo — showing %s", b.Active().Name())
	}
	if delta, ok := b.tabStep(pad); ok {
		where := "previous"
		if delta > 0 {
			where = "next"
		}
		return fmt.Sprintf("%s screen — %s", where, b.screens[b.step(delta)].Name())
	}
	if !pad.IsGrid() && !b.ownsChrome(pad) {
		return "not bound to anything"
	}
	if s, ok := b.Active().(Inspector); ok {
		return s.Describe(now, pad)
	}
	return ""
}
