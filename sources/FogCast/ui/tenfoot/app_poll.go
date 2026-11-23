package tenfoot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func (a *App) pollSession(ctx context.Context) {
	a.fetchSession(ctx)
	a.fetchSessionEvents(ctx)
	a.fetchHealth(ctx)
	ticker := time.NewTicker(sessionPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.fetchSession(ctx)
			a.fetchSessionEvents(ctx)
			a.fetchHealth(ctx)
		case <-a.sessionKick:
			a.fetchSession(ctx)
			a.fetchSessionEvents(ctx)
			a.fetchHealth(ctx)
		}
	}
}

func (a *App) fetchSession(ctx context.Context) {
	a.mu.Lock()
	gen := a.sessionGen
	a.mu.Unlock()
	result, err := a.client.Session(ctx)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		a.mu.Lock()
		a.sessionObservedAt = time.Time{}
		a.clearSessionReadyLocked()
		a.mu.Unlock()
		a.noteKitMenuSessionObservation(result, err)
		return
	}
	a.mu.Lock()
	if gen != a.sessionGen {
		a.mu.Unlock()
		a.noteKitMenuSessionObservation(result, nil)
		return
	}
	if a.launch.Phase == "launching" || a.stopPhase == "stopping" || a.inputBusy || a.sessionDisplayBusy || a.developmentLoadingLocked() {
		a.mu.Unlock()
		a.noteKitMenuSessionObservation(result, nil)
		return
	}
	if a.hostAwaitStatus && result.ErrorCode == "" && (result.State == "idle" || result.State == "active" || result.State == "failed") {
		a.hostAwaitStatus = false
		a.hostPresentsPaused = false
	}
	a.applySessionLocked(result)
	if result.ErrorCode == "" {
		a.sessionObservedAt = time.Now()
	}
	a.mu.Unlock()
	a.noteKitMenuSessionObservation(result, nil)
}

func (a *App) noteKitMenuSessionObservation(session hostclient.SessionResult, sessionErr error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.kitHost {
		a.kitMenuSessionSeen = true
		a.kitMenuSessionState = session.State
		a.kitMenuSessionHTTPStatus = session.HTTPStatus
		a.kitMenuSessionError = ""
		if sessionErr != nil {
			a.kitMenuSessionError = sessionErr.Error()
		} else if session.ErrorCode != "" {
			a.kitMenuSessionError = session.ErrorCode + ": " + session.ErrorMessage
		}
	}
	a.mu.Unlock()
	a.logKitMenuReadiness()
}

// pollKitMenuReadiness reports startup readiness independently of Session and
// Health requests. Those requests can be slow or fail while the menu is held.
func (a *App) pollKitMenuReadiness(ctx context.Context) {
	if a == nil {
		return
	}
	a.mu.Lock()
	enabled := a.kitHost && a.menuDisplay != nil
	a.mu.Unlock()
	if !enabled {
		return
	}
	a.logKitMenuReadiness()
	ticker := time.NewTicker(kitMenuReadinessLogPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.logKitMenuReadiness()
		}
	}
}

