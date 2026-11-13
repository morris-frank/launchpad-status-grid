package domain_test

import (
	"testing"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// litRow reads one grid row back as brightness levels, which is what a widget
// test actually wants to assert: the shape of the light, not the colour values.
func litRow(f domain.Frame, row int) []domain.Level {
	out := make([]domain.Level, domain.GridSize)
	for c := 0; c < domain.GridSize; c++ {
		p, _ := domain.Grid(row, c)
		out[c] = f.At(p).Level
	}
	return out
}

func countLit(f domain.Frame) int {
	n := 0
	domain.AllPads(func(p domain.Pad) {
		if !f.At(p).Dark() {
			n++
		}
	})
	return n
}

// A bar with no track is indistinguishable from a shorter bar, so "how much"
// and "out of how much" have to be visible together. This is the assertion that
// keeps the track from being optimised away as decoration.
func TestABarDrawsItsUnfilledTrackAsWellAsItsFill(t *testing.T) {
	f := domain.Blank().Bar(domain.Rows(0, 1), 0.5, domain.Green)

	got := litRow(f, 0)
	for c := 0; c < 4; c++ {
		if got[c] != domain.LevelFull {
			t.Errorf("column %d: level %v, want full", c, got[c])
		}
	}
	for c := 4; c < 8; c++ {
		if got[c] != domain.LevelFaint {
			t.Errorf("column %d: level %v, want the faint track", c, got[c])
		}
	}
}

// An 8-cell bar reads as more than 8 values because the leading cell is drawn
// part-lit. Without this a quantity appears frozen for an eighth of its range,
// which is exactly the interval over which a person is watching it move.
func TestAPartiallyFilledCellIsDrawnDimRatherThanRoundedAway(t *testing.T) {
	quarterOfACell := 1.0/8.0/4.0 + 2.0/8.0 // two full cells and a quarter
	f := domain.Blank().Bar(domain.Rows(0, 1), quarterOfACell, domain.Green)

	got := litRow(f, 0)
	if got[0] != domain.LevelFull || got[1] != domain.LevelFull {
		t.Fatalf("the two whole cells should be full, got %v", got[:2])
	}
	if got[2] == domain.LevelFaint || got[2] == domain.LevelFull {
		t.Errorf("the partial cell should be between the track and full, got %v", got[2])
	}
}

func TestABarClampsRatherThanOverflowing(t *testing.T) {
	for _, f := range []float64{-1, 2} {
		frame := domain.Blank().Bar(domain.Rows(0, 1), f, domain.Green)
		for c, l := range litRow(frame, 0) {
			if l != domain.LevelOff && l != domain.LevelFaint && l != domain.LevelFull {
				t.Errorf("fraction %v column %d: level %v is neither empty nor full", f, c, l)
			}
		}
	}
}

// Direction is not cosmetic: a draining quantity has to empty toward its own
// origin, or the eye reads it as filling.
func TestABarGrowsFromTheEndItsAxisNames(t *testing.T) {
	right := domain.Blank().BarAlong(domain.Rows(0, 1), 0.25, domain.Green, domain.Right)
	left := domain.Blank().BarAlong(domain.Rows(0, 1), 0.25, domain.Green, domain.Left)

	if litRow(right, 0)[0] != domain.LevelFull {
		t.Error("a rightward bar should fill from column 0")
	}
	if litRow(left, 0)[domain.GridSize-1] != domain.LevelFull {
		t.Error("a leftward bar should fill from the last column")
	}
}

// The ramp exists so two screens showing "how much is left" cannot disagree
// about when it becomes worrying.
func TestAMeterTakesItsColourFromWhatIsLeft(t *testing.T) {
	cases := []struct {
		remaining float64
		want      domain.Colour
	}{
		{1.0, domain.Green},
		{0.5, domain.Green},
		{0.2, domain.Amber},
		{0.05, domain.Red},
	}
	for _, tc := range cases {
		f := domain.Blank().Meter(domain.Rows(0, 1), tc.remaining, domain.Right)
		p, _ := domain.Grid(0, 0)
		if got := f.At(p).Palette; got != tc.want.Palette {
			t.Errorf("remaining %v: palette %d, want %d", tc.remaining, got, tc.want.Palette)
		}
	}
}

func TestColumnsDrawOneBarPerColumnAndStopWhereTheValuesDo(t *testing.T) {
	f := domain.Blank().Columns(domain.FullGrid(), []float64{1, 0.5, 0}, nil)

	bottom, _ := domain.Grid(domain.GridSize-1, 0)
	if f.At(bottom).Level != domain.LevelFull {
		t.Error("a full column should reach the bottom row at full brightness")
	}
	top, _ := domain.Grid(0, 0)
	if f.At(top).Level != domain.LevelFull {
		t.Error("a value of 1 should fill its column to the top")
	}
	// Columns past the supplied values are left alone entirely, so a caller
	// streaming a growing series does not have to pad it.
	for c := 3; c < domain.GridSize; c++ {
		p, _ := domain.Grid(0, c)
		if !f.At(p).Dark() {
			t.Errorf("column %d had no value and should be dark", c)
		}
	}
}

// A spinner has to move and has to come back, and a caller keeps an int rather
// than a position — so the phase has to wrap on its own, including negatively.
func TestASpinnerTravelsAndWrapsWithoutTheCallerBoundingThePhase(t *testing.T) {
	r := domain.Region{Rows: 3, Cols: 3}
	ringLength := 8 // the border of a 3x3

	first := domain.Blank().Spinner(r, 0, domain.Cyan, 0)
	moved := domain.Blank().Spinner(r, 1, domain.Cyan, 0)
	wrapped := domain.Blank().Spinner(r, ringLength, domain.Cyan, 0)
	negative := domain.Blank().Spinner(r, -ringLength, domain.Cyan, 0)

	if first.Equal(moved) {
		t.Error("advancing the phase should move the head")
	}
	if !first.Equal(wrapped) {
		t.Error("a full lap should return to the start")
	}
	if !first.Equal(negative) {
		t.Error("a negative phase should wrap rather than draw nothing")
	}
}

func TestASpinnersTailFadesBehindTheHead(t *testing.T) {
	r := domain.Region{Rows: 3, Cols: 3}
	f := domain.Blank().Spinner(r, 0, domain.Cyan, 2)

	if got := countLit(f); got != 3 {
		t.Fatalf("a head plus a two-cell tail should light 3 pads, lit %d", got)
	}
	ring := []domain.Level{}
	for _, rc := range [][2]int{{0, 0}, {2, 0}, {2, 1}} { // head, then backwards
		p, _ := domain.Grid(rc[0], rc[1])
		ring = append(ring, f.At(p).Level)
	}
	if ring[0] <= ring[1] || ring[1] <= ring[2] {
		t.Errorf("the tail should fade behind the head, got %v", ring)
	}
}

// An effect that has finished must leave nothing behind, or compositing it
// keeps painting over the board forever. Blank at the end is what makes these
// safe to composite unconditionally.
func TestOverlayEffectsAreBlankOnceSpent(t *testing.T) {
	centre, _ := domain.Grid(3, 3)
	for _, tc := range []struct {
		name  string
		frame domain.Frame
	}{
		{"ripple at phase 1", domain.Ripple(centre, 1, domain.Cyan)},
		{"ripple past its end", domain.Ripple(centre, 1.5, domain.Cyan)},
		{"wipe at phase 1", domain.Wipe(domain.Right, 1, domain.Cyan)},
		{"wipe before it starts", domain.Wipe(domain.Right, -0.1, domain.Cyan)},
	} {
		if !tc.frame.Equal(domain.Blank()) {
			t.Errorf("%s should be blank, lit %d pads", tc.name, countLit(tc.frame))
		}
	}
}

func TestARippleExpandsAwayFromItsCentre(t *testing.T) {
	centre, _ := domain.Grid(3, 3)
	early := domain.Ripple(centre, 0.05, domain.Cyan)
	later := domain.Ripple(centre, 0.4, domain.Cyan)

	if countLit(early) == 0 || countLit(later) == 0 {
		t.Fatal("a ripple mid-flight should light something")
	}
	if !early.At(centre).Dark() && later.At(centre).Dark() {
		return // the ring has left the centre, which is the point
	}
	if early.Equal(later) {
		t.Error("a ripple should have moved between two phases")
	}
}

func TestAWipeCrossesTheGridInTheDirectionGiven(t *testing.T) {
	early := domain.Wipe(domain.Right, 0.15, domain.Cyan)
	late := domain.Wipe(domain.Right, 0.85, domain.Cyan)

	leftEarly, _ := domain.Grid(0, 0)
	rightLate, _ := domain.Grid(0, domain.GridSize-1)

	if early.At(leftEarly).Dark() {
		t.Error("a rightward wipe should be at the left edge early")
	}
	if late.At(rightLate).Dark() {
		t.Error("a rightward wipe should be at the right edge late")
	}
}

// Blink and Breathe are modifiers, not paints: the pad that starts flashing has
// to be recognisably the same pad, or urgency costs you the category colour.
func TestBlinkPreservesColourAndSkipsDarkCells(t *testing.T) {
	f := domain.Blank().
		SetGrid(0, 0, domain.Green.Cell(domain.LevelFull)).
		Blink(domain.FullGrid(), domain.Red.Palette)

	lit, _ := domain.Grid(0, 0)
	dark, _ := domain.Grid(0, 1)

	got := f.At(lit)
	if got.Palette != domain.Green.Palette {
		t.Errorf("blinking changed the hue: palette %d, want %d", got.Palette, domain.Green.Palette)
	}
	if got.Behaviour.Kind != domain.Flash || got.Behaviour.Alt != domain.Red.Palette {
		t.Errorf("behaviour = %+v, want a flash alternating with red", got.Behaviour)
	}
	if f.At(dark).Behaviour.Kind != domain.Solid {
		t.Error("a dark pad should not be given something to flash")
	}
}

func TestBreatheMakesLitCellsPulseAndLeavesDarkOnesAlone(t *testing.T) {
	f := domain.Blank().
		SetGrid(2, 2, domain.Magenta.Cell(domain.LevelMid)).
		Breathe(domain.FullGrid())

	lit, _ := domain.Grid(2, 2)
	if f.At(lit).Behaviour.Kind != domain.Pulse {
		t.Error("a lit cell should pulse")
	}
	if f.At(lit).Level != domain.LevelMid {
		t.Error("breathing should not change brightness")
	}
	if countLit(f) != 1 {
		t.Error("breathing should not light anything new")
	}
}

// Widgets clip by drawing outside the grid and letting the frame discard it,
// which is what keeps every widget free of boundary arithmetic. If Set ever
// starts panicking or wrapping, every widget breaks at once.
func TestDrawingOutsideTheGridIsDiscardedRatherThanWrapped(t *testing.T) {
	f := domain.Blank().Fill(domain.Region{Row: 6, Col: 6, Rows: 8, Cols: 8}, domain.White.Cell(domain.LevelFull))

	if got := countLit(f); got != 4 {
		t.Errorf("a 8x8 region overhanging the corner should light its 2x2 overlap, lit %d", got)
	}
	topLeft, _ := domain.Grid(0, 0)
	if !f.At(topLeft).Dark() {
		t.Error("overhang wrapped around to the opposite corner")
	}
}

func TestOutlineDrawsABorderAndNotABlock(t *testing.T) {
	f := domain.Blank().Outline(domain.Region{Rows: 3, Cols: 3}, domain.White.Cell(domain.LevelFull))

	if got := countLit(f); got != 8 {
		t.Errorf("a 3x3 outline should light 8 pads, lit %d", got)
	}
	middle, _ := domain.Grid(1, 1)
	if !f.At(middle).Dark() {
		t.Error("the inside of an outline should be untouched")
	}
}

// Over is how a board layers. The rule that matters is that a dark cell in the
// top layer is transparent, not black — otherwise chrome erases the screen.
func TestOverTreatsDarkCellsInTheTopLayerAsTransparent(t *testing.T) {
	under := domain.Blank().Fill(domain.FullGrid(), domain.Green.Cell(domain.LevelMid))
	top := domain.Blank().SetGrid(0, 0, domain.Red.Cell(domain.LevelFull))

	f := under.Over(top)

	corner, _ := domain.Grid(0, 0)
	elsewhere, _ := domain.Grid(4, 4)
	if f.At(corner).Palette != domain.Red.Palette {
		t.Error("the top layer should win where it is lit")
	}
	if f.At(elsewhere).Palette != domain.Green.Palette {
		t.Error("the top layer's dark cells should not erase what is under them")
	}
}

func TestDimLowersBrightnessWithoutExtinguishing(t *testing.T) {
	f := domain.Blank().
		SetGrid(0, 0, domain.Green.Cell(domain.LevelFull)).
		SetGrid(0, 1, domain.Green.Cell(domain.LevelFaint)).
		Dim(3)

	bright, _ := domain.Grid(0, 0)
	faint, _ := domain.Grid(0, 1)
	if f.At(bright).Level >= domain.LevelFull {
		t.Error("dimming should lower a bright cell")
	}
	if f.At(faint).Level == domain.LevelOff {
		t.Error("dimming must not extinguish a cell that was lit; it floors at faint")
	}
}

// A meter reading 1% has to be visible as "some" rather than as empty, which is
// the difference between a quota you can act on and one you notice too late.
func TestAnyPositiveFractionIsAtLeastFaintlyVisible(t *testing.T) {
	if got := domain.ScaleLevel(0.0001); got == domain.LevelOff {
		t.Error("a positive fraction should never quantise to off")
	}
	if got := domain.ScaleLevel(0); got != domain.LevelOff {
		t.Error("zero should be off")
	}
	if got := domain.ScaleLevel(1); got != domain.LevelFull {
		t.Error("one should be full")
	}
}

// Blit is what lets a widget be written without knowing where it will sit: it
// draws at its own origin and is placed afterwards, with the overhang dropped
// rather than wrapping onto the next row.
func TestBlitPlacesAtTheRegionAndClipsTheOverhang(t *testing.T) {
	src := domain.Blank().Fill(domain.FullGrid(), domain.Green.Cell(domain.LevelFull))
	f := domain.Blank().Blit(domain.Region{Row: 6, Col: 6, Rows: 4, Cols: 4}, src)

	inside, _ := domain.Grid(7, 7)
	if f.At(inside).Palette != domain.Green.Palette {
		t.Error("the region's own cells were not painted")
	}
	outside, _ := domain.Grid(5, 5)
	if !f.At(outside).Dark() {
		t.Error("Blit painted outside the region it was given")
	}
	// The region runs to row 9, two rows past the grid. Nothing may appear on
	// row 0 as a result: an overhang that wrapped would draw on the far side of
	// the board, which reads as a second widget.
	wrapped, _ := domain.Grid(0, 6)
	if !f.At(wrapped).Dark() {
		t.Error("the overhang wrapped onto the grid instead of being clipped")
	}
}
