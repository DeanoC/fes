package tenfoot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/corenames"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/localfeed"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const (
	localPhaseLaunching = "launching"
	localPhaseRunning   = "running"
	localPhaseStopping  = "stopping"

	localInUseCopy          = rooms.InUseStatus + ". " + rooms.InUseDetail
	machineStatusUnknown    = "Can't tell if this machine is free."
	savedListOfflineCopy    = "Offline, showing your saved list"
	shelfEmptyCopy          = "No games in this library yet."
	shelfMissingCopy        = "The game files for this library can't be found."
	localUnavailableCopy    = "This core isn't available right now."
	localCoreMissingCopy    = "The Master System core is not installed."
	localCoreMissingAction  = "Install the Master System core."
	localCoreCheckingCopy   = "Checking whether the Master System core is installed."
	localCoreCheckingAction = "Wait — still checking."
	// localStopBudget outlives the local client's stop POST (runtime stop
	// plus lease cleanup, 65s) and its short status reconcile. A lost stop
	// that is still revoking stays ErrUnavailable, so presents stay paused.
	localStopBudget = 75 * time.Second
	localChordHold  = time.Second
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

// hostKitCores keeps the kit control socket for the remaining local actions,
// but reads room inventory from the kit host's package store.
type hostKitCores struct {
	*localcores.Client
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
// for the kit display. Local-control operations remain available for the
// legacy direct-launch path until that path is removed.
func (a *App) EnableKitHost() {
	if a == nil || a.client == nil {
		return
	}
	a.SetKitLocal(&hostKitCores{Client: localcores.NewClient(localcores.DefaultSocket), host: a.client.Client}, localfeed.New(localfeed.DefaultSocket, nil))
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
			if err != nil {
				continue
			}
			a.rememberInstalled(cores, gen, listed)
			return
		}
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

func (a *App) localCoreBusyLocked() bool {
	switch a.localPhase {
	case localPhaseLaunching, localPhaseRunning, localPhaseStopping:
		return true
	default:
		return false
	}
}

const localLaunchCheckingCopy = "Still checking whether the previous game started."

func (a *App) localLaunchBlockedLocked() bool {
	return a.localReconcileAfterFailure
}

// RequestLocalStop is the single-press exit from a kit-local core: keyboard
// F12 or a pad's Guide button. Neither is ever forwarded to a core, so no
// game loses a control. The menu cannot draw while the core owns HDMI, so
// there is no confirmation. It matches the Select+Start hold: during the
// load the stop waits until the core is running, and repeats do nothing.
// It reports whether a kit-local core owned the press.
func (a *App) RequestLocalStop() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.localUnownedRun {
		// Another launch owns this core; this shell has no generation to stop.
		return true
	}
	switch a.localPhase {
	case localPhaseLaunching:
		a.localStopAfterStart = true
		return true
	case localPhaseRunning:
		a.localChordFired = true
		a.beginLocalStopLocked()
		return true
	case localPhaseStopping:
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

func (a *App) completeLocalLaunchHandoff(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if launch := a.hostLaunchPending; launch != nil {
		a.hostLaunchPending = nil
		if err != nil {
			a.hostPresentsPaused = false
			a.launch.Phase = "error"
			a.launch.Message = "display handoff failed: " + err.Error()
			return
		}
		launch()
		return
	}

}

// holdUnownedLocalRunLocked drops Stop ownership of a replacement that is
// still running. Presents stay paused and the pad stays off the menu until
// status is idle with an empty generation. There is no time limit.
func (a *App) holdUnownedLocalRunLocked() {
	a.localUnownedRun = true
	a.localWatchStartup = false
	a.localLeaseGeneration = ""
	a.localPackageID = ""
	a.localStopAfterStart = false
	a.localChordSince = time.Time{}
	a.localChordFired = false
	a.localPair = chordPairs{}
	a.localSent = nil
	a.localPhase = localPhaseLaunching
	a.localReconcileAfterFailure = true
	a.localPresentsPaused = true
	a.localStatus = localInUseCopy
	a.status = a.localStatus
	a.localStatusNext = time.Time{}
	a.localRedraw++
}

func (a *App) beginLocalStopLocked() {
	if a.localPhase != localPhaseRunning || !validLocalGeneration(a.localLeaseGeneration) {
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
	leaseGeneration := a.localLeaseGeneration
	client := a.localCores
	go func() {
		var err error
		if client == nil {
			err = rooms.ErrNoLocalCores
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), localStopBudget)
			err = client.Stop(ctx, leaseGeneration)
			cancel()
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.localGen != gen || a.localLeaseGeneration != leaseGeneration || a.localPhase != localPhaseStopping {
			return
		}
		// Unavailable can mean the core is still up, or that stop's response
		// was lost while the lease was still revoking. in_use means the lease
		// is gone (takeover or expiry); staying paused would wedge presents.
		if errors.Is(err, localcores.ErrUnavailable) {
			a.localStatusEpoch++
			a.localPhase = localPhaseRunning
			a.localStatus = localUnavailableCopy
			a.status = a.localStatus
			a.localChordFired = false
			a.localChordSince = time.Time{}
			if a.localPair.anyHeld() {
				a.localChordSince = time.Now()
			}
			return
		}
		a.finishLocalCoreLocked()
	}()
}

func validLocalGeneration(id string) bool {
	return len(id) == 64 && strings.Trim(id, "0123456789abcdef") == ""
}

func (a *App) finishLocalCoreLocked() {
	a.localLeaseGeneration = ""
	a.localPackageID = ""
	a.localReconcileAfterFailure = false
	a.localUnownedRun = false
	a.localWatchStartup = false
	a.localPhase = ""
	a.localPresentsPaused = false
	a.localLateAdopt = false
	if a.status == a.localStatus {
		a.status = ""
	}
	a.localStatus = ""
	a.localPair = chordPairs{}
	a.localChordSince = time.Time{}
	a.localChordFired = false
	a.localStopAfterStart = false
	a.localSent = nil
	a.localRedraw++
	a.roomWasParked = true
}

func (a *App) tickLocalCoreLocked(now time.Time) {
	a.pollLocalStatusLocked(now)

	if (a.localPhase != localPhaseRunning && a.localPhase != localPhaseLaunching) || a.localChordFired || a.localChordSince.IsZero() {
		return
	}
	if now.Sub(a.localChordSince) < localChordHold {
		return
	}
	a.localChordFired = true
	if a.localPhase == localPhaseLaunching {
		a.localStopAfterStart = true
	} else {
		a.beginLocalStopLocked()
	}
}

// localStatusPollWantedLocked is true while this process owns a running
// core, while a startup launching or stopping generation is still unsettled,
// while an unowned core needs a status read, or until startup has
// observed one successful status.
func (a *App) localStatusPollWantedLocked() bool {
	if a.localStatusBusy || a.localCores == nil {
		return false
	}
	if a.localPhase == localPhaseRunning {
		return true
	}
	if a.localWatchStartup && (a.localPhase == localPhaseLaunching || a.localPhase == localPhaseStopping) {
		return true
	}
	if a.localReconcileAfterFailure && (a.localPhase == localPhaseLaunching || a.localPhase == "") {
		return true
	}
	return !a.localStartupStatusChecked && a.localPhase == "" && !a.localUnownedRun
}

// localStatusStoppable reports a running generation this process can stop.
// Launching is not stoppable: the agent refuses Stop until the phase is
// running, and that refusal drops the claim.
func localStatusStoppable(status localcores.RunStatus) bool {
	return status.Phase == "running" && validLocalGeneration(status.Generation)
}

// adoptStartupLocalRunLocked takes a running generation as this launcher's
// session. Menu presents stay paused. Late adopt drains outstanding menu
// work and skips the stray menu frame, because the core already owns HDMI.
// An older menu failure does not keep the menu presenting. Select+Start
// stops that generation. The In use copy is the session status. It does not
// claim this process started the run.
func (a *App) adoptStartupLocalRunLocked(status localcores.RunStatus) {
	a.applyStartupClaimLocked(status, localPhaseRunning)
	a.localWatchStartup = false
	a.localStopAfterStart = false
	a.localChordSince = time.Time{}
	a.localChordFired = false
	a.localPair = chordPairs{}
	a.localStatus = localInUseCopy
	a.status = a.localStatus
}

// observeStartupPendingLocked remembers a launching or stopping generation
// and keeps reading status. Stop waits until that generation is running.
func (a *App) observeStartupPendingLocked(status localcores.RunStatus) {
	phase := localPhaseLaunching
	label := "Starting "
	if status.Phase == "stopping" {
		phase = localPhaseStopping
		label = "Stopping "
	}
	a.applyStartupClaimLocked(status, phase)
	a.localWatchStartup = true
	a.localStopAfterStart = false
	a.localChordSince = time.Time{}
	a.localChordFired = false
	a.localPair = chordPairs{}
	a.localStatus = label + a.localTitle + "…"
	a.status = a.localStatus
}

func (a *App) applyStartupClaimLocked(status localcores.RunStatus, phase string) {
	title := corenames.Name(status.CoreID, status.CoreID)
	if title == "" {
		title = "core"
	}
	a.localTitle = title
	a.localLeaseGeneration = status.Generation
	a.localPackageID = status.PackageID
	a.localPhase = phase
	a.localUnownedRun = false
	a.localReconcileAfterFailure = false
	a.localPresentsPaused = true
	a.localLateAdopt = true
	a.localStartedAt = time.Time{}
	a.localSent = nil
	a.localStatusNext = time.Time{}
	a.localRedraw++
}

// promoteWatchedStartupLocked is the running answer for a generation this
// process is already watching. A Select+Start that arrived while the agent
// was still launching posts Stop now.
func (a *App) promoteWatchedStartupLocked(status localcores.RunStatus) {
	stopNow := a.localStopAfterStart
	a.applyStartupClaimLocked(status, localPhaseRunning)
	a.localWatchStartup = false
	a.localStatus = localInUseCopy
	a.status = a.localStatus
	if stopNow {
		a.localStopAfterStart = false
		a.localChordFired = true
		a.beginLocalStopLocked()
		return
	}
	// Loading time does not count toward the one-second hold. A chord that
	// already completed is stopNow above; a pair still held starts again.
	if a.localPair.anyHeld() {
		a.localChordSince = time.Now()
	}
}

func (a *App) startupClaimMatchesLocked(status localcores.RunStatus) bool {
	return validLocalGeneration(status.Generation) &&
		status.Generation == a.localLeaseGeneration &&
		status.PackageID == a.localPackageID
}

func (a *App) settleStartupStatusLocked(status localcores.RunStatus) {
	switch status.Phase {
	case "launching", "stopping":
		if validLocalGeneration(status.Generation) {
			a.localStartupStatusChecked = true
			a.observeStartupPendingLocked(status)
			return
		}
	case "running":
		if localStatusStoppable(status) {
			a.localStartupStatusChecked = true
			a.adoptStartupLocalRunLocked(status)
			return
		}
	}
	a.localStartupStatusChecked = true
}

func (a *App) settleWatchedStartupLocked(status localcores.RunStatus) {
	switch status.Phase {
	case "idle":
		if status.Generation == "" {
			a.localWatchStartup = false
			a.finishLocalCoreLocked()
		}
		return
	case "running":
		if !validLocalGeneration(status.Generation) {
			return
		}
		if !a.startupClaimMatchesLocked(status) {
			a.localWatchStartup = false
			a.holdUnownedLocalRunLocked()
			return
		}
		a.promoteWatchedStartupLocked(status)
		return
	case "launching", "stopping":
		if !validLocalGeneration(status.Generation) {
			return
		}
		if !a.startupClaimMatchesLocked(status) {
			a.localWatchStartup = false
			a.holdUnownedLocalRunLocked()
			return
		}
		if status.Phase == "stopping" && a.localPhase != localPhaseStopping {
			a.localPhase = localPhaseStopping
			a.localStatus = "Stopping " + a.localTitle + "…"
			a.status = a.localStatus
			a.localRedraw++
		}
		if status.Phase == "launching" && a.localPhase != localPhaseLaunching {
			a.localPhase = localPhaseLaunching
			a.localStatus = "Starting " + a.localTitle + "…"
			a.status = a.localStatus
			a.localRedraw++
		}
	}
}

// pollLocalStatusLocked reads GET /v1/local/status while a core is running,
// while a startup launching or stopping generation is still unsettled, while
// a failed launch is still unreconciled, and once at startup before this
// process has observed status. At most one poll is in flight, and it is not
// called under a.mu: Status can take the client's 3s timeout. Only an
// explicit idle phase resumes presents.
func (a *App) pollLocalStatusLocked(now time.Time) {
	if !a.localStatusPollWantedLocked() {
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
		if err != nil || a.localGen != gen || a.localStatusEpoch != epoch {
			return
		}
		ownsRunning := a.localPhase == localPhaseRunning
		reconciling := a.localReconcileAfterFailure && (a.localPhase == localPhaseLaunching || a.localPhase == "")
		startup := !a.localStartupStatusChecked && a.localPhase == "" && !a.localReconcileAfterFailure && !a.localUnownedRun
		watching := a.localWatchStartup && (a.localPhase == localPhaseLaunching || a.localPhase == localPhaseStopping)
		if !ownsRunning && !reconciling && !startup && !watching {
			return
		}
		if startup {
			a.settleStartupStatusLocked(status)
			return
		}
		if watching {
			a.settleWatchedStartupLocked(status)
			return
		}
		// An unavailable launch has no grant identity. A later running status
		// may belong to another local launch, even for the same package.
		// A confirmed replacement stays held until generation-free idle: the
		// failed-launch deadline must not hand the pad back to the menu.
		if a.localUnownedRun {
			if status.Phase == "idle" && status.Generation == "" {
				a.finishLocalCoreLocked()
			}
			return
		}
		if status.Phase == "running" && a.localPhase == localPhaseRunning &&
			(status.Generation != a.localLeaseGeneration || status.PackageID != a.localPackageID) {
			a.holdUnownedLocalRunLocked()
			return
		}
		if status.Phase == "idle" {
			wasAmbiguous := a.localReconcileAfterFailure
			a.localReconcileAfterFailure = false
			if wasAmbiguous {
				a.localPhase = ""
				a.localPresentsPaused = false
				a.localLateAdopt = false
				a.localStartedAt = time.Time{}
				a.localPair = chordPairs{}
				a.localSent = nil
				a.localRedraw++
				a.roomWasParked = true
				return
			}
			a.finishLocalCoreLocked()
		}
	}()
}

// HandleLocalPad feeds the running core and watches Select+Start.
// It returns false when no local core owns the pad, so the menu keeps the event.
// Launching and stopping consume the pad and do not forward it.
//
// Chord: Select and Start must be on the same pad. A lone Select or Start is
// forwarded. When the second of the pair goes down, any of the pair already
// sent is released immediately and both are suppressed while both stay down.
// Releasing either before one second cancels the stop and does not replay
// the swallowed press. At one second
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
	if a.kitHost && (a.hostAwaitStatus || a.kitHostPreparing) {
		a.mu.Unlock()
		return true
	}
	if a.localUnownedRun {
		feed := a.localFeed
		a.mu.Unlock()
		if feed != nil {
			_ = feed.Send(e, now)
		}
		return true
	}
	if !a.localCoreBusyLocked() {
		handled, forward := a.sessionPadLocked(e, now)
		feed := a.localFeed
		a.mu.Unlock()
		for _, ev := range forward {
			if feed != nil {
				_ = feed.Send(ev, now)
			}
		}
		return handled
	}
	if a.localPhase != localPhaseRunning {
		a.noteLocalHeldLocked(e, now)
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

func (a *App) noteLocalHeldLocked(e remoteinput.Event, now time.Time) {
	down, ok := localButtonDown(e)
	if !ok {
		return
	}
	if !a.localPair.press(e.Player, e.Code, down, &a.localChordSince, now) && !a.localPair.anyHeld() {
		a.localChordFired = false
	}
}

func (a *App) localForwardLocked(e remoteinput.Event, now time.Time) []remoteinput.Event {
	down, pair := localButtonDown(e)
	if !pair || (e.Code != remoteinput.ButtonSelect && e.Code != remoteinput.ButtonStart) {
		return []remoteinput.Event{e}
	}
	if a.localPair.press(e.Player, e.Code, down, &a.localChordSince, now) {
		var out []remoteinput.Event
		out = a.releaseSentLocked(remoteinput.ButtonSelect, e.Player, out)
		out = a.releaseSentLocked(remoteinput.ButtonStart, e.Player, out)
		return out
	}
	// Another pad's chord stays armed; this pad is playing.
	if !a.localPair.anyHeld() {
		a.localChordFired = false
	}
	key := padKey{e.Player, e.Code}
	if !down {
		if a.localSent[key] {
			delete(a.localSent, key)
			return []remoteinput.Event{e}
		}
		return nil
	}
	if a.localSent == nil {
		a.localSent = map[padKey]bool{}
	}
	a.localSent[key] = true
	return []remoteinput.Event{e}
}

func (a *App) releaseSentLocked(code remoteinput.Code, player uint8, out []remoteinput.Event) []remoteinput.Event {
	key := padKey{player, code}
	if !a.localSent[key] {
		return out
	}
	delete(a.localSent, key)
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
