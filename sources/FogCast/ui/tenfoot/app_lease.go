package tenfoot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

// releaseOwnedIdleLease posts the shell-exit release when this shell still
// owns a retained idle grant or a Soft-stop response was lost. The local
// session may already show another play; that does not skip the release.
// A surviving play is not stopped: that path posts release_idle. A confirmed
// idle service posts an empty-body Stop. The pending flag clears only after
// that release succeeds.
func (a *App) releaseOwnedIdleLease() {
	if a == nil {
		return
	}
	a.mu.Lock()
	client := a.client
	release := a.retainedIdleLease || a.stopResponseLost
	var stamp ClientStamp
	if release {
		stamp = a.clientStampLocked()
	}
	a.mu.Unlock()
	if !release || client == nil {
		return
	}
	if releaseIdleLeaseNow(client, stamp) {
		a.mu.Lock()
		a.clearRetainedLeasesLocked()
		a.stopResponseLost = false
		a.mu.Unlock()
	}
}

// startReleaseIdleLeaseLocked releases a kit this shell kept after returning
// to the menu. The menu stays open. A running game is left owned.
// The idle read and the Stop are separate steps. Launches stay closed until
// the goroutine finishes, including after settings closes, so that Stop
// cannot end a game that started in between.
func (a *App) startReleaseIdleLeaseLocked() {
	if a.leaseReleaseBusy {
		return
	}
	if a.session.State == "active" || a.session.State == "launching" || a.launch.Phase == "launching" || a.stopPhase == "stopping" {
		a.settingsStatus = "A running game keeps the kit"
		a.status = a.settingsStatus
		return
	}
	if !a.retainedIdleLease && !a.stopResponseLost {
		a.settingsStatus = "The kit is already free"
		a.status = a.settingsStatus
		return
	}
	client := a.client
	if client == nil {
		a.settingsStatus = "The kit is already free"
		a.status = a.settingsStatus
		return
	}
	stamp := a.clientStampLocked()
	a.leaseReleaseBusy = true
	a.settingsStatus = "Releasing the kit"
	a.status = a.settingsStatus
	go func() {
		ok := releaseIdleLeaseNow(client, stamp)
		a.mu.Lock()
		defer a.mu.Unlock()
		a.leaseReleaseBusy = false
		if !ok {
			if a.settingsOpen {
				a.settingsStatus = "Could not release the kit"
				a.status = a.settingsStatus
			}
			return
		}
		a.clearRetainedLeasesLocked()
		a.stopResponseLost = false
		if a.settingsOpen {
			a.settingsStatus = "Kit released"
			a.status = "Kit released. The menu stays open."
		}
	}()
}

func releaseIdleLeaseNow(client *Client, stamp ClientStamp) bool {
	if client == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	view := observeLeaseExit(ctx, client)
	cancel()
	var releaseFn func(*Client, context.Context, ClientStamp) (hostclient.SessionResult, error)
	switch view {
	case leaseExitIdle:
		releaseFn = (*Client).ReleaseIdleLease
	default:
		// A surviving play, or a service we could not read. release_idle does
		// not stop the foreground. An empty-body Stop would.
		releaseFn = (*Client).ReleaseIdleGrants
	}
	return postLeaseRelease(client, stamp, releaseFn)
}

type leaseExitView int

const (
	leaseExitUnknown leaseExitView = iota
	leaseExitIdle
	leaseExitPlaySurvives
)

func observeLeaseExit(ctx context.Context, client *Client) leaseExitView {
	if client == nil {
		return leaseExitUnknown
	}
	session, err := client.Session(ctx)
	if err != nil || session.ErrorCode != "" {
		return leaseExitUnknown
	}
	count, err := client.survivingPlayCount(ctx)
	switch {
	case err == nil && count > 0:
		return leaseExitPlaySurvives
	case err != nil && !errors.Is(err, errSessionsUnsupported):
		return leaseExitUnknown
	}
	if !idleRetainedSession(session.State) {
		return leaseExitPlaySurvives
	}
	return leaseExitIdle
}

func postLeaseRelease(client *Client, stamp ClientStamp, release func(*Client, context.Context, ClientStamp) (hostclient.SessionResult, error)) bool {
	if client == nil || release == nil {
		return false
	}
	for attempt := 0; attempt < shellLeaseReleaseAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		result, err := release(client, ctx, stamp)
		cancel()
		if err == nil && strings.TrimSpace(result.ErrorCode) == "" {
			return true
		}
	}
	return false
}

func idleRetainedSession(state string) bool {
	switch strings.TrimSpace(state) {
	case "", "idle":
		return true
	default:
		return false
	}
}