func (a *App) logKitMenuReadiness() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.kitHost {
		a.mu.Unlock()
		return
	}
	display := a.menuDisplay
	startedAt := a.kitMenuStartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
		a.kitMenuStartedAt = startedAt
	}
	sessionSeen, sessionState, sessionHTTPStatus, sessionError := a.kitMenuSessionSeen, a.kitMenuSessionState, a.kitMenuSessionHTTPStatus, a.kitMenuSessionError
	healthSeen, healthError := a.kitMenuHealthSeen, a.kitMenuHealthError
	health, hostUnreachable := a.health, a.hostUnreachable
	hostAwaitStatus, hostPresentsPaused, kitHostPreparing := a.hostAwaitStatus, a.hostPresentsPaused, a.kitHostPreparing
	baseURL := ""
	if a.client != nil && a.client.Client != nil {
		baseURL = a.client.BaseURL()
	}
	logf := a.kitMenuReadyLogf
	a.mu.Unlock()
	if display == nil {
		return
	}
	readiness := display.ReadinessDiagnostic()
	healthReady := healthSeen && healthError == "" && health.Ready && health.TargetReachable && health.TargetReady
	pending := !readiness.Ready || !healthReady || !sessionSeen || sessionError != "" || hostAwaitStatus || hostPresentsPaused || kitHostPreparing
	if !pending {
		a.mu.Lock()
		a.kitMenuReadyLogKey = ""
		a.mu.Unlock()
		return
	}
	now := time.Now()
	elapsed := now.Sub(startedAt).Round(time.Millisecond)
	key := fmt.Sprintf("host_api=%q session_endpoint=%q host_session_seen=%t host_session_http_status=%d host_session_state=%q host_session_error=%q host_await_status=%t host_presents_paused=%t kit_host_preparing=%t host_unreachable=%t health_endpoint=%q host_health_seen=%t host_health_error=%q host_health_ready=%t target_reachable=%t target_ready=%t target_id=%q target_state=%q runtime_status_known=%t runtime_ready=%t runtime_available=%t runtime_generation=%d presented_generation=%d has_presented=%t display_paused=%t display_closed=%t underflows=%d recovery_abandoned=%t last_err=%q",
		baseURL, strings.TrimRight(baseURL, "/")+"/api/v1/session", sessionSeen, sessionHTTPStatus, sessionState, sessionError, hostAwaitStatus, hostPresentsPaused, kitHostPreparing, hostUnreachable,
		strings.TrimRight(baseURL, "/")+"/api/v1/health", healthSeen, healthError, health.Ready, health.TargetReachable, health.TargetReady, health.Connection.TargetID, health.Connection.State,
		readiness.StatusKnown, readiness.Ready, readiness.Available, readiness.Generation, readiness.PresentedGen, readiness.HasPresented,
		readiness.Paused, readiness.Closed, readiness.Underflows, readiness.RecoveryAbandoned, readiness.LastErr)
	a.mu.Lock()
	if key == a.kitMenuReadyLogKey && now.Sub(a.kitMenuReadyLogAt) < kitMenuReadinessLogPeriod {
		a.mu.Unlock()
		return
	}
	a.kitMenuReadyLogKey = key
	a.kitMenuReadyLogAt = now
	a.mu.Unlock()
	if logf == nil {
		logf = func(format string, args ...any) { _, _ = fmt.Fprintf(os.Stderr, format, args...) }
	}
	logf("%s", fmt.Sprintf("tenfoot: kit menu readiness pending elapsed=%s %s\n", elapsed, key))
}

func (a *App) bumpSessionGenLocked() {
	a.sessionGen++
}

func (a *App) kickSessionPollLocked() {
	if a.sessionKick == nil {
		return
	}
	select {
	case a.sessionKick <- struct{}{}:
	default:
	}
}

