// Package worker reads what the board draws from a mario worker's HTTP API:
// the same `board` and `session-list` rows mario's own desk client (cappy)
// reads, under /v1, as the same caller.
//
// It depends on none of mario's code — Go's internal rule forbids it, and the
// point of this repository is that mario can change without it. What it
// shares with mario is the wire: the JSON the worker answers. The handful of
// fields decoded here are the whole contract, and testdata/ holds mario's own
// board fixtures (testdata/board in soilytix/mario) so a change to that shape
// is caught by `mise run check` on the next copy.
//
// Who it asks as: MARIO_WORKER_TOKEN, sent as a bearer, as cappy sends a
// pasted IAP token. cappy's `cappy login` (an Entra refresh token held in the
// macOS Keychain) is not read here; a worker that needs a token and is handed
// none answers 401, which comes back as ErrUnauthorised.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
)

// prefix is what every route of the worker's API sits under.
const prefix = "/v1"

// maxAnswer bounds an answer read whole. A board answer is tens of kilobytes;
// a workspace of hundreds of repositories is still well under this.
const maxAnswer = 32 << 20

// boardFresh is how long one board read serves every port that asks: the
// repositories and the quota both come from it, and a refresh tick asks for
// both.
const boardFresh = 2 * time.Second

// ErrUnauthorised is a worker that refused the caller: no token, or one it
// would not take.
var ErrUnauthorised = errors.New("the worker refused the token; set MARIO_WORKER_TOKEN")

// ErrUnreachable is a worker that did not answer at all.
var ErrUnreachable = errors.New("the worker is not answering")

// Client is one worker.
type Client struct {
	base  *url.URL
	token string
	http  *http.Client
	now   func() time.Time

	mu    sync.Mutex
	held  board
	err   error
	taken time.Time

	// failing is the last read's error words, so a failure is logged when it
	// starts and when it ends rather than on every refresh.
	failMu  sync.Mutex
	failing string
}

// New is a client for the worker at base, sending token as a bearer where it
// is not empty. httpClient is never http.DefaultClient's zero timeout.
func New(base, token string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("worker: %q is not a worker URL (want e.g. https://mario.example.com)", base)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{base: u, token: strings.TrimSpace(token), http: httpClient, now: time.Now}, nil
}

// envelope is the API's answer, its data left for the caller to decode.
type envelope struct {
	Operation string          `json:"operation"`
	Data      json.RawMessage `json:"data"`
	Error     string          `json:"error"`
}

// read is a GET of one row, answered as its data. The board swallows a
// source's error (it draws "not read" instead), so this is where a failure
// reaches the log: once as it starts, once as it clears.
func (c *Client) read(ctx context.Context, verb string) (json.RawMessage, error) {
	data, err := c.get(ctx, verb)
	words := ""
	if err != nil {
		words = err.Error()
	}
	c.failMu.Lock()
	defer c.failMu.Unlock()
	switch words {
	case c.failing:
	case "":
		slog.Info("worker answering again", "worker", c.base.String())
	default:
		slog.Warn("worker read failed; its pads show nothing read", "err", err)
	}
	c.failing = words
	return data, err
}

// Check reads the board once, so a caller can refuse to start against a
// worker that will refuse every read.
func (c *Client) Check(ctx context.Context) error {
	_, err := c.board(ctx)
	return err
}

func (c *Client) get(ctx context.Context, verb string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+prefix+"/"+verb, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("worker: %w: %s (%w)", ErrUnreachable, c.base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return nil, fmt.Errorf("worker: read the answer to %s: %w", verb, err)
	}
	if len(raw) > maxAnswer {
		return nil, fmt.Errorf("worker: the answer to %s is over %d MiB", verb, maxAnswer>>20)
	}
	var e envelope
	_ = json.Unmarshal(raw, &e) // a refusal may carry no JSON; the status says enough
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("worker: %s: %w", verb, ErrUnauthorised)
	case resp.StatusCode != http.StatusOK:
		why := e.Error
		if why == "" {
			why = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("worker: %s answered %d: %s", verb, resp.StatusCode, why)
	case len(e.Data) == 0:
		return nil, fmt.Errorf("worker: %s answered no data", verb)
	}
	return e.Data, nil
}

// board is the part of the board answer this binary draws.
type board struct {
	Desk     app.QuotaStatus `json:"desk"`
	Snapshot struct {
		Board struct {
			Repos []repo
		}
		Failed map[string]string
	} `json:"snapshot"`
}

