package tenfoot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const (
	localCoreMissingCopy    = "The Master System core is not installed."
	localCoreMissingAction  = "Install the Master System core."
	localCoreCheckingCopy   = "Checking whether the Master System core is installed."
	localCoreCheckingAction = "Wait — still checking."
	localInUseCopy          = rooms.InUseStatus + ". " + rooms.InUseDetail
	machineStatusUnknown    = "Can't tell if this machine is free."
	savedListOfflineCopy    = "Offline, showing your saved list"
	shelfEmptyCopy          = "No games in this library yet."
	shelfMissingCopy        = "The game files for this library can't be found."
	sessionStopChordHold    = time.Second
)

// localPadSender is the kit-local input socket. *localfeed.Feed implements it.
type localPadSender interface {
	Send(remoteinput.Event, time.Time) error
	Close()
}

// EnableKitLocal connects the default local-control and local-input sockets.
// Run calls it only for -gfx menu-display, which is how the kit image starts
// tenfoot. A host session does not.
func (a *App) EnableKitLocal() {
	if a == nil {
		return
	}
	a.SetKitLocal(localcores.NewClient(localcores.DefaultSocket), localfeed.New(localfeed.DefaultSocket, nil))
}

// hostKitCores reads room inventory from the kit host package store.
type hostKitCores struct {
	host *hostclient.Client
}

func (c *hostKitCores) List(ctx context.Context) ([]localcores.Core, error) {
	cores, err := c.host.KitLocalCores(ctx)
	if err != nil {
		return nil, errors.Join(localcores.ErrUnavailable, err)
	}
	return cores, nil
}

// EnableKitHost uses the local host for inventory and the local input socket
// for the kit display.
func (a *App) EnableKitHost() {
	if a == nil || a.client == nil {
		return
	}
	a.SetKitLocal(&hostKitCores{host: a.client.Client}, localfeed.New(localfeed.DefaultSocket, nil))
}

// SetKitLocal installs the control client and the pad feed. Both stay nil
// on the host. Call it before Start so the home room sees the client.
// The installed-core list is read here, outside the room lock. A failed
// list keeps Play hidden and retries until one list succeeds.
func (a *App) SetKitLocal(cores rooms.LocalCores, feed localPadSender) {
	if a == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	old := a.localFeed
	previous := a.localInstallCancel
	a.localInstallGen++
	gen := a.localInstallGen
	a.localInstallCancel = cancel
	a.localCores = cores
	a.localFeed = feed
	a.localInstallKnown = false
	a.localInstalled = nil
	a.mu.Unlock()
	if previous != nil {
		previous()
	}
	if old != nil && feed != old {
		old.Close()
	}
	if cores == nil {
		return
	}
	listed, err := cores.List(ctx)
	if err == nil {
		a.rememberInstalled(cores, gen, listed)
		return
	}
	if ctx.Err() != nil {
		return
	}
	go a.retryLocalInstall(ctx, cores, gen)
}

func (a *App) retryLocalInstall(ctx context.Context, cores rooms.LocalCores, gen uint64) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			listed, err := cores.List(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				continue
			}
			a.rememberInstalled(cores, gen, listed)
			return
		}
	}
}

// A successful empty inventory during startup is only a snapshot. Refresh
// when the kit's target becomes ready, including the first health observation,
// without adding an inventory poll while it stays ready. Suspended Home rooms
// refresh through their normal on_resume callback when they become visible.
func (a *App) refreshKitInventoryLocked(parent context.Context) {
	if cores := a.localCores; cores != nil {
		if a.localInstallCancel != nil {
			a.localInstallCancel()
		}
		ctx, cancel := context.WithCancel(parent)
		a.localInstallCancel = cancel
		a.localInstallGen++
		gen := a.localInstallGen
		a.localInstallKnown = false
		a.localInstalled = nil
		go func() {
			listed, err := cores.List(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				a.retryLocalInstall(ctx, cores, gen)
				return
			}
			a.rememberInstalled(cores, gen, listed)
		}()
	}
	if a.room != nil && a.room.ID() == installedCoresHomeID {
		a.room.Resume()
	}
}

func (a *App) rememberInstalled(cores rooms.LocalCores, gen uint64, listed []localcores.Core) {
	ids := make(map[string]struct{}, len(listed))
	for _, core := range listed {
		id := strings.TrimSpace(core.CoreID)
		if id != "" {
			ids[id] = struct{}{}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.localCores != cores || a.localInstallGen != gen {
		return
	}
	a.localInstalled = ids
	a.localInstallKnown = true
}

// HandleLocalPad sends physical pad events to the active host session.
func (a *App) HandleLocalPad(e remoteinput.Event, now time.Time) bool {
	if a == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	a.mu.Lock()
	if a.kitHost && (a.hostAwaitStatus || a.kitHostPreparing) {
		a.mu.Unlock()
		return true
	}
	handled, forward := a.sessionPadLocked(e, now)
	feed := a.localFeed
	a.mu.Unlock()
	for _, event := range forward {
		if feed != nil {
			_ = feed.Send(event, now)
		}
	}
	return handled
}