func (a *App) fetchHealth(ctx context.Context) {
	var paired KitLeaseStatus
	var pairedErr error
	pairedHave := a.client != nil && a.client.paired
	if pairedHave {
		paired, pairedErr = a.client.PairedKitLease(ctx)
		if ctx.Err() != nil {
			return
		}
	}
	result, err := a.client.Health(ctx)
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		lease := kitLeaseFromConnection(result.Connection)
		if pairedHave {
			if pairedErr == nil {
				lease = paired
				result.TargetReachable = paired.TargetReachable
				result.TargetReady = paired.TargetReady
				result.Connection = hostclient.TargetConnection{}
			} else {
				lease = KitLeaseStatus{Unavailable: true, Reason: "paired kit lease status unavailable"}
				result.TargetReachable = false
				result.TargetReady = false
			}
		}
		a.mu.Lock()
		wasForeign := a.foreignKitLeaseLocked()
		wasReady := a.healthHave && !a.hostUnreachable && a.kitMenuHealthError == "" && a.health.Ready && a.health.TargetReachable && a.health.TargetReady
		a.hostUnreachable = false
		a.healthHave = true
		a.health = result
		a.kitLeaseHave = true
		a.kitLease = lease
		a.clearLeaseRefusalAfterTransitionLocked(wasForeign)
		if a.kitHost && !wasReady && result.Ready && result.TargetReachable && result.TargetReady {
			a.refreshKitInventoryLocked(ctx)
		}
		a.mu.Unlock()
		a.noteKitMenuHealthObservation(nil)
		return
	}
	if pairedHave && pairedErr == nil {
		// Paired target status remains useful when the host's selected target
		// cannot be observed. The authenticated projection belongs to this kit.
		a.mu.Lock()
		wasForeign := a.foreignKitLeaseLocked()
		a.hostUnreachable = false
		a.healthHave = true
		health := hostclient.HealthResult{Ready: true, TargetReachable: paired.TargetReachable, TargetReady: paired.TargetReady}
		a.health = health
		a.kitLeaseHave = true
		a.kitLease = paired
		a.clearLeaseRefusalAfterTransitionLocked(wasForeign)
		a.mu.Unlock()
		a.noteKitMenuHealthObservation(err)
		return
	}
	if pairedHave && pairedErr != nil {
		// Keep the scoped status failure visible even when the selected host's
		// health endpoint is also unreachable.
		a.mu.Lock()
		a.kitLeaseHave = true
		a.kitLease = KitLeaseStatus{Unavailable: true, Reason: "paired kit lease status unavailable"}
		a.mu.Unlock()
	}
	if isHostTransportError(err) {
		a.mu.Lock()
		a.hostUnreachable = true
		a.mu.Unlock()
		a.noteKitMenuHealthObservation(err)
		return
	}
	status, statusErr := a.client.Status(ctx)
	if ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	if statusErr == nil && status.Unavailable {
		a.hostUnreachable = false
		a.healthHave = true
		a.health = hostclient.HealthResult{Ready: true, TargetReachable: false, TargetReady: false}
	}
	a.mu.Unlock()
	if statusErr != nil {
		err = fmt.Errorf("host health: %v; host status: %w", err, statusErr)
	}
	a.noteKitMenuHealthObservation(err)
}

func (a *App) noteKitMenuHealthObservation(healthErr error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.kitHost {
		a.kitMenuHealthSeen = true
		a.kitMenuHealthError = ""
		if healthErr != nil {
			a.kitMenuHealthError = healthErr.Error()
		}
	}
	a.mu.Unlock()
	a.logKitMenuReadiness()
}

func (a *App) clearLeaseRefusalAfterTransitionLocked(wasForeign bool) {
	if !wasForeign || a.foreignKitLeaseLocked() {
		return
	}
	if a.statusLeaseRefusal && a.status == localInUseCopy {
		a.status = ""
	}
	a.statusLeaseRefusal = false
	if a.launchLeaseRefusal {
		a.launch.Message = ""
		a.launch.Phase = "idle"
		a.launchLeaseRefusal = false
	}
}

func (a *App) healthSnapshotLocked() HealthSnapshot {
	line := kitHealthLine(a.hostUnreachable, a.healthHave, a.health)
	return HealthSnapshot{
		HostUnreachable: a.hostUnreachable,
		Ready:           a.health.Ready,
		TargetReachable: a.health.TargetReachable,
		TargetReady:     a.health.TargetReady,
		Line:            line,
		Connection:      a.health.Connection,
	}
}

func kitHealthLine(hostUnreachable, have bool, health hostclient.HealthResult) string {
	if hostUnreachable {
		return "host unreachable"
	}
	if !have {
		return ""
	}
	if !health.Ready {
		return "host not ready"
	}
	if health.Connection.State != "" {
		return connectionLine(health.Connection)
	}
	if !health.TargetReachable {
		return "kit unreachable"
	}
	if !health.TargetReady {
		return "kit not ready"
	}
	return ""
}

func connectionLine(connection hostclient.TargetConnection) string {
	state := strings.TrimSpace(connection.State)
	label := strings.ReplaceAll(state, "-", " ")
	parts := []string{label}
	if state == "busy" && strings.TrimSpace(connection.Owner) != "" {
		parts = append(parts, "owned by "+strings.TrimSpace(connection.Owner))
	} else if strings.TrimSpace(connection.Message) != "" {
		parts = append(parts, strings.TrimSpace(connection.Message))
	}
	if (state == "ready" || state == "active") && strings.TrimSpace(connection.Address) != "" {
		parts = append(parts, strings.TrimSpace(connection.Address))
	}
	return strings.Join(parts, " · ")
}

