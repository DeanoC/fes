package tenfoot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const (
	localPhaseLaunching = "launching"
	localPhaseRunning   = "running"
	localPhaseStopping  = "stopping"

	localInUseCopy       = "In use. Someone else is playing on this machine. You can play when they're done."
	localUnavailableCopy = "This core isn't available right now."
	localStopBudget      = 35 * time.Second
	localChordHold       = time.Second
	// localStatusEvery is the fastest the running-phase status poll runs.
	// The client's own timeout is 3s, so the poll never runs under a.mu.
	localStatusEvery = time.Second
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

// SetKitLocal installs the control client and the pad feed. Both stay nil
// on the host. Call it before Start so the home room sees the client.
func (a *App) SetKitLocal(cores rooms.LocalCores, feed localPadSender) {
	if a == nil {
		return
	}
	a.mu.Lock()
	old := a.localFeed
	a.localCores = cores
	a.localFeed = feed
	a.mu.Unlock()
	if old != nil && feed != old {
		old.Close()
	}
}

func (a *App) localCoreBusyLocked() bool {
	switch a.localPhase {
	case localPhaseLaunching, localPhaseRunning, localPhaseStopping:
		return true
	default:
		return false
	}
}

// localCoreOwnsInput reports that menu commands must not see the pad.
// Launching, running, and stopping all own it.
func (a *App) localCoreOwnsInput() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.localCoreBusyLocked()
}

func (a *App) startLocalCoreLocked(dest rooms.Destination) {
	if a.kitMutationBlockedLocked() {
		return
	}
	if a.localCoreBusyLocked() {
		return
	}
	title := strings.TrimSpace(dest.Label)
	if title == "" {
		title = "core"
	}
	a.localGen++
	gen := a.localGen
	a.localPhase = localPhaseLaunching
	a.localTitle = title
	a.localStatus = "Starting " + title + "…"
	a.status = a.localStatus
	a.localPresentsPaused = true
	a.localChordSince = time.Time{}
	a.localChordFired = false
	block := dest.CoreBlock
	// The room instance is only touched on this lock. Launch can take the
	// agent's full load, so the goroutine calls the client directly with the
	// package id Confirm already accepted. It does not call
	// ActivateDestination, which writes the destination from another thread.
	packageID := dest.PackageID
	client := a.localCores
	go func() {
		var err error
		if client == nil {
			err = rooms.ErrNoLocalCores
		} else {
			err = client.Launch(context.Background(), packageID)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.localGen != gen {
			return
		}
		if err != nil {
			a.failLocalCoreLocked(err, block)
			return
		}
		a.localPhase = localPhaseRunning
		if a.status == a.localStatus {
			a.status = ""
		}
		a.localStatus = ""
		// A pair already held when the core becomes current starts its
		// second here, not during the load.
		if a.localSelectDown && a.localStartDown {
			a.localChordSince = time.Now()
		}
	}()
}

func (a *App) failLocalCoreLocked(err error, block string) {
	a.localPhase = ""
	a.localPresentsPaused = false
	a.localChordSince = time.Time{}
	a.localChordFired = false
	a.localStatus = localCoreFailureCopy(err, block)
	a.status = a.localStatus
	a.roomWasParked = true
}

func localCoreFailureCopy(err error, block string) string {
	switch {
	case errors.Is(err, localcores.ErrInUse):
		return localInUseCopy
	case errors.Is(err, localcores.ErrBlocked):
		if text := strings.TrimSpace(block); text != "" {
			return text
		}
		return localUnavailableCopy
	default:
		return localUnavailableCopy
	}
}

func (a *App) beginLocalStopLocked() {
	if a.localPhase != localPhaseRunning {
		return
	}
	a.localStatusEpoch++
	a.localPhase = localPhaseStopping
	title := a.localTitle
	if title == "" {
		title = "core"
	}
	a.localStatus = "Stopping " + title + "…"
	a.status = a.localStatus
	a.localPresentsPaused = true
	gen := a.localGen
	client := a.localCores
	go func() {
		var err error
		if client == nil {
			err = rooms.ErrNoLocalCores
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), localStopBudget)
			err = client.Stop(ctx)
			cancel()
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.localGen != gen || a.localPhase != localPhaseStopping {
			return
		}
		// Unavailable can mean the core is still up. in_use means the lease is
		// gone (takeover or expiry); staying paused would wedge presents.
		if errors.Is(err, localcores.ErrUnavailable) {
			a.localStatusEpoch++
			a.localPhase = localPhaseRunning
			a.localStatus = localUnavailableCopy
			a.status = a.localStatus
			a.localChordFired = false
			a.localChordSince = time.Time{}
			if a.localSelectDown && a.localStartDown {
				a.localChordSince = time.Now()
			}
			return
		}
		a.finishLocalCoreLocked()
	}()
}

func (a *App) finishLocalCoreLocked() {
	a.localPhase = ""
	a.localPresentsPaused = false
	if a.status == a.localStatus {
		a.status = ""
	}
	a.localStatus = ""
	a.localSelectDown = false
	a.localStartDown = false
	a.localChordSince = time.Time{}
	a.localChordFired = false
	a.localSent = nil
	a.localRedraw++
	a.roomWasParked = true
}

