package app

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// Loudness is one reading of a sound: how loud it is overall and how that
// energy splits across frequency bands, lowest band first. Every value is 0..1.
//
// Source names what produced the reading, because a picture that moves looks
// the same whether a microphone or a sine wave moved it, and the board must be
// able to say which.
type Loudness struct {
	Amplitude float64
	Bands     []float64
	Source    string
}

// Levels is the visualiser's port: a stream of loudness, sampled at the moment
// a frame is drawn. It is read inside Render, so an implementation must answer
// from memory — an audio adapter keeps its latest reading and hands it over,
// and never blocks on the device for a frame.
type Levels interface {
	Level(now time.Time) Loudness
}

// SyntheticLevels is loudness computed from the clock alone: two detuned sines
// per band, the overall amplitude their mean. It is what the board runs
// on until an audio input is wired, and it says so in every reading.
type SyntheticLevels struct{}

// Level implements Levels.
func (SyntheticLevels) Level(now time.Time) Loudness {
	t := float64(now.UnixNano()) / float64(time.Second)
	bands := make([]float64, domain.GridSize)
	sum := 0.0
	for i := range bands {
		f := float64(i + 1)
		v := 0.5 + 0.3*math.Sin(t*(1.3+0.4*f)+f) + 0.2*math.Sin(t*0.7*f+2*f)
		// Low bands carry more energy, as they do in most music.
		v *= 1 - 0.06*f
		bands[i] = clamp(v)
		sum += bands[i]
	}
	return Loudness{Amplitude: sum / float64(len(bands)), Bands: bands, Source: "synthetic"}
}

// visualiserHues is the ring the picture cycles through: the semantic palette
// without red, which is reserved for attention and never decorative, and
// without the greys, which read as "unmeasured" everywhere else on the board.
// The meter's top row is the one exception (operator decision 2026-09-30): a
// level meter's red is the top of its scale, so a bar reaching it is red, as
// on every desk it imitates, and it is a reading rather than decoration.
var visualiserHues = []domain.Colour{
	domain.Blue, domain.Cyan, domain.Mint, domain.Green, domain.Yellow, domain.Amber, domain.Magenta,
}

// VisualiserControl is a milkdrop-style picture of a sound: one bar per column
// for its band, over a drifting plasma whose brightness follows the overall
// amplitude. It is decoration with a reading behind it, so it acts on nothing.
//
// A press turns to the next mode: bars, then rings, then the tracked beat, then
// its pulse, then the meter. The tracker is fed every frame whatever the mode, so turning from
// the beat to the pulse keeps its lock.
type VisualiserControl struct {
	levels     Levels
	mode       visualiserMode
	last       time.Time
	smoothed   trio
	procedural proceduralField
	tracker    beatTracker
	meter      meter
}

// NewVisualiserControl draws what levels reports.
func NewVisualiserControl(levels Levels) *VisualiserControl {
	return &VisualiserControl{levels: levels}
}

// Render implements Control.
func (v *VisualiserControl) Render(now time.Time, size domain.Size) domain.Frame {
	l := v.levels.Level(now)
	dt := 0.0
	if !v.last.IsZero() {
		dt = now.Sub(v.last).Seconds()
	}
	v.last = now
	v.smoothed = v.smoothed.smooth(trioOf(l))
	v.tracker.update(dt, bassOf(l))
	switch v.mode {
	case modeProcedural:
		return v.procedural.render(dt, v.smoothed, size)
	case modeBeat:
		return beatFrame(&v.tracker, size)
	case modePulse:
		return pulseFrame(&v.tracker, v.smoothed, size)
	case modeMeter:
		return v.meter.frame(&v.tracker, l, dt, size)
	}
	t := secondsOf(now)
	f := domain.Blank()
	for col := 0; col < size.Cols; col++ {
		height := band(l, col, size.Cols) * float64(size.Rows)
		for row := 0; row < size.Rows; row++ {
			i := float64(size.Rows - 1 - row) // how far up the bar this cell sits
			var cell domain.Cell
			switch {
			case i < math.Floor(height):
				cell = hue(t*2 + float64(col)).Cell(domain.LevelFull)
			case i < height:
				cell = hue(t*2 + float64(col)).Cell(domain.ScaleLevel(height - i))
			default:
				p := math.Sin(float64(col)*0.9+t) + math.Sin(float64(row)*0.7-t*1.3) + math.Sin(float64(col+row)*0.5+t*0.6)
				level := domain.LevelFaint
				if clamp(l.Amplitude) > 0.5 && p > 0 {
					level = domain.LevelDim
				}
				cell = hue(p*2 + t).Cell(level)
			}
			f = f.SetGrid(row, col, cell)
		}
	}
	return f
}

// Press implements Control: it turns to the next mode, and acts on nothing.
func (v *VisualiserControl) Press(context.Context, Slot) (Outcome, error) {
	v.mode = (v.mode + 1) % modeCount
	return Outcome{Note: "visualiser: " + v.mode.String()}, nil
}

// Describe implements Describer.
func (v *VisualiserControl) Describe(now time.Time, at Slot) string {
	l := v.levels.Level(now)
	if v.mode == modeBeat || v.mode == modePulse {
		bpm, _, conf := v.tracker.estimate()
		return fmt.Sprintf("%s · %.0f BPM · %s sure · %s source · press for the next mode", v.mode, bpm, percent(int(conf*100)), l.Source)
	}
	if v.mode == modeMeter {
		bpm, _, conf := v.tracker.estimate()
		return fmt.Sprintf("%s · band %d of %d · %s · peak %s · %.0f BPM · %s sure · %s source · press for the next mode",
			v.mode, at.Col+1, at.Size.Cols, percent(int(band(l, at.Col, at.Size.Cols)*100)), percent(int(v.meter.peak(at.Col, at.Size.Rows)*100)),
			bpm, percent(int(conf*100)), l.Source)
	}
	return fmt.Sprintf("%s · band %d of %d · %s · overall %s · %s source · press for the next mode",
		v.mode, at.Col+1, at.Size.Cols, percent(int(band(l, at.Col, at.Size.Cols)*100)), percent(int(clamp(l.Amplitude)*100)), l.Source)
}

// band is the reading column col of cols shows: the bands resampled to the
// width, or the overall amplitude when the source reports no bands.
func band(l Loudness, col, cols int) float64 {
	if len(l.Bands) == 0 || cols <= 0 {
		return clamp(l.Amplitude)
	}
	return clamp(l.Bands[col*len(l.Bands)/cols])
}

func hue(x float64) domain.Colour {
	n := len(visualiserHues)
	return visualiserHues[((int(math.Floor(x))%n)+n)%n]
}

func clamp(x float64) float64 { return math.Max(0, math.Min(1, x)) }
