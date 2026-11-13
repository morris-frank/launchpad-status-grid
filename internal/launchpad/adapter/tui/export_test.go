package tui

import (
	"time"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// WithClock overrides the model's clock, so a test can assert on a frame that
// would otherwise be mid-pulse.
func WithClock(now func() time.Time) Option {
	return func(m *Model) { m.now = now }
}

// WithSize fixes the rendered size, for a frame captured rather than watched.
func WithSize(w, h int) Option {
	return func(m *Model) { m.width, m.height = w, h }
}

// WithFrame starts the preview showing f, so one picture can be rendered
// without a run loop behind it.
func WithFrame(f domain.Frame) Option {
	return func(m *Model) { m.frame = f }
}

// Grid is the surface alone at scale, unindented.
func (m *Model) Grid(scale int) []string { return m.surfaceAt(m.now(), max(1, scale)) }

// The cursor's characters, for the external tests.
const (
	EncloseOpen  = encloseOpen
	EncloseClose = encloseClose
	SelectionBar = selectionBar
)
