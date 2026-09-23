package hostapi

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/google/uuid"
)

type sessionService interface {
	Game(context.Context, string) (catalog.Game, error)
	Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
	LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
	Status(context.Context) (protocol.Status, error)
}

type sessionLaunchOnService interface {
	LaunchOn(context.Context, string, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
}

type sessionCoreService interface {
	LoadCore(context.Context, int64, io.Reader) (protocol.Status, error)
}

// MediaSession is the session-owned seam for opt-in media transport.
type MediaSession interface {
	Start(context.Context, string) (MediaHandle, error)
}

type MediaHandle interface {
	Stop(context.Context) error
}

type sessionExecutionService interface {
	SessionExecution(context.Context, string) (string, error)
}

type sessionDevelopmentService interface {
	DevelopmentActive(context.Context) (bool, error)
}

type sessionDevelopmentStateService interface {
	DevelopmentSessionState(context.Context) (bool, string, error)
}

type sessionPackageOwnerService interface {
	ActivePackageOwned() bool
}

type sessionProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type sessionResult struct {
	ID          string                      `json:"id"`
	Target      string                      `json:"target,omitempty"`
	TargetID    string                      `json:"target_id,omitempty"`
	State       protocol.State              `json:"state"`
	GameID      *string                     `json:"game_id,omitempty"`
	System      *protocol.System            `json:"system,omitempty"`
	Execution   string                      `json:"execution,omitempty"`
	Media       string                      `json:"media,omitempty"`
	Progress    *sessionProgress            `json:"progress,omitempty"`
	Input       *host.RemoteInputStatus     `json:"input,omitempty"`
	CorePackage *protocol.CorePackageStatus `json:"core_package,omitempty"`
	FlightID    string                      `json:"flight_id,omitempty"`
}

// sessionEvent is one public host session event. Protocol 1 fields are
// unchanged; flight_id is an additive UUID for one launch, stop, or
// development-RBF / described-package mutation and the related events in
// that flight.
type sessionEvent struct {
	Sequence     uint64                  `json:"sequence"`
	FlightID     string                  `json:"flight_id,omitempty"`
	TSUTC        string                  `json:"ts_utc"`
	MonoMS       int64                   `json:"mono_ms"`
	ClientTSUTC  string                  `json:"client_ts_utc,omitempty"`
	ClientMonoMS *int64                  `json:"client_mono_ms,omitempty"`
	Event        string                  `json:"event"`
	State        protocol.State          `json:"state"`
	GameID       *string                 `json:"game_id,omitempty"`
	System       *protocol.System        `json:"system,omitempty"`
	Media        string                  `json:"media,omitempty"`
	Progress     *sessionProgress        `json:"progress,omitempty"`
	Input        *host.RemoteInputStatus `json:"input,omitempty"`
}

type sessionCoordinator struct {
	id              string
	service         sessionService
	remoteInput     host.RemoteInputController
	media           MediaSession
	mediaHandle     MediaHandle
	mediaGeneration uint64
	execution       string
	packageOwned    bool
	mediaState      string
	terminalStatus  *protocol.Status
	inputBinding    sessionInputBinding
	mu              sync.Mutex
	observationMu   sync.Mutex
	busy            bool
	sequence        uint64
	flightID        string
	started         time.Time
	events          []sessionEvent
}

type sessionInputBinding struct {
	core       string
	packageID  string
	generation uint64
	keyboard   bool
}

type remoteInputCapabilityAttacher interface {
	AttachWithCapabilities(context.Context, string, bool) error
}

func newSessionCoordinator(service sessionService, remoteInput host.RemoteInputController, media MediaSession) *sessionCoordinator {
	return &sessionCoordinator{id: uuid.NewString(), service: service, remoteInput: remoteInput, media: media, started: time.Now()}
}

func (s *sessionCoordinator) beginFlight() {
	id := uuid.NewString()
	s.mu.Lock()
	s.flightID = id
	s.mu.Unlock()
}

func (s *sessionCoordinator) restoreFlight(id string) {
	s.mu.Lock()
	s.flightID = id
	s.mu.Unlock()
}

func (s *sessionCoordinator) ensureFlight() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.flightID == "" {
		s.flightID = uuid.NewString()
	}
}

func (s *sessionCoordinator) record(event string, result sessionResult, progress *sessionProgress) {
	s.recordStamp(event, result, progress, clientStamp{})
}

func (s *sessionCoordinator) recordStamp(event string, result sessionResult, progress *sessionProgress, stamp clientStamp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started.IsZero() {
		s.started = time.Now()
	}
	s.sequence++
	item := sessionEvent{
		Sequence: s.sequence,
		FlightID: s.flightID,
		TSUTC:    time.Now().UTC().Format(time.RFC3339Nano),
		MonoMS:   time.Since(s.started).Milliseconds(),
		Event:    event,
		State:    result.State,
		GameID:   result.GameID,
		System:   result.System,
		Media:    result.Media,
		Progress: progress,
		Input:    cloneRemoteInputStatus(result.Input),
	}
	if stamp.TsUTC != "" {
		item.ClientTSUTC = stamp.TsUTC
	}
	if stamp.MonoSet {
		mono := stamp.MonoMS
		item.ClientMonoMS = &mono
	}
	s.events = append(s.events, item)
	if len(s.events) > 256 {
		s.events = s.events[len(s.events)-256:]
	}
}

func (s *sessionCoordinator) eventsAfter(after uint64) []sessionEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]sessionEvent, 0)
	for _, event := range s.events {
		if event.Sequence > after {
			result = append(result, event)
		}
	}
	return result
}