func (a *App) tickLocalCoreLocked(now time.Time) {
	a.pollLocalStatusLocked(now)
	if a.localPhase != localPhaseRunning || a.localChordFired || a.localChordSince.IsZero() {
		return
	}
	if now.Sub(a.localChordSince) < localChordHold {
		return
	}
	a.localChordFired = true
	a.beginLocalStopLocked()
}

// pollLocalStatusLocked reads GET /v1/local/status while a core is running.
// At most one poll is in flight, and it is not called under a.mu: Status can
// take the client's 3s timeout. Idle, or running false, resumes presents.
func (a *App) pollLocalStatusLocked(now time.Time) {
	if a.localPhase != localPhaseRunning || a.localStatusBusy || a.localCores == nil {
		return
	}
	if !a.localStatusNext.IsZero() && now.Before(a.localStatusNext) {
		return
	}
	a.localStatusBusy = true
	a.localStatusNext = now.Add(localStatusEvery)
	gen := a.localGen
	epoch := a.localStatusEpoch
	client := a.localCores
	go func() {
		status, err := client.Status(context.Background())
		a.mu.Lock()
		defer a.mu.Unlock()
		a.localStatusBusy = false
		if err != nil || a.localGen != gen || a.localPhase != localPhaseRunning || a.localStatusEpoch != epoch {
			return
		}
		if status.Phase == "idle" || !status.Running {
			a.finishLocalCoreLocked()
		}
	}()
}

// HandleLocalPad feeds the running core and watches Select+Start.
// It returns false when no local core owns the pad, so the menu keeps the event.
// Launching and stopping consume the pad and do not forward it.
//
// Chord: a lone Select or Start is forwarded. When the second of the pair
// goes down, any of the pair already sent is released immediately and both
// are suppressed while both stay down. Releasing either before one second
// cancels the stop and does not replay the swallowed press. At one second
// Stop runs. The core therefore sees at most a brief press of whichever
// button went down first, not a one-second hold. The one-second mark is
// evaluated on pad events and on Tick; there is no extra timer.
// The event that completes the chord is not forwarded.
func (a *App) HandleLocalPad(e remoteinput.Event, now time.Time) bool {
	if a == nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	a.mu.Lock()
	if !a.localCoreBusyLocked() {
		a.mu.Unlock()
		return false
	}
	if a.localPhase != localPhaseRunning {
		a.noteLocalHeldLocked(e)
		a.mu.Unlock()
		return true
	}
	forward := a.localForwardLocked(e, now)
	if !a.localChordFired && !a.localChordSince.IsZero() && now.Sub(a.localChordSince) >= localChordHold {
		a.localChordFired = true
		a.beginLocalStopLocked()
		// This event crossed the one-second mark. Forwarding it can land on
		// the menu generation Stop is about to install.
		forward = nil
	}
	feed := a.localFeed
	a.mu.Unlock()
	for _, ev := range forward {
		if feed != nil {
			_ = feed.Send(ev, now)
		}
	}
	return true
}

func (a *App) noteLocalHeldLocked(e remoteinput.Event) {
	down, ok := localButtonDown(e)
	if !ok {
		return
	}
	switch e.Code {
	case remoteinput.ButtonSelect:
		a.localSelectDown = down
	case remoteinput.ButtonStart:
		a.localStartDown = down
	}
}

func (a *App) localForwardLocked(e remoteinput.Event, now time.Time) []remoteinput.Event {
	down, pair := localButtonDown(e)
	if !pair || (e.Code != remoteinput.ButtonSelect && e.Code != remoteinput.ButtonStart) {
		return []remoteinput.Event{e}
	}
	if e.Code == remoteinput.ButtonSelect {
		a.localSelectDown = down
	} else {
		a.localStartDown = down
	}
	if a.localSelectDown && a.localStartDown {
		if a.localChordSince.IsZero() {
			a.localChordSince = now
		}
		var out []remoteinput.Event
		out = a.releaseSentLocked(remoteinput.ButtonSelect, e.Player, out)
		out = a.releaseSentLocked(remoteinput.ButtonStart, e.Player, out)
		return out
	}
	a.localChordSince = time.Time{}
	a.localChordFired = false
	if !down {
		if a.localSent[e.Code] {
			delete(a.localSent, e.Code)
			return []remoteinput.Event{e}
		}
		return nil
	}
	if a.localSent == nil {
		a.localSent = map[remoteinput.Code]bool{}
	}
	a.localSent[e.Code] = true
	return []remoteinput.Event{e}
}

func (a *App) releaseSentLocked(code remoteinput.Code, player uint8, out []remoteinput.Event) []remoteinput.Event {
	if !a.localSent[code] {
		return out
	}
	delete(a.localSent, code)
	return append(out, remoteinput.Event{
		Player: player,
		Device: remoteinput.DeviceGamepad,
		Kind:   remoteinput.KindButton,
		Action: remoteinput.ActionRelease,
		Code:   code,
	})
}

func localButtonDown(e remoteinput.Event) (down bool, ok bool) {
	if e.Kind != remoteinput.KindButton {
		return false, false
	}
	switch e.Action {
	case remoteinput.ActionPress:
		return true, true
	case remoteinput.ActionRelease:
		return false, true
	default:
		return false, false
	}
}
