//go:build darwin && cgo

package liveaudio

import (
	"math"
	"os"
	"testing"
	"time"
)

// TestProbe measures what a capture really hands over on this machine, from
// whatever process runs it: the PR's terminal-versus-launchd measurement is
// this test, run once from a shell and once as a launchd job. It touches a
// real device, so it runs only when MARIO_LIVEAUDIO_PROBE names a source.
func TestProbe(t *testing.T) {
	src := Source(os.Getenv("MARIO_LIVEAUDIO_PROBE"))
	if src == "" {
		t.Skip("set MARIO_LIVEAUDIO_PROBE=system or microphone to measure a real capture")
	}
	c, err := start(src)
	if err != nil {
		t.Logf("probe %s: start failed: %v", src, err)
		return
	}
	defer c.close()
	buf := make([]float64, 4096)
	peak, nonzero := 0.0, 0
	var rate int
	var delivered uint64
	for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		rate, delivered = c.latest(buf)
		for _, s := range buf {
			if s != 0 {
				nonzero++
			}
			peak = math.Max(peak, math.Abs(s))
		}
	}
	t.Logf("probe %s: rate=%d delivered=%d nonzero=%d peak=%.5f zerosAreReal=%v", src, rate, delivered, nonzero, peak, c.zerosAreReal())
}