func (s *sessionCoordinator) status(ctx context.Context) (sessionResult, error) {
	// Serialize the service observation itself with launch, stop, and terminal
	// media observation. Otherwise a status call can begin against one host
	// generation, block while launch installs the next generation, then apply
	// its stale idle result to the replacement media handle.
	s.observationMu.Lock()
	defer s.observationMu.Unlock()

	st, err := s.service.Status(ctx)
	if err != nil {
		result := s.publicSession(st, nil)
		s.mu.Lock()
		if s.execution != "" {
			result.Execution = s.execution
		}
		if s.mediaState != "" {
			result.Media = s.mediaState
		}
		s.mu.Unlock()
		return result, err
	}
	if err := s.reconcileInput(ctx, st); err != nil {
		return s.publicSession(st, nil), err
	}

	// Service.Status is also the observation point for host-only processes:
	// the host adapter can reap a process between requests and report idle
	// without knowing about this coordinator's media handle. Serialize this
	// transition so concurrent status requests cannot stop the same handle or
	// emit duplicate exit events.
	s.mu.Lock()
	execution, mediaHandle, mediaState, terminalStatus := s.execution, s.mediaHandle, s.mediaState, s.terminalStatus
	s.mu.Unlock()
	if mediaState == "stopped" && terminalStatus != nil {
		st = *terminalStatus
	}
	if mediaHandle != nil && mediaState == "active" && mediaHandleDone(mediaHandle) {
		if !mediaOwnsSession(execution) {
			mediaErr := s.stopMediaBounded(execution)
			result := s.publicSession(st, nil)
			result.Execution = execution
			result.Media = s.currentMediaState()
			if mediaErr != nil {
				s.record("session.media.exit_failed", result, nil)
				return result, nil
			}
			s.record("session.media.exit", result, nil)
			return result, nil
		}
		inputErr := s.detachInputBounded("media_exit")
		mediaErr := s.stopMediaBounded(execution)
		stopped, stopErr := s.stopServiceBounded()
		result := s.publicSession(stopped, nil)
		result.Execution = execution
		result.Media = s.currentMediaState()
		if mediaErr != nil || inputErr != nil || stopErr != nil {
			s.record("session.media.exit_failed", result, nil)
			if mediaErr != nil {
				return result, mediaErr
			}
			if inputErr != nil {
				return result, inputErr
			}
			return result, stopErr
		}
		s.record("session.media.exit", result, nil)
		return result, nil
	}
	if st.State == protocol.StateIdle && mediaHandle != nil && mediaState == "active" {
		if err := s.detachInputBounded("session_exit"); err != nil {
			return s.publicSession(st, nil), err
		}
		if err := s.stopMediaBounded(execution); err != nil {
			result := s.publicSession(st, nil)
			result.Execution = execution
			result.Media = s.currentMediaState()
			return result, err
		}
		result := s.publicSession(st, nil)
		result.Execution = execution
		result.Media = "stopped"
		s.record("session.exit", result, nil)
		return result, nil
	}

	result := s.publicSession(st, nil)
	s.mu.Lock()
	if s.execution == "" {
		if execution, packageOwned := reconstructedSessionExecution(st); execution != "" {
			s.execution = execution
			s.packageOwned = packageOwned
			s.terminalStatus = nil
		}
	}
	if st.State == protocol.StateIdle && (s.execution == fogcast.ExecutionFPGADevelopment || s.packageOwned) {
		s.execution = ""
		s.packageOwned = false
		s.terminalStatus = nil
	}
	if s.execution != "" {
		result.Execution = s.execution
	}
	if s.mediaState != "" {
		result.Media = s.mediaState
	}
	s.mu.Unlock()
	s.record("session.status", result, nil)
	return result, nil
}

func (s *sessionCoordinator) reconcileInput(ctx context.Context, st protocol.Status) error {
	if s.remoteInput == nil {
		return nil
	}
	input := s.remoteInput.Status()
	if inputEligibleStatus(st) {
		desired := inputBindingForStatus(st)
		s.mu.Lock()
		current := s.inputBinding
		s.mu.Unlock()
		if st.LastError != nil && st.LastError.Code == protocol.CodeSaveFailed && current != desired {
			if input.State != host.RemoteInputDetached {
				return s.detachInputNow(ctx, "session_reconcile")
			}
			return nil
		}
		if current == desired && (input.State == host.RemoteInputAttached || input.State == host.RemoteInputReconnecting) {
			return nil
		}
		if input.State != host.RemoteInputDetached {
			if err := s.detachInputNow(ctx, "session_reconcile"); err != nil {
				return remoteInputError()
			}
		}
		if err := s.attachInputForStatus(ctx, st); err != nil {
			return remoteInputError()
		}
		return nil
	}
	if input.State == host.RemoteInputAttached || input.Ready || input.SessionID != "" {
		if err := s.detachInputNow(ctx, "session_reconcile"); err != nil {
			return remoteInputError()
		}
	}
	return nil
}

func mediaHandleDone(handle MediaHandle) bool {
	terminal, ok := handle.(interface{ Done() <-chan struct{} })
	if !ok || terminal.Done() == nil {
		return false
	}
	select {
	case <-terminal.Done():
		return true
	default:
		return false
	}
}

