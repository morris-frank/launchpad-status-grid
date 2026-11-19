package app

import (
	"math"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// beatTracker is a Bayesian estimate of one steady beat's tempo and phase, fed
// by the bass: a posterior over a grid of tempo × phase — the bar-pointer
// model, one beat long. Each frame every hypothesis advances its phase by
// dt/period, a little mass diffuses to neighbouring tempos and back toward the
// prior, and the frame's bass onset is evidence for the hypotheses that put a
// beat now. An onset off the beat moves little, because one frame's evidence
// is small against the mass the regular beat has gathered; that is what keeps
// the flash regular, and also why a song change or a voice takes seconds to
// settle, and a podcast never does. It assumes one steady beat and does not
// know when that assumption is false — confidence is its only answer, and the
// posterior's own concentration is not enough of one: random onsets sharpen it
// too. So confidence is also how much of the recent onset energy landed where
// the tracker predicted a beat before seeing it.
type beatTracker struct {
	post   [beatTempi][beatPhases]float64
	scr    [beatPhases]float64
	prev   float64 // the last frame's bass, for the flux
	mean   float64 // the flux's running mean, its scale
	onAll  float64 // the onsets' running mean
	onBeat float64 // the running mean of the onsets that fell on a predicted beat
	primed bool
}

const (
	beatTempi  = 56
	beatPhases = 48
	beatMinBPM = 70.0
	beatMaxBPM = 180.0
	// beatPriorBPM is the tempo assumed before any evidence: most music with
	// a beat sits within an octave of it, and it breaks the half/double tie.
	beatPriorBPM = 120.0
	// beatWindow is how many phase bins either side of the beat count as "on
	// it": ±1/24 of a beat, a frame at 120 BPM.
	beatWindow = 2
	// beatDrift is how fast a tempo hypothesis wanders to a neighbour, per
	// second; beatForget how fast the posterior returns to the prior, per
	// second, so a new song can take over.
	beatDrift  = 0.3
	beatForget = 0.05
	// beatAgreeWindow is how near a predicted beat an onset must fall, as a
	// fraction of a beat, to agree with it; beatAgreeSeconds how long the
	// agreement remembers. Noise agrees about twice the window's share, a
	// steady kick nearly always, so agreement counts from beatAgreeFloor.
	beatAgreeWindow  = 0.1
	beatAgreeSeconds = 4.0
	beatAgreeFloor   = 0.35
)

func beatBPM(i int) float64 {
	return beatMinBPM * math.Pow(beatMaxBPM/beatMinBPM, float64(i)/float64(beatTempi-1))
}

func beatPrior(i int) float64 {
	d := math.Log(beatBPM(i) / beatPriorBPM)
	return math.Exp(-d * d / (2 * 0.25 * 0.25))
}

func (b *beatTracker) reset() {
	sum := 0.0
	for i := range b.post {
		sum += beatPrior(i) * beatPhases
	}
	for i := range b.post {
		for j := range b.post[i] {
			b.post[i][j] = beatPrior(i) / sum
		}
	}
	b.primed = true
}

// update advances the posterior by dt seconds and weighs one bass reading.
func (b *beatTracker) update(dt, bass float64) {
	if !b.primed {
		b.reset()
		b.prev = bass
		return
	}
	if dt <= 0 {
		return
	}
	flux := math.Max(0, bass-b.prev)
	b.prev = bass
	b.mean += (flux - b.mean) * math.Min(1, dt/3)
	onset := 0.0
	if b.mean > 1e-6 {
		onset = clamp((flux - b.mean) / (3 * b.mean))
	}

	// Predict: every hypothesis moves along its beat.
	for i := range b.post {
		s := dt * beatBPM(i) / 60 * beatPhases
		k := int(math.Floor(s))
		f := s - float64(k)
		for j := range beatPhases {
			a := b.post[i][((j-k)%beatPhases+beatPhases)%beatPhases]
			c := b.post[i][((j-k-1)%beatPhases+beatPhases)%beatPhases]
			b.scr[j] = (1-f)*a + f*c
		}
		b.post[i] = b.scr
	}
	// Drift between neighbouring tempos, keeping the phase.
	m := math.Min(0.25, beatDrift*dt)
	prev := b.post[0]
	for i := range b.post {
		cur := b.post[i]
		next := cur
		if i+1 < beatTempi {
			next = b.post[i+1]
		}
		for j := range beatPhases {
			b.post[i][j] = (1-2*m)*cur[j] + m*prev[j] + m*next[j]
		}
		prev = cur
	}
	// Judge the onset against the prediction before it becomes evidence.
	_, predicted, _ := b.posterior()
	hit := 0.0
	if predicted < beatAgreeWindow || predicted > 1-beatAgreeWindow {
		hit = onset
	}
	k := math.Min(1, dt/beatAgreeSeconds)
	b.onAll += (onset - b.onAll) * k
	b.onBeat += (hit - b.onBeat) * k
	// Weigh the evidence: an onset favours a beat now, a silence mildly
	// disfavours it, and everything else is untouched.
	onBeat := 0.7 + 4*onset
	sum := 0.0
	for i := range b.post {
		for j := range beatPhases {
			if j <= beatWindow || j >= beatPhases-beatWindow {
				b.post[i][j] *= onBeat
			}
			sum += b.post[i][j]
		}
	}
	// Normalise, and forget toward the prior.
	e := math.Min(1, beatForget*dt)
	var norm float64
	for i := range b.post {
		norm += beatPrior(i) * beatPhases
	}
	for i := range b.post {
		for j := range beatPhases {
			b.post[i][j] = (1-e)*b.post[i][j]/sum + e*beatPrior(i)/norm
		}
	}
}

// estimate is the posterior's best tempo, the phase within the current beat
// (0 is the beat), and a confidence in 0..1: the posterior's, scaled by how
// well recent onsets agreed with the predicted beat.
func (b *beatTracker) estimate() (bpm, phase, confidence float64) {
	bpm, phase, confidence = b.posterior()
	agree := 0.0
	if b.onAll > 1e-6 {
		agree = clamp((b.onBeat/b.onAll - beatAgreeFloor) / (1 - beatAgreeFloor) * 2)
	}
	return bpm, phase, confidence * agree
}

// posterior is estimate's tempo and phase, and a confidence that is how
// concentrated that tempo's phase is, times how much of the posterior backs
// that tempo and its neighbours.
func (b *beatTracker) posterior() (bpm, phase, confidence float64) {
	best, bestMass, total := 0, -1.0, 0.0
	mass := [beatTempi]float64{}
	for i := range b.post {
		for _, p := range b.post[i] {
			mass[i] += p
		}
		total += mass[i]
	}
	for i := range mass {
		near := mass[i]
		if i > 0 {
			near += mass[i-1]
		}
		if i+1 < beatTempi {
			near += mass[i+1]
		}
		if near > bestMass {
			best, bestMass = i, near
		}
	}
	var x, y float64
	for j, p := range b.post[best] {
		a := 2 * math.Pi * float64(j) / beatPhases
		x += p * math.Cos(a)
		y += p * math.Sin(a)
	}
	if mass[best] <= 0 || total <= 0 {
		return beatBPM(best), 0, 0
	}
	phase = math.Atan2(y, x) / (2 * math.Pi)
	if phase < 0 {
		phase++
	}
	r := math.Hypot(x, y) / mass[best]
	return beatBPM(best), phase, clamp(r * bestMass / total * 3)
}

// bassOf is the reading the tracker is fed: the two lowest bands, or the
// overall amplitude when the source reports fewer.
func bassOf(l Loudness) float64 {
	if len(l.Bands) >= 2 {
		return clamp((l.Bands[0] + l.Bands[1]) / 2)
	}
	return clamp(l.Amplitude)
}

// beatFlash is how bright the beat is phase into it: full on the beat, fading
// within a tenth of a second, as bright as the tracker is sure.
func beatFlash(bpm, phase, conf float64) float64 {
	return math.Exp(-phase*60/bpm/0.07) * conf
}

// beatFrame is the tracked beat drawn as one flash of the whole rectangle.
func beatFrame(t *beatTracker, size domain.Size) domain.Frame {
	v := beatFlash(t.estimate())
	f := domain.Blank()
	for row := range size.Rows {
		for col := range size.Cols {
			// Lime, #8EDE3D, at half-intensity scale.
			f = f.SetGrid(row, col, rgbCell(0.56*v, 0.87*v, 0.24*v))
		}
	}
	return f
}
