package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/audiofile"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/liveaudio"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/domain"
)

// The environment, named as mario's desk client names it, so an operator who
// ran the board from cappy keeps the same settings here.
const (
	envWorkerURL   = "MARIO_WORKER_URL"
	envWorkerToken = "MARIO_WORKER_TOKEN"
	// envContextLimit states the context window session pads are measured
	// against, in tokens: a transcript records how many tokens a turn carried
	// and never the window, so the meter is drawn against a number the
	// operator supplies. Too small reads fuller than it is, the safe side.
	envContextLimit = "MARIO_LAUNCHPAD_CONTEXT_LIMIT"
	// envFocus is the command a session pad's press runs. Placeholders: %d the
	// session's directory, %t the tool, %i the session id. Unset, a press
	// says so and runs nothing.
	envFocus = "MARIO_LAUNCHPAD_FOCUS"
	// envAudioFile names a 16-bit PCM WAV that drives the visualiser instead
	// of a live input, and wins over envAudio.
	envAudioFile = "MARIO_LAUNCHPAD_AUDIO_FILE"
	// envAudio is the visualiser's live input: system (the default),
	// microphone or synthetic.
	envAudio = "MARIO_LAUNCHPAD_AUDIO"
)

// defaultContextLimit is the window most Claude Code sessions run in.
const defaultContextLimit = 200_000

// sources is everything the desk reads and acts through, so a test composes
// the real layout over fakes.
type sources struct {
	sessions app.Sessions
	focus    app.Focus
	repos    app.Repos
	quota    app.QuotaReadings
	levels   app.Levels
	media    app.Media
}

// Rows of the operator's layout (mario's decision of 2026-09-24), in grid
// rows: the round buttons along the top are the layout's row 1, so grid row 0
// is its row 2.
const (
	sessionsRow = 0
	reposRow    = 1
	burnRow     = 2 // burn at columns 1-4; columns 5-8 unbound (mario's CPU and RAM)
	meterRow    = 3 // subscription pads left; columns 5-8 unbound
	fiveHourRow = 4 // two rows each, beside the visualiser
	sevenDayRow = 6
)

// newBoard composes the one screen this binary drives:
//
//	row 1    media play/pause (shift: next) at column 6, shift at column 8
//	row 2    eight session pads
//	row 3    eight repository pads
//	row 4    the five-hour burn at columns 1-4
//	row 5    a pad per stored subscription
//	rows 6-7 each subscription's five-hour quota, beside the visualiser
//	rows 8-9 its seven-day quota, beside the visualiser
//
// It is mario's desk layout without what stays with mario's desk client: the
// host's CPU and RAM pads, the worker switch, and column 9's explain, note
// and task pads, every one of which acts on the desk machine or the worker
// rather than showing what the worker reads. Those pads are left dark, so
// every remaining pad is where the hand learned it.
func newBoard(src sources) (*app.Board, error) {
	shift := &app.Shift{}
	quota := app.NewQuotaFeed(src.quota)
	left := func(row, rows int) domain.Region { return domain.Region{Row: row, Rows: rows, Cols: 4} }
	right := func(row, rows int) domain.Region { return domain.Region{Row: row, Col: 4, Rows: rows, Cols: 4} }
	board, err := app.New(app.NewPanel("desk", domain.Amber,
		app.AtTop(5, 1, app.NewMediaControl(src.media, shift)),
		app.AtTop(7, 1, app.NewShiftControl(shift)),
		app.At(domain.Rows(sessionsRow, 1), app.NewSessionsControl(src.sessions, src.focus)),
		app.At(domain.Rows(reposRow, 1), app.NewReposControl(src.repos)),
		app.At(left(meterRow, 1), app.NewSubscriptionsControl(quota)),
		app.At(left(burnRow, 1), app.NewBurnControl(quota)),
		app.At(left(fiveHourRow, 2), app.NewQuotaWindowControl(quota, app.FiveHour)),
		app.At(left(sevenDayRow, 2), app.NewQuotaWindowControl(quota, app.SevenDay)),
		app.At(right(fiveHourRow, 4), app.NewVisualiserControl(src.levels)),
	))
	if err != nil {
		return nil, fmt.Errorf("board: %w", err)
	}
	return board, nil
}