func (s *sessionCoordinator) watchMedia(handle MediaHandle, generation uint64, execution string) {
	terminal, ok := handle.(interface{ Done() <-chan struct{} })
	if !ok || terminal.Done() == nil {
		return
	}
	go func() {
		<-terminal.Done()
		s.observationMu.Lock()
		defer s.observationMu.Unlock()
		s.mu.Lock()
		current := s.mediaHandle == handle && s.mediaGeneration == generation && (s.mediaState == "active" || s.mediaState == "failed")
		s.mu.Unlock()
		if !current {
			return
		}
		if !mediaOwnsSession(execution) {
			mediaErr := s.stopMediaBounded(execution)
			st, _ := s.service.Status(context.Background())
			result := s.publicSession(st, nil)
			result.Execution = execution
			result.Media = s.currentMediaState()
			if mediaErr != nil {
				s.record("session.media.exit_failed", result, nil)
				return
			}
			s.record("session.media.exit", result, nil)
			return
		}
		inputErr := s.detachInputBounded("media_exit")
		mediaErr := s.stopMediaBounded(execution)
		stopped, stopErr := s.stopServiceBounded()
		if stopErr == nil {
			s.mu.Lock()
			terminal := stopped
			s.terminalStatus = &terminal
			s.mu.Unlock()
		}
		result := s.publicSession(stopped, nil)
		result.Execution = execution
		if mediaErr != nil {
			result.Media = "active"
		} else {
			result.Media = "stopped"
		}
		if mediaErr != nil || inputErr != nil || stopErr != nil {
			s.record("session.media.exit_failed", result, nil)
			return
		}
		s.record("session.media.exit", result, nil)
	}()
}

func (s *sessionCoordinator) launch(ctx context.Context, id, target string, stamp clientStamp) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	// Package admission must run before retiring the previous input/media owner.
	execution := fogcast.ExecutionFPGANative
	if resolver, ok := s.service.(sessionExecutionService); ok {
		resolved, err := resolver.SessionExecution(ctx, id)
		if err != nil {
			return sessionResult{}, err
		}
		if resolved != "" {
			execution = resolved
		}
	}
	packageLaunch := execution == fogcast.ExecutionFPGADevelopment
	if game, err := s.service.Game(ctx, id); err == nil && game.Kind == catalog.SourceKindCorePackage {
		packageLaunch = true
	}
	if packageLaunch {
		response, err := s.launchGame(ctx, id, target, nil)
		result, err := s.finishCoreLoad(ctx, response.Status, err, "session.launch", stamp, execution)
		if err == nil && result.State == protocol.StateActive {
			if recorder, ok := s.service.(interface {
				RecordPlay(context.Context, string) error
			}); ok {
				_ = recorder.RecordPlay(ctx, id)
			}
		}
		return result, err
	}
	development, err := s.developmentActive(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	if development {
		return sessionResult{}, developmentMustStopError()
	}
	if err := s.stopPackageOwnedForReplacement(ctx, target); err != nil {
		return sessionResult{}, err
	}

	if s.remoteInput != nil {
		if err := s.detachInputNow(ctx, "session_replace"); err != nil {
			return sessionResult{}, remoteInputError()
		}
	}
	s.mu.Lock()
	previousExecution := s.execution
	previousFlight := s.flightID
	s.mu.Unlock()
	if err := s.stopMediaBounded(previousExecution); err != nil {
		return sessionResult{}, err
	}

	s.beginFlight()

	s.mu.Lock()
	s.execution = execution
	s.terminalStatus = nil
	if execution != fogcast.ExecutionHostOnly {
		s.mediaHandle = nil
		s.mediaState = ""
	}
	s.mu.Unlock()
	if execution == fogcast.ExecutionHostOnly {
		if err := s.startMedia(ctx, id, execution); err != nil {
			s.restoreExecution(previousExecution)
			s.restoreFlight(previousFlight)
			return sessionResult{}, err
		}
	}

	var progress sessionProgress
	resp, err := s.launchGame(ctx, id, target, func(value fogcast.Progress) {
		progress = sessionProgress{Stage: value.Stage, Message: value.Message}
	})
	if err != nil {
		_ = s.stopMediaBounded(execution)
		s.restoreExecution(previousExecution)
		s.restoreFlight(previousFlight)
		return sessionResult{}, err
	}
	result := s.publicSession(resp.Status, &progress)
	result.Execution = execution
	if execution == fogcast.ExecutionHostOnly {
		if resp.Status.State != protocol.StateActive {
			if err := s.stopMediaBounded(execution); err != nil {
				return sessionResult{}, err
			}
		}
		result.Media = s.currentMediaState()
	}
	if execution != fogcast.ExecutionHostOnly && resp.Status.State == protocol.StateActive {
		if err := s.startMedia(ctx, id, execution); err != nil {
			_ = s.stopMediaBounded(execution)
			s.mu.Lock()
			if s.mediaHandle == nil {
				s.mediaState = "failed"
			}
			s.mu.Unlock()
			result.Media = s.currentMediaState()
			if result.Media == "" {
				result.Media = "failed"
			}
		} else {
			result.Media = s.currentMediaState()
		}
	}
	if s.remoteInput != nil && execution != fogcast.ExecutionHostOnly && resp.Status.State == protocol.StateActive {
		core := sessionCore(resp.Status)
		if core == "" {
			return sessionResult{}, s.stopAfterFailedAttach(execution)
		}
		if err := s.attachInputForStatus(ctx, resp.Status); err != nil {
			return sessionResult{}, s.stopAfterFailedAttach(execution)
		}
		result = s.publicSession(resp.Status, &progress)
		result.Execution = execution
		result.Media = s.currentMediaState()
	}
	s.recordStamp("session.launch", result, &progress, stamp)
	if resp.Status.State == protocol.StateActive {
		if recorder, ok := s.service.(interface {
			RecordPlay(context.Context, string) error
		}); ok {
			_ = recorder.RecordPlay(ctx, id)
		}
	}
	return result, nil
}

