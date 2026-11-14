package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func (a *App) startLaunchLocked() {
	if a.launch.Phase == "launching" || a.sessionStopOfferedLocked() || a.developmentLoadingLocked() {
		if a.session.State == "active" && a.colecoSessionLocked() && a.roomDuringPlay {
			a.status = colecoCartridgeSessionStatus
		}
		return
	}
	if a.searchPending {
		a.applyPendingSearchLocked()
		return
	}
	if a.loading {
		return
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		a.launchLeaseRefusal = false
		a.launch = LaunchSnapshot{Phase: "error", Message: "no title selected"}
		return
	}
	game := a.games[a.grid.Focus]
	if libraryPlayDestination(game).Confirm() == rooms.ConfirmWait {
		a.status = rooms.CheckingStatus
		return
	}
	a.startLaunchGameLocked(game)
}

func (a *App) startLaunchGameLocked(game hostclient.Game) {
	a.startLaunchGameWithPackageLocked(game, "", "")
}

func (a *App) startLaunchGameWithPackageLocked(game hostclient.Game, packageID, expectedPackageID string) {
	if a.kitHost && (a.hostAwaitStatus || a.kitHostPreparing) {
		return
	}
	// Release kit may already have read an idle session and still be about
	// to post an empty-body Stop. That Stop would end this game.
	if a.leaseReleaseBusy {
		a.status = "Releasing the kit"
		return
	}
	if a.launch.Phase == "launching" || a.sessionStopOfferedLocked() || a.developmentLoadingLocked() {
		if a.session.State == "active" && a.colecoSessionLocked() && a.roomDuringPlay {
			a.status = colecoCartridgeSessionStatus
		}
		return
	}
	// Firmware import and other catalog blocks do not claim the kit.
	// A foreign lease applies only when Confirm would launch.
	a.clearStaleDevelopmentLoadLocked()
	if missingFirmwareGame(game) {
		a.openFirmwarePickerLocked(game)
		return
	}
	if a.client != nil && a.client.paired && a.kitMutationBlockedLocked() {
		message := a.status
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: message}
		a.launchLeaseRefusal = a.statusLeaseRefusal
		if a.room != nil {
			a.closeRoomOverlaysLocked()
		} else {
			a.openLibraryDetailLocked(game)
		}
		return
	}
	if reason := launchBlockReason(game); reason != "" {
		a.launchLeaseRefusal = false
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: reason}
		if a.room != nil {
			a.closeRoomOverlaysLocked()
		} else {
			// The reason and its remedy are in Details, not only the header.
			a.openLibraryDetailLocked(game)
		}
		return
	}
	if a.foreignKitLeaseLocked() {
		a.launch = LaunchSnapshot{GameID: game.ID, Phase: "error", Message: localInUseCopy}
		a.launchLeaseRefusal = true
		if a.room != nil {
			a.closeRoomOverlaysLocked()
		} else {
			a.openLibraryDetailLocked(game)
		}
		return
	}
	a.launchPackageID = packageID
	a.launchExpectedPackageID = expectedPackageID
	a.sessionTitle = displayGameTitle(game)
	a.cancelPlayHIDLocked()
	a.launchLeaseRefusal = false
	a.launch = LaunchSnapshot{
		GameID:    game.ID,
		Phase:     "launching",
		Message:   "launching " + displayGameTitle(game),
		StartedAt: time.Now(),
	}
	if a.room != nil {
		a.closeRoomOverlaysLocked()
	}
	a.hold.Clear()
	a.closeFiltersLocked()
	a.bumpSessionGenLocked()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	stamp := a.clientStampLocked()
	a.postUIEventLocked("ui.launch", map[string]string{
		"game_id": game.ID,
		"title":   game.Title,
	})
	dispatch := func() { go a.doLaunch(ctx, game, stamp, packageID, expectedPackageID) }
	if a.kitHost && a.menuDisplay != nil {
		a.hostPresentsPaused = true
		a.hostLaunchPending = dispatch
	} else {
		dispatch()
	}
}

// launchBlockReason maps hostclient catalog ineligibility to sofa copy.
// Admission rules live on hostclient.Game; unavailable titles must not
// POST /api/v1/session/launch.
func launchBlockReason(game hostclient.Game) string {
	if game.LaunchBlock() == "" {
		return ""
	}
	return rooms.LaunchBlockCopy(game)
}

func libraryPlayDestination(game hostclient.Game) rooms.Destination {
	state, matches := rooms.ClassifyGames([]hostclient.Game{game}, "")
	d := rooms.Destination{Kind: rooms.KindGame, Availability: state, Matches: matches, GameID: game.ID, Label: displayGameTitle(game), System: game.System}
	d.FillCopy()
	return d
}

