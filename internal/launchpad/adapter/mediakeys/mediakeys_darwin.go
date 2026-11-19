//go:build darwin && cgo

// Package mediakeys presses the system play/pause and next-track keys, as the
// keyboard's own media keys would, for the Launchpad's media pads.
//
// It is our own twenty lines of cgo rather than robotgo, which would bring a
// whole input-automation library to post two events. The events are
// NX_SYSDEFINED aux-control-button events built by AppKit and posted with
// CGEventPost, which macOS delivers only for a process holding the
// Accessibility permission — so an untrusted process is refused here, with a
// sentence, rather than posting an event the system silently drops.
//
// Like internal/transform/dictate's hotkey adapter this file is never compiled
// on the Linux runner; the release workflow's darwin leg asks the built asset
// for AppKit instead.
package mediakeys

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit -framework ApplicationServices
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#import <IOKit/hidsystem/ev_keymap.h>

static int cTrusted(void) { return AXIsProcessTrusted(); }

// A media key is a key-down then a key-up, each an NSSystemDefined event of
// subtype 8 carrying the key in data1's high half and the state in its flags.
static void cPost(int key) {
	@autoreleasepool {
		for (int down = 1; down >= 0; down--) {
			int flags = down ? 0xa00 : 0xb00;
			NSEvent *ev = [NSEvent otherEventWithType:NSEventTypeSystemDefined location:NSZeroPoint
				modifierFlags:flags timestamp:0 windowNumber:0 context:nil
				subtype:8 data1:((key << 16) | flags) data2:-1];
			CGEventPost(kCGHIDEventTap, [ev CGEvent]);
		}
	}
}
*/
import "C"

import (
	"context"
	"errors"

	"github.com/morris-frank/launchpad-status-grid/internal/launchpad/app"
)

// ErrUnavailable is returned when this process may not post a media key.
var ErrUnavailable = errors.New("mediakeys: launchpad-status-grid needs the Accessibility permission to press media keys (System Settings → Privacy & Security → Accessibility)")

// Keys implements app.Media.
type Keys struct{}

// Send implements app.Media.
func (Keys) Send(_ context.Context, k app.MediaKey) error {
	if C.cTrusted() == 0 {
		return ErrUnavailable
	}
	code := C.NX_KEYTYPE_PLAY
	if k == app.NextTrack {
		code = C.NX_KEYTYPE_NEXT
	}
	C.cPost(C.int(code))
	return nil
}