// openLiveAudio is a package var so a test never opens a real tap.
var openLiveAudio = liveaudio.Open

// visualiserLevels picks the visualiser's source and returns what releases
// it. A source that will not start is logged and drawn as the synthetic one,
// which every reading names, rather than as a quiet room.
func visualiserLevels(getenv func(string) string) (app.Levels, func()) {
	synthetic := app.SyntheticLevels{}
	if path := getenv(envAudioFile); path != "" {
		levels, err := audiofile.Open(path, time.Now())
		if err == nil {
			return levels, func() {}
		}
		slog.Warn("visualiser file unreadable; drawing the synthetic source", "err", err)
		return synthetic, func() {}
	}
	src := liveaudio.Source(strings.TrimSpace(getenv(envAudio)))
	switch src {
	case "":
		src = liveaudio.System
	case liveaudio.System, liveaudio.Microphone:
	case "synthetic":
		return synthetic, func() {}
	default:
		slog.Warn("no such visualiser input; drawing the synthetic source", "input", string(src))
		return synthetic, func() {}
	}
	levels, err := openLiveAudio(src, time.Now(), synthetic, func(err error) {
		slog.Warn("visualiser input refused; drawing the synthetic source", "input", string(src), "err", err)
	})
	if err != nil {
		slog.Warn("visualiser input unavailable; drawing the synthetic source", "input", string(src), "err", err)
		return synthetic, func() {}
	}
	return levels, levels.Close
}

// contextLimit is envContextLimit's window, or the default.
func contextLimit(getenv func(string) string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(getenv(envContextLimit))); err == nil && n > 0 {
		return n
	}
	return defaultContextLimit
}

// cachedSessions reuses one read for a second, and states the window every
// session is measured against.
type cachedSessions struct {
	src   app.Sessions
	limit int

	mu   sync.Mutex
	at   time.Time
	last []app.Session
	err  error
}

// sessionCacheFor is shorter than the board's refresh, so a second reader on
// one tick is served from the cache and the next tick reads afresh.
const sessionCacheFor = time.Second

func (c *cachedSessions) Sessions(ctx context.Context) ([]app.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < sessionCacheFor {
		return c.last, c.err
	}
	sessions, err := c.src.Sessions(ctx)
	c.at = time.Now()
	if err != nil {
		c.err = err
		return c.last, err
	}
	for i := range sessions {
		sessions[i].ContextLimit = c.limit
	}
	c.last, c.err = sessions, nil
	return sessions, nil
}

// errNoFocus is a session press with no envFocus to run.
var errNoFocus = errors.New("no focus command: set " + envFocus)

// focusCommand runs the operator's own command for a session.
//
// It shells out because the command is the operation: raising a terminal pane
// or opening an app's thread is the operator's incantation for their desktop,
// and nothing in-process stands in for it. Through sh, so pipelines and
// osascript one-liners work as typed.
type focusCommand struct{ template string }

func newFocus(getenv func(string) string) focusCommand {
	return focusCommand{template: strings.TrimSpace(getenv(envFocus))}
}

func (f focusCommand) Focus(ctx context.Context, s app.Session) error {
	if f.template == "" {
		return errNoFocus
	}
	line := expandFocus(f.template, s)
	cmd := exec.CommandContext(ctx, "sh", "-c", line)
	cmd.Dir = s.Dir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run %q: %w", line, err)
	}
	// Not waited on: the board must not block on however long the window it
	// raised then lives.
	go func() { _ = cmd.Wait() }()
	return nil
}

func expandFocus(template string, s app.Session) string {
	return strings.NewReplacer("%d", s.Dir, "%t", s.Tool, "%i", s.ID).Replace(template)
}
