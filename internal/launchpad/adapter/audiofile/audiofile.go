// Package audiofile is a Levels source that reads loudness out of a recording
// on disk instead of a live input, so the visualiser can be driven by a fixed,
// repeatable sound. It reads 16-bit PCM WAV only: that needs no decoder
// dependency, and any other format is one `afconvert -f WAVE -d LEI16` away.
package audiofile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/spectrum"
	launchpadapp "github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
)

// Levels plays the recording against the clock from the moment it was opened,
// looping, and reports the loudness at the current position.
type Levels struct {
	samples  []float64 // mono, -1..1
	rate     int
	start    time.Time
	name     string
	analyser spectrum.Analyser
}

// Open reads a 16-bit PCM WAV file whole.
func Open(path string, now time.Time) (*Levels, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	samples, rate, err := readWAV(f)
	if err != nil {
		return nil, fmt.Errorf("audiofile: %s: %w", path, err)
	}
	if len(samples) <= spectrum.Window { // Level loops modulo len-window

		return nil, fmt.Errorf("audiofile: %s: shorter than one window", path)
	}
	return &Levels{samples: samples, rate: rate, start: now, name: "file"}, nil
}

// Level implements app.Levels.
func (l *Levels) Level(now time.Time) launchpadapp.Loudness {
	pos := int(now.Sub(l.start).Seconds()*float64(l.rate)) % (len(l.samples) - spectrum.Window)
	return l.analyser.Read(l.samples[pos:pos+spectrum.Window], l.rate, l.name)
}

// readWAV walks the RIFF chunks for fmt and data and mixes to mono.
func readWAV(r io.Reader) ([]float64, int, error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return nil, 0, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a WAV file")
	}
	var channels, bits uint16
	var rate uint32
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, 0, errors.New("no data chunk")
		}
		size := binary.LittleEndian.Uint32(hdr[4:])
		body := make([]byte, size+size%2)
		if _, err := io.ReadFull(r, body); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, 0, err
		}
		switch string(hdr[0:4]) {
		case "fmt ":
			if len(body) < 16 {
				return nil, 0, errors.New("fmt chunk too short")
			}
			if binary.LittleEndian.Uint16(body[0:]) != 1 {
				return nil, 0, errors.New("not PCM")
			}
			channels = binary.LittleEndian.Uint16(body[2:])
			rate = binary.LittleEndian.Uint32(body[4:])
			bits = binary.LittleEndian.Uint16(body[14:])
		case "data":
			if bits != 16 || channels == 0 {
				return nil, 0, fmt.Errorf("want 16-bit PCM, got %d-bit", bits)
			}
			frames := int(size) / int(2*channels)
			out := make([]float64, frames)
			for i := range out {
				s := 0.0
				for c := 0; c < int(channels); c++ {
					s += float64(int16(binary.LittleEndian.Uint16(body[2*(i*int(channels)+c):])))
				}
				out[i] = s / float64(channels) / 32768
			}
			return out, int(rate), nil
		}
	}
}