func (s *sessionCoordinator) loadDevelopmentRBF(ctx context.Context, size int64, content io.Reader, stamp clientStamp) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	development, err := s.developmentActive(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	if development {
		return sessionResult{}, developmentMustStopError()
	}
	s.mu.Lock()
	previousExecution := s.execution
	s.mu.Unlock()
	nativeAlreadyIdle := false
	if previousExecution == fogcast.ExecutionFPGANative {
		observed, err := s.service.Status(ctx)
		if err != nil {
			return sessionResult{}, err
		}
		if observed.Development && observed.State != protocol.StateIdle {
			return sessionResult{}, developmentMustStopError()
		}
		nativeAlreadyIdle = exactIdleStatus(observed)
	}

	if s.remoteInput != nil {
		if err := s.detachInputNow(ctx, "session_replace"); err != nil {
			return sessionResult{}, remoteInputError()
		}
	}
	if err := s.stopMediaBounded(previousExecution); err != nil {
		return sessionResult{}, err
	}
	if previousExecution == fogcast.ExecutionFPGANative && !nativeAlreadyIdle {
		stopped, err := s.stopServiceForReplacement()
		if err != nil {
			return sessionResult{}, err
		}
		if !exactIdleStatus(stopped) {
			return sessionResult{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "native game did not stop to idle"}
		}
	}
	if previousExecution == fogcast.ExecutionFPGANative {
		s.restoreExecution("")
	}
	if err := ctx.Err(); err != nil {
		return sessionResult{}, err
	}

	status, err := s.service.LoadDevelopmentRBF(ctx, size, content)
	if err != nil {
		if observed, observeErr := s.service.Status(ctx); observeErr == nil {
			s.mu.Lock()
			switch {
			case observed.Development && observed.State != protocol.StateIdle:
				s.execution = fogcast.ExecutionFPGADevelopment
				s.packageOwned = false
				s.terminalStatus = nil
			case observed.State == protocol.StateIdle:
				s.execution = ""
				s.packageOwned = false
				s.terminalStatus = nil
			}
			s.mu.Unlock()
		}
		return sessionResult{}, err
	}
	s.beginFlight()
	s.mu.Lock()
	s.execution = fogcast.ExecutionFPGADevelopment
	s.packageOwned = false
	s.mediaHandle = nil
	s.mediaState = ""
	s.terminalStatus = nil
	s.mu.Unlock()
	result := s.publicSession(status, nil)
	result.Execution = fogcast.ExecutionFPGADevelopment
	s.recordStamp("session.development_rbf", result, nil, stamp)
	return result, nil
}

func (s *sessionCoordinator) loadDevelopmentCore(ctx context.Context, size int64, content io.Reader, stamp clientStamp) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	loader, ok := s.service.(sessionCoreService)
	if !ok {
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	status, err := loader.LoadCore(ctx, size, content)
	return s.finishCoreLoad(ctx, status, err, "session.development_core", stamp, fogcast.ExecutionFPGADevelopment)
}

// finishCoreLoad shares the confirmed-package ownership transition between
// explicit development loading and package-backed library entries.
// execution is the host play label: recognized-ABI library titles use
// FPGANative; no-ABI and LoadDevelopmentCore stay FPGADevelopment.
// The caller holds observationMu and the coordinator operation admission.
func (s *sessionCoordinator) finishCoreLoad(ctx context.Context, status protocol.Status, err error, event string, stamp clientStamp, execution string) (sessionResult, error) {
	var identityErr *protocol.APIError
	if errors.As(err, &identityErr) && identityErr.Phase == "identity" {
		status.LastError = identityErr
	}

	if err != nil && corePackagePreMutationFailure(err) {
		// These failures are proven to precede target mutation, so the current
		// host-owned input and media session still describe the active target.
		return sessionResult{}, err
	}
	if !corePackageInputStatus(status) {
		// The target result is ambiguous or post-mutation. Retire stale target
		// input ownership. Preserve a still-running host-only owner when the
		// service proves target idle but cannot stop that executor.
		_ = s.detachInputBounded("session_replace_ambiguous")
		s.mu.Lock()
		previousExecution := s.execution
		preserveHostOnly := hostOnlyCorePackageCleanupFailure(previousExecution, status, err)
		if !preserveHostOnly {
			s.execution = ""
			s.packageOwned = false
			s.terminalStatus = nil
		}
		s.mu.Unlock()
		if !preserveHostOnly {
			_ = s.stopMediaBounded(previousExecution)
		}
		if err != nil {
			return sessionResult{}, err
		}
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "target package state is unavailable", Phase: "recovery"}
	}
	if execution == "" {
		execution = fogcast.ExecutionFPGADevelopment
	}
	s.mu.Lock()
	previousExecution := s.execution
	s.execution = execution
	s.packageOwned = true
	s.terminalStatus = nil
	s.mu.Unlock()
	// Publish the confirmed target owner before cleaning up the retired host
	// session. Cleanup failures must not make subsequent status calls report
	// that the old game still owns the target.
	var cleanupErr error
	detachErr := s.detachInputBounded("session_replace")
	if detachErr != nil {
		s.clearInputBinding()
		cleanupErr = detachErr
	}
	if mediaErr := s.stopMediaBounded(previousExecution); mediaErr != nil && cleanupErr == nil {
		cleanupErr = mediaErr
	}
	s.mu.Lock()
	if s.mediaHandle == nil {
		s.mediaState = ""
	}
	s.mu.Unlock()
	s.beginFlight()
	if detachErr == nil && s.remoteInput != nil && inputEligibleStatus(status) {
		core := sessionCore(status)
		if core == "" || s.attachInputForStatus(ctx, status) != nil {
			return sessionResult{}, s.stopAfterFailedAttach(execution)
		}
	}
	result := s.publicSession(status, nil)
	result.Execution = execution
	s.recordStamp(event, result, nil, stamp)
	if err != nil {
		return result, err
	}
	if cleanupErr != nil {
		return result, &protocol.APIError{Code: protocol.CodeInternal, Message: "retired host session cleanup failed", Phase: "recovery"}
	}
	return result, nil
}

