package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

type mediaRecorder struct {
	sent []app.MediaKey
	err  error
}

func (m *mediaRecorder) Send(_ context.Context, k app.MediaKey) error {
	m.sent = append(m.sent, k)
	return m.err
}

var one = domain.Size{Rows: 1, Cols: 1}

// The pad plays or pauses; armed, shift makes the next press skip, and is
// spent by it so the press after is play/pause again.
func TestThePadPlaysOrPausesAndShiftedItSkips(t *testing.T) {
	t.Parallel()
	media, shift := &mediaRecorder{}, &app.Shift{}
	c, s := app.NewMediaControl(media, shift), app.NewShiftControl(shift)
	press := func(ctl app.Control) {
		t.Helper()
		if _, err := ctl.Press(context.Background(), app.Slot{Size: one}); err != nil {
			t.Fatal(err)
		}
	}

	press(c)
	press(s)
	if got := c.Render(time.Now(), one).At(0).Palette; got != domain.Cyan.Palette {
		t.Errorf("an armed shift leaves the media pad palette %d, want next's cyan", got)
	}
	press(c)
	press(c)
	want := []app.MediaKey{app.PlayPause, app.NextTrack, app.PlayPause}
	if len(media.sent) != 3 || media.sent[0] != want[0] || media.sent[1] != want[1] || media.sent[2] != want[2] {
		t.Fatalf("sent %v, want %v", media.sent, want)
	}
	if shift.Armed() {
		t.Error("the shifted press did not spend the shift")
	}
}

// A second press on shift takes it back, so a latch pressed by mistake costs
// nothing.
func TestPressingShiftTwiceReleasesIt(t *testing.T) {
	t.Parallel()
	shift := &app.Shift{}
	s := app.NewShiftControl(shift)
	for range 2 {
		if _, err := s.Press(context.Background(), app.Slot{Size: one}); err != nil {
			t.Fatal(err)
		}
	}
	if shift.Armed() {
		t.Error("shift is still armed after a second press")
	}
}

// A desk that cannot post the key must say so on the press, or the pad looks
// like it worked and the music simply did not stop.
func TestAKeyThatCouldNotBeSentIsSaidAndFlagged(t *testing.T) {
	t.Parallel()
	c := app.NewMediaControl(&mediaRecorder{err: errors.New("no accessibility")}, &app.Shift{})
	out, err := c.Press(context.Background(), app.Slot{Size: one})
	if err == nil || !out.Attention || !strings.Contains(out.Note, "play/pause") {
		t.Fatalf("outcome %+v, err %v; want a flagged note naming the key and the error", out, err)
	}
	if got := c.Describe(time.Now(), app.Slot{Size: one}); !strings.Contains(got, "shift: next") {
		t.Errorf("the media pad describes itself as %q, want its shifted meaning named", got)
	}
}
