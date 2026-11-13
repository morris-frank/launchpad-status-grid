package spectrum

import (
	"math"
	"math/cmplx"
	"testing"
)

// The transform agrees with the definition of a DFT, bin for bin.
func TestTheFFTIsTheDiscreteFourierTransform(t *testing.T) {
	const n = 64
	in := make([]complex128, n)
	for i := range in {
		in[i] = complex(math.Sin(float64(i)*0.3)+0.5*math.Cos(float64(i)*1.7), 0)
	}
	got := append([]complex128(nil), in...)
	fft(got)
	for k := range n {
		var want complex128
		for i, x := range in {
			want += x * cmplx.Exp(complex(0, -2*math.Pi*float64(k*i)/n))
		}
		if cmplx.Abs(got[k]-want) > 1e-9 {
			t.Fatalf("bin %d = %v, want %v", k, got[k], want)
		}
	}
}

// A 3kHz tone at 48kHz — the tap's rate, not a file's — lights the band it
// sits in (1697–3600Hz, the sixth) and leaves the lowest dark.
func TestAToneAtTheTapsRateLightsItsOwnBand(t *testing.T) {
	const rate = 48000
	frame := make([]float64, Window)
	for i := range frame {
		frame[i] = 0.5 * math.Sin(2*math.Pi*3000*float64(i)/rate)
	}
	var a Analyser
	got := a.Read(frame, rate, "system")
	if got.Source != "system" || got.Amplitude == 0 {
		t.Fatalf("reading = %+v", got)
	}
	if got.Bands[5] < 0.99 || got.Bands[0] > 0.05 {
		t.Errorf("bands = %.2f, want the sixth full and the lowest dark", got.Bands)
	}
}

// Silence is silence: no band, no amplitude, and no division by a zero peak.
func TestSilenceReadsAsNothing(t *testing.T) {
	var a Analyser
	got := a.Read(make([]float64, Window), 48000, "system")
	if got.Amplitude != 0 {
		t.Errorf("amplitude = %v", got.Amplitude)
	}
	for b, v := range got.Bands {
		if v != 0 || math.IsNaN(v) {
			t.Errorf("band %d = %v", b, v)
		}
	}
}