func remoteInputCanAttach(session hostclient.SessionResult) bool {
	state := remoteInputState(session)
	if !remoteInputOffered(session) || state == "" {
		return false
	}
	if remoteInputTransitioning(state) {
		return false
	}
	return state != "attached"
}

func remoteInputCanDetach(session hostclient.SessionResult) bool {
	return remoteInputOffered(session) && remoteInputState(session) == "attached"
}

func remoteInputOffered(session hostclient.SessionResult) bool {
	return session.State == "active" && session.Execution == "fpga_native" && session.Input != nil
}

func remoteInputState(session hostclient.SessionResult) string {
	if session.Input == nil {
		return ""
	}
	return strings.TrimSpace(session.Input.State)
}

func remoteInputTransitioning(state string) bool {
	return state == "starting" || state == "reconnecting"
}

func remoteInputHint(session hostclient.SessionResult, busy bool, action string, kind InputKind) string {
	west := westWord(kind)
	if busy {
		switch action {
		case "detach":
			return west + " detaching"
		default:
			return west + " attaching"
		}
	}
	if remoteInputCanDetach(session) {
		return west + " detach"
	}
	if remoteInputCanAttach(session) {
		return west + " attach"
	}
	return ""
}

func (a *App) startInputToggleLocked() {
	if a.inputBusy || a.stopPhase == "stopping" {
		return
	}
	if !remoteInputOffered(a.session) {
		return
	}
	state := remoteInputState(a.session)
	if remoteInputTransitioning(state) {
		a.inputMessage = "input busy"
		return
	}
	action := "attach"
	if remoteInputCanDetach(a.session) {
		action = "detach"
	} else if !remoteInputCanAttach(a.session) {
		a.inputMessage = "input busy"
		return
	}
	a.inputBusy = true
	a.inputAction = action
	if action == "detach" {
		a.inputMessage = "detaching remote input"
	} else {
		a.inputMessage = "attaching remote input"
	}
	a.bumpSessionGenLocked()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.doInputMutation(ctx, action)
}

