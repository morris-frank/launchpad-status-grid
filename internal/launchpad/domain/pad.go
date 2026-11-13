// Package domain is the Launchpad's surface as a value: where the 81 lights
// are, what one of them can look like, and the drawing primitives that put a
// picture on them.
//
// Nothing here knows about MIDI, CoreMIDI, a device, or a terminal. A Frame is
// a picture; turning one into bytes is an adapter's job, and rendering one into
// a terminal is another adapter's job. That split is the whole reason the
// toolbox is usable before anybody plugs a Launchpad in: every widget in this
// package is a pure function over a Frame, so a test and a TUI preview exercise
// exactly the code the hardware runs.
//
// The device facts this package encodes were measured against a Novation
// Launchpad Mini MK3 (firmware 2.00) rather than read out of documentation.
package domain

// Pad is one addressable light, in a flat index space of 0..80.
//
// Two coordinate systems meet here and nowhere else. Everything this binary writes
// speaks reading order — row 0 is the TOP row, column 0 the LEFT one — because
// that is how a person describes a grid and how a terminal draws one. The
// device speaks `row*10 + col` with row 1 at the BOTTOM, so the top-left grid
// pad is 81 and the bottom-left is 11. DeviceNumber is the only place the
// second system exists; a MIDI number appears in no other file in this context.
type Pad int

// The layout of the flat index space.
//
//	 0..63  the 8x8 grid, row-major from the top-left
//	64..71  the round buttons along the top, left to right
//	72..79  the round buttons down the right side, top to bottom
//	    80  the logo LED, which is output-only
const (
	// GridSize is the side of the square grid.
	GridSize = 8
	// PadCount is every addressable light, grid and chrome together.
	PadCount = 81

	firstTop  = GridSize * GridSize // 64
	firstSide = firstTop + GridSize // 72
	// Logo is the LED behind the Novation mark. It has no switch under it, so
	// it is the one pad that can say something without inviting a press.
	Logo Pad = firstSide + GridSize // 80
)

// Grid is the pad at row and col, both zero-based from the top-left. The second
// result is false for coordinates off the grid, which is the normal way a
// widget clips itself rather than a caller's mistake.
func Grid(row, col int) (Pad, bool) {
	if row < 0 || row >= GridSize || col < 0 || col >= GridSize {
		return 0, false
	}
	return Pad(row*GridSize + col), true
}

// Top is one of the eight round buttons along the top edge, numbered 1..8 left
// to right. This row is where a board puts navigation, because it is the only
// run of pads a thumb reaches without covering the grid.
func Top(n int) (Pad, bool) {
	if n < 1 || n > GridSize {
		return 0, false
	}
	return Pad(firstTop + n - 1), true
}

// Side is one of the eight round buttons down the right edge, numbered 1..8 top
// to bottom.
func Side(n int) (Pad, bool) {
	if n < 1 || n > GridSize {
		return 0, false
	}
	return Pad(firstSide + n - 1), true
}

// TopIndex is p's position along the top row, 0..7 from the left, or false
// when p is not one of the round buttons there.
func (p Pad) TopIndex() (int, bool) {
	if p < firstTop || p >= firstSide {
		return 0, false
	}
	return int(p) - firstTop, true
}

// SideIndex is p's position down the side column, 0..7 from the top, or false
// when p is not one of the round buttons there.
func (p Pad) SideIndex() (int, bool) {
	if p < firstSide || p >= Logo {
		return 0, false
	}
	return int(p) - firstSide, true
}

// Valid reports whether p addresses a light that exists.
func (p Pad) Valid() bool { return p >= 0 && p < PadCount }

// IsGrid reports whether p is one of the 64 square pads rather than chrome.
func (p Pad) IsGrid() bool { return p >= 0 && p < firstTop }

// IsNote reports how the device spells this pad: grid pads arrive and are lit
// as Note On, the round buttons as Control Change. An adapter needs this to
// decode a press; nothing else does.
func (p Pad) IsNote() bool { return p.IsGrid() }

// Pressable reports whether a person can press p. Only the logo cannot be,
// which is why a board may light it without implying an action.
func (p Pad) Pressable() bool { return p.Valid() && p != Logo }

// RowCol is p's position in reading order, or false when p is not a grid pad.
func (p Pad) RowCol() (row, col int, ok bool) {
	if !p.IsGrid() {
		return 0, 0, false
	}
	return int(p) / GridSize, int(p) % GridSize, true
}

// DeviceNumber is the number the hardware knows this pad by — the note for a
// grid pad, the controller for a round one. Row 1 is the bottom row on the
// device and row 0 is the top row here, so the conversion flips the axis.
func (p Pad) DeviceNumber() byte {
	switch {
	case p.IsGrid():
		row, col, _ := p.RowCol()
		return byte((GridSize-row)*10 + col + 1)
	case p < firstSide:
		return byte(91 + int(p) - firstTop)
	case p < Logo:
		return byte(89 - (int(p)-firstSide)*10)
	default:
		return 99
	}
}

// FromDevice is DeviceNumber inverted: the pad a press or an echo names. isNote
// distinguishes the two message kinds, because 19 is a valid note and a valid
// controller and they are different pads.
func FromDevice(n byte, isNote bool) (Pad, bool) {
	if isNote {
		row, col := int(n)/10, int(n)%10
		if row < 1 || row > GridSize || col < 1 || col > GridSize {
			return 0, false
		}
		return Pad((GridSize-row)*GridSize + col - 1), true
	}
	switch {
	case n >= 91 && n <= 98:
		return Pad(firstTop + int(n) - 91), true
	case n >= 19 && n <= 89 && n%10 == 9:
		return Pad(firstSide + (89-int(n))/10), true
	case n == 99:
		return Logo, true
	default:
		return 0, false
	}
}

// AllPads calls fn for every addressable light, in index order.
func AllPads(fn func(Pad)) {
	for p := Pad(0); p < PadCount; p++ {
		fn(p)
	}
}