func (a *App) doLaunch(ctx context.Context, game hostclient.Game, stamp ClientStamp, packageID, expectedPackageID string) {
	if packageID != "" {
		_, err := a.client.SelectCoreEntryPackage(ctx, game.ID, expectedPackageID, packageID)
		if err != nil {
			// The host may commit the compare-and-swap and lose its response.
			// Confirm the saved entry before deciding that retry must remain
			// pending or that another selection should stop this launch.
			if current, readErr := a.client.CoreEntry(ctx, game.ID); readErr == nil {
				switch {
				case current.PackageID == packageID:
					err = nil
				case current.PackageID != expectedPackageID:
					err = errors.New("saved package selection changed while launch was pending")
				}
			}
		}
		if err != nil {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.launch.GameID != game.ID {
				return
			}
			a.bumpSessionGenLocked()
			a.hostPresentsPaused = false
			a.launch.Phase = "error"
			a.launchLeaseRefusal = false
			a.launch.Message = "package selection failed: " + err.Error()
			a.launch.ErrorMessage = err.Error()
			return
		}
		a.mu.Lock()
		if a.launch.GameID == game.ID && a.launch.Phase == "launching" {
			a.launchPackageID = ""
			a.launchExpectedPackageID = ""
		}
		a.mu.Unlock()
	}
	result, err := a.client.LaunchStamped(ctx, game.ID, stamp)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.launch.GameID != game.ID {
		return
	}
	a.bumpSessionGenLocked()
	if err != nil {
		if a.kitHost && a.hostPresentsPaused {
			// A lost response can follow a successful launch. Keep HDMI
			// paused until a fresh host status resolves that ambiguity.
			a.hostAwaitStatus = true
			a.kickSessionPollLocked()
		}
		a.launch.Phase = "error"
		a.launchLeaseRefusal = false
		a.launch.Message = "launch failed: " + err.Error()
		a.launch.ErrorMessage = err.Error()
		return
	}
	a.launch.HTTPStatus = result.HTTPStatus
	a.launch.ErrorCode = result.ErrorCode
	a.launch.ErrorMessage = result.ErrorMessage
	a.launch.State = result.State
	if result.ErrorCode != "" {
		if a.kitHost && a.hostPresentsPaused {
			a.hostAwaitStatus = true
			a.kickSessionPollLocked()
		}
		a.launch.Phase = "host"
		a.launchLeaseRefusal = false
		a.launch.Message = fmt.Sprintf("host launch %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		return
	}
	a.launch.Phase = "ok"
	a.hostPresentsPaused = false
	a.launchLeaseRefusal = false
	a.launch.Message = fmt.Sprintf("host accepted launch for %s", game.ID)
	a.applySessionLocked(result)
	if a.session.State == "active" && a.session.GameID == "" {
		a.session.GameID = game.ID
	}
	activePackage := ""
	if a.session.CorePackage != nil {
		activePackage = a.session.CorePackage.PackageID
	}
	expectedPackage := game.PackageID
	if packageID != "" {
		expectedPackage = packageID
	}
	if a.session.State == "active" && a.session.GameID == game.ID && game.CoreID != "" && activePackage == expectedPackage {
		a.rememberPlayingSetupLocked(game.CoreID)
	}
	a.kickSessionPollLocked()
}

func (a *App) sessionStopOfferedLocked() bool {
	return a.retryStopLock || a.session.State == "active" || a.stopPhase == "stopping"
}

func (a *App) startStopLocked() {
	a.startStopModeLocked(false)
}

// startReleaseStopLocked is the kit Select+Start completed exit. Idle cleanup
// releases the captured play's kit grant. Rooms B/Back stays on startStopLocked
// and retains the lease for the unpaired sofa Soft-stop.
func (a *App) startReleaseStopLocked() {
	a.startStopModeLocked(true)
}

func (a *App) startStopModeLocked(releaseLease bool) {
	if a.stopPhase == "stopping" {
		return
	}
	if !a.retryStopLock && a.session.State != "active" {
		if a.stopCaptured {
			a.discardCapturedStopLocked()
		}
		return
	}
	if a.inputBusy {
		a.stopQueued = true
		if !a.retryStopLock {
			a.stopExpected = a.session
			a.stopReleasesLease = releaseLease
			a.stopCaptured = true
		}
		return
	}
	if !a.retryStopLock && a.stopCaptured && !samePlayHIDSession(a.stopExpected, a.session) {
		// The chord was for the play on screen while input was in flight.
		// Flushing after a replacement must not capture that replacement.
		a.discardCapturedStopLocked()
		return
	}
	if !a.retryStopLock {
		if !a.stopCaptured {
			a.stopExpected = a.session
			a.stopReleasesLease = releaseLease
		}
		a.stopCaptured = false
	}
	a.roomDuringPlay = false
	a.roomPickerOpen = false
	// Stop can fail while the core still owns this display generation. Keep
	// its binding and any pending display reply until idle is confirmed;
	// otherwise Back and shell exit could no longer return the HDMI plane.
	a.sessionDisplayUncertain = a.sessionDisplayUncertain || a.launcherDisplayFocusedLocked() || a.sessionDisplayBusy
	a.suppressHeldPlayKeysLocked()
	a.suppressHeldSessionPadLocked()
	// Neutralize on the existing queue so an admitted press cannot overtake
	// its release. A refused Stop can leave this same core running.
	a.releasePlayHIDLocked()
	hidDone := a.playHIDTail
	a.stopPhase = "stopping"
	a.stopMessage = "stopping session"
	a.bumpSessionGenLocked()
	a.syncGPUParkLocked()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	stamp := a.clientStampLocked()
	a.postUIEventLocked("ui.stop", map[string]string{
		"game_id": a.session.GameID,
		"state":   a.session.State,
	})
	expected := a.stopExpected
	release := a.stopReleasesLease
	idleRetry := a.stopRetryIdleRelease
	done := make(chan struct{})
	a.stopWait = done
	go func() {
		if hidDone != nil {
			select {
			case <-hidDone:
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
		}
		if idleRetry {
			a.doIdleLeaseRetry(stamp)
		} else {
			a.doStop(ctx, stamp, expected, release)
		}
		a.mu.Lock()
		if a.stopWait == done {
			a.stopWait = nil
		}
		a.mu.Unlock()
		close(done)
	}()
}

func (a *App) doStop(ctx context.Context, stamp ClientStamp, expected hostclient.SessionResult, releaseLease bool) {
	var result hostclient.SessionResult
	var err error
	switch {
	case packageStopIdentity(expected):
		if releaseLease {
			result, err = a.client.StopReleaseExpectedStamped(ctx, expected, stamp)
		} else {
			result, err = a.client.StopExpectedStamped(ctx, expected, stamp)
		}
	case releaseLease && flightStopIdentity(expected):
		result, err = a.client.StopReleaseIdentityStamped(ctx, expected, stamp)
	case releaseLease:
		// No package generation and no launch flight. An empty body would
		// stop whichever play is foreground when the host admits it.
		result = hostclient.SessionResult{ErrorCode: "SESSION_CHANGED", ErrorMessage: "the displayed session identity is unavailable; refresh before stopping"}
	default:
		result, err = a.client.StopStamped(ctx, stamp)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopPhase != "stopping" {
		return
	}
	a.bumpSessionGenLocked()
	if err != nil {
		a.stopPhase = "error"
		a.stopMessage = "stop failed: " + err.Error()
		a.lockRetryStopLocked("", err.Error())
		if result.ErrorCode == "" {
			a.stopResponseLost = true
		}
		if releaseLease {
			// The exit asked to drop the grant. Failure can leave it renewing
			// after the machine is already idle, so Release kit can retry.
			a.noteRetainedLeaseLocked(expected)
			a.stopRetryIdleRelease = result.StopStage == "lease_release"
		}
		a.syncGPUParkLocked()
		return
	}
	if result.ErrorCode == "SESSION_CHANGED" {
		// Admission rejected this identity without stopping it. A later explicit
		// Stop must capture refreshed play, rather than retry the stale request.
		a.retryStopLock = false
		a.retryStopHint = ""
		a.retryStopCode = ""
		a.stopExpected = hostclient.SessionResult{}
		a.stopReleasesLease = false
		a.stopRetryIdleRelease = false
		a.stopCaptured = false
		a.stopPhase = "host"
		a.stopMessage = "The running machine changed. Review it before stopping."
		a.status = a.stopMessage
		if a.room != nil {
			a.room.Resume()
			a.dropRoomNavActionsLocked()
		}
		a.kickSessionPollLocked()
		a.syncGPUParkLocked()
		return
	}
	if result.ErrorCode != "" {
		a.stopPhase = "host"
		a.stopMessage = fmt.Sprintf("host stop %d %s: %s", result.HTTPStatus, result.ErrorCode, result.ErrorMessage)
		a.lockRetryStopLocked(result.ErrorCode, result.ErrorMessage)
		if releaseLease {
			a.noteRetainedLeaseLocked(expected)
			a.stopRetryIdleRelease = result.StopStage == "lease_release"
		}
		a.syncGPUParkLocked()
		return
	}
	a.retryStopLock = false
	a.retryStopHint = ""
	a.retryStopCode = ""
	a.stopRetryIdleRelease = false
	a.stopPhase = "ok"
	a.stopMessage = ""
	a.applySessionLocked(result)
	if result.State == "" || result.State == "idle" {
		if releaseLease {
			a.noteExplicitReleaseLocked(expected)
		} else {
			a.noteRetainedLeaseLocked(expected)
		}
		a.stopResponseLost = false
	}
	a.kickSessionPollLocked()
}

// doIdleLeaseRetry posts the shell's idle-grant release. The captured play
// is already idle, so replaying expected_session is rejected before the
// grant can be released.
func (a *App) doIdleLeaseRetry(stamp ClientStamp) {
	client := a.client
	ok := releaseIdleLeaseNow(client, stamp)
	var observed hostclient.SessionResult
	if ok && client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		observed, _ = client.Session(ctx)
		cancel()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopPhase != "stopping" {
		return
	}
	a.bumpSessionGenLocked()
	if !ok {
		a.stopPhase = "error"
		a.stopMessage = "stop failed: lease release failed"
		a.lockRetryStopLocked("", "lease release failed")
		a.stopRetryIdleRelease = true
		a.retainedIdleLease = true
		a.syncGPUParkLocked()
		return
	}
	a.stopRetryIdleRelease = false
	a.retryStopLock = false
	a.retryStopHint = ""
	a.retryStopCode = ""
	a.stopReleasesLease = false
	a.stopExpected = hostclient.SessionResult{}
	a.stopPhase = "ok"
	a.stopMessage = ""
	a.clearRetainedLeasesLocked()
	a.stopResponseLost = false
	if observed.ErrorCode == "" && observed.State != "" {
		a.applySessionLocked(observed)
	} else {
		a.session.State = "idle"
		a.session.GameID = ""
		a.session.System = ""
	}
	a.kickSessionPollLocked()
}

func packageStopIdentity(expected hostclient.SessionResult) bool {
	return expected.ID != "" && expected.CorePackage != nil && expected.Target != "" &&
		expected.CorePackage.PackageID != "" && expected.CorePackage.Generation != 0
}

func flightStopIdentity(expected hostclient.SessionResult) bool {
	return strings.TrimSpace(expected.ID) != "" && strings.TrimSpace(expected.FlightID) != "" &&
		strings.TrimSpace(expected.GameID) != "" && !packageStopIdentity(expected)
}

func (a *App) discardCapturedStopLocked() {
	a.stopCaptured = false
	a.stopReleasesLease = false
	a.stopExpected = hostclient.SessionResult{}
	a.stopPhase = "host"
	a.stopMessage = "The running machine changed. Review it before stopping."
	a.status = a.stopMessage
}

func (a *App) clearRetainedLeasesLocked() {
	a.retainedIdleLease = false
	a.retainedIdleTargets = nil
	a.retainedIdleUnscoped = false
}

// noteRetainedLeaseLocked records a grant this stop did not release.
// A later bound release of one target must not forget the others.
func (a *App) noteRetainedLeaseLocked(play hostclient.SessionResult) {
	if strings.TrimSpace(play.Target) == "" {
		a.retainedIdleUnscoped = true
	} else {
		if a.retainedIdleTargets == nil {
			a.retainedIdleTargets = make(map[string]struct{})
		}
		a.retainedIdleTargets[strings.TrimSpace(play.Target)] = struct{}{}
	}
	a.retainedIdleLease = true
}

// noteExplicitReleaseLocked drops the grant a bound explicit stop released.
// Any other retained target, and any grant whose target was not named,
// stays until the global idle release.
func (a *App) noteExplicitReleaseLocked(play hostclient.SessionResult) {
	target := strings.TrimSpace(play.Target)
	if target == "" {
		return
	}
	delete(a.retainedIdleTargets, target)
	if len(a.retainedIdleTargets) == 0 && !a.retainedIdleUnscoped {
		a.retainedIdleLease = false
	}
}

func (a *App) lockRetryStopLocked(code, message string) {
	a.retryStopLock = true
	a.retryStopHint = retryStopStatus(code, message)
	a.retryStopCode = strings.TrimSpace(code)
}
