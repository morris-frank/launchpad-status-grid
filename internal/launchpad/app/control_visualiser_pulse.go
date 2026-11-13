package app

import (
	"math"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

const (
	// pulseTail is how far behind its front the ring fades, in cells, and
	// pulseFront how sharply it starts ahead of it.
	pulseTail  = 0.8
	pulseFront = 0.5
	// pulseFloor is the hue's brightness with no ring on it, so a sound with no
	// beat is a dim wash of its mix and never a pulse the tracker does not hold.
	pulseFloor = 0.08
)

// pulseFrame is the tracked beat as one ring per beat, emitted from the centre
// on the beat and reaching the rectangle's corner exactly one beat later: its
// radius is the phase times the distance to the corner. Its front is sharp and
// its tail short; its hue is the rings' bass:mid:treble mix at full strength,
// and the centre pads flash on the beat as the beat mode does. Everything is
// scaled by the tracker's confidence, down to pulseFloor.
func pulseFrame(t *beatTracker, a trio, size domain.Size) domain.Frame {
	bpm, phase, conf := t.estimate()
	flash := beatFlash(bpm, phase, conf)
	strongest := math.Max(0.25, math.Max(a.bass, math.Max(a.mid, a.treble)))
	r, g, b := a.bass/strongest, a.mid/strongest, a.treble/strongest
	cy, cx := float64(size.Rows-1)/2, float64(size.Cols-1)/2
	front := phase * math.Hypot(cy, cx)
	f := domain.Blank()
	for row := range size.Rows {
		for col := range size.Cols {
			dist := math.Hypot(float64(row)-cy, float64(col)-cx)
			behind := front - dist
			ring := 0.0
			if behind >= 0 {
				ring = math.Exp(-behind / pulseTail * 3)
			} else {
				ring = math.Max(0, 1+behind/pulseFront)
			}
			v := pulseFloor + (1-pulseFloor)*ring*conf
			if dist < 1 {
				v = math.Max(v, flash)
			}
			f = f.SetGrid(row, col, rgbCell(r*v, g*v, b*v))
		}
	}
	return f
}
