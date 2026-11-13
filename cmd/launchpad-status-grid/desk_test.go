package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

type fakeSessions []app.Session

func (f fakeSessions) Sessions(context.Context) ([]app.Session, error) { return f, nil }

type fakeRepos []app.Repo

func (f fakeRepos) Repos(context.Context) ([]app.Repo, error) { return f, nil }

type fakeQuota app.QuotaStatus

func (f fakeQuota) Quota(context.Context) (app.QuotaStatus, error) { return app.QuotaStatus(f), nil }

type fakeMedia struct{}

func (fakeMedia) Send(context.Context, app.MediaKey) error { return nil }

// deskStatus is one measured seat with a full five-hour window and a nearly
// spent seven-day one, and one seat nothing measured.
var deskStatus = app.QuotaStatus{
	Subscriptions: []app.SubscriptionReading{
		{Name: "max", Measured: true, FiveHour: app.WindowReading{Known: true, Remaining: 0.9}, SevenDay: app.WindowReading{Known: true, Remaining: 0.05}},
		{Name: "pro"},
	},
	Burn: app.BurnReading{Known: true, LastFiveHours: 700, MedianFiveHours: 500, Ratio: 1.4},
}

// desk is the real layout over fakes, refreshed once so every control has read.
func desk(t *testing.T) *app.Board {
	t.Helper()
	board, err := newBoard(sources{
		sessions: fakeSessions{{Tool: "claude", ID: "a", Dir: "/w/mario", LastWrite: time.Now()}},
		focus:    focusCommand{},
		repos:    fakeRepos{{Name: "soilytix/mario", Dirty: true}},
		quota:    fakeQuota(deskStatus),
		levels:   app.SyntheticLevels{},
		media:    fakeMedia{},
	})
	if err != nil {
		t.Fatal(err)
	}
	board.Refresh(context.Background())
	return board
}

func grid(t *testing.T, row, col int) domain.Pad {
	t.Helper()
	p, ok := domain.Grid(row, col)
	if !ok {
		t.Fatalf("no grid pad at %d,%d", row, col)
	}
	return p
}

// The worker's quota reaches the pads as mario's desk draws it: a measured
// window in its ramp hue, a nearly spent one red and blinking, an unmeasured
// seat grey — never a hue that claims a quantity nobody read.
func TestTheWorkersQuotaLightsTheQuotaPads(t *testing.T) {
	t.Parallel()
	f := desk(t).Render(time.Now())
	if got, want := f.At(grid(t, fiveHourRow, 0)), domain.Ramp(0.9).Cell(domain.LevelFull); got != want {
		t.Errorf("max's five-hour pad = %+v, want %+v", got, want)
	}
	nearly := f.At(grid(t, sevenDayRow, 0))
	if nearly.RGB != domain.Ramp(0.05).RGB || nearly.Behaviour.Kind != domain.Flash {
		t.Errorf("max's nearly spent seven-day pad = %+v, want the ramp's red, blinking", nearly)
	}
	for _, row := range []int{fiveHourRow, fiveHourRow + 1, sevenDayRow, sevenDayRow + 1} {
		if got := f.At(grid(t, row, 1)); got.RGB != domain.Grey.RGB {
			t.Errorf("the unmeasured seat's pad at row %d = %+v, want grey", row, got)
		}
		if got := f.At(grid(t, row, 2)); !got.Dark() {
			t.Errorf("a column with no seat at row %d is lit: %+v", row, got)
		}
	}
}

// What stays with mario's desk client is left dark rather than drawn as
// something it is not: the CPU and RAM pads, column 9's dictation pads and the
// worker switch.
func TestThePadsMariosDeskKeepsAreDark(t *testing.T) {
	t.Parallel()
	f := desk(t).Render(time.Now())
	for col := 4; col < domain.GridSize; col++ {
		for _, row := range []int{burnRow, meterRow} {
			if got := f.At(grid(t, row, col)); !got.Dark() {
				t.Errorf("unbound pad %d,%d is lit: %+v", row, col, got)
			}
		}
	}
	for _, n := range []int{6, 7, 8} {
		side, _ := domain.Side(n)
		if got := f.At(side); !got.Dark() {
			t.Errorf("side button %d is lit: %+v", n, got)
		}
	}
	if top, _ := domain.Top(7); !f.At(top).Dark() {
		t.Errorf("the worker switch's round button is lit: %+v", f.At(top))
	}
}

// Every bound pad says what it shows, from the worker's reading.
func TestTheBoundPadsDescribeTheWorkersReading(t *testing.T) {
	t.Parallel()
	b := desk(t)
	now := time.Now()
	for pad, want := range map[domain.Pad]string{
		grid(t, reposRow, 0):    "soilytix/mario",
		grid(t, sessionsRow, 0): "mario",
		grid(t, fiveHourRow, 0): "max",
		grid(t, sevenDayRow, 1): "unmeasured",
	} {
		if got := b.Describe(now, pad); !strings.Contains(got, want) {
			t.Errorf("pad %v describes %q, want it to name %q", pad, got, want)
		}
	}
}

// A session press with no focus command says how to get one, and runs nothing.
func TestASessionPressWithNoFocusCommandSaysSo(t *testing.T) {
	t.Parallel()
	err := newFocus(func(string) string { return "" }).Focus(context.Background(), app.Session{Dir: t.TempDir()})
	if !errors.Is(err, errNoFocus) {
		t.Errorf("err = %v, want errNoFocus", err)
	}
	if got := expandFocus("open %d %t %i", app.Session{Dir: "/w", Tool: "claude", ID: "x"}); got != "open /w claude x" {
		t.Errorf("expandFocus = %q", got)
	}
}

func TestContextLimitIsStatedOrTheDefault(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]int{"": defaultContextLimit, "1000000": 1_000_000, "-3": defaultContextLimit, "x": defaultContextLimit} {
		if got := contextLimit(func(string) string { return raw }); got != want {
			t.Errorf("contextLimit(%q) = %d, want %d", raw, got, want)
		}
	}
}

// The interaction contract: --version answers on stdout, a stray argument is
// an invalid invocation, and no worker is a dependency unavailable.
func TestTheInvocationContract(t *testing.T) {
	t.Parallel()
	none := func(string) string { return "" }
	for name, tc := range map[string]struct {
		args []string
		code int
		out  string
	}{
		"version":   {[]string{"--version"}, exitOK, "dev\n"},
		"stray":     {[]string{"board"}, exitInvocation, ""},
		"bad flag":  {[]string{"--nope"}, exitInvocation, ""},
		"no worker": {nil, exitUnavailable, ""},
		"bad url":   {[]string{"--worker", "mario.example.com"}, exitInvocation, ""},
	} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), tc.args, none, &stdout, &stderr)
		if got := exitCode(err, false, &stderr); got != tc.code {
			t.Errorf("%s: exit %d, want %d (%v)", name, got, tc.code, err)
		}
		if stdout.String() != tc.out {
			t.Errorf("%s: stdout %q, want %q", name, stdout.String(), tc.out)
		}
	}
}

// A worker that refuses the token stops the start with exit 4, before any
// device is opened, rather than leaving every worker pad grey for good.
func TestARefusedTokenStopsTheStart(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"--worker", srv.URL}, func(string) string { return "" }, &stdout, &stderr)
	if got := exitCode(err, false, &stderr); got != exitUnavailable || !strings.Contains(stderr.String(), "MARIO_WORKER_TOKEN") {
		t.Errorf("exit %d, stderr %q; want 4 naming MARIO_WORKER_TOKEN", got, stderr.String())
	}
}
