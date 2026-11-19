// Command launchpad-status-grid drives a Novation Launchpad as a desk status
// display for a mario worker: agent sessions, repositories, subscription
// quota and burn read from the worker's HTTP API, beside a visualiser of what
// the desk is playing and the media keys.
//
// It is mario's Launchpad board moved out of mario: the same screens over the
// same frames, its data read through the worker's API rather than mario's
// packages. --preview draws the board in the terminal, mirrored with an
// attached Launchpad when there is one, so it runs and can be pressed with no
// hardware and on any OS; the device itself needs macOS with cgo.
//
// Exit codes: 0 ok, 1 the board failed, 2 invalid invocation, 4 a dependency
// unavailable (no worker URL, no Launchpad attached), 130 interrupted.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/coremidi"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/mediakeys"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/tui"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/adapter/worker"
	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
)

// version is stamped by the build (-X main.version=…); "dev" otherwise.
var version = "dev"

// Exit codes, mario's interaction contract.
const (
	exitOK          = 0
	exitFailure     = 1
	exitInvocation  = 2
	exitUnavailable = 4
	exitInterrupted = 130
)

// exitError carries an exit code with its error.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string { return e.err.Error() }
func (e exitError) Unwrap() error { return e.err }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	interrupted := ctx.Err() != nil
	stop()
	os.Exit(exitCode(err, interrupted, os.Stderr))
}

func exitCode(err error, interrupted bool, stderr io.Writer) int {
	switch {
	case interrupted:
		return exitInterrupted
	case err == nil:
		return exitOK
	}
	_, _ = fmt.Fprintln(stderr, "launchpad-status-grid:", err)
	var e exitError
	if errors.As(err, &e) {
		return e.code
	}
	return exitFailure
}

// run is the whole program: flags, the worker, the board, and where it is
// drawn. stdout carries only --version's answer; everything else is stderr.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("launchpad-status-grid", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print the version and exit")
	preview := fs.Bool("preview", false, "draw the board in the terminal, mirrored with an attached Launchpad")
	workerURL := fs.String("worker", getenv(envWorkerURL), "the mario worker's base URL (default $"+envWorkerURL+")")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(stderr, "usage: launchpad-status-grid [--preview] [--worker URL]\n\n"+
			"Drives a Novation Launchpad with a mario worker's board. Environment:\n"+
			"  %s   the worker's base URL\n  %s the bearer sent to it, where it needs one\n"+
			"  %s, %s, %s, %s\n\n", envWorkerURL, envWorkerToken, envContextLimit, envFocus, envAudio, envAudioFile)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return exitError{exitInvocation, err}
	}
	if fs.NArg() > 0 {
		return exitError{exitInvocation, fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	if *showVersion {
		_, _ = fmt.Fprintln(stdout, version)
		return nil
	}
	if *workerURL == "" {
		return exitError{exitUnavailable, fmt.Errorf("no worker: set %s or pass --worker", envWorkerURL)}
	}
	client, err := worker.New(*workerURL, getenv(envWorkerToken), &http.Client{Timeout: 60 * time.Second})
	if err != nil {
		return exitError{exitInvocation, err}
	}

	// A refused token would leave every worker pad grey for good, so it stops
	// the start (a missing credential is a dependency unavailable). A worker
	// not answering yet may come up; the board shows "not read" until then.
	if err := client.Check(ctx); errors.Is(err, worker.ErrUnauthorised) {
		return exitError{exitUnavailable, err}
	} else if err != nil {
		slog.Warn("starting without the worker's board", "err", err)
	}

	levels, release := visualiserLevels(getenv)
	defer release()
	board, err := newBoard(sources{
		sessions: &cachedSessions{src: client, limit: contextLimit(getenv)},
		focus:    newFocus(getenv),
		repos:    client,
		quota:    client,
		levels:   levels,
		media:    mediakeys.Keys{},
	})
	if err != nil {
		return err
	}

	hw, hwErr := coremidi.Open()
	if hwErr == nil {
		defer func() { _ = hw.Close() }()
		go hw.Run()
	}
	if !*preview {
		if hwErr != nil {
			return exitError{exitUnavailable, fmt.Errorf("no Launchpad: %w (--preview draws the board in the terminal)", hwErr)}
		}
		return app.Run(ctx, board, hw, app.Real{})
	}
	return runPreview(ctx, board, hw, hwErr == nil)
}

// runPreview draws board in the terminal until q, mirrored onto hw where one
// is attached. Logs would tear the alternate screen, so they are dropped
// while it shows.
func runPreview(ctx context.Context, board *app.Board, hw *coremidi.Device, attached bool) error {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	term := tui.NewDevice()
	var dev app.Device = term
	if attached {
		dev = app.NewMirror(hw, term)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, board, dev, app.Real{}) }()

	_, err := tea.NewProgram(tui.New(term, board), tea.WithContext(ctx)).Run()
	cancel()
	_ = term.Close()
	runErr := <-done
	switch {
	case err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, context.Canceled):
		return err
	case runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, tui.ErrClosed):
		return runErr
	}
	return nil
}