func (a *App) doInputMutation(ctx context.Context, action string) {
	var result hostclient.SessionResult
	var err error
	if action == "detach" {
		result, err = a.client.DetachInput(ctx)
	} else {
		result, err = a.client.AttachInput(ctx)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inputAction != action || !a.inputBusy {
		return
	}
	a.bumpSessionGenLocked()
	a.inputBusy = false
	a.inputAction = ""
	if err != nil || result.ErrorCode != "" {
		a.inputMessage = inputFailureStatus(action)
		a.kickSessionPollLocked()
		a.flushQueuedStopLocked()
		return
	}
	a.applySessionLocked(result)
	if action == "detach" {
		a.inputMessage = "input detached"
	} else {
		a.inputMessage = "input attached"
	}
	a.kickSessionPollLocked()
	a.flushQueuedStopLocked()
}

func (a *App) flushQueuedStopLocked() {
	if !a.stopQueued {
		return
	}
	a.stopQueued = false
	a.startStopModeLocked(a.stopReleasesLease)
}

func inputFailureStatus(action string) string {
	if action == "detach" {
		return "input detach failed"
	}
	return "input attach failed"
}

func (a *App) applySessionLocked(result hostclient.SessionResult) {
	a.sessionObservation++
	a.sessionObservedAt = time.Time{}
	if result.ErrorCode != "" || !samePlayHIDSession(a.session, result) {
		a.clearSessionReadyLocked()
	}
	if result.ErrorCode != "" {
		return
	}
	if !samePlayHIDSession(a.session, result) {
		a.sessionTitleRequest++
		a.sessionTitlePending = false
		a.sessionTitleNext = time.Time{}
		a.resolvedSessionTitle = ""
		a.resolvedSessionGameID = ""
		a.clearPlayingSetupLocked()
		a.cancelPlayHIDLocked()
		a.invalidateSessionDisplayLocked()
	}
	a.session = result
	a.rememberFlightLocked(result.FlightID)
	// Keep retainedIdleLease. Soft-stop of foreground B can promote A, and
	// a later launch can go active, while B's idle grant is still retained.
	// Shell exit releases that grant; this poll does not.
	if result.State != "active" {
		a.session.GameID = ""
		a.session.System = ""
	}
	if a.session.State == "active" && !a.developmentLoadingLocked() && !sessionDevelopmentActive(a.session) {
		a.clearStaleDevelopmentLoadLocked()
	}
	if a.session.State != "active" && a.stopPhase != "stopping" && !a.retryStopLock {
		a.roomDuringPlay = false
		a.playHIDHeld = nil
		a.stopPhase = "idle"
		a.stopMessage = ""
		if a.launch.Phase == "ok" {
			a.launch.Phase = "idle"
			a.launch.Message = ""
			a.launch.GameID = ""
		}
		a.clearCompletedDevelopmentLoadLocked()
		a.closeTapePickerLocked()
	}
	a.syncGPUParkLocked()
	a.resolveKitHostSessionTitleLocked()
}

func (a *App) syncGPUParkLocked() {
	if a.session.State == "active" || a.session.State == "launching" || a.stopPhase == "stopping" || a.retryStopLock || a.developmentLoadingLocked() {
		a.hold.Clear()
	}
	want := a.session.State == "active" && !a.roomDuringPlay || a.stopPhase == "stopping" || a.retryStopLock
	if want == a.gpuParked {
		if want {
			a.closeCollectionOverlaysLocked()
			a.searchOpen = false
			a.closeSettingsLocked()
			a.closeFiltersLocked()
			a.closeDetailLocked()
		}
		a.syncPreviewLocked()
		return
	}
	a.gpuParked = want
	a.stopPreviewLocked()
	if !want {
		a.syncPreviewLocked()
		return
	}
	a.hideAttractLocked()
	a.closeCollectionOverlaysLocked()
	a.searchOpen = false
	a.closeSettingsLocked()
	a.closeFiltersLocked()
	a.closeDetailLocked()
	a.loadGen++
	// A held retry has loading false so Select can launch the cached title.
	// Parking still cancels the in-flight generation; continue that retry
	// instead of treating it as a finished load. The new load reads the
	// current filter, so a superseded generation stays cancelled.
	resumeHeld := a.libraryRetrying && !a.loading
	restart := a.loading || a.libraryRetrying
	ctx := a.replaceLoadContextLocked()
	if restart {
		a.libraryRetrying = true
		go a.loadLibraryResume(ctx, a.loadGen, resumeHeld)
	} else {
		a.libraryRetrying = false
	}
	a.covers = map[string]*coverSlot{}
	a.shots = map[string]*shotSlot{}
	a.shotIDs = map[string][]string{}
	a.syncPreviewLocked()
}

func (a *App) sessionSnapshotLocked() SessionSnapshot {
	title := strings.TrimSpace(a.sessionTitle)
	if a.session.GameID != "" {
		found := false
		for _, game := range a.games {
			if game.ID == a.session.GameID {
				title = displayGameTitle(game)
				found = true
				break
			}
		}
		if !found && (a.launch.GameID != a.session.GameID || title == "") {
			if a.resolvedSessionGameID == a.session.GameID && a.resolvedSessionTitle != "" {
				title = a.resolvedSessionTitle
			} else {
				title = a.session.GameID
			}
		}
		if title == "" {
			title = a.session.GameID
		}
	} else {
		title = ""
	}
	progress := ""
	if a.session.Progress != nil {
		progress = strings.TrimSpace(a.session.Progress.Message)
		if progress == "" {
			progress = strings.TrimSpace(a.session.Progress.Stage)
		}
	}
	inputState := remoteInputState(a.session)
	events := make([]string, 0, len(a.sessionEvents))
	for _, ev := range a.sessionEvents {
		if line := formatSessionEvent(ev); line != "" {
			events = append(events, line)
		}
	}
	retry := a.retryStopLock
	devActive := sessionDevelopmentActive(a.session)
	devState := sessionDevelopmentState(a.session)
	chrome := sessionChromeState(a.session.State, a.session.Execution, a.stopPhase == "stopping", retry)
	diagnostic := chrome == sessionChromeDevelopment || (devActive && (a.session.State == "active" || a.session.State == "launching"))
	return SessionSnapshot{
		State:             a.session.State,
		Chrome:            chrome,
		GameID:            a.session.GameID,
		Title:             title,
		System:            a.session.System,
		Coleco:            a.colecoSessionLocked(),
		Execution:         a.session.Execution,
		Media:             a.session.Media,
		InputState:        inputState,
		InputHint:         remoteInputHint(a.session, a.inputBusy, a.inputAction, a.affinity.current.Kind),
		InputBusy:         a.inputBusy || remoteInputTransitioning(inputState),
		Progress:          progress,
		Stopping:          a.stopPhase == "stopping",
		RetryStop:         retry,
		RetryHint:         strings.TrimSpace(a.retryStopHint),
		RetryCode:         strings.TrimSpace(a.retryStopCode),
		LaunchLocked:      a.sessionStopOfferedLocked() || a.launch.Phase == "launching" || a.developmentLoadingLocked(),
		Events:            events,
		DevelopmentActive: devActive,
		DevelopmentState:  devState,
		Diagnostic:        diagnostic,
		LoadTape:          a.sessionLiveMediaOfferedLocked(),
		ControlsNotice:    a.roomSessionNotice,
		ControlsOpen:      a.roomDuringPlay,
		MediaSelector:     a.needsSessionDisplayLocked() && (a.sessionLiveMediaOfferedLocked() || a.mediaControlsDirect),
		KeypadSelect:      a.sessionKeypadLocked(),
		KeyboardHID:       a.forwardsKeyboardHIDLocked(),
		HardwareRoom:      a.playingHardwareRoomAvailableLocked(),
	}
}

func (a *App) kitLeaseSnapshotLocked() KitLeaseSnapshot {
	if !a.kitLeaseHave {
		return KitLeaseSnapshot{}
	}
	status := a.kitLease
	return KitLeaseSnapshot{
		Line:        formatKitLeaseLine(status),
		State:       status.State,
		Owner:       status.Owner,
		Purpose:     status.Purpose,
		Generation:  status.Generation,
		Expires:     formatLeaseExpiry(status),
		Reason:      status.Reason,
		Unreachable: status.Unavailable,
	}
}

func (a *App) sessionLiveLocked() bool {
	switch a.session.State {
	case "active", "failed", "stopping", "launching":
		return true
	}
	switch a.stopPhase {
	case "stopping", "error", "host":
		return true
	}
	return a.retryStopLock
}

func (a *App) fetchSessionEvents(ctx context.Context) {
	a.mu.Lock()
	after := a.sessionEventAfter
	a.mu.Unlock()
	events, err := a.client.SessionEvents(ctx, after)
	if err != nil || ctx.Err() != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionEvents, a.sessionEventAfter = mergeSessionEvents(a.sessionEvents, events, after)
	a.rememberFlightsFromEventsLocked(events)
	retain, ev := eventsRetainRetryStop(a.sessionEvents)
	if !retain || !a.sessionLiveLocked() {
		return
	}
	msg := ""
	if ev.Progress != nil {
		msg = ev.Progress.Message
	}
	a.lockRetryStopLocked(ev.Event, msg)
	a.syncGPUParkLocked()
}

func kitLeaseFromConnection(connection hostclient.TargetConnection) KitLeaseStatus {
	status := KitLeaseStatus{State: connection.State, Owner: connection.Owner, Reason: connection.Message}
	switch connection.State {
	case "busy":
		status.State = "held"
	case "recovery-required":
		status.State = "blocked"
	}
	return status
}

func (a *App) stopStatusLocked() string {
	if strings.TrimSpace(a.stopMessage) == "" {
		return ""
	}
	switch a.stopPhase {
	case "stopping", "error", "host":
		return a.stopMessage
	default:
		return ""
	}
}

func (a *App) nowPlayingStatusLocked() string {
	if a.session.State != "active" {
		return ""
	}
	if a.session.Progress != nil {
		if msg := strings.TrimSpace(a.session.Progress.Message); msg != "" {
			return msg
		}
		if stage := strings.TrimSpace(a.session.Progress.Stage); stage != "" {
			return stage
		}
	}
	return ""
}
