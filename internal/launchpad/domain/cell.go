package domain

// RGB is a 7-bit colour triple. The device rejects anything above 127 per
// channel, so the type carries the device's range rather than a byte's.
type RGB struct{ R, G, B byte }

// Scale dims c by f in 0..1, which is how Level is applied to a solid pad.
// Saturating rather than wrapping: a widget that computes 1.4 should produce
// full brightness, not a dark pad.
func (c RGB) Scale(f float64) RGB {
	if f <= 0 {
		return RGB{}
	}
	if f > 1 {
		f = 1
	}
	s := func(v byte) byte { return byte(float64(v)*f + 0.5) }
	return RGB{s(c.R), s(c.G), s(c.B)}
}

// BehaviourKind is how the DEVICE animates a pad.
//
// All three are rendered by the hardware off its own clock, so a pad that
// flashes or pulses forever costs the host nothing: no timer, no bytes, no
// goroutine. That is the difference that decides the whole design — anything
// expressible as a behaviour is free to leave running, and anything that is not
// needs a host frame every tick. Prefer a behaviour.
type BehaviourKind byte

const (
	// Solid is a steady light. The only kind that honours per-pad RGB.
	Solid BehaviourKind = iota
	// Flash alternates the cell's hue with Alt at 2 Hz. Both are palette
	// indices, because the device's flash command takes nothing else.
	Flash
	// Pulse fades the cell's hue down and back at 1 Hz.
	Pulse
)

// Behaviour is a kind plus the one parameter Flash needs.
type Behaviour struct {
	Kind BehaviourKind
	Alt  byte
}

// Behaviour constructors, so a caller writes the intent and not a struct.
var (
	BSolid = Behaviour{Kind: Solid}
	BPulse = Behaviour{Kind: Pulse}
)

// BFlash alternates with the palette colour alt.
func BFlash(alt byte) Behaviour { return Behaviour{Kind: Flash, Alt: alt} }

// Level is brightness, quantised to six perceptually even steps.
//
// Six rather than a float because brightness is the only per-pad intensity the
// device offers — flash and pulse rates are global to the surface — so it has
// to read as distinct states at arm's length rather than as a continuum. A
// value between two steps is a value nobody can see the difference of.
type Level int

// The six steps. Off is genuinely dark: an unlit pad is how the board's lit
// SHAPE stays readable, and that shape is what a hand memorises before it
// memorises any single pad.
const (
	LevelOff Level = iota
	LevelFaint
	LevelDim
	LevelMid
	LevelBright
	LevelFull
)

var levelFactor = [...]float64{0, 0.06, 0.16, 0.32, 0.55, 1.00}

// Factor is the brightness multiplier for this level.
func (l Level) Factor() float64 {
	if l < LevelOff || int(l) >= len(levelFactor) {
		return 0
	}
	return levelFactor[l]
}

// ScaleLevel maps a fraction in 0..1 onto the six steps, so a widget computing a
// proportion does not have to know how many steps there are. 0 stays off, and
// anything above 0 is at least faint — a meter reading 1% should be visible as
// "some" rather than indistinguishable from empty.
func ScaleLevel(f float64) Level {
	switch {
	case f <= 0:
		return LevelOff
	case f >= 1:
		return LevelFull
	}
	l := Level(int(f*float64(LevelFull)) + 1)
	if l > LevelFull {
		l = LevelFull
	}
	return l
}

// Cell is what one light should look like right now.
//
// Palette and RGB carry the same hue twice on purpose. The device's flash and
// pulse commands accept a palette index and nothing else, so a colour that is
// ever allowed to blink must exist in the palette — which is why a semantic
// colour in this system is always a palette entry and there is no conversion
// step anywhere. RGB is what a solid pad uses so it can carry per-pad
// brightness, and what the terminal preview draws.
type Cell struct {
	Palette   byte
	RGB       RGB
	Level     Level
	Behaviour Behaviour
}

// Off is a dark light. The zero Cell is deliberately this.
var Off = Cell{}

// Dark reports whether this cell emits nothing, by either of the two routes to
// darkness — level zero, or a palette index of 0 which is the device's own
// "off" entry.
func (c Cell) Dark() bool { return c.Level == LevelOff || (c.Palette == 0 && c.RGB == RGB{}) }

// At sets the cell's brightness, returning a new cell.
func (c Cell) At(l Level) Cell { c.Level = l; return c }

// With sets the cell's behaviour, returning a new cell.
func (c Cell) With(b Behaviour) Cell { c.Behaviour = b; return c }

// Lit is the cell's colour after brightness is applied — what a renderer should
// actually draw for a solid pad.
func (c Cell) Lit() RGB {
	if c.Level == LevelOff {
		return RGB{}
	}
	return c.RGB.Scale(c.Level.Factor())
}
