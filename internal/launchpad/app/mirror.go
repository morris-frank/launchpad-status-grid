package app

import (
	"errors"
	"sync"

	"github.com/Soilytix/launchpad-status-grid/internal/launchpad/domain"
)

// Mirror is one board on several surfaces at once: every frame goes to all of
// them and a press from any of them reaches the board.
//
// It exists because the hardware and the terminal preview were an either/or,
// and that made the preview useless for the thing it is best at. Eighty-one
// lights are unreadable until you have learnt the colours, and the surface that
// can spell them out in words was only available when the device was absent —
// so the moment you plugged in the Launchpad you lost the only thing that could
// tell you what you were looking at. Run both and the terminal becomes a twin:
// press a pad with your thumb and the terminal says what you just pressed.
//
// It is itself a Device, so nothing above it knows there is more than one
// surface, and a third one would be a longer argument list and no other change.
// The watch a launch hosts drives its pads tab through one.
type Mirror struct {
	devs    []Device
	presses chan Press

	closeOnce sync.Once
}

// NewMirror drives devs as a single surface.
func NewMirror(devs ...Device) *Mirror {
	m := &Mirror{devs: devs, presses: make(chan Press, 64)}

	// One forwarder per surface, and the merged channel closes only once every
	// one of them has. A device that has lost its hardware blocks rather than
	// closing, which is the contract Device states, so in practice this closes
	// when the terminal is quit and never because a cable moved.
	var wg sync.WaitGroup
	for _, d := range devs {
		wg.Add(1)
		go func(d Device) {
			defer wg.Done()
			for p := range d.Presses() {
				m.presses <- p
			}
		}(d)
	}
	go func() {
		wg.Wait()
		close(m.presses)
	}()
	return m
}

// Render draws the frame on every surface.
//
// A failure on one surface is not the board's failure. The Device contract
// already says a Launchpad being unplugged is not the board being over, and a
// mirror that propagated the first error would break exactly that: pull the
// cable and the terminal you were reading would exit with it. So an error is
// returned only when there is no surface left that took the frame.
func (m *Mirror) Render(f domain.Frame) error {
	var failed []error
	for _, d := range m.devs {
		if err := d.Render(f); err != nil {
			failed = append(failed, err)
		}
	}
	if len(failed) == len(m.devs) {
		return errors.Join(failed...)
	}
	return nil
}

// Presses implements Device, yielding the presses of every surface as one
// stream. Which surface a press came from is deliberately not carried: a board
// that could tell a thumb from a keystroke would eventually behave differently
// for the two, and then the preview would have stopped being a preview.
func (m *Mirror) Presses() <-chan Press { return m.presses }

// Close closes every surface, keeping going past a failure so that one surface
// refusing to shut down cannot leave the others lit.
func (m *Mirror) Close() error {
	var err error
	m.closeOnce.Do(func() {
		var failed []error
		for _, d := range m.devs {
			if e := d.Close(); e != nil {
				failed = append(failed, e)
			}
		}
		err = errors.Join(failed...)
	})
	return err
}
