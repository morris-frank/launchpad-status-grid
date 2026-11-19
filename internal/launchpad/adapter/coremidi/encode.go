package coremidi

import "github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"

// This file has no build tag, on purpose. Everything in it is arithmetic over
// bytes, so it compiles and is tested on the Linux runner that never sees
// CoreMIDI — which is the only reason the adapter has any test coverage in CI
// at all. Keep the framework out of here.

// The device's batched LED message. One SysEx carries any number of specs, so a
// whole board is one write rather than 81, and a partial repaint is one write
// carrying only what moved.
//
//	F0 00 20 29 02 0D 03 <spec>... F7
var ledHeader = []byte{0xF0, 0x00, 0x20, 0x29, 0x02, 0x0D, 0x03}

const ledEnd = byte(0xF7)

// The four spec kinds the device accepts, which are not the same set as
// domain.BehaviourKind: the domain has three behaviours, and solid splits here
// into an RGB spec while flash and pulse stay on palette indices.
const (
	specStatic byte = 0 // 00 <index> <colour>
	specFlash  byte = 1 // 01 <index> <colour A> <colour B>
	specPulse  byte = 2 // 02 <index> <colour>
	specRGB    byte = 3 // 03 <index> <r> <g> <b>
)

// spec is one pad's lighting instruction: a kind, the device's number for the
// pad, and up to three payload bytes. Fixed width rather than a slice so that a
// surface stays comparable and a whole board diffs with one ==.
type spec struct {
	kind byte
	idx  byte
	p    [3]byte
}

// surface is the encoded board: what the device was last told, pad by pad.
type surface [domain.PadCount]spec

// encode turns a picture into instructions.
//
// The interesting case is that brightness survives only for solid pads. The
// device's flash and pulse commands take a palette index and nothing else, so a
// pulsing cell at level dim pulses at full — a hardware limit, not a rounding
// choice, and the reason a screen wanting a dim animation has to animate it from
// the host instead.
func encode(f domain.Frame) surface {
	var s surface
	domain.AllPads(func(pad domain.Pad) {
		c, n := f.At(pad), pad.DeviceNumber()

		switch {
		case c.Dark():
			// Static palette 0 is the device's own "off", and is the one
			// instruction that cancels a flash or pulse already running on the
			// pad. Sending an RGB black would leave it blinking black.
			s[pad] = spec{kind: specStatic, idx: n}
		case c.Behaviour.Kind == domain.Flash:
			s[pad] = spec{kind: specFlash, idx: n, p: [3]byte{c.Palette, c.Behaviour.Alt}}
		case c.Behaviour.Kind == domain.Pulse:
			s[pad] = spec{kind: specPulse, idx: n, p: [3]byte{c.Palette}}
		default:
			lit := c.Lit()
			s[pad] = spec{kind: specRGB, idx: n, p: [3]byte{lit.R, lit.G, lit.B}}
		}
	})
	return s
}

// width is how many wire bytes this spec occupies.
func (sp spec) width() int {
	switch sp.kind {
	case specRGB:
		return 5
	case specFlash:
		return 4
	default:
		return 3
	}
}

// appendTo writes this spec's wire bytes.
func (sp spec) appendTo(dst []byte) []byte {
	return append(append(dst, sp.kind, sp.idx), sp.p[:sp.width()-2]...)
}

// sysexFor is the message that moves the device from prev to next.
//
// It returns nil when nothing changed, which is the common case by a wide
// margin: a settled board recomputes four times a second and sends nothing at
// all. When there is no previous surface — first render, or the first render
// after a replug — every pad is written, because the board on the other side is
// dark and the diff has no way to know it.
func sysexFor(next, prev surface, havePrev bool) []byte {
	out := append([]byte(nil), ledHeader...)
	changed := false
	for i := range next {
		if havePrev && next[i] == prev[i] {
			continue
		}
		out = next[i].appendTo(out)
		changed = true
	}
	if !changed {
		return nil
	}
	return append(out, ledEnd)
}
