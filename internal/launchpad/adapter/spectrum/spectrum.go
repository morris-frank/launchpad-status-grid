// Package spectrum turns a window of samples into a Loudness: one small
// radix-2 FFT and eight log-spaced bands. It is shared by every Levels source
// that has samples — a file on disk, the system's output, the microphone — so
// the same sound draws the same picture whichever of them carried it.
//
// Pure Go and no dependency: 1024 points thirty times a second is nothing a
// library would earn its place for.
package spectrum

import (
	"math"
	"math/cmplx"

	launchpadapp "github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// Window is how many samples one reading looks at: ~23ms at 44.1kHz, short
// enough to follow a beat and long enough to resolve the lowest band.
const Window = 1024

// Analyser reads windows one after another. It keeps the loudest band lately,
// decaying, so the bars use the whole height of whatever is playing.
type Analyser struct {
	peak float64
}

// Read analyses frame, mono and -1..1, sampled at rate. A frame shorter than
// Window is zero-padded; a longer one is cut to its last Window samples.
func (a *Analyser) Read(frame []float64, rate int, source string) launchpadapp.Loudness {
	if len(frame) > Window {
		frame = frame[len(frame)-Window:]
	}
	buf := make([]complex128, Window)
	sum := 0.0
	for i, s := range frame {
		hann := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(Window-1))
		buf[i] = complex(s*hann, 0)
		sum += s * s
	}
	fft(buf)

	// Log-spaced bands from ~40Hz to ~16kHz, as a spectrum is heard.
	bands := make([]float64, domain.GridSize)
	lo, hi := 40.0, math.Min(16000, float64(rate)/2)
	for b := range bands {
		f0 := lo * math.Pow(hi/lo, float64(b)/float64(len(bands)))
		f1 := lo * math.Pow(hi/lo, float64(b+1)/float64(len(bands)))
		i0 := max(1, int(f0*Window/float64(rate)))
		i1 := max(i0+1, int(f1*Window/float64(rate)))
		e := 0.0
		for i := i0; i < i1 && i < Window/2; i++ {
			e += cmplx.Abs(buf[i])
		}
		bands[b] = e / float64(i1-i0)
	}
	// One peak across all bands, not one per band, or every band would read
	// full on its own terms and the spectrum's shape would be lost.
	loudest := 0.0
	for _, e := range bands {
		loudest = math.Max(loudest, e)
	}
	a.peak = math.Max(loudest, a.peak*0.995)
	for b := range bands {
		if a.peak > 0 {
			bands[b] /= a.peak
		}
	}
	rms := math.Sqrt(sum / Window)
	return launchpadapp.Loudness{Amplitude: math.Min(1, rms*3), Bands: bands, Source: source}
}

// fft is an in-place radix-2 Cooley-Tukey transform; len(a) is a power of two.
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u, v := a[start+k], a[start+k+size/2]*wk
				a[start+k], a[start+k+size/2] = u+v, u-v
				wk *= w
			}
		}
	}
}
