package domain

import "math"

// Region is a rectangle in grid coordinates, in reading order from the
// top-left. A region may extend past the grid; every widget draws through
// Frame.SetGrid, which discards what falls outside, so a caller never does
// boundary arithmetic and a widget never has to be told how big the board is.
type Region struct {
	Row, Col   int
	Rows, Cols int
}

// FullGrid is the whole 8x8 square, excluding the round chrome buttons.
func FullGrid() Region { return Region{Rows: GridSize, Cols: GridSize} }

// Rows is a horizontal band of the grid, n rows tall starting at row.
func Rows(row, n int) Region { return Region{Row: row, Rows: n, Cols: GridSize} }

// Empty reports whether the region encloses nothing.
func (r Region) Empty() bool { return r.Rows <= 0 || r.Cols <= 0 }

// Contains reports whether the grid position row, col falls inside r.
func (r Region) Contains(row, col int) bool {
	return row >= r.Row && row < r.Row+r.Rows && col >= r.Col && col < r.Col+r.Cols
}

// Size is a rectangle's extent with no position: how big something is, asked
// separately from where it sits. A control is drawn against one of these
// because it is placed by whoever composes the board and may be moved or
// resized there without being rebuilt.
type Size struct{ Rows, Cols int }

// Size is r's extent.
func (r Region) Size() Size { return Size{Rows: r.Rows, Cols: r.Cols} }

// Region is s placed at the grid origin.
func (s Size) Region() Region { return Region{Rows: s.Rows, Cols: s.Cols} }

// Count is how many cells s encloses.
func (s Size) Count() int {
	if s.Rows <= 0 || s.Cols <= 0 {
		return 0
	}
	return s.Rows * s.Cols
}

// Each calls fn for every cell position in the region, in reading order. The
// two indices are relative to the region's own origin, which is what a widget
// wants — a bar filling left to right cares about its own column 0, not the
// board's.
func (r Region) Each(fn func(row, col int)) {
	for dr := 0; dr < r.Rows; dr++ {
		for dc := 0; dc < r.Cols; dc++ {
			fn(dr, dc)
		}
	}
}

// Fill paints every cell of r with c.
func (f Frame) Fill(r Region, c Cell) Frame {
	r.Each(func(dr, dc int) { f = f.SetGrid(r.Row+dr, r.Col+dc, c) })
	return f
}

// Outline paints only r's border, leaving the inside untouched — a frame around
// something rather than a block.
func (f Frame) Outline(r Region, c Cell) Frame {
	r.Each(func(dr, dc int) {
		if dr == 0 || dc == 0 || dr == r.Rows-1 || dc == r.Cols-1 {
			f = f.SetGrid(r.Row+dr, r.Col+dc, c)
		}
	})
	return f
}

// Axis is which way a bar grows.
type Axis int

// The four fill directions. Down and Left exist because a quantity that is
// draining reads better emptying toward its origin than filling away from it.
const (
	Right Axis = iota
	Left
	Up
	Down
)

// Bar paints a proportional fill of r along axis, in colour col, over a track
// of the same colour at faint. The track matters: a bar with no track is
// indistinguishable from a shorter bar, so "how much" and "out of how much" have
// to be visible at the same time.
//
// The last partial cell is drawn at a reduced brightness rather than rounded
// away, which is what makes an 8-cell bar read as more than 8 values — the
// quantity moves visibly long before it crosses a whole cell.
func (f Frame) Bar(r Region, fraction float64, col Colour) Frame {
	return f.barAlong(r, clamp01(fraction), col, Right)
}

// BarAlong is Bar with an explicit direction.
func (f Frame) BarAlong(r Region, fraction float64, col Colour, axis Axis) Frame {
	return f.barAlong(r, clamp01(fraction), col, axis)
}

func (f Frame) barAlong(r Region, fraction float64, col Colour, axis Axis) Frame {
	if r.Empty() {
		return f
	}
	span := r.Cols
	if axis == Up || axis == Down {
		span = r.Rows
	}

	exact := fraction * float64(span)
	whole := int(exact)
	partial := exact - float64(whole)

	track := col.Cell(LevelFaint)
	full := col.Cell(LevelFull)

	r.Each(func(dr, dc int) {
		// How far along the axis this cell sits, counted from the end the bar
		// grows out of.
		var i int
		switch axis {
		case Right:
			i = dc
		case Left:
			i = r.Cols - 1 - dc
		case Down:
			i = dr
		case Up:
			i = r.Rows - 1 - dr
		}

		cell := track
		switch {
		case i < whole:
			cell = full
		case i == whole && partial > 0:
			cell = col.Cell(ScaleLevel(partial))
		}
		f = f.SetGrid(r.Row+dr, r.Col+dc, cell)
	})
	return f
}

// Meter is Bar with the colour chosen by how much is left rather than by the
// caller — the shape for anything draining, where the number itself is the
// warning. A context window filling up, a quota emptying, a disk.
func (f Frame) Meter(r Region, remaining float64, axis Axis) Frame {
	return f.barAlong(r, remaining, Ramp(remaining), axis)
}

// Columns paints one vertical bar per column of r, each scaled to its own value
// in 0..1. This is the spectrum shape: eight live quantities side by side, read
// as a silhouette rather than eight numbers.
//
// Values shorter than the region are drawn as far as they go and the rest is
// left dark, so a caller streaming a growing series does not have to pad it.
func (f Frame) Columns(r Region, values []float64, colour func(i int, v float64) Colour) Frame {
	if r.Empty() {
		return f
	}
	if colour == nil {
		colour = func(int, float64) Colour { return Green }
	}
	for dc := 0; dc < r.Cols && dc < len(values); dc++ {
		v := clamp01(values[dc])
		f = f.barAlong(Region{Row: r.Row, Col: r.Col + dc, Rows: r.Rows, Cols: 1}, v, colour(dc, v), Up)
	}
	return f
}

