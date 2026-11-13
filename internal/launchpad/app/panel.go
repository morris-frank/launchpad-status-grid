package app

import (
	"context"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// Control is a widget that occupies a rectangle of the grid.
//
// It draws in its own coordinates — 0,0 is its top-left corner, never the
// board's — and is told how big it is at the moment it draws rather than when
// it is built. That is the whole of the model: a control does not know where it
// sits, so the composition root can move it, resize it, or place two of them
// side by side without the control changing. It is how a widget is placed on a
// phone's home screen, and for the same reason — the thing being placed and the
// decision about where it goes belong to different people.
//
// Anything drawn outside the size given is clipped by the placement, so a
// control clips by drawing and never by arithmetic. Both methods take a
// domain.Size or a Slot rather than a domain.Pad, so a control cannot read a
// board coordinate even by accident.
type Control interface {
	Render(now time.Time, size domain.Size) domain.Frame
	Press(ctx context.Context, at Slot) (Outcome, error)
}

// Slot is where a press landed inside a control, together with how big the
// control was when it landed.
//
// The size travels with the position because the two are only meaningful
// together: a list control turns them into an index, and an index computed
// against the wrong width acts on something other than what was touched. That
// bug class is why this is one value rather than two arguments.
type Slot struct {
	Row, Col int
	Size     domain.Size
}

// Index is the slot's row-major position, which is how a control laying out a
// list addresses its items.
func (s Slot) Index() int { return s.Row*s.Size.Cols + s.Col }

// Placement is one control and the rectangle it was given.
type Placement struct {
	Region  domain.Region
	Control Control
	// top places Region along the round buttons above the grid rather than on
	// the grid itself, as a one-row rectangle whose columns are theirs.
	top bool
	// side places Region down the round buttons right of the grid, as a
	// one-column rectangle whose rows are theirs.
	side bool
}

// At places c in the rectangle r.
func At(r domain.Region, c Control) Placement { return Placement{Region: r, Control: c} }

// AtTop places c on cols of the round buttons along the top, starting at col
// from the left. The control draws a one-row rectangle exactly as it would on
// the grid and never learns the difference; the board still owns whichever of
// those buttons it binds itself, and draws over them.
func AtTop(col, cols int, c Control) Placement {
	return Placement{Region: domain.Region{Col: col, Rows: 1, Cols: cols}, Control: c, top: true}
}

// AtSide places c on rows of the round buttons down the right edge, starting
// at row from the top — the operator's column 9. The control draws a
// one-column rectangle and never learns the difference, as AtTop's does.
func AtSide(row, rows int, c Control) Placement {
	return Placement{Region: domain.Region{Row: row, Rows: rows, Cols: 1}, Control: c, side: true}
}

// Panel is a screen assembled from controls placed by rectangle. It implements
// Screen, so the board neither knows nor cares that a screen has parts.
//
// Placements are drawn in order and pressed in reverse, so the control drawn
// last is the one on top in both directions. Nothing here stops two placements
// overlapping: a board is composed by a person and an overlap is occasionally
// what they meant.
type Panel struct {
	name   string
	hue    domain.Colour
	places []Placement
}

// NewPanel builds a screen from placements. A panel with none draws nothing,
// which is a legitimate placeholder and not an error.
func NewPanel(name string, hue domain.Colour, places ...Placement) *Panel {
	return &Panel{name: name, hue: hue, places: places}
}

// Name implements Screen.
func (p *Panel) Name() string { return p.name }

// Colour implements Screen.
func (p *Panel) Colour() domain.Colour { return p.hue }

// Render implements Screen, drawing each control into its own rectangle.
func (p *Panel) Render(now time.Time) domain.Frame {
	f := domain.Blank()
	for _, pl := range p.places {
		drawn := pl.Control.Render(now, pl.Region.Size())
		switch {
		case pl.side:
			for dr := range pl.Region.Rows {
				src, _ := domain.Grid(dr, 0)
				if pad, ok := domain.Side(pl.Region.Row + dr + 1); ok {
					f = f.Set(pad, drawn.At(src))
				}
			}
			continue
		case !pl.top:
			f = f.Blit(pl.Region, drawn)
			continue
		}
		for dc := range pl.Region.Cols {
			src, _ := domain.Grid(0, dc)
			if pad, ok := domain.Top(pl.Region.Col + dc + 1); ok {
				f = f.Set(pad, drawn.At(src))
			}
		}
	}
	return f
}

// locate is the placement owning pad and the slot it lands on, walked in
// reverse so the control drawn last is the one on top. Press and Describe both
// ask it, so what a pad says it is and what it does cannot disagree.
func (p *Panel) locate(pad domain.Pad) (Placement, Slot, bool) {
	row, col, ok := pad.RowCol()
	top, side := false, false
	if !ok {
		if col, ok = pad.TopIndex(); ok {
			row, top = 0, true
		} else if row, ok = pad.SideIndex(); ok {
			col, side = 0, true
		} else {
			return Placement{}, Slot{}, false
		}
	}
	for i := len(p.places) - 1; i >= 0; i-- {
		pl := p.places[i]
		if pl.top != top || pl.side != side || !pl.Region.Contains(row, col) {
			continue
		}
		return pl, Slot{Row: row - pl.Region.Row, Col: col - pl.Region.Col, Size: pl.Region.Size()}, true
	}
	return Placement{}, Slot{}, false
}

// OwnsChrome implements ChromeOwner: the round buttons a placement covers are
// this panel's to answer.
func (p *Panel) OwnsChrome(pad domain.Pad) bool {
	if pad.IsGrid() {
		return false
	}
	_, _, ok := p.locate(pad)
	return ok
}

// Press implements Screen, routing to whichever control owns the pad.
//
// A press on a pad no control covers is not an error and not silence either:
// it returns an Outcome asking for attention, so an empty part of a board says
// "nothing here" rather than looking like a board that stopped listening.
func (p *Panel) Press(ctx context.Context, pad domain.Pad) (Outcome, error) {
	pl, at, ok := p.locate(pad)
	if !ok {
		return Outcome{Attention: pad.IsGrid()}, nil
	}
	return pl.Control.Press(ctx, at)
}

// Refresh implements Refresher for every control that has a source to re-read,
// and is a no-op for a panel of controls that do not.
func (p *Panel) Refresh(ctx context.Context) error {
	var first error
	for _, pl := range p.places {
		r, ok := pl.Control.(Refresher)
		if !ok {
			continue
		}
		// Every control is refreshed even after one fails, because they are
		// independent sources and a panel that stopped at the first failure
		// would blank the controls that were working.
		if err := r.Refresh(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Describer is a control that can say in words what one of its slots is
// showing. It is optional: a control that does not implement it simply has
// nothing to say, and a surface with room for words says nothing there.
//
// It takes the moment for the same reason Render does — "wrote four minutes
// ago" is the useful sentence and it is only true at an instant — and it must
// not act, only describe. Press is where a pad does something; this is the
// question you ask before you are willing to press it.
type Describer interface {
	Describe(now time.Time, at Slot) string
}

// Describe implements Inspector by asking whichever control owns the pad.
//
// The lookup is the same reverse walk Press does, so what a pad says it is and
// what it does when pressed can never disagree — the alternative, two
// traversals that could drift, is how a help text ends up describing the pad
// next door.
func (p *Panel) Describe(now time.Time, pad domain.Pad) string {
	pl, at, ok := p.locate(pad)
	switch {
	case !ok && pad.IsGrid():
		return "nothing placed here"
	case !ok:
		return ""
	}
	d, ok := pl.Control.(Describer)
	if !ok {
		return ""
	}
	return d.Describe(now, at)
}
