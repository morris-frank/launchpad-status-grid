package domain_test

import (
	"testing"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// The device numbering is the one fact in this package that was measured
// against hardware rather than reasoned about, so it is the one that gets
// pinned by value. Getting it wrong lights the wrong pad, which is a bug you
// find with your eyes and not with a stack trace.
func TestDeviceNumbersMatchTheMeasuredLayout(t *testing.T) {
	cases := []struct {
		name string
		pad  func() (domain.Pad, bool)
		want byte
	}{
		{"top-left grid pad is 81, because device row 1 is the bottom", func() (domain.Pad, bool) { return domain.Grid(0, 0) }, 81},
		{"bottom-left grid pad is 11", func() (domain.Pad, bool) { return domain.Grid(7, 0) }, 11},
		{"bottom-right grid pad is 18", func() (domain.Pad, bool) { return domain.Grid(7, 7) }, 18},
		{"top-right grid pad is 88", func() (domain.Pad, bool) { return domain.Grid(0, 7) }, 88},
		{"first top button is 91", func() (domain.Pad, bool) { return domain.Top(1) }, 91},
		{"last top button is 98", func() (domain.Pad, bool) { return domain.Top(8) }, 98},
		{"first side button is 89, counting from the top", func() (domain.Pad, bool) { return domain.Side(1) }, 89},
		{"last side button is 19", func() (domain.Pad, bool) { return domain.Side(8) }, 19},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := tc.pad()
			if !ok {
				t.Fatalf("pad did not resolve")
			}
			if got := p.DeviceNumber(); got != tc.want {
				t.Errorf("DeviceNumber() = %d, want %d", got, tc.want)
			}
		})
	}

	if got := domain.Logo.DeviceNumber(); got != 99 {
		t.Errorf("logo DeviceNumber() = %d, want 99", got)
	}
}

// A press arrives as a device number and has to become the same pad that was
// lit, or the board acts on a pad nobody touched. Round-tripping every pad is
// the only check that covers the two numbering schemes meeting.
func TestEveryPadRoundTripsThroughItsDeviceNumber(t *testing.T) {
	domain.AllPads(func(p domain.Pad) {
		got, ok := domain.FromDevice(p.DeviceNumber(), p.IsNote())
		if !ok {
			t.Errorf("pad %d: device number %d did not decode", p, p.DeviceNumber())
			return
		}
		if got != p {
			t.Errorf("pad %d round-tripped to %d via device number %d", p, got, p.DeviceNumber())
		}
	})
}

// The two number spaces turn out to be disjoint — a grid note is row*10+col
// with both in 1..8, so it never contains a 9, and every chrome controller
// does. That is a property of the device worth pinning rather than assuming,
// because it is what makes a mis-flagged message fail loudly instead of
// lighting the wrong pad.
func TestTheNoteAndControllerNumberSpacesDoNotOverlap(t *testing.T) {
	notes := map[byte]bool{}
	controllers := map[byte]bool{}
	domain.AllPads(func(p domain.Pad) {
		if p.IsNote() {
			notes[p.DeviceNumber()] = true
		} else {
			controllers[p.DeviceNumber()] = true
		}
	})

	for n := range notes {
		if controllers[n] {
			t.Errorf("device number %d is both a note and a controller", n)
		}
		if _, ok := domain.FromDevice(n, false); ok {
			t.Errorf("note %d decoded as a controller", n)
		}
	}
	for n := range controllers {
		if _, ok := domain.FromDevice(n, true); ok {
			t.Errorf("controller %d decoded as a note", n)
		}
	}
}

func TestOffGridCoordinatesDoNotResolve(t *testing.T) {
	for _, c := range [][2]int{{-1, 0}, {0, -1}, {8, 0}, {0, 8}} {
		if _, ok := domain.Grid(c[0], c[1]); ok {
			t.Errorf("Grid(%d, %d) resolved but is off the grid", c[0], c[1])
		}
	}
	for _, n := range []int{0, 9, -1} {
		if _, ok := domain.Top(n); ok {
			t.Errorf("Top(%d) resolved", n)
		}
		if _, ok := domain.Side(n); ok {
			t.Errorf("Side(%d) resolved", n)
		}
	}
}

// The logo is the one light that can say something without inviting a press.
func TestTheLogoIsLightableButNotPressable(t *testing.T) {
	if !domain.Logo.Valid() {
		t.Error("the logo should be addressable")
	}
	if domain.Logo.Pressable() {
		t.Error("the logo has no switch under it and must not be pressable")
	}
}
