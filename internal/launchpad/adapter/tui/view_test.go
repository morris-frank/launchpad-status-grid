package tui_test

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/tui"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

type screen struct {
	name   string
	colour domain.Colour
	frame  domain.Frame
}

func (s screen) Name() string                  { return s.name }
func (s screen) Colour() domain.Colour         { return s.colour }
func (s screen) Render(time.Time) domain.Frame { return s.frame }
func (s screen) Press(context.Context, domain.Pad) (app.Outcome, error) {
	return app.Outcome{}, nil
}

func board(t *testing.T) *app.Board {
	t.Helper()
	b, err := app.New(
		screen{name: "one", colour: domain.Green},
		screen{name: "two", colour: domain.Cyan},
	)
	if err != nil {
		t.Fatal(err)
	}
	b.Render(time.Now()) // publish a status for the legend
	return b
}

// The preview is 81 lights and a legend, and the legend is the half that
// cannot be got from the picture: eight hues in a row say there are eight
// screens and nothing about what they are.
func TestThePreviewNamesWhatThePictureCannot(t *testing.T) {
	m := tui.New(tui.NewDevice(), board(t), tui.WithSize(90, 30))
	out := m.Render()

	for _, want := range []string{"launchpad", "one", "two", "nothing pressed yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("the preview does not mention %q", want)
		}
	}
}

// Every light is drawn in a fixed number of columns, brackets included, so that
// moving the cursor moves nothing else. A layout that shifted under the cursor
// is unreadable in exactly the situation the cursor exists for.
func TestTheBoardDoesNotMoveUnderTheCursor(t *testing.T) {
	b := board(t)
	widths := func(m *tui.Model) []int {
		var out []int
		for _, line := range strings.Split(stripANSI(m.Render()), "\n") {
			if strings.Contains(line, "██") || strings.Contains(line, "●●") {
				out = append(out, len([]rune(line)))
			}
		}
		return out
	}

	at := widths(tui.New(tui.NewDevice(), b, tui.WithSize(90, 30)))
	if len(at) != domain.GridSize+1 {
		t.Fatalf("the surface drew %d rows, want %d", len(at), domain.GridSize+1)
	}
	for _, w := range at[1:] {
		if w != at[0] {
			t.Errorf("the surface rows are %v columns wide; they must all match", at)
			break
		}
	}
}

// A pulsing pad and a steady one are the difference between "this agent is
// writing" and "this agent wrote". The device animates that from its own clock
// and sends no frames for it, so a preview that did not animate it itself would
// draw the two identically and hide the signal the screen is built on.
func TestThePreviewAnimatesWhatTheDeviceWouldAnimateItself(t *testing.T) {
	pad, _ := domain.Grid(0, 0)
	frame := domain.Blank().Set(pad, domain.Green.Cell(domain.LevelFull).With(domain.BPulse))
	b := board(t)

	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	at := func(offset time.Duration) string {
		m := tui.New(tui.NewDevice(), b,
			tui.WithSize(90, 30),
			tui.WithFrame(frame),
			tui.WithClock(func() time.Time { return base.Add(offset) }))
		return strings.SplitN(m.Render(), "\n", 4)[3]
	}

	// A quarter of the pulse's second apart is a quarter of its cycle, which is
	// as far from its own colour as it ever gets.
	if at(0) == at(250*time.Millisecond) {
		t.Error("a pulsing pad renders identically across the pulse, so it reads as steady")
	}
}

