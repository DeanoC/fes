//go:build linux

package tenfoot

import (
	"context"

	"github.com/DeanoC/FogCast/ui/localfeed"
)

// nativeKeyboardWatchConnection is a synchronous watcher for the exact
// handle and binding returned together by recovery. The worker owns its
// context and must keep it alive through retirement; this helper creates no
// goroutine and never treats EOF as a neutral acknowledgment.
func (a *App) nativeKeyboardWatchConnection(ctx context.Context, handle localfeed.V2Handle, expected nativeGuestBinding) bool {
	if a == nil || ctx == nil || handle.Done() == nil {
		return false
	}
	done := handle.Done()
	select {
	case <-done:
		return a.nativeKeyboardTransportLost(expected)
	case <-ctx.Done():
		// Prefer a concurrently observed loss to silently abandoning it.
		select {
		case <-done:
			return a.nativeKeyboardTransportLost(expected)
		default:
			return false
		}
	}
}
