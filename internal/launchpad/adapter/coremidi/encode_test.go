package coremidi

import (
	"testing"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// A SysEx body may not contain a byte with the top bit set — the device reads
// one as the start of a new message and the rest of the frame becomes garbage.
// Every value in a spec comes from either a device number or a 7-bit colour, so
// this should hold by construction; it is checked because the failure is a
// corrupted stream rather than an error, and the board would just misbehave.
func TestNoBodyByteHasTheTopBitSet(t *testing.T) {
	f := domain.Blank()
	domain.AllPads(func(p domain.Pad) {
		f = f.Set(p, domain.White.Cell(domain.LevelFull))
	})

	msg := sysexFor(encode(f), surface{}, false)
	if len(msg) < 2 {
		t.Fatalf("a full repaint produced %d bytes", len(msg))
	}
	for i, b := range msg[1 : len(msg)-1] {
		if b&0x80 != 0 {
			t.Errorf("body byte %d is 0x%02X, which the device reads as a new message", i, b)
		}
	}
	if msg[0] != 0xF0 || msg[len(msg)-1] != 0xF7 {
		t.Errorf("message is framed 0x%02X..0x%02X, want 0xF0..0xF7", msg[0], msg[len(msg)-1])
	}
}

// Darkness has to be spelled as static palette 0 rather than as an RGB black,
// because only the static command cancels a flash or a pulse the pad is already
// running. An RGB black would leave the pad blinking black — invisible until the
// next frame lights it, and then blinking for a reason nobody can find.
func TestADarkCellCancelsRatherThanBlackens(t *testing.T) {
	pad, _ := domain.Grid(0, 0)
	blinking := domain.Blank().Set(pad, domain.Red.Cell(domain.LevelFull).With(domain.BPulse))
	dark := domain.Blank()

	got := encode(dark)[pad]
	want := spec{kind: specStatic, idx: pad.DeviceNumber()}
	if got != want {
		t.Errorf("dark cell encoded as %+v, want %+v", got, want)
	}

	// And the transition has to actually be sent, not diffed away.
	if msg := sysexFor(encode(dark), encode(blinking), true); len(msg) == 0 {
		t.Error("turning a pulsing pad off sent nothing")
	}
}

// Brightness is a per-pad RGB scale, which the device offers for solid pads and
// not for flashing or pulsing ones. Pinning that asymmetry here is what stops
// somebody "fixing" a dim pulse by scaling a palette index, which is not a
// number that can be scaled.
func TestBrightnessAppliesToSolidPadsOnly(t *testing.T) {
	pad, _ := domain.Grid(2, 3)

	full := encode(domain.Blank().Set(pad, domain.Green.Cell(domain.LevelFull)))[pad]
	dim := encode(domain.Blank().Set(pad, domain.Green.Cell(domain.LevelDim)))[pad]
	if full.kind != specRGB || dim.kind != specRGB {
		t.Fatalf("a solid cell should encode as RGB, got kinds %d and %d", full.kind, dim.kind)
	}
	if dim.p[1] >= full.p[1] {
		t.Errorf("dim green (%d) is not dimmer than full green (%d)", dim.p[1], full.p[1])
	}

	pulseFull := encode(domain.Blank().Set(pad, domain.Green.Cell(domain.LevelFull).With(domain.BPulse)))[pad]
	pulseDim := encode(domain.Blank().Set(pad, domain.Green.Cell(domain.LevelDim).With(domain.BPulse)))[pad]
	if pulseFull != pulseDim {
		t.Errorf("brightness changed a pulse spec (%+v vs %+v); the device has no such command", pulseFull, pulseDim)
	}
	if pulseFull.p[0] != domain.Green.Palette {
		t.Errorf("pulse carries palette %d, want %d", pulseFull.p[0], domain.Green.Palette)
	}
}

// The diff is the reason a board left showing costs nothing. A settled surface
// must produce no message at all, and a one-pad change must produce a message
// about one pad.
func TestOnlyChangedPadsAreSent(t *testing.T) {
	pad, _ := domain.Grid(4, 4)
	before := encode(domain.Blank())
	after := encode(domain.Blank().Set(pad, domain.Mint.Cell(domain.LevelFull)))

	if msg := sysexFor(before, before, true); msg != nil {
		t.Errorf("an unchanged surface sent %d bytes, want none", len(msg))
	}

	msg := sysexFor(after, before, true)
	want := len(ledHeader) + 5 + 1 // header + one RGB spec + F7
	if len(msg) != want {
		t.Errorf("a one-pad change sent %d bytes, want %d", len(msg), want)
	}
	if msg[len(ledHeader)+1] != pad.DeviceNumber() {
		t.Errorf("the message names pad %d, want %d", msg[len(ledHeader)+1], pad.DeviceNumber())
	}
}

// The first render after a replug has no usable previous surface, and the board
// on the other side is dark. Every pad has to be written or the parts that
// happen to match the stale surface stay unlit.
func TestAFirstRenderWritesEveryPad(t *testing.T) {
	msg := sysexFor(encode(domain.Blank()), surface{}, false)
	want := len(ledHeader) + domain.PadCount*3 + 1 // all dark: 81 static specs
	if len(msg) != want {
		t.Errorf("first render sent %d bytes, want %d", len(msg), want)
	}
}