func corePackagePreMutationFailure(err error) bool {
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Phase == "recovery" {
		return false
	}
	if apiErr.Code == protocol.CodeBusy {
		return true
	}
	switch apiErr.Phase {
	case "request", "admission", "compatibility", "save":
		return true
	default:
		return false
	}
}

func hostOnlyCorePackageCleanupFailure(execution string, status protocol.Status, err error) bool {
	if execution != fogcast.ExecutionHostOnly || status.LastError == nil {
		return false
	}
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeInternal || apiErr.Phase != "recovery" {
		return false
	}
	if status.Development && status.CorePackage != nil {
		return (status.LastError.Code == protocol.CodeUnrecognizedCore && status.LastError.Phase == "identity") ||
			(status.LastError.Code == protocol.CodeInternal && status.LastError.Phase == "recovery")
	}
	return status.State == protocol.StateIdle && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && status.ObservedCore == nil && !status.Development &&
		status.Recovery == "" && status.CorePackage == nil
}

func (s *sessionCoordinator) clearInputBinding() {
	s.mu.Lock()
	s.inputBinding = sessionInputBinding{}
	s.mu.Unlock()
}

func (s *sessionCoordinator) stopServiceForReplacement() (protocol.Status, error) {
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.service.Stop(stopCtx)
}

func packageReplacementApplies(requestedTarget, packageOwnerTarget string) bool {
	requestedTarget = strings.TrimSpace(requestedTarget)
	packageOwnerTarget = strings.TrimSpace(packageOwnerTarget)
	if requestedTarget == "" || packageOwnerTarget == "" {
		return true
	}
	return requestedTarget == packageOwnerTarget
}

func (s *sessionCoordinator) stopPackageOwnedForReplacement(ctx context.Context, requestedTarget string) error {
	s.mu.Lock()
	packageOwned := s.packageOwned
	previousExecution := s.execution
	s.mu.Unlock()
	if !packageOwned {
		return nil
	}
	packageOwnerTarget := ""
	if binder, ok := s.service.(interface{ SessionTarget() (string, string) }); ok {
		packageOwnerTarget, _ = binder.SessionTarget()
	}
	if !packageReplacementApplies(requestedTarget, packageOwnerTarget) {
		return nil
	}
	if s.remoteInput != nil {
		if err := s.detachInputNow(ctx, "session_replace"); err != nil {
			return remoteInputError()
		}
	}
	if err := s.stopMediaBounded(previousExecution); err != nil {
		return err
	}
	stopped, err := s.service.Stop(ctx)
	if err != nil {
		return err
	}
	if !exactIdleStatus(stopped) {
		return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "package session did not stop to idle"}
	}
	s.restoreExecution("")
	return nil
}

func (s *sessionCoordinator) stopMedia(ctx context.Context, execution string) error {
	s.mu.Lock()
	handle := s.mediaHandle
	state := s.mediaState
	s.mu.Unlock()
	if handle == nil {
		return nil
	}
	parent := ctx
	if parent == nil || parent.Err() != nil {
		parent = context.Background()
	}
	cleanupCtx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	if err := handle.Stop(cleanupCtx); err != nil {
		s.mu.Lock()
		if state == "" {
			s.mediaState = "active"
		} else {
			s.mediaState = state
		}
		state = s.mediaState
		s.mu.Unlock()
		s.record("session.media.stop_failed", sessionResult{State: protocol.StateActive, Execution: execution, Media: state}, nil)
		return &protocol.APIError{Code: protocol.CodeInternal, Message: "media session could not be stopped"}
	}
	s.mu.Lock()
	s.mediaHandle = nil
	s.mediaState = "stopped"
	s.mu.Unlock()
	s.record("session.media.stop", sessionResult{State: protocol.StateIdle, Execution: execution, Media: "stopped"}, nil)
	return nil
}

func (s *sessionCoordinator) startMedia(ctx context.Context, id, execution string) error {
	if s.media == nil {
		return nil
	}
	handle, err := s.media.Start(ctx, id)
	if err != nil {
		media := "failed"
		if handle != nil && mediaOwnsSession(execution) {
			s.mu.Lock()
			s.execution = execution
			s.mediaHandle = handle
			s.mediaState = "active"
			s.mediaGeneration++
			generation := s.mediaGeneration
			s.mu.Unlock()
			s.watchMedia(handle, generation, execution)
			media = "active"
		} else {
			s.mu.Lock()
			var generation uint64
			if handle != nil {
				s.mediaHandle = handle
				s.mediaGeneration++
				generation = s.mediaGeneration
			}
			s.mediaState = "failed"
			s.mu.Unlock()
			if handle != nil {
				s.watchMedia(handle, generation, execution)
			}
		}
		s.record("session.media.failed", sessionResult{State: protocol.StateIdle, Execution: execution, Media: media}, nil)
		return err
	}
	s.mu.Lock()
	s.execution = execution
	s.mediaHandle = handle
	s.mediaState = mediaState(handle)
	s.mediaGeneration++
	generation := s.mediaGeneration
	s.mu.Unlock()
	s.watchMedia(handle, generation, execution)
	s.record("session.media.start", sessionResult{State: protocol.StateActive, Execution: execution, Media: mediaState(handle)}, nil)
	return nil
}

func (s *sessionCoordinator) stopMediaBounded(execution string) error {
	var first error
	for attempt := 0; attempt < 2; attempt++ {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.stopMedia(cleanupCtx, execution)
		cancel()
		if err == nil {
			return first
		}
		if first == nil {
			first = err
		}
	}
	return first
}

