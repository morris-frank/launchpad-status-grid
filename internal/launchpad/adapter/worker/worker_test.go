package worker_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/adapter/worker"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
)

// fakeWorker answers /v1/board with a fixture's data and /v1/session-list
// with sessions, counting board reads and recording the last bearer.
type fakeWorker struct {
	board    string // testdata file
	sessions string
	status   int

	boards atomic.Int32
	bearer atomic.Value
}

func (f *fakeWorker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.bearer.Store(r.Header.Get("Authorization"))
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = fmt.Fprint(w, `{"error":"no"}`)
		return
	}
	switch r.URL.Path {
	case "/v1/board":
		f.boards.Add(1)
		data, err := os.ReadFile("testdata/" + f.board)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = fmt.Fprintf(w, `{"operation":"board","status":"ok","summary":"","data":%s}`, data)
	case "/v1/session-list":
		_, _ = fmt.Fprintf(w, `{"operation":"session-list","status":"ok","summary":"","data":{"sessions":%s}}`, f.sessions)
	default:
		http.NotFound(w, r)
	}
}

func client(t *testing.T, f *fakeWorker, token string) *worker.Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := worker.New(srv.URL+"/", token, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The desk reading is mario's own fixture, decoded field for field: a known
// window is its share, an unknown one stays unknown, and a burn with a median
// carries its ratio.
func TestQuotaIsTheBoardsDeskReading(t *testing.T) {
	t.Parallel()
	got, err := client(t, &fakeWorker{board: "whole.json"}, "").Quota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := app.QuotaStatus{
		Subscriptions: []app.SubscriptionReading{
			{Name: "max", Measured: true, FiveHour: app.WindowReading{Known: true, Remaining: 0.7}, SevenDay: app.WindowReading{Known: true, Remaining: 0.4}},
			{Name: "ada's", Owner: "ada@example.com", Measured: true, Spent: true, FiveHour: app.WindowReading{Known: true}},
		},
		Burn: app.BurnReading{Known: true, LastFiveHours: 700, MedianFiveHours: 500, Ratio: 1.4},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("quota = %+v\nwant    %+v", got, want)
	}
}

// An unmeasured seat and an unread burn arrive as unknown, never as zero.
func TestADegradedBoardsQuotaIsUnknownNotZero(t *testing.T) {
	t.Parallel()
	got, err := client(t, &fakeWorker{board: "degraded.json"}, "").Quota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Burn.Known || len(got.Subscriptions) != 1 || got.Subscriptions[0].Measured {
		t.Errorf("degraded quota = %+v, want an unknown burn and one unmeasured seat", got)
	}
}

func TestReposAreTheBoardsRepositories(t *testing.T) {
	t.Parallel()
	got, err := client(t, &fakeWorker{board: "whole.json"}, "").Repos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's repository has linked worktrees only and no checkout, so
	// nothing about its own tree is claimed.
	if len(got) != 1 || got[0] != (app.Repo{Name: "mario"}) {
		t.Errorf("repos = %+v, want mario alone, nothing claimed", got)
	}
}

// One board read serves the quota and the repositories of one refresh tick.
func TestOneBoardReadServesQuotaAndRepos(t *testing.T) {
	t.Parallel()
	f := &fakeWorker{board: "whole.json"}
	c := client(t, f, "")
	ctx := context.Background()
	if _, err := c.Quota(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Repos(ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.boards.Load(); n != 1 {
		t.Errorf("read the board %d times, want once", n)
	}
}

func TestTheTokenIsSentAsABearerAndOnlyWhenSet(t *testing.T) {
	t.Parallel()
	for token, want := range map[string]string{"tok": "Bearer tok", "": ""} {
		f := &fakeWorker{board: "whole.json"}
		if _, err := client(t, f, token).Quota(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := f.bearer.Load(); got != want {
			t.Errorf("token %q sent Authorization %q, want %q", token, got, want)
		}
	}
}

func TestARefusalIsUnauthorised(t *testing.T) {
	t.Parallel()
	_, err := client(t, &fakeWorker{status: http.StatusUnauthorized}, "").Quota(context.Background())
	if !errors.Is(err, worker.ErrUnauthorised) {
		t.Errorf("err = %v, want ErrUnauthorised", err)
	}
}

func TestCheckReportsARefusal(t *testing.T) {
	t.Parallel()
	if err := client(t, &fakeWorker{status: http.StatusForbidden}, "").Check(context.Background()); !errors.Is(err, worker.ErrUnauthorised) {
		t.Errorf("Check = %v, want ErrUnauthorised", err)
	}
	if err := client(t, &fakeWorker{board: "whole.json"}, "").Check(context.Background()); err != nil {
		t.Errorf("Check against a good worker = %v", err)
	}
}

func TestAWorkerNotAnsweringIsUnreachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	c, err := worker.New(srv.URL, "", &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Sessions(context.Background()); !errors.Is(err, worker.ErrUnreachable) {
		t.Errorf("err = %v, want ErrUnreachable", err)
	}
}

func TestANonURLIsRefused(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "mario.example.com", "://"} {
		if _, err := worker.New(bad, "", nil); err == nil {
			t.Errorf("New(%q) took it", bad)
		}
	}
}

// Sessions come newest first, a Claude Code harness reads as claude, the
// session's own directory wins over its checkout, and only a measured death
// ends one.
func TestSessionsAreNewestFirstAndOnlyAMeasuredDeathEndsOne(t *testing.T) {
	t.Parallel()
	f := &fakeWorker{sessions: `[
		{"harness":"codex","sessionId":"old","Path":"/w/a","LastWrite":"2026-11-02T10:00:00Z","Live":2,"ContextTokens":5},
		{"harness":"claude-code","sessionId":"new","Path":"/w/b","Dir":"/w/b/sub","LastWrite":"2026-11-02T12:00:00Z","Live":0,"ConfigDir":"/p"}
	]`}
	got, err := client(t, f, "").Sessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []app.Session{
		{Tool: "claude", ID: "new", Dir: "/w/b/sub", ConfigDir: "/p", LastWrite: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)},
		{Tool: "codex", ID: "old", Dir: "/w/a", Ended: true, LastWrite: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC), ContextTokens: 5},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("sessions = %+v\nwant       %+v", got, want)
	}
}
