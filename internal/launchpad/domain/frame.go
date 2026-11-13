package domain

// Frame is the whole surface as one picture: 81 cells, produced by a function
// and never mutated in place.
//
// It is a value type, and every method that changes it returns a new one. That
// is 81 cells of copying per widget, which at this size is a few hundred bytes
// and is worth every one of them: nothing in this system ever has to save the
// board, draw over it, and restore it afterwards, which is where the whole
// class of "the animation ate my board" bugs lives. A frame is recomputed from
// state each tick, and the only stateful thing in the pipeline is the diff an
// adapter keeps so it can send the changed lights and no others.
type Frame struct {
	cells [PadCount]Cell
}

// Blank is a frame with every light off. The zero Frame is already this; the
// constructor exists so calling code reads as a drawing.
func Blank() Frame { return Frame{} }

// At is the cell at p, or Off when p addresses nothing.
func (f Frame) At(p Pad) Cell {
	if !p.Valid() {
		return Off
	}
	return f.cells[p]
}

// Set returns a frame with p set to c. A pad off the surface is ignored rather
// than an error: widgets clip by drawing outside the grid and letting the frame
// discard it, which keeps every widget free of boundary arithmetic.
func (f Frame) Set(p Pad, c Cell) Frame {
	if p.Valid() {
		f.cells[p] = c
	}
	return f
}

// SetGrid is Set addressed in reading order.
func (f Frame) SetGrid(row, col int, c Cell) Frame {
	if p, ok := Grid(row, col); ok {
		return f.Set(p, c)
	}
	return f
}

// Map returns a frame with fn applied to every cell. The pad is passed so a
// transform can depend on position — a fade toward one edge, a mask.
func (f Frame) Map(fn func(Pad, Cell) Cell) Frame {
	for p := Pad(0); p < PadCount; p++ {
		f.cells[p] = fn(p, f.cells[p])
	}
	return f
}

// Over composites top onto f, taking top's cell wherever it is not dark. This
// is how a board layers: a screen draws its content, then chrome draws the tab
// row over it, then an effect draws over that.
//
// Compositing is per-cell and not per-field, deliberately. Per-field merging —
// where one layer sets hue and another sets behaviour — is the right model for
// a board whose pads are bindings, because "running" and "is a shell command"
// are independent facts about one pad. It is the wrong model for a board whose
// pads are a picture, because two widgets that each set half a cell produce a
// colour neither of them chose. Frames here are pictures; use Merge when a
// layer genuinely means to modify rather than replace.
func (f Frame) Over(top Frame) Frame {
	for p := Pad(0); p < PadCount; p++ {
		if !top.cells[p].Dark() {
			f.cells[p] = top.cells[p]
		}
	}
	return f
}

// Merge composites with a caller-supplied rule, for the layer that means to
// modify what is underneath rather than replace it — a press highlight that
// raises brightness without touching hue, a job status that sets a behaviour
// and leaves the category colour alone.
func (f Frame) Merge(top Frame, rule func(under, over Cell) Cell) Frame {
	for p := Pad(0); p < PadCount; p++ {
		f.cells[p] = rule(f.cells[p], top.cells[p])
	}
	return f
}

// Dim scales every lit cell's brightness down by steps, floored at faint rather
// than off. Used for everything that is still there but not what you are being
// asked to look at: the board behind a splash, the pads that do not match a
// search.
func (f Frame) Dim(steps int) Frame {
	return f.Map(func(_ Pad, c Cell) Cell {
		if c.Level == LevelOff {
			return c
		}
		l := c.Level - Level(steps)
		if l < LevelFaint {
			l = LevelFaint
		}
		return c.At(l)
	})
}

// Blit copies src into the rectangle r, mapping src's grid origin to r's
// top-left and discarding whatever falls outside.
//
// This is the one operation that lets a widget be written without knowing
// where it will end up: it draws at 0,0 in its own coordinates and is placed
// afterwards. Only grid cells travel — the round chrome buttons belong to the
// board and are not inside anybody's rectangle.
//
// It overwrites rather than compositing, because a rectangle is owned: a
// control that went dark would otherwise leave the previous occupant of that
// cell showing through, which is the "the animation ate my board" bug moved
// one level up. Overlapping placements are therefore last-wins, per cell.
func (f Frame) Blit(r Region, src Frame) Frame {
	r.Each(func(dr, dc int) {
		if p, ok := Grid(dr, dc); ok {
			f = f.SetGrid(r.Row+dr, r.Col+dc, src.At(p))
		}
	})
	return f
}

// Equal reports whether two frames light the surface identically. Frames are
// compared, not sent: an adapter that diffs before writing uses this to decide
// whether it has anything to say at all.
func (f Frame) Equal(other Frame) bool { return f.cells == other.cells }
