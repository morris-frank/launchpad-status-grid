package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// Minimum usable size. Below it the preview says so rather than drawing a
// board with the rows folded into each other, which is worse than no board.
const (
	minWidth  = 40
	minHeight = 16
)

// The two glyphs the surface is made of: square pads are square, round buttons
// are round. It is the only cue that says which run of lights is chrome, and on
// the device that distinction is made by your fingertips.
const (
	squarePad = "██"
	roundPad  = "●●"
	// The logo is the one light with no switch under it, so it gets a glyph
	// nobody will try to press.
	logoPad = "◆◆"
)

// Which colours are whose. A lit pad, a flash's alternate and a screen's name
// are the hardware's own palette — domain's RGB, drawn as the device would
// light it, because a preview in another hue lies about the hardware. Every
// other ink is style.go's: the unlit pad (the device draws nothing there,
// so the terminal's track stands in), the cursor, and the legend's words.
// paletteRGB is the terminal's approximation of the device palette, for the two
// commands that name a colour by index instead of by RGB.
//
// It covers the semantic palette and nothing else, because nothing else is ever
// asked for: a colour that is allowed to flash or pulse has to be a palette
// entry, and the palette is the eight-or-so hues in domain. An index from
// outside it draws dark rather than guessing — a wrong hue in a preview is a
// lie about what the hardware will do.
var paletteRGB = func() map[byte]domain.RGB {
	m := map[byte]domain.RGB{}
	for _, c := range []domain.Colour{
		domain.White, domain.Grey, domain.Red, domain.Amber, domain.Yellow,
		domain.Green, domain.Mint, domain.Cyan, domain.Blue, domain.Magenta,
	} {
		m[c.Palette] = c.RGB
	}
	return m
}()

// View satisfies bubbletea.
func (m *Model) View() tea.View {
	v := tea.NewView(m.Render())
	// The alternate screen, for the same reason the workspace board uses it: a
	// surface that repaints twelve times a second in the primary buffer
	// destroys the scrollback you were reading before you opened it.
	v.AltScreen = true
	return v
}

// Render draws the whole preview: the board's screens are its tab row, the
// surface its content, then the legend and the keys. It is an ordinary function of the model's
// state, so it can be asserted on without a terminal.
func (m *Model) Render() string {
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("launchpad-status-grid: the preview needs at least %dx%d; the terminal is %dx%d\n",
			minWidth, minHeight, m.width, m.height)
	}

	status := m.board.Status()
	now := m.now()
	var names []string
	for _, s := range m.board.Screens() {
		names = append(names, s.Name())
	}
	var content strings.Builder
	for _, line := range m.surface(now) {
		content.WriteString("  " + line + "\n")
	}
	return screen("launchpad", names, status.Index, content.String(),
		strings.Join(indent(m.legend(now)), "\n"), previewKeys, m.width, m.height) + "\n"
}

// previewKeys is the preview's shortcut line.
const previewKeys = "arrows move · enter press · tab screen · q quit"

func indent(lines []string) []string {
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return lines
}

func (m *Model) label(name string) string { return muted(fmt.Sprintf("%-7s", name)) }

// surface draws the nine rows: the round buttons and the logo along the top,
// then eight grid rows each followed by its side button, a line each.
func (m *Model) surface(now time.Time) []string { return m.surfaceAt(now, 1) }

// surfaceAt is surface with every pad scale lines tall.
func (m *Model) surfaceAt(now time.Time, scale int) []string {
	lines := make([]string, 0, (domain.GridSize+1)*scale)
	for row := 0; row <= domain.GridSize; row++ {
		for range scale {
			var b strings.Builder
			for col := 0; col <= domain.GridSize; col++ {
				b.WriteString(m.pad(row, col, now, scale))
			}
			lines = append(lines, b.String())
		}
	}
	return lines
}

// pad draws one line of one light in four columns per scale, the cursor's
// cell included, so that moving the cursor never moves anything else. The
// cursor is enclose's brackets around every line of its pad on the
// selection's tint: characters and not a colour, so the hovered pad is found
// under NO_COLOR and --no-glyphs alike.
func (m *Model) pad(row, col int, now time.Time, scale int) string {
	pad, ok := padAt(row, col)
	if !ok {
		return strings.Repeat(" ", 4*scale)
	}

	glyph := squarePad
	switch {
	case pad == domain.Logo:
		glyph = logoPad
	case !pad.IsGrid():
		glyph = roundPad
	}
	body := glow(m.colourOf(m.frame.At(pad), now), false, strings.Repeat(glyph, scale))

	gap := strings.Repeat(" ", scale-1)
	cell := " " + gap + body + gap + " "
	if row == m.row && col == m.col {
		return enclose(cell)
	}
	return cell
}

// colourOf is what one cell looks like at this instant, including the two
// behaviours the hardware would animate by itself.
//
// Doing it here rather than leaving them static is what makes the preview
// honest: a pulsing pad and a steady one are the difference between "this agent
// is writing" and "this agent wrote", and a preview that drew them identically
// would hide the signal the screen is built on.
func (m *Model) colourOf(c domain.Cell, now time.Time) color.Color {
	if c.Dark() {
		return m.unlit
	}

	rgb := c.Lit()
	switch c.Behaviour.Kind {
	case domain.Pulse:
		// 1 Hz, and never all the way to black: the device's pulse dips and
		// recovers, it does not blink.
		phase := float64(now.UnixMilli()%1000) / 1000
		rgb = c.RGB.Scale(c.Level.Factor() * (0.35 + 0.65*(0.5+0.5*math.Sin(2*math.Pi*phase))))
	case domain.Flash:
		// 2 Hz between the cell's hue and its alternate, which is a palette
		// index — index 0 being the device's own "off".
		if now.UnixMilli()%500 < 250 {
			alt, known := paletteRGB[c.Behaviour.Alt]
			if !known || c.Behaviour.Alt == 0 {
				return m.unlit
			}
			rgb = alt
		}
	}
	return lamp(rgb)
}

// lamp widens the device's 7-bit channels to the terminal's 8-bit ones. Doubling
// rather than scaling by 255/127, because 127 must land on 255 and the error
// everywhere else is under half a step. The hue is the device's, not a role.
func lamp(c domain.RGB) color.Color {
	return color.RGBA{R: uint8(c.R) * 2, G: uint8(c.G) * 2, B: uint8(c.B) * 2, A: 0xFF}
}

// legend is the pad under the cursor and the last press.
//
// Which screens exist is the one part of the board that cannot be read from the
// picture at all: the two arrows show the hue either side of you and never how
// many places there are. On the device you learn them; here the tab row says
// them.
func (m *Model) legend(now time.Time) []string {
	note := m.board.Status().Note
	if note == "" {
		note = "nothing pressed yet"
	}
	lines := []string{m.label("pad") + m.describe(now), m.label("last") + note}
	return lines
}

// describe is the pad under the cursor, in words.
//
// It is the answer to the question the hardware raises and cannot settle: a
// board of 81 coloured lights says everything at once and explains none of it,
// and until you have learnt the palette a pad is a colour rather than a
// session. The cursor follows a press from either surface, so this line names
// the pad you just pressed with your thumb as readily as the one you walked
// the cursor onto.
func (m *Model) describe(now time.Time) string {
	pad, ok := padAt(m.row, m.col)
	if !ok {
		return muted("—")
	}
	what := m.board.Describe(now, pad)
	if what == "" {
		return muted("nothing to say about this pad")
	}
	return what
}