// stripANSI removes styling so a layout assertion is about columns rather than
// about escape sequences.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Grid at 2x draws every pad twice as wide and twice as tall, the cursor's
// brackets around it, so a host with room draws the same board bigger.
func TestTheGridAtTwiceIsTwiceAsWideAndTwiceAsTall(t *testing.T) {
	m := tui.New(tui.NewDevice(), board(t), tui.WithSize(90, 30))
	one, two := m.Grid(1), m.Grid(2)
	if len(two) != 2*len(one) {
		t.Fatalf("the 2x grid is %d rows, want %d", len(two), 2*len(one))
	}
	for i, line := range two {
		if got, want := len([]rune(stripANSI(line))), 2*len([]rune(stripANSI(one[i/2]))); got != want {
			t.Errorf("2x row %d is %d columns, want %d", i, got, want)
		}
	}
	if !strings.Contains(stripANSI(strings.Join(two, "\n")), tui.EncloseOpen+" ●●●● "+tui.EncloseClose) {
		t.Errorf("the cursor's pad is not doubled inside its brackets:\n%s", stripANSI(strings.Join(two, "\n")))
	}
}

// The pad under the cursor is enclosed by the brackets on its tint, so the
// hovered pad is found at a glance rather than by reading the legend, and no
// mark sits in the gap it shares with the pad to its left. The brackets are
// characters and not a colour, so they survive NO_COLOR — which reaches this
// output as its ANSI stripped at the writer — and --no-glyphs, since neither is
// a Nerd Font's.
func TestTheHoveredPadIsMarkedInColourAndWithout(t *testing.T) {
	lit := domain.Blank()
	for col := range domain.GridSize {
		p, _ := domain.Grid(0, col)
		lit = lit.Set(p, domain.Green.Cell(domain.LevelFull))
	}
	dev := tui.NewDevice()
	b := board(t)
	away := tui.New(tui.NewDevice(), b, tui.WithSize(90, 30), tui.WithFrame(lit))
	m := tui.New(dev, b, tui.WithSize(90, 30), tui.WithFrame(lit))
	for _, k := range []rune{'j', 'l'} { // onto the first grid row's second pad
		m.Update(tea.KeyPressMsg{Code: k, Text: string(k)})
	}

	for _, scale := range []int{1, 2} {
		grid, plain := m.Grid(scale), away.Grid(scale)
		for i := range scale {
			line := grid[scale+i] // the first grid row, under the round buttons
			if line == plain[scale+i] {
				t.Errorf("at %dx the row under the cursor draws as it does with the cursor away", scale)
			}
			r := []rune(stripANSI(line))
			pad := func(col int) string { return string(r[col*4*scale : (col+1)*4*scale]) }
			if !strings.HasPrefix(pad(1), tui.EncloseOpen) || !strings.HasSuffix(pad(1), tui.EncloseClose) ||
				strings.Contains(stripANSI(line), tui.SelectionBar) {
				t.Errorf("at %dx the hovered pad is not enclosed by the brackets: %q", scale, pad(1))
			}
			for _, n := range []int{0, 2} {
				if pad(1) == pad(n) {
					t.Errorf("at %dx without colour the hovered pad draws as its neighbour %d: %q", scale, n, pad(1))
				}
			}
		}
	}

	// Hovering is the preview's own: the device is sent no frame and no press.
	select {
	case f := <-dev.Frames():
		t.Errorf("moving the cursor sent the device a frame: %v", f)
	default:
	}
	select {
	case p := <-dev.Presses():
		t.Errorf("moving the cursor pressed %v", p)
	default:
	}
}

// The preview names the board's screens on its first line and its keys on
// its last, and every line fits the terminal.
func TestThePreviewNamesItsScreensAndKeysAndFits(t *testing.T) {
	for _, width := range []int{50, 60, 80} {
		m := tui.New(tui.NewDevice(), board(t), tui.WithSize(width, 30))
		lines := strings.Split(strings.TrimRight(stripANSI(m.Render()), "\n"), "\n")
		if !strings.Contains(lines[0], "one") || !strings.Contains(lines[0], "two") {
			t.Errorf("at %d the tab row is %q, want both screens", width, lines[0])
		}
		if last := lines[len(lines)-1]; !strings.Contains(last, "enter") || !strings.Contains(last, "press") {
			t.Errorf("at %d the shortcut line is %q, want the pad keys", width, last)
		}
		for _, line := range lines {
			if got := len([]rune(line)); got > width {
				t.Errorf("at %d a line is %d wide: %q", width, got, line)
			}
		}
	}
}