func (s *sessionCoordinator) stop(ctx context.Context, stamp clientStamp, retainLease bool) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	// Capture host idle before probing the currently selected target. After
	// Soft-stop, idle settings may select an unrelated kit; that probe must
	// not hide retained grants from an explicit Stop.
	alreadyIdle := s.idleWithoutPlay()
	if _, err := s.developmentActive(ctx); err != nil {
		if relErr := s.releaseKitLeaseAfterIdleExplicitStop(alreadyIdle, retainLease); relErr != nil {
			return sessionResult{}, relErr
		}
		return sessionResult{}, fogcast.WithStopStage(err, "development_probe")
	}
	s.ensureFlight()

	s.mu.Lock()
	priorBinding := s.inputBinding
	s.mu.Unlock()
	var inputErr error
	if s.remoteInput != nil {
		inputErr = s.detachInputNow(ctx, "session_stop")
	}
	s.mu.Lock()
	hadMedia := s.mediaHandle != nil
	execution := s.execution
	packageOwned := s.packageOwned
	failedWithoutHandle := !hadMedia && s.mediaState == "failed"
	s.mu.Unlock()
	var mediaErr error
	if hadMedia {
		mediaErr = s.stopMediaBounded(execution)
	}
	var st protocol.Status
	var serviceErr error
	if execution == fogcast.ExecutionFPGADevelopment || packageOwned {
		st, serviceErr = s.service.Stop(ctx)
	} else {
		st, serviceErr = s.stopServiceBounded()
	}
	if mediaErr != nil {
		return sessionResult{}, fogcast.WithStopStage(mediaErr, "media_stop")
	}
	if serviceErr != nil {
		var failure *protocol.APIError
		if errors.As(serviceErr, &failure) && failure.Code == protocol.CodeSaveFailed && inputErr == nil && s.remoteInput != nil && priorBinding.packageID != "" {
			observation, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			status, statusErr := s.service.Status(observation)
			if statusErr == nil && resumedCoreDataStatus(status) && inputBindingForStatus(status) == priorBinding {
				if attachErr := s.attachInputForStatus(observation, status); attachErr != nil {
					cancel()
					return sessionResult{}, remoteInputError()
				}
			}
			cancel()
		}
		if relErr := s.releaseKitLeaseAfterIdleExplicitStop(alreadyIdle, retainLease); relErr != nil {
			return sessionResult{}, relErr
		}
		return sessionResult{}, serviceErr
	}
	if inputErr != nil {
		return sessionResult{}, fogcast.WithStopStage(remoteInputError(), "input_detach")
	}
	result := s.publicSession(st, nil)
	if hadMedia {
		result.Execution = execution
		result.Media = "stopped"
	} else if failedWithoutHandle {
		s.mu.Lock()
		if s.mediaHandle == nil && s.mediaState == "failed" {
			s.mediaState = "stopped"
		}
		s.mu.Unlock()
		result.Execution = execution
		result.Media = "stopped"
	}
	if execution == fogcast.ExecutionFPGADevelopment || packageOwned {
		s.mu.Lock()
		if s.execution == execution {
			s.execution = ""
			s.packageOwned = false
			s.terminalStatus = nil
		}
		s.mu.Unlock()
	}
	// Idle explicit Stop releases every kit grant retained by prior Stops,
	// including one whose client was dropped when idle settings changed
	// selected_target. Sofa Soft-stop (retain_lease) keeps those grants.
	// A stop that does not reach idle, including reboot_required, never releases.
	if st.State == protocol.StateIdle && !retainLease {
		if err := s.releaseKitLeaseNow(); err != nil {
			return sessionResult{}, err
		}
	}
	s.recordStamp("session.stop", result, nil, stamp)
	return result, nil
}

func (s *sessionCoordinator) idleWithoutPlay() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.execution == "" && s.mediaHandle == nil && !s.packageOwned
}

