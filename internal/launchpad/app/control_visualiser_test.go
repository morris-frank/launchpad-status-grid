package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

type fixedLevels struct{ l app.Loudness }

func (f *fixedLevels) Level(time.Time) app.Loudness { return f.l }

// The bars are the source's bands and nothing else: a loud band is a full
// column, a silent one lights no bar, and a new reading is a new picture.
func TestTheVisualiserFollowsItsLevelSource(t *testing.T) {
	t.Parallel()
	src := &fixedLevels{l: app.Loudness{Amplitude: 0.5, Bands: []float64{1, 0, 0.5, 0, 0, 0, 0, 0}}}
	c := app.NewVisualiserControl(src)
	f := c.Render(now, full)

	bright := func(f domain.Frame, col int) int {
		n := 0
		for row := range full.Rows {
			pad, _ := domain.Grid(row, col)
			if f.At(pad).Level == domain.LevelFull {
				n++
			}
		}
		return n
	}
	if got := bright(f, 0); got != full.Rows {
		t.Errorf("a band at 100%% lit %d of %d rows", got, full.Rows)
	}
	if got := bright(f, 1); got != 0 {
		t.Errorf("a silent band lit %d rows of bar", got)
	}
	if got := bright(f, 2); got != full.Rows/2 {
		t.Errorf("a band at 50%% lit %d rows, want %d", got, full.Rows/2)
	}

	src.l.Bands = []float64{0, 1, 0, 0, 0, 0, 0, 0}
	g := c.Render(now, full)
	if bright(g, 0) != 0 || bright(g, 1) != full.Rows {
		t.Error("the picture did not follow a new reading")
	}

	if got := c.Describe(now, app.Slot{Col: 2, Size: full}); !strings.Contains(got, "band 3 of 8") || !strings.Contains(got, "50%") {
		t.Errorf("the third column says %q, want its band and level", got)
	}
}

// Whatever the source says, whatever size the control is given and whichever
// mode it shows, nothing is drawn outside the rectangle — the chrome pads
// included.
func TestEveryVisualiserFrameFitsItsGrid(t *testing.T) {
	t.Parallel()
	for _, size := range []domain.Size{full, {Rows: 3, Cols: 4}, {Rows: 1, Cols: 1}} {
		for _, src := range []app.Levels{
			app.SyntheticLevels{},
			&fixedLevels{l: app.Loudness{Amplitude: 7, Bands: []float64{-3, 9}}},
			&fixedLevels{},
		} {
			c := app.NewVisualiserControl(src)
			for tick := range 1000 {
				if tick > 0 && tick%200 == 0 {
					_, _ = c.Press(t.Context(), app.Slot{})
				}
				f := c.Render(now.Add(time.Duration(tick)*app.FrameRate), size)
				for p := domain.Pad(0); p < domain.PadCount; p++ {
					if f.At(p).Dark() {
						continue
					}
					row, col, ok := p.RowCol()
					if !ok || row >= size.Rows || col >= size.Cols {
						t.Fatalf("size %v tick %d lit pad %d outside the rectangle", size, tick, p)
					}
					if lv := f.At(p).Level; lv < domain.LevelOff || lv > domain.LevelFull {
						t.Fatalf("pad %d has level %d", p, lv)
					}
				}
			}
		}
	}
}

// The synthetic source says it is synthetic, so a moving board never passes for
// one listening to a room.
func TestTheSyntheticSourceSaysWhatItIs(t *testing.T) {
	t.Parallel()
	c := app.NewVisualiserControl(app.SyntheticLevels{})
	if got := c.Describe(now, app.Slot{Size: full}); !strings.Contains(got, "synthetic") {
		t.Errorf("the pad says %q, want it to name the synthetic source", got)
	}
}

// A press turns the visualiser to the rings, the next to the beat, the third to
// the pulse, the fourth to the meter and the fifth back to the bars, and the
// pad says which it is showing.
func TestAPressTurnsTheVisualiserThroughItsModesAndBack(t *testing.T) {
	t.Parallel()
	c := app.NewVisualiserControl(&fixedLevels{l: app.Loudness{Amplitude: 0.5, Bands: make([]float64, 8)}})
	at := app.Slot{Size: full}
	if !strings.HasPrefix(c.Describe(now, at), "bars") {
		t.Fatalf("a fresh visualiser says %q, want the bars", c.Describe(now, at))
	}
	out, _ := c.Press(t.Context(), at)
	if !strings.Contains(out.Note, "rings") || !strings.HasPrefix(c.Describe(now, at), "rings") {
		t.Errorf("one press noted %q and says %q, want the rings", out.Note, c.Describe(now, at))
	}
	_, _ = c.Press(t.Context(), at)
	if !strings.HasPrefix(c.Describe(now, at), "beat") {
		t.Errorf("a second press says %q, want the beat", c.Describe(now, at))
	}
	_, _ = c.Press(t.Context(), at)
	if !strings.HasPrefix(c.Describe(now, at), "pulse") {
		t.Errorf("a third press says %q, want the pulse", c.Describe(now, at))
	}
	_, _ = c.Press(t.Context(), at)
	if !strings.HasPrefix(c.Describe(now, at), "meter") {
		t.Errorf("a fourth press says %q, want the meter", c.Describe(now, at))
	}
	_, _ = c.Press(t.Context(), at)
	if !strings.HasPrefix(c.Describe(now, at), "bars") {
		t.Errorf("a fifth press says %q, want the bars again", c.Describe(now, at))
	}
}

// The rings' hue is the band mix at full strength however quiet the sound, so
// a bass-only reading draws some pad at full red and none with green or blue,
// and the rings flow: two frames apart are two pictures.
func TestTheRingsAreTheBandMixAtFullStrengthAndTheyMove(t *testing.T) {
	t.Parallel()
	bass := make([]float64, 8)
	bass[1], bass[2] = 0.4, 0.4
	c := app.NewVisualiserControl(&fixedLevels{l: app.Loudness{Amplitude: 0.3, Bands: bass}})
	_, _ = c.Press(t.Context(), app.Slot{Size: full})
	var first, second domain.Frame
	for i := range 20 { // let the smoothing settle on the reading
		first = c.Render(now.Add(time.Duration(i)*33*time.Millisecond), full)
	}
	second = c.Render(now.Add(20*33*time.Millisecond+100*time.Millisecond), full)

	brightest := byte(0)
	for row := range full.Rows {
		for col := range full.Cols {
			pad, _ := domain.Grid(row, col)
			lit := first.At(pad).Lit()
			if lit.G != 0 || lit.B != 0 {
				t.Fatalf("a bass-only reading lit (%d,%d) as %+v", row, col, lit)
			}
			brightest = max(brightest, lit.R)
		}
	}
	if brightest < 100 { // a ring crest need not sit exactly on a pad; the raw level would be ~50
		t.Errorf("the brightest red is %d of 127; a quiet bass should still draw at full strength", brightest)
	}
	if first.Equal(second) {
		t.Error("the rings drew the same picture 100ms apart; they should flow")
	}
}
