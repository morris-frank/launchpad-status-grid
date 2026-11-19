package app

import (
	"math"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

const (
	// meterSure is the tracker confidence from which the meter believes the
	// beat: above it the caps fall on the beat and the bars are accented; below
	// it (a podcast, a call, a song change) it is a plain meter.
	meterSure = 0.25
	// meterFall is how long a cap holds a row when no beat is believed: slow
	// enough to read the peak, and unlike any tempo the tracker locks onto.
	meterFall = 0.75
)

// meter is a DAW level meter of the bands, one column per band, with a
// peak-hold cap per column that the tracked beat drops.
type meter struct {
	caps  []float64 // each column's held peak, in cells
	phase float64   // the last frame's beat phase, to see the beat wrap
	held  float64   // seconds the caps have held since they last fell, untracked
}

// meterColour is the colour of a meter cell on row of rows, top row first: the
// same in every column and every frame, green at the bottom through yellow
// and amber to red on the top row.
func meterColour(row, rows int) domain.Colour {
	if row == 0 {
		return domain.Red
	}
	up := float64(rows-1-row) / float64(max(1, rows-1))
	switch {
	case up < 0.45:
		return domain.Green
	case up < 0.65:
		return domain.Yellow
	default:
		return domain.Amber
	}
}

// frame draws the meter of l. A lit cell is bright, and full on the beat while
// the accent lasts; a cap is one dim cell; everything else is dark.
func (m *meter) frame(t *beatTracker, l Loudness, dt float64, size domain.Size) domain.Frame {
	if len(m.caps) != size.Cols {
		m.caps = make([]float64, size.Cols)
	}
	bpm, phase, conf := t.estimate()
	sure := conf >= meterSure
	beat := sure && m.phase > 0.75 && phase < 0.25
	m.phase = phase
	fall := beat
	if !sure {
		m.held += dt
		if m.held >= meterFall {
			m.held, fall = 0, true
		}
	} else {
		m.held = 0
	}
	accent := domain.LevelOff
	if sure && beatFlash(bpm, phase, conf) >= 0.4 {
		accent = 1
	}

	f := domain.Blank()
	for col := range size.Cols {
		height := band(l, col, size.Cols) * float64(size.Rows)
		if fall {
			m.caps[col]--
		}
		m.caps[col] = math.Max(m.caps[col], height)
		capCell := int(math.Ceil(m.caps[col])) - 1
		for row := range size.Rows {
			i := float64(size.Rows - 1 - row) // how far up the bar this cell sits
			c := meterColour(row, size.Rows)
			switch {
			case i < height:
				lv := min(domain.ScaleLevel(height-i), domain.LevelBright) + accent
				f = f.SetGrid(row, col, c.Cell(min(lv, domain.LevelFull)))
			case int(i) == capCell:
				f = f.SetGrid(row, col, c.Cell(domain.LevelDim))
			}
		}
	}
	return f
}

// peak is column col's held peak as a fraction of rows.
func (m *meter) peak(col, rows int) float64 {
	if col >= len(m.caps) || rows <= 0 {
		return 0
	}
	return clamp(m.caps[col] / float64(rows))
}
