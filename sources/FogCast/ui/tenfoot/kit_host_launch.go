package tenfoot

import (
	"context"
	"strings"
	"time"
)

// A generated host entry is deliberately absent from ordinary library pages.
// Resolve its display name after reattachment without holding the UI lock.
func (a *App) resolveKitHostSessionTitleLocked() {
	if !a.kitHost || a.client == nil || a.session.State != "active" || a.session.GameID == "" || (a.launch.GameID == a.session.GameID && strings.TrimSpace(a.sessionTitle) != "") || a.sessionTitlePending || a.resolvedSessionGameID == a.session.GameID || time.Now().Before(a.sessionTitleNext) {
		return
	}
	for _, game := range a.games {
		if game.ID == a.session.GameID {
			return
		}
	}
	prior := a.session
	a.sessionTitleRequest++
	request := a.sessionTitleRequest
	a.sessionTitlePending = true
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()
		game, err := a.client.Game(ctx, prior.GameID)
		a.mu.Lock()
		defer a.mu.Unlock()
		if request != a.sessionTitleRequest || !samePlayHIDSession(a.session, prior) {
			return
		}
		a.sessionTitlePending = false
		if err != nil || game.ID != prior.GameID || strings.TrimSpace(displayGameTitle(game)) == "" {
			a.sessionTitleNext = time.Now().Add(time.Second)
			return
		}
		a.resolvedSessionGameID = game.ID
		a.resolvedSessionTitle = displayGameTitle(game)
	}()
}

// startKitHostEntryLocked prepares a host-owned machine or cartridge entry
// before using the normal host session path. The room lock is never held while
// content is imported or the host checks the package.
func (a *App) startKitHostEntryLocked(packageID, gameID, label string) {
	if a.hostAwaitStatus || a.kitHostPreparing || a.launch.Phase == "launching" || a.sessionStopOfferedLocked() || a.localCoreBusyLocked() || a.localLaunchBlockedLocked() {
		return
	}
	a.kitHostPreparing = true
	a.status = "Preparing " + label + "…"
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		entry, err := a.client.PrepareLocalCoreEntry(ctx, packageID, gameID)
		if err == nil {
			game, readErr := a.client.Game(ctx, entry.GameID)
			err = readErr
			if err == nil {
				a.mu.Lock()
				defer a.mu.Unlock()
				a.kitHostPreparing = false
				a.status = ""
				a.startLaunchGameLocked(game)
				return
			}
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		a.kitHostPreparing = false
		a.status = "Could not prepare " + label + ": " + err.Error()
	}()
}
