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

// A kick at 128 BPM, sampled at the frame rate with a frame's jitter and with
// stray offbeat hits, is tracked to its tempo, and the flash lands on it.
func TestTheBeatModeLocksOntoASteadyKick(t *testing.T) {
	t.Parallel()
	const bpm = 128.0
	period := 60 / bpm
	rng := rand.New(rand.NewPCG(1, 2))
	src := &fixedLevels{}
	c := app.NewVisualiserControl(src)
	for range 2 {
		if _, err := c.Press(context.Background(), app.Slot{}); err != nil {
			t.Fatal(err)
		}
	}
	size := domain.Size{Rows: 4, Cols: 4}
	var onBeat, offBeat, nOn, nOff float64
	at := now
	for elapsed := 0.0; elapsed < 30; {
		dt := app.FrameRate.Seconds() * (0.8 + 0.4*rng.Float64())
		elapsed += dt
		at = at.Add(time.Duration(dt * float64(time.Second)))
		since := math.Mod(elapsed, period)
		kick := math.Exp(-since / 0.08)
		if rng.Float64() < 0.03 {
			kick = 1 // a stray hit, anywhere
		}
		src.l = app.Loudness{Amplitude: kick, Bands: []float64{kick, kick, 0, 0, 0, 0, 0, 0}, Source: "test"}
		f := c.Render(at, size)
		if elapsed < 20 {
			continue
		}
		pad, _ := domain.Grid(0, 0)
		v := float64(f.At(pad).RGB.G)
		if since < 0.06 || since > period-0.01 {
			onBeat, nOn = onBeat+v, nOn+1
		} else if math.Abs(since-period/2) < 0.1 {
			offBeat, nOff = offBeat+v, nOff+1
		}
	}
	desc := c.Describe(at, app.Slot{Size: size})
	var got float64
	if _, err := fmt.Sscanf(desc[strings.Index(desc, "· ")+2:], "%f BPM", &got); err != nil {
		t.Fatalf("describe %q: %v", desc, err)
	}
	if math.Abs(got-bpm) > 4 {
		t.Errorf("tracked %v BPM, want %v (%s)", got, bpm, desc)
	}
	if onBeat/nOn < 4*offBeat/nOff || onBeat/nOn < 30 {
		t.Errorf("flash on the beat %.1f, between beats %.1f", onBeat/nOn, offBeat/nOff)
	}
}
