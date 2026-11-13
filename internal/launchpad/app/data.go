package app

import (
	"context"
	"sync"
)

// Data is a screen's cached view of something a port has to go and fetch.
//
// It exists because Render is called several times a second and must not do
// IO, while the sources behind these screens are directory walks. Refresh runs
// on the board's slow cadence and on a screen becoming active; Render reads
// whatever was last fetched, including while a fetch is in flight.
//
// A failed refresh keeps the last good value and records the error alongside
// it. A screen that blanked on a transient failure would flicker its whole
// grid every time a directory was busy, and a blank board is indistinguishable
// from "nothing is running" — which is the one wrong answer available here.
type Data[T any] struct {
	load func(context.Context) (T, error)

	mu    sync.RWMutex
	value T
	err   error
	got   bool
}

// NewData caches load.
func NewData[T any](load func(context.Context) (T, error)) *Data[T] {
	return &Data[T]{load: load}
}

// Refresh fetches once.
func (d *Data[T]) Refresh(ctx context.Context) error {
	v, err := d.load(ctx)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
	if err == nil {
		d.value, d.got = v, true
	}
	return err
}

// Value is the last good fetch, whether one has happened, and the most recent
// error. All three, because "no data yet" and "data, and the last refresh
// failed" are different things for a board to draw.
func (d *Data[T]) Value() (v T, loaded bool, err error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.value, d.got, d.err
}