func (s *sessionCoordinator) releaseKitLeaseNow() error {
	owner, ok := s.service.(interface{ ReleaseKitLease(context.Context) error })
	if !ok {
		return nil
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := owner.ReleaseKitLease(releaseCtx); err != nil {
		return fogcast.WithStopStage(err, "lease_release")
	}
	return nil
}

func (s *sessionCoordinator) releaseKitLeaseAfterIdleExplicitStop(alreadyIdle, retainLease bool) error {
	if retainLease || !alreadyIdle {
		return nil
	}
	return s.releaseKitLeaseNow()
}

func (s *sessionCoordinator) developmentActive(ctx context.Context) (bool, error) {
	s.mu.Lock()
	execution := s.execution
	s.mu.Unlock()
	if execution == fogcast.ExecutionFPGADevelopment {
		return true, nil
	}
	if execution != "" {
		return false, nil
	}
	var development bool
	var reconstructedExecution string
	var err error
	if probe, ok := s.service.(sessionDevelopmentStateService); ok {
		development, reconstructedExecution, err = probe.DevelopmentSessionState(ctx)
	} else if probe, ok := s.service.(sessionDevelopmentService); ok {
		development, err = probe.DevelopmentActive(ctx)
	} else {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	packageOwned := false
	if owner, ok := s.service.(sessionPackageOwnerService); ok {
		packageOwned = owner.ActivePackageOwned()
	}
	s.mu.Lock()
	if s.execution == "" {
		switch {
		case reconstructedExecution == fogcast.ExecutionFPGANative:
			s.execution = fogcast.ExecutionFPGANative
			s.packageOwned = packageOwned
			s.terminalStatus = nil
		case development:
			s.execution = fogcast.ExecutionFPGADevelopment
			s.packageOwned = packageOwned
			s.terminalStatus = nil
		}
	}
	development = s.execution == fogcast.ExecutionFPGADevelopment
	s.mu.Unlock()
	return development, nil
}

func (s *sessionCoordinator) restoreExecution(execution string) {
	s.mu.Lock()
	s.execution = execution
	if execution == "" {
		s.packageOwned = false
	}
	s.terminalStatus = nil
	s.mu.Unlock()
}

func (s *sessionCoordinator) stopAfterFailedAttach(execution string) error {
	_ = s.detachInputBounded("input_attach_failed")
	mediaErr := s.stopMediaBounded(execution)
	_, stopErr := s.stopServiceBounded()
	if mediaErr != nil {
		return mediaErr
	}
	if stopErr != nil {
		return stopErr
	}
	return remoteInputError()
}

func (s *sessionCoordinator) detachInputBounded(reason string) error {
	if s.remoteInput == nil {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.detachInputNow(cleanupCtx, reason); err != nil {
		return remoteInputError()
	}
	return nil
}

func (s *sessionCoordinator) stopServiceBounded() (protocol.Status, error) {
	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	status, err := s.service.Stop(stopCtx)
	cancel()
	if err == nil || !ambiguousBoundedStopError(err) {
		return status, err
	}
	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer reconcileCancel()
	for {
		status, statusErr := s.service.Status(reconcileCtx)
		if statusErr != nil {
			return protocol.Status{}, err
		}
		if exactIdleStatus(status) {
			return status, nil
		}
		if !provisionalStopStatus(status) {
			return protocol.Status{}, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-reconcileCtx.Done():
			timer.Stop()
			return protocol.Status{}, err
		case <-timer.C:
		}
	}
}

func ambiguousBoundedStopError(err error) bool {
	if !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *protocol.APIError
	return !errors.As(err, &apiErr) || apiErr.Code == protocol.CodeMiSTerUnavailable
}

func provisionalStopStatus(status protocol.Status) bool {
	return (status.State == protocol.StateActive || status.State == protocol.StateStopping) &&
		status.LastError == nil && !status.Development && status.Recovery == ""
}

func exactIdleStatus(status protocol.Status) bool {
	return status.State == protocol.StateIdle && status.GameID == nil && status.System == nil &&
		status.ExpectedCore == nil && status.ObservedCore == nil && status.LastError == nil &&
		!status.Development && status.Recovery == ""
}

func reconstructedSessionExecution(st protocol.Status) (execution string, packageOwned bool) {
	if st.Development && st.State != protocol.StateIdle {
		if (st.State == protocol.StateActive || st.State == protocol.StateStopping) &&
			st.CorePackage != nil && fogcast.RecognizedPlayABI(st.CorePackage.ABI.ID, int64(st.CorePackage.ABI.Major), int64(st.CorePackage.ABI.Minor)) {
			return fogcast.ExecutionFPGANative, true
		}
		return fogcast.ExecutionFPGADevelopment, st.CorePackage != nil
	}
	if st.State == protocol.StateActive {
		return fogcast.ExecutionFPGANative, false
	}
	return "", false
}

func (s *sessionCoordinator) inputStatus() (host.RemoteInputStatus, bool) {
	if s.remoteInput == nil {
		return host.RemoteInputStatus{}, false
	}
	return s.remoteInput.Status(), true
}

func (s *sessionCoordinator) sendCoreKey(ctx context.Context, event remoteinput.Event) error {
	if s.playHIDForeign() {
		return playHIDLeaseError()
	}
	if s.remoteInput == nil {
		return remoteInputError()
	}
	status := s.remoteInput.Status()
	if status.State != host.RemoteInputAttached && status.State != host.RemoteInputReconnecting {
		return remoteInputError()
	}
	sender, ok := s.remoteInput.(interface {
		SendEvent(context.Context, remoteinput.Event, time.Time) error
	})
	if !ok {
		return remoteInputError()
	}
	return sender.SendEvent(ctx, event, time.Now())
}

func (s *sessionCoordinator) playHIDForeign() bool {
	provider, ok := s.service.(interface {
		TargetConnection() fogcast.TargetConnection
	})
	if !ok {
		return false
	}
	c := provider.TargetConnection()
	return kitlease.ForeignHID(kitlease.Status{State: c.State, Owner: c.Owner})
}

func playHIDLeaseError() error {
	return &protocol.APIError{Code: protocol.CodeKitLeaseDenied, Message: "kit lease is foreign; HID is fail-closed"}
}

func (s *sessionCoordinator) attachInput(ctx context.Context) (sessionResult, error) {
	if s.remoteInput == nil {
		return sessionResult{}, remoteInputError()
	}
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()

	st, err := s.service.Status(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	if !inputEligibleStatus(st) {
		return sessionResult{}, remoteInputError()
	}
	core := sessionCore(st)
	if core == "" {
		return sessionResult{}, remoteInputError()
	}
	if err := s.attachInputForStatus(ctx, st); err != nil {
		return sessionResult{}, remoteInputError()
	}
	result := s.publicSession(st, nil)
	s.record("session.input.attach", result, nil)
	return result, nil
}

func (s *sessionCoordinator) detachInput(ctx context.Context) (sessionResult, error) {
	if s.remoteInput == nil {
		return sessionResult{}, remoteInputError()
	}
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()

	if err := s.detachInputNow(ctx, "operator_detach"); err != nil {
		return sessionResult{}, remoteInputError()
	}
	st, err := s.service.Status(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicSession(st, nil)
	s.record("session.input.detach", result, nil)
	return result, nil
}

func (s *sessionCoordinator) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	return true
}

func (s *sessionCoordinator) end() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func (s *sessionCoordinator) launchGame(ctx context.Context, id, target string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	if launcher, ok := s.service.(sessionLaunchOnService); ok {
		return launcher.LaunchOn(ctx, id, target, progress)
	}
	return s.service.Launch(ctx, id, progress)
}

func (s *sessionCoordinator) publicSession(st protocol.Status, progress *sessionProgress) sessionResult {
	result := publicSession(st, progress)
	s.mu.Lock()
	result.ID = s.id
	result.FlightID = s.flightID
	s.mu.Unlock()
	if binder, ok := s.service.(interface{ SessionTarget() (string, string) }); ok {
		result.Target, result.TargetID = binder.SessionTarget()
	}
	if s.remoteInput != nil {
		input := s.remoteInput.Status()
		result.Input = &input
	}
	return result
}

func busyError() error {
	return &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
}

func developmentMustStopError() error {
	return &protocol.APIError{Code: protocol.CodeBusy, Message: "stop the development RBF before launching a game"}
}

func remoteInputError() error {
	return &protocol.APIError{Code: protocol.CodeMiSTerUnavailable, Message: "remote input bridge is unavailable"}
}

func sessionCore(st protocol.Status) string {
	if st.ObservedCore != nil {
		return *st.ObservedCore
	}
	if st.ExpectedCore != nil {
		return *st.ExpectedCore
	}
	return ""
}

func cloneRemoteInputStatus(status *host.RemoteInputStatus) *host.RemoteInputStatus {
	if status == nil {
		return nil
	}
	copy := *status
	return &copy
}

func publicSession(st protocol.Status, progress *sessionProgress) sessionResult {
	gameID := st.GameID
	system := st.System
	if st.State != protocol.StateActive {
		gameID = nil
		system = nil
	}
	result := sessionResult{State: st.State, GameID: gameID, System: system, Progress: progress,
		CorePackage: cloneCorePackageStatus(st.CorePackage)}
	if st.State == protocol.StateActive && st.Development {
		result.Execution = fogcast.ExecutionFPGADevelopment
	} else if system != nil {
		result.Execution = fogcast.ExecutionFPGANative
	}
	return result
}

func corePackageInputStatus(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development && status.CorePackage != nil &&
		status.CorePackage.PackageID != "" && status.CorePackage.Generation != 0 && status.LastError == nil
}

func inputEligibleStatus(status protocol.Status) bool {
	if status.State != protocol.StateActive || sessionCore(status) == "" {
		return false
	}
	if !status.Development {
		return true
	}
	return (corePackageInputStatus(status) || resumedCoreDataStatus(status)) &&
		(status.CorePackage.Gamepad || corePackageHasKeyboard(status))
}

func corePackageHasKeyboard(status protocol.Status) bool {
	if status.CorePackage == nil {
		return false
	}
	for _, contract := range status.CorePackage.ActiveInterfaces {
		if contract.ID == "fes.keyboard" && contract.Major == 1 && contract.Minor == 0 {
			return true
		}
	}
	return false
}

func inputBindingForStatus(status protocol.Status) sessionInputBinding {
	binding := sessionInputBinding{core: sessionCore(status), keyboard: corePackageHasKeyboard(status)}
	if status.CorePackage != nil {
		binding.packageID = status.CorePackage.PackageID
		binding.generation = status.CorePackage.Generation
	}
	return binding
}

func (s *sessionCoordinator) attachInputForStatus(ctx context.Context, status protocol.Status) error {
	core := sessionCore(status)
	var err error
	if attacher, ok := s.remoteInput.(interface {
		AttachWithControllerPorts(context.Context, string, bool, bool, bool) error
	}); ok {
		ports, keypad := protocol.ControllerPorts(status.CorePackage)
		err = attacher.AttachWithControllerPorts(ctx, core, corePackageHasKeyboard(status), ports, keypad)
	} else if attacher, ok := s.remoteInput.(remoteInputCapabilityAttacher); ok {
		err = attacher.AttachWithCapabilities(ctx, core, corePackageHasKeyboard(status))
	} else {
		err = s.remoteInput.Attach(ctx, core)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.inputBinding = inputBindingForStatus(status)
	s.mu.Unlock()
	return nil
}

func (s *sessionCoordinator) detachInputNow(ctx context.Context, reason string) error {
	if err := s.remoteInput.Detach(ctx, reason); err != nil {
		return err
	}
	s.mu.Lock()
	s.inputBinding = sessionInputBinding{}
	s.mu.Unlock()
	return nil
}

func cloneCorePackageStatus(value *protocol.CorePackageStatus) *protocol.CorePackageStatus {
	if value == nil {
		return nil
	}
	copy := *value
	copy.ActiveInterfaces = append([]protocol.RuntimeInterface(nil), value.ActiveInterfaces...)
	if value.MediaStream != nil {
		stream := *value.MediaStream
		copy.MediaStream = &stream
	}
	return &copy
}

func (s *sessionCoordinator) currentMediaState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mediaState
}

func mediaState(handle MediaHandle) string {
	if handle == nil {
		return "inactive"
	}
	return "active"
}

func mediaOwnsSession(execution string) bool {
	return execution == fogcast.ExecutionHostOnly
}

func publicEvents(events []sessionEvent) []sessionEvent {
	result := make([]sessionEvent, len(events))
	copy(result, events)
	return result
}

type sessionEventsResult struct {
	Events []sessionEvent `json:"events"`
}

func resumedCoreDataStatus(status protocol.Status) bool {
	return status.State == protocol.StateActive && status.Development && status.Recovery == "" && status.CorePackage != nil && status.CorePackage.Generation != 0 && status.CorePackage.PersistenceMode == "persistent" &&
		(status.LastError == nil || (status.LastError.Code == protocol.CodeSaveFailed && status.LastError.Phase == "save"))
}
