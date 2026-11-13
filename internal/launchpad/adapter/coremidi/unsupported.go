//go:build !darwin || !cgo

package coremidi

import (
	"errors"
	"runtime"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/app"
	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// This file is what every build that is not macOS-with-cgo links, and it exists
// so that `go build ./...` and `go vet ./...` pass on the Linux runner without
// anybody having to remember a build tag. It carries the same exported shape as
// the real adapter, so the binary has one code path rather than two.
//
// It is deliberately not a silent no-op device. A Launchpad that appears to
// connect and lights nothing is a worse failure than one that says why.

// ErrNoDevice is returned by Open. On this platform there is no CoreMIDI, so
// there is never a device.
var ErrNoDevice = errors.New("launchpad: the Launchpad adapter needs macOS with cgo (built for " + runtime.GOOS + ")")

// ErrAlreadyOpen exists to match the darwin build's surface. It is never
// returned here, because Open never succeeds.
var ErrAlreadyOpen = errors.New("launchpad: a device is already open in this process")

// Device matches the darwin type's shape. No value of it is ever produced.
type Device struct{}

// Open always fails on this platform.
func Open() (*Device, error) { return nil, ErrNoDevice }

func (*Device) Render(domain.Frame) error { return ErrNoDevice }
func (*Device) Presses() <-chan app.Press { return nil }
func (*Device) Close() error              { return nil }
func (*Device) Run()                      {}
