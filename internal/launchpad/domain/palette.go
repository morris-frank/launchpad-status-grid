package domain

// Colour is a semantic hue: a device palette index and the RGB that matches it.
//
// The pair travels together because the two device commands need different
// halves — flash and pulse take the index, a per-brightness solid pad takes the
// RGB — and a colour that existed only as RGB would change hue the moment it
// started blinking. Keeping them in one value is what makes "a category colour
// is a palette entry" a fact of the type rather than a rule somebody remembers.
type Colour struct {
	Palette byte
	RGB     RGB
}

// Cell makes a solid cell of this colour at the given brightness.
func (c Colour) Cell(l Level) Cell {
	return Cell{Palette: c.Palette, RGB: c.RGB, Level: l, Behaviour: BSolid}
}

// The semantic palette. Eight hues is a hard cap, not a starting point: the
// board is read at a glance and by muscle memory, and a ninth colour costs more
// in recognition than the distinction it adds is worth.
//
// Indices are the device's stock palette, verified against the unit. Red is
// reserved for destructive and for attention, and is never a resting category —
// a red pad on a settled board always means something.
var (
	Off_    = Colour{Palette: 0, RGB: RGB{0, 0, 0}}
	White   = Colour{Palette: 3, RGB: RGB{127, 127, 127}}
	Grey    = Colour{Palette: 2, RGB: RGB{60, 60, 60}}
	Red     = Colour{Palette: 5, RGB: RGB{127, 0, 0}}
	Amber   = Colour{Palette: 9, RGB: RGB{127, 60, 0}}
	Yellow  = Colour{Palette: 13, RGB: RGB{127, 127, 0}}
	Green   = Colour{Palette: 21, RGB: RGB{0, 127, 0}}
	Mint    = Colour{Palette: 25, RGB: RGB{0, 127, 80}}
	Cyan    = Colour{Palette: 37, RGB: RGB{0, 110, 127}}
	Blue    = Colour{Palette: 45, RGB: RGB{0, 30, 127}}
	Magenta = Colour{Palette: 53, RGB: RGB{127, 0, 100}}
)

// Ramp is the colour a proportion should be drawn in when the proportion is
// itself the message — a quota emptying, a disk filling, a context window being
// consumed. Green while there is room, amber as it tightens, red when it is
// nearly gone.
//
// Thresholds are stated once, here, so two screens that both show "how much is
// left" cannot disagree about when it becomes worrying. The mapping is on
// REMAINING rather than used, because every caller of this has a "how much is
// left" number and half of them would invert it wrongly.
func Ramp(remaining float64) Colour {
	switch {
	case remaining <= 0.10:
		return Red
	case remaining <= 0.30:
		return Amber
	default:
		return Green
	}
}
