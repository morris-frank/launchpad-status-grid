package liveaudio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/audiofile"
	launchpadapp "github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
)

// fakeCapture hands over whatever samples it holds, as the ring buffer does.
type fakeCapture struct {
	samples   []float64
	rate      int
	delivered uint64
	real      bool
	closed    bool
}

func (f *fakeCapture) latest(dst []float64) (int, uint64) {
	for i := range dst {
		dst[i] = 0
		if j := len(f.samples) - len(dst) + i; j >= 0 {
			dst[i] = f.samples[j]
		}
	}
	return f.rate, f.delivered
}
func (f *fakeCapture) zerosAreReal() bool { return f.real }
func (f *fakeCapture) close()             { f.closed = true }

type constLevels struct{}

func (constLevels) Level(time.Time) launchpadapp.Loudness {
	return launchpadapp.Loudness{Source: "synthetic"}
}

func tone(rate, n int, hz float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = float64(int16(20000*math.Sin(2*math.Pi*hz*float64(i)/float64(rate)))) / 32768
	}
	return out
}

// The same sound draws the same bars whether a WAV file or a live capture
// carried it: the live sources and the file share one analyser.
func TestALiveCaptureDrawsWhatTheSameWAVFileDraws(t *testing.T) {
	const rate = 44100
	samples := tone(rate, rate, 100)

	var pcm bytes.Buffer
	for _, s := range samples {
		_ = binary.Write(&pcm, binary.LittleEndian, int16(math.Round(s*32768)))
	}
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(36+pcm.Len()))
	wav.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		_ = binary.Write(&wav, binary.LittleEndian, v)
	}
	wav.WriteString("data")
	_ = binary.Write(&wav, binary.LittleEndian, uint32(pcm.Len()))
	wav.Write(pcm.Bytes())
	path := filepath.Join(t.TempDir(), "tone.wav")
	if err := os.WriteFile(path, wav.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Unix(0, 0)
	file, err := audiofile.Open(path, start)
	if err != nil {
		t.Fatal(err)
	}
	// The file reads the window starting at 0 at its start instant; the
	// capture's latest window is the same 1024 samples.
	fromFile := file.Level(start)

	live := newLevels(Microphone, &fakeCapture{samples: samples[:1024], rate: rate, delivered: 1024}, start, constLevels{}, nil)
	fromLive := live.Level(start)

	if fromLive.Source != "microphone" {
		t.Errorf("source = %q", fromLive.Source)
	}
	for b := range fromFile.Bands {
		if math.Abs(fromFile.Bands[b]-fromLive.Bands[b]) > 1e-9 {
			t.Fatalf("band %d: file %.4f, live %.4f", b, fromFile.Bands[b], fromLive.Bands[b])
		}
	}
	if fromLive.Bands[1] < 0.99 {
		t.Errorf("bands = %.2f, want the 100Hz band full", fromLive.Bands)
	}
}

// A tap handing over exact zeros while something is playing is refused: it
// is said once, the capture is released, and the fallback draws — never a
// dark grid that claims the room is quiet.
func TestARefusedTapSaysSoAndFallsBackRatherThanDrawingAQuietRoom(t *testing.T) {
	start := time.Unix(0, 0)
	c := &fakeCapture{rate: 48000, delivered: 48000, real: false}
	var said []error
	l := newLevels(System, c, start, constLevels{}, func(err error) { said = append(said, err) })

	if got := l.Level(start.Add(time.Second)); got.Source != "system" {
		t.Fatalf("inside the proof window the capture draws: %+v", got)
	}
	l.Level(start.Add(proofWindow))                   // first sighting of unexplained zeros
	got := l.Level(start.Add(proofWindow + askEvery)) // confirmed a second later
	if got.Source != "synthetic" {
		t.Fatalf("source = %q, want the fallback", got.Source)
	}
	if len(said) != 1 || !errors.Is(said[0], ErrRefused) || !c.closed {
		t.Fatalf("said = %v, closed = %v", said, c.closed)
	}
	l.Level(start.Add(10 * time.Second))
	if len(said) != 1 {
		t.Errorf("a refusal is said once, got %d", len(said))
	}
}

// With nothing playing, zeros from the tap are the truth and are drawn.
func TestAQuietRoomWithNothingPlayingIsDrawnAsQuiet(t *testing.T) {
	start := time.Unix(0, 0)
	c := &fakeCapture{rate: 48000, delivered: 48000, real: true}
	l := newLevels(System, c, start, constLevels{}, func(err error) { t.Fatalf("refused: %v", err) })
	for s := 0; s < 10; s++ {
		if got := l.Level(start.Add(time.Duration(s) * time.Second)); got.Source != "system" || got.Amplitude != 0 {
			t.Fatalf("at %ds: %+v", s, got)
		}
	}
}

// A capture that never delivered a buffer is refused without asking anything.
func TestACaptureThatDeliversNothingFallsBack(t *testing.T) {
	start := time.Unix(0, 0)
	var said error
	l := newLevels(Microphone, &fakeCapture{real: true}, start, constLevels{}, func(err error) { said = err })
	if got := l.Level(start.Add(proofWindow)); got.Source != "microphone" || said != nil {
		t.Fatalf("a device slow to start is waited for: %+v, said %v", got, said)
	}
	if got := l.Level(start.Add(deliveryWindow)); got.Source != "synthetic" || said == nil {
		t.Fatalf("got %+v, said %v", got, said)
	}
}

// One non-zero sample proves the capture; zeros after it are a room gone
// quiet, not a refusal.
func TestAProvenCaptureThatGoesQuietStaysLive(t *testing.T) {
	start := time.Unix(0, 0)
	c := &fakeCapture{samples: tone(48000, 1024, 440), rate: 48000, delivered: 1024}
	l := newLevels(System, c, start, constLevels{}, func(err error) { t.Fatalf("refused: %v", err) })
	l.Level(start)
	c.samples = nil
	for s := 2; s < 6; s++ {
		if got := l.Level(start.Add(time.Duration(s) * time.Second)); got.Source != "system" {
			t.Fatalf("at %ds: %+v", s, got)
		}
	}
}
