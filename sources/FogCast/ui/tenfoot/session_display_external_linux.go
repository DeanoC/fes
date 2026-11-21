//go:build linux

package tenfoot

import (
	"context"
	"fmt"
	"os"

	"github.com/DeanoC/FogCast/internal/sessiondisplayipc"
)

// startKitDisplayIPC publishes the launcher before it starts processing input.
// The socket's publication lock is shared with a headless target mutation.
func (a *App) startKitDisplayIPC(parent context.Context) (context.CancelFunc, error) {
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("kit launcher display control requires root")
	}
	ctx, cancel := context.WithCancel(parent)
	ready := make(chan error, 1)
	failure := make(chan error, 1)
	go func() {
		err := sessiondisplayipc.ListenLauncher(ctx, sessiondisplayipc.LauncherSocket, 0, a.prepareExternalSessionDisplay, ready)
		if ctx.Err() == nil {
			if err == nil {
				err = fmt.Errorf("launcher display listener stopped")
			}
			failure <- err
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			cancel()
			return nil, err
		}
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
	a.mu.Lock()
	a.displayIPC = true
	a.displayIPCFailure = failure
	a.mu.Unlock()
	return cancel, nil
}
