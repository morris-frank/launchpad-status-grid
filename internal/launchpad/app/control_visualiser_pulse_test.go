package app_test

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// press turns c n modes on.
func press(t *testing.T, c *app.VisualiserControl, n int) {
	t.Helper()
	for range n {
		if _, err := c.Press(context.Background(), app.Slot{}); err != nil {
			t.Fatal(err)
		}
	}
}

// brightness is a pad's strongest channel, 0..127.
func brightness(f domain.Frame, row, col int) float64 {
	pad, _ := domain.Grid(row, col)
	c := f.At(pad).RGB
	return float64(max(c.R, c.G, c.B))
}

// A kick at 128 BPM is locked while the bars are showing, so the pulse names
// its tempo the moment it is turned to, and its ring's radius then follows the
// beat's phase: from the centre on the beat to the corner one beat later.
func TestThePulseRingFollowsTheBeatsPhase(t *testing.T) {
	t.Parallel()
	const bpm = 128.0
	period := 60 / bpm
	rng := rand.New(rand.NewPCG(3, 4))
	src := &fixedLevels{}
	c := app.NewVisualiserControl(src)
	size := full
	cy, cx := float64(size.Rows-1)/2, float64(size.Cols-1)/2
	corner := math.Hypot(cy, cx)
	var errSum, n float64
	at := now
	for elapsed := 0.0; elapsed < 30; {
		dt := app.FrameRate.Seconds() * (0.8 + 0.4*rng.Float64())
		elapsed += dt
		at = at.Add(time.Duration(dt * float64(time.Second)))
		since := math.Mod(elapsed, period)
		kick := math.Exp(-since / 0.08)
		src.l = app.Loudness{Amplitude: kick, Bands: []float64{kick, kick, kick, 0, 0, 0, 0, 0}, Source: "test"}
		if elapsed >= 20 && n == 0 && errSum == 0 {
			press(t, c, 3)
			desc := c.Describe(at, app.Slot{Size: size})
			var got float64
			if !strings.HasPrefix(desc, "pulse") {
				t.Fatalf("three presses say %q, want the pulse", desc)
			}
			if _, err := fmt.Sscanf(desc[strings.Index(desc, "· ")+2:], "%f BPM", &got); err != nil || math.Abs(got-bpm) > 4 {
				t.Fatalf("the pulse says %q on turning to it, want the lock on %v BPM kept", desc, bpm)
			}
		}
		f := c.Render(at, size)
		if elapsed < 20 || since < 0.2*period || since > 0.85*period {
			continue
		}
		best, bestDist := -1.0, 0.0
		for row := range size.Rows {
			for col := range size.Cols {
				if v := brightness(f, row, col); v > best {
					best, bestDist = v, math.Hypot(float64(row)-cy, float64(col)-cx)
				}
			}
		}
		errSum += math.Abs(bestDist - since/period*corner)
		n++
	}
	if n == 0 {
		t.Fatal("no frame was measured")
	}
	if mean := errSum / n; mean > 0.75 {
		t.Errorf("the ring sat %.2f cells from where the phase puts it, on average", mean)
	}
}

// A bass with no beat in it, steady or noise, is held by no tracker, so the
// pulse draws only its dim floor and never a ring it is not sure of.
func TestThePulseDrawsNothingBrightWithoutABeat(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(5, 6))
	for name, bass := range map[string]func() float64{
		"constant": func() float64 { return 0.8 },
		"noise":    rng.Float64,
	} {
		src := &fixedLevels{}
		c := app.NewVisualiserControl(src)
		press(t, c, 3)
		at := now
		for tick := range 15 * 60 {
			at = at.Add(app.FrameRate)
			b := bass()
			src.l = app.Loudness{Amplitude: b, Bands: []float64{b, b, b, 0, 0, 0, 0, 0}, Source: "test"}
			f := c.Render(at, full)
			if tick < 5*60 {
				continue
			}
			for row := range full.Rows {
				for col := range full.Cols {
					if v := brightness(f, row, col); v > 0.3*127 {
						t.Fatalf("%s bass lit pad %d,%d at %.0f of 127 at tick %d (%s)", name, row, col, v, tick, c.Describe(at, app.Slot{Size: full}))
					}
				}
			}
		}
	}
}
