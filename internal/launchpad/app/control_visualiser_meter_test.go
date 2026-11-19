package app_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

func meterOf(t *testing.T, src app.Levels) *app.VisualiserControl {
	t.Helper()
	c := app.NewVisualiserControl(src)
	for range 4 {
		_, _ = c.Press(t.Context(), app.Slot{})
	}
	if got := c.Describe(now, app.Slot{Size: full}); !strings.HasPrefix(got, "meter") {
		t.Fatalf("four presses say %q, want the meter", got)
	}
	return c
}

// A cell's colour is its row's, whatever the column, the reading or the time:
// a full band lights the whole column, green at the bottom and red on top.
func TestTheMetersColourIsItsRowsAlone(t *testing.T) {
	t.Parallel()
	ones := []float64{1, 1, 1, 1, 1, 1, 1, 1}
	c := meterOf(t, &fixedLevels{l: app.Loudness{Amplitude: 1, Bands: ones}})
	f := c.Render(now, full)
	rowColour := make([]byte, full.Rows)
	for row := range full.Rows {
		for col := range full.Cols {
			pad, _ := domain.Grid(row, col)
			cell := f.At(pad)
			if cell.Dark() {
				t.Fatalf("a full band left (%d,%d) dark", row, col)
			}
			if col == 0 {
				rowColour[row] = cell.Palette
			} else if cell.Palette != rowColour[row] {
				t.Errorf("row %d is palette %d in column 0 and %d in column %d", row, rowColour[row], cell.Palette, col)
			}
		}
	}
	if rowColour[0] != domain.Red.Palette {
		t.Errorf("a full band's top row is palette %d, want red", rowColour[0])
	}
	if rowColour[full.Rows-1] != domain.Green.Palette {
		t.Errorf("the bottom row is palette %d, want green", rowColour[full.Rows-1])
	}

	c = meterOf(t, app.SyntheticLevels{})
	for tick := range 300 {
		f := c.Render(now.Add(time.Duration(tick)*app.FrameRate), full)
		for row := range full.Rows {
			for col := range full.Cols {
				pad, _ := domain.Grid(row, col)
				if cell := f.At(pad); !cell.Dark() && cell.Palette != rowColour[row] {
					t.Fatalf("tick %d lit (%d,%d) as palette %d, want its row's %d", tick, row, col, cell.Palette, rowColour[row])
				}
			}
		}
	}
}

// capTrace runs a meter on a 128 BPM kick in the bass, or on a steady sound
// without one, for twenty seconds, then flicks band 5 to full for one frame
// and records when column 5's cap — the only light left in it — falls a row,
// and where in the beat it fell. It also says whether any lit bar cell was
// ever accented to full.
func capTrace(t *testing.T, kicking bool) (drops, since []float64, accented bool) {
	t.Helper()
	const period = 60 / 128.0
	src := &fixedLevels{}
	c := meterOf(t, src)
	at, last := now, -1
	for elapsed := 0.0; elapsed < 26; elapsed += app.FrameRate.Seconds() {
		at = at.Add(app.FrameRate)
		beat := math.Mod(elapsed, period)
		bass := 0.5
		if kicking {
			bass = math.Exp(-beat / 0.08)
		}
		bands := []float64{bass, bass, 0, 0, 0, 0.3, 0, 0}
		if elapsed < 20 {
			bands[5] = 0
		}
		if math.Abs(elapsed-20) < app.FrameRate.Seconds()/2 {
			bands[5] = 1
		}
		src.l = app.Loudness{Amplitude: bass, Bands: bands, Source: "test"}
		f := c.Render(at, full)
		for row := range full.Rows {
			pad, _ := domain.Grid(row, 0)
			if f.At(pad).Level == domain.LevelFull {
				accented = true
			}
		}
		if elapsed < 20 {
			continue
		}
		top := -1
		for row := range full.Rows - 3 { // band 5 at 0.3 lights the bottom three
			pad, _ := domain.Grid(row, 5)
			if !f.At(pad).Dark() {
				top = row
				break
			}
		}
		if last >= 0 && top > last {
			drops, since = append(drops, elapsed), append(since, beat)
		}
		if top >= 0 {
			last = top
		}
	}
	return drops, since, accented
}

// Against a steady kick the caps fall one row per tracked beat, on the beat.
func TestTheMetersCapsFallOnTheTrackedBeat(t *testing.T) {
	t.Parallel()
	const period = 60 / 128.0
	drops, since, accented := capTrace(t, true)
	if len(drops) < 3 {
		t.Fatalf("the cap fell %d times in six seconds of a steady kick", len(drops))
	}
	for i := 1; i < len(drops); i++ {
		if gap := drops[i] - drops[i-1]; math.Abs(gap-period) > 0.1*period {
			t.Errorf("the cap fell %.3fs after the last fall, want a beat, %.3fs", gap, period)
		}
	}
	for i, s := range since {
		if s > 0.1 && s < period-0.1 {
			t.Errorf("fall %d came %.3fs into the beat", i, s)
		}
	}
	if !accented {
		t.Error("a tracked kick never accented a lit cell")
	}
}

// Without a beat the meter is a plain one: the caps fall on a slow timer and
// nothing is accented.
func TestTheMetersCapsFallOnATimerWithoutABeat(t *testing.T) {
	t.Parallel()
	drops, _, accented := capTrace(t, false)
	if len(drops) < 3 {
		t.Fatalf("the cap fell %d times in six seconds", len(drops))
	}
	for i := 1; i < len(drops); i++ {
		if gap := drops[i] - drops[i-1]; math.Abs(gap-0.75) > 0.05 {
			t.Errorf("the cap fell %.3fs after the last fall, want the timer's 0.75s", gap)
		}
	}
	if accented {
		t.Error("a sound without a beat accented a lit cell")
	}
}

// The meter's pad names the mode, the column's band, level and peak, and the
// tracker's tempo and confidence.
func TestTheMeterSaysItsBandLevelAndBeat(t *testing.T) {
	t.Parallel()
	c := meterOf(t, &fixedLevels{l: app.Loudness{Amplitude: 0.5, Bands: []float64{0, 0, 0.5, 0, 0, 0, 0, 0}, Source: "test"}})
	c.Render(now, full)
	got := c.Describe(now, app.Slot{Col: 2, Size: full})
	for _, want := range []string{"meter", "band 3 of 8", "50%", "peak  50%", "BPM", "sure"} {
		if !strings.Contains(got, want) {
			t.Errorf("the third column says %q, want %q in it", got, want)
		}
	}
}
