package app

import (
	"math"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// A visualiser mode is one way of turning a reading into a picture. A press on
// the visualiser turns to the next one, so they can be compared on the board
// against the same sound.
//
// A milkdrop-style feedback field was tried and cut (2026-09-23): its
// rotate-zoom-blur needs resolution an 8×8 grid does not have, so it read as a
// wash, and its picture depended on every earlier frame rather than on the
// reading. The rings are a function of the reading and the phase alone.
type visualiserMode int

const (
	modeBars visualiserMode = iota
	modeProcedural
	modeBeat
	modePulse
	modeMeter
	modeCount
)

func (m visualiserMode) String() string {
	return [...]string{"bars", "rings", "beat", "pulse", "meter"}[m]
}

// trio is a reading folded to bass, mid and treble. The bands are the
// sources' eight log-spaced bands over 40Hz–16kHz, whose edges fall at
// 84, 178, 377, 800, 1697, 3600 and 7640Hz: bass is 84–377, mid 377–1697 and
// treble 1697–7640 — the nearest edges to 80–250, 250–2000 and 2000–8000.
type trio struct{ bass, mid, treble float64 }

func trioOf(l Loudness) trio {
	if len(l.Bands) < 8 {
		a := clamp(l.Amplitude)
		return trio{a, a, a}
	}
	avg := func(i, j int) float64 { return clamp((l.Bands[i] + l.Bands[j]) / 2) }
	return trio{avg(1, 2), avg(3, 4), avg(5, 6)}
}

// smooth moves toward x quickly on the way up and slowly on the way down, so a
// beat lands at once and fades rather than flickering off.
func (t trio) smooth(x trio) trio {
	s := func(old, new float64) float64 {
		k := 0.15
		if new > old {
			k = 0.6
		}
		return old + (new-old)*k
	}
	return trio{s(t.bass, x.bass), s(t.mid, x.mid), s(t.treble, x.treble)}
}

func rgbCell(r, g, b float64) domain.Cell {
	c := func(v float64) byte { return byte(clamp(v)*127 + 0.5) }
	return domain.Cell{RGB: domain.RGB{R: c(r), G: c(g), B: c(b)}, Level: domain.LevelFull, Behaviour: domain.BSolid}
}

// proceduralField is rings from the centre, drawn fresh every frame: they flow
// outward, faster on the bass, and the bass tightens them. The hue is the
// bass:mid:treble mix scaled so its strongest channel is full, and the ring is
// the brightness — raw band levels as channels read faint whenever the music
// is quiet in two of the three. The sketch's 0.15 rad/s was one ring every
// forty seconds; the drift is 3 rad/s and the bass adds up to eight more.
type proceduralField struct{ phase float64 }

func (pf *proceduralField) render(dt float64, a trio, size domain.Size) domain.Frame {
	pf.phase += dt * (3 + a.bass*8)
	strongest := math.Max(0.25, math.Max(a.bass, math.Max(a.mid, a.treble)))
	r, g, b := a.bass/strongest, a.mid/strongest, a.treble/strongest
	cy, cx := float64(size.Rows-1)/2, float64(size.Cols-1)/2
	f := domain.Blank()
	for row := range size.Rows {
		for col := range size.Cols {
			dist := math.Hypot(float64(row)-cy, float64(col)-cx)
			ring := 0.5 + 0.5*math.Sin(dist*(2+a.bass*3)-pf.phase)
			ring *= ring // sharper rings, darker troughs
			f = f.SetGrid(row, col, rgbCell(r*ring, g*ring, b*ring))
		}
	}
	return f
}

func secondsOf(now time.Time) float64 { return float64(now.UnixNano()) / float64(time.Second) }
