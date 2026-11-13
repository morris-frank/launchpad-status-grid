//go:build !darwin || !cgo

package mediakeys

import (
	"context"
	"errors"
	"runtime"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
)

// This is what every build that is not macOS-with-cgo links. It says it is
// unavailable rather than pretending to press a key, because a pad that
// appears to work and never stops the music is a worse failure than one that
// says why.

// ErrUnavailable is returned by every Send on this platform.
var ErrUnavailable = errors.New("mediakeys: media keys need macOS with cgo (built for " + runtime.GOOS + ")")

// Keys implements app.Media.
type Keys struct{}

// Send implements app.Media and always fails here.
func (Keys) Send(context.Context, app.MediaKey) error { return ErrUnavailable }