// Spinner paints one lit cell travelling clockwise around r's border, plus a
// short fading tail behind it. phase advances by one per tick and wraps on its
// own, so a caller keeps an int and never a position.
//
// A spinner is the answer for "something is happening and I cannot say how far
// along it is". When progress IS known, a Bar says more and costs the same.
func (f Frame) Spinner(r Region, phase int, col Colour, tail int) Frame {
	ring := borderPath(r)
	if len(ring) == 0 {
		return f
	}
	if tail < 0 {
		tail = 0
	}
	head := ((phase % len(ring)) + len(ring)) % len(ring)

	levels := []Level{LevelFull, LevelBright, LevelMid, LevelDim, LevelFaint}
	for t := 0; t <= tail && t < len(ring); t++ {
		l := LevelFaint
		if t < len(levels) {
			l = levels[t]
		}
		at := ((head-t)%len(ring) + len(ring)) % len(ring)
		f = f.SetGrid(ring[at][0], ring[at][1], col.Cell(l))
	}
	return f
}

// borderPath is r's border as a clockwise ring of absolute grid coordinates,
// starting at the top-left. A one-cell-thick region degenerates to a line, and a
// single cell to a point, both of which spin correctly by standing still.
func borderPath(r Region) [][2]int {
	if r.Empty() {
		return nil
	}
	if r.Rows == 1 || r.Cols == 1 {
		var path [][2]int
		r.Each(func(dr, dc int) { path = append(path, [2]int{r.Row + dr, r.Col + dc}) })
		return path
	}
	var path [][2]int
	for dc := 0; dc < r.Cols; dc++ { // top edge, left to right
		path = append(path, [2]int{r.Row, r.Col + dc})
	}
	for dr := 1; dr < r.Rows; dr++ { // right edge, down
		path = append(path, [2]int{r.Row + dr, r.Col + r.Cols - 1})
	}
	for dc := r.Cols - 2; dc >= 0; dc-- { // bottom edge, right to left
		path = append(path, [2]int{r.Row + r.Rows - 1, r.Col + dc})
	}
	for dr := r.Rows - 2; dr >= 1; dr-- { // left edge, up
		path = append(path, [2]int{r.Row + dr, r.Col})
	}
	return path
}

// Blink turns everything already lit in r into a flashing cell, alternating
// with alt. Applied as a modifier rather than a paint, so a widget's colours
// survive being made urgent — the pad that starts flashing is recognisably the
// same pad.
//
// Nothing here is host-animated: the device flashes it from its own clock, so a
// board left flashing sends zero bytes per second.
func (f Frame) Blink(r Region, alt byte) Frame {
	r.Each(func(dr, dc int) {
		if p, ok := Grid(r.Row+dr, r.Col+dc); ok {
			if c := f.At(p); !c.Dark() {
				f = f.Set(p, c.With(BFlash(alt)))
			}
		}
	})
	return f
}

// Breathe is Blink's quieter sibling: everything lit in r pulses. Also
// device-rendered and also free to leave running. Use it for "this is live" and
// Blink for "this wants you".
func (f Frame) Breathe(r Region) Frame {
	r.Each(func(dr, dc int) {
		if p, ok := Grid(r.Row+dr, r.Col+dc); ok {
			if c := f.At(p); !c.Dark() {
				f = f.Set(p, c.With(BPulse))
			}
		}
	})
	return f
}

// Ripple paints an expanding ring centred on a grid pad, for phase in 0..1.
// Returns a frame to composite over the board rather than a modified board,
// because an effect that has finished must leave nothing behind — at phase 1 it
// is blank, and compositing it is a no-op.
//
// This is the press acknowledgement and, from the centre, the connect splash.
func Ripple(centre Pad, phase float64, col Colour) Frame {
	f := Blank()
	row, col0, ok := centre.RowCol()
	if !ok || phase < 0 || phase >= 1 {
		return f
	}
	// The furthest a ring has to travel to leave an 8x8 grid from any pad in it.
	const maxRadius = 10.0
	radius := phase * maxRadius
	// The ring thins and fades as it expands, so the effect reads as spending
	// itself rather than as being switched off.
	width := 1.4 * (1 - phase)
	fade := 1 - phase

	FullGrid().Each(func(r, c int) {
		d := math.Hypot(float64(r-row), float64(c-col0))
		if math.Abs(d-radius) > width {
			return
		}
		near := 1 - math.Abs(d-radius)/math.Max(width, 0.001)
		f = f.SetGrid(r, c, col.Cell(ScaleLevel(near*fade)))
	})
	return f
}

// Wipe paints a moving band that crosses the whole grid, for phase in 0..1. Two
// of them run back to back on a screen change, which is what makes a new screen
// announce its identity before you have read anything on it.
//
// Like Ripple it is an overlay and is blank once spent.
func Wipe(axis Axis, phase float64, col Colour) Frame {
	f := Blank()
	if phase < 0 || phase >= 1 {
		return f
	}
	const band = 2.5
	// Travel far enough that the band has fully left the grid at phase 1.
	lead := phase*(GridSize+band) - band

	FullGrid().Each(func(r, c int) {
		var pos float64
		switch axis {
		case Right:
			pos = float64(c)
		case Left:
			pos = float64(GridSize - 1 - c)
		case Down:
			pos = float64(r)
		case Up:
			pos = float64(GridSize - 1 - r)
		}
		d := pos - lead
		if d < 0 || d > band {
			return
		}
		f = f.SetGrid(r, c, col.Cell(ScaleLevel(1-d/band)))
	})
	return f
}

func clamp01(v float64) float64 {
	switch {
	case v < 0 || math.IsNaN(v):
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
