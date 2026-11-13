package audiofile

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A pure tone lights the band it sits in and leaves the far bands dark.
func TestAToneLightsItsOwnBand(t *testing.T) {
	const rate = 44100
	var pcm bytes.Buffer
	for i := 0; i < rate; i++ {
		_ = binary.Write(&pcm, binary.LittleEndian, int16(20000*math.Sin(2*math.Pi*100*float64(i)/rate)))
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
	l, err := Open(path, start)
	if err != nil {
		t.Fatal(err)
	}
	got := l.Level(start.Add(300 * time.Millisecond))
	if got.Source != "file" || got.Amplitude == 0 {
		t.Fatalf("reading = %+v", got)
	}
	// 100Hz sits in the second of eight log-spaced bands from 40Hz (84-178Hz).
	if got.Bands[1] < 0.99 || got.Bands[len(got.Bands)-1] > 0.05 {
		t.Errorf("bands = %.2f, want the second full and the highest dark", got.Bands)
	}
}