// repo is one repository on the worker's board, reduced to its fields a pad
// can show.
type repo struct {
	Name     string
	Branches []struct {
		Upstream *struct{ Ahead, Behind int }
		Worktree *struct {
			Main       bool
			Dirty      int
			DirtyKnown bool
		}
	}
	// Err is the worker's error value, which encodes as an object when there
	// is one and null when there is none: its words never cross the wire.
	Err json.RawMessage
}

func (c *Client) board(ctx context.Context) (board, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.taken.IsZero() && c.now().Sub(c.taken) < boardFresh {
		return c.held, c.err
	}
	c.held, c.err = board{}, nil
	data, err := c.read(ctx, "board")
	if err == nil {
		if err = json.Unmarshal(data, &c.held); err != nil {
			err = fmt.Errorf("worker: the board answer: %w", err)
		}
	}
	c.err, c.taken = err, c.now()
	return c.held, c.err
}

// Quota implements app.QuotaReadings: the board answer's desk reading, which
// the worker measured and which has no field a credential fits in.
func (c *Client) Quota(ctx context.Context) (app.QuotaStatus, error) {
	b, err := c.board(ctx)
	return b.Desk, err
}

// Repos implements app.Repos.
func (c *Client) Repos(ctx context.Context) ([]app.Repo, error) {
	b, err := c.board(ctx)
	if err != nil {
		return nil, err
	}
	if why, failed := b.Snapshot.Failed["Board"]; failed {
		return nil, fmt.Errorf("worker: the board's repositories: %s", why)
	}
	out := make([]app.Repo, 0, len(b.Snapshot.Board.Repos))
	for _, r := range b.Snapshot.Board.Repos {
		out = append(out, padRepo(r))
	}
	return out, nil
}

// padRepo is one repository as a pad takes it. The checkout is the branch in
// the main worktree; a tree the worker could not measure is drawn clean, as
// mario's own pad draws it, and a repository it could not read at all is
// Unreadable — never clean.
func padRepo(r repo) app.Repo {
	out := app.Repo{Name: r.Name, Unreadable: len(r.Err) > 0 && string(r.Err) != "null"}
	for _, b := range r.Branches {
		if b.Worktree == nil || !b.Worktree.Main {
			continue
		}
		out.Dirty = b.Worktree.DirtyKnown && b.Worktree.Dirty > 0
		if b.Upstream != nil {
			out.Ahead, out.Behind = b.Upstream.Ahead, b.Upstream.Behind
		}
		break
	}
	return out
}

// session is one agent session as the worker's session-list answers it.
type session struct {
	Harness       string    `json:"harness"`
	SessionID     string    `json:"sessionId"`
	Path          string    `json:"Path"`
	Dir           string    `json:"Dir"`
	ConfigDir     string    `json:"ConfigDir"`
	LastWrite     time.Time `json:"LastWrite"`
	Live          int       `json:"Live"`
	ContextTokens int       `json:"ContextTokens"`
}

// liveDead is the worker's Liveness for a process measured gone; unknown is
// 0 and running 1. Only a measured death ends a session on a pad.
const liveDead = 2

// Sessions implements app.Sessions, newest first, through the worker's
// session-list row rather than its board: the board's workspace walk counts
// every tree and asks GitHub, and a pad wants only the sessions.
func (c *Client) Sessions(ctx context.Context) ([]app.Session, error) {
	data, err := c.read(ctx, "session-list")
	if err != nil {
		return nil, err
	}
	var answer struct {
		Sessions []session `json:"sessions"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, fmt.Errorf("worker: the session-list answer: %w", err)
	}
	out := make([]app.Session, 0, len(answer.Sessions))
	for _, s := range answer.Sessions {
		out = append(out, padSession(s))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastWrite.After(out[j].LastWrite) })
	return out, nil
}

// padSession is one session as a pad takes it. ContextLimit is not the
// worker's to say (a transcript names no window); the caller states it.
func padSession(s session) app.Session {
	dir := s.Dir
	if dir == "" {
		dir = s.Path
	}
	tool := s.Harness
	if tool == "claude-code" {
		tool = "claude"
	}
	return app.Session{
		Tool:          tool,
		ID:            s.SessionID,
		Dir:           dir,
		ConfigDir:     s.ConfigDir,
		Ended:         s.Live == liveDead,
		LastWrite:     s.LastWrite,
		ContextTokens: s.ContextTokens,
	}
}
