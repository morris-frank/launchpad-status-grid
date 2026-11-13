//go:build !darwin || !cgo

package liveaudio

import (
	"errors"
	"runtime"
)

// What every build that is not macOS-with-cgo links, for the reason
// coremidi's stub gives: `go build ./...` passes on the Linux runner and
// the binary has one code path. It starts nothing, and says why, so the
// board draws its fallback rather than a live source that hears nothing.

// ErrNoCapture is returned by Open on this platform.
var ErrNoCapture = errors.New("liveaudio: live audio needs macOS with cgo (built for " + runtime.GOOS + ")")

func start(Source) (capture, error) { return nil, ErrNoCapture }
