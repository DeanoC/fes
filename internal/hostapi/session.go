package hostapi

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/protocol"
)

type sessionService interface {
	Game(context.Context, string) (catalog.Game, error)
	Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
	LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
	Status(context.Context) (protocol.Status, error)
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

type sessionProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type sessionResult struct {
	State     protocol.State          `json:"state"`
	GameID    *string                 `json:"game_id,omitempty"`
	System    *protocol.System        `json:"system,omitempty"`
	Execution string                  `json:"execution,omitempty"`
	Media     string                  `json:"media,omitempty"`
	Progress  *sessionProgress        `json:"progress,omitempty"`
	Input     *host.RemoteInputStatus `json:"input,omitempty"`
}

type sessionEvent struct {
	Sequence uint64                  `json:"sequence"`
	Event    string                  `json:"event"`
	State    protocol.State          `json:"state"`
	GameID   *string                 `json:"game_id,omitempty"`
	System   *protocol.System        `json:"system,omitempty"`
	Media    string                  `json:"media,omitempty"`
	Progress *sessionProgress        `json:"progress,omitempty"`
	Input    *host.RemoteInputStatus `json:"input,omitempty"`
}

type sessionCoordinator struct {
	service         sessionService
	remoteInput     host.RemoteInputController
	media           MediaSession
	mediaHandle     MediaHandle
	mediaGeneration uint64
	execution       string
	mediaState      string
	terminalStatus  *protocol.Status
	mu              sync.Mutex
	observationMu   sync.Mutex
	busy            bool
	sequence        uint64
	events          []sessionEvent
}

func newSessionCoordinator(service sessionService, remoteInput host.RemoteInputController, media MediaSession) *sessionCoordinator {
	return &sessionCoordinator{service: service, remoteInput: remoteInput, media: media}
}

func (s *sessionCoordinator) record(event string, result sessionResult, progress *sessionProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	s.events = append(s.events, sessionEvent{
		Sequence: s.sequence,
		Event:    event,
		State:    result.State,
		GameID:   result.GameID,
		System:   result.System,
		Media:    result.Media,
		Progress: progress,
		Input:    cloneRemoteInputStatus(result.Input),
	})
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
	if st.State == protocol.StateIdle && s.execution == fogcast.ExecutionFPGADevelopment {
		s.execution = ""
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

func (s *sessionCoordinator) launch(ctx context.Context, id string) (sessionResult, error) {
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

	if s.remoteInput != nil {
		if err := s.remoteInput.Detach(ctx, "session_replace"); err != nil {
			return sessionResult{}, remoteInputError()
		}
	}
	s.mu.Lock()
	previousExecution := s.execution
	s.mu.Unlock()
	if err := s.stopMediaBounded(previousExecution); err != nil {
		return sessionResult{}, err
	}

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
			return sessionResult{}, err
		}
	}

	var progress sessionProgress
	resp, err := s.service.Launch(ctx, id, func(value fogcast.Progress) {
		progress = sessionProgress{Stage: value.Stage, Message: value.Message}
	})
	if err != nil {
		_ = s.stopMediaBounded(execution)
		s.restoreExecution(previousExecution)
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
		if err := s.remoteInput.Attach(ctx, core); err != nil {
			return sessionResult{}, s.stopAfterFailedAttach(execution)
		}
		result = s.publicSession(resp.Status, &progress)
		result.Execution = execution
		result.Media = s.currentMediaState()
	}
	s.record("session.launch", result, &progress)
	if resp.Status.State == protocol.StateActive {
		if recorder, ok := s.service.(interface {
			RecordPlay(context.Context, string) error
		}); ok {
			_ = recorder.RecordPlay(ctx, id)
		}
	}
	return result, nil
}

func (s *sessionCoordinator) loadDevelopmentRBF(ctx context.Context, size int64, content io.Reader) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()

	if s.remoteInput != nil {
		if err := s.remoteInput.Detach(ctx, "session_replace"); err != nil {
			return sessionResult{}, remoteInputError()
		}
	}
	s.mu.Lock()
	previousExecution := s.execution
	s.mu.Unlock()
	if err := s.stopMediaBounded(previousExecution); err != nil {
		return sessionResult{}, err
	}

	status, err := s.service.LoadDevelopmentRBF(ctx, size, content)
	if err != nil {
		if observed, observeErr := s.service.Status(ctx); observeErr == nil {
			s.mu.Lock()
			switch {
			case observed.Development && observed.State != protocol.StateIdle:
				s.execution = fogcast.ExecutionFPGADevelopment
				s.terminalStatus = nil
			case observed.State == protocol.StateIdle:
				s.execution = ""
				s.terminalStatus = nil
			}
			s.mu.Unlock()
		}
		return sessionResult{}, err
	}
	s.mu.Lock()
	s.execution = fogcast.ExecutionFPGADevelopment
	s.mediaHandle = nil
	s.mediaState = ""
	s.terminalStatus = nil
	s.mu.Unlock()
	result := s.publicSession(status, nil)
	result.Execution = fogcast.ExecutionFPGADevelopment
	s.record("session.development_rbf", result, nil)
	return result, nil
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

func (s *sessionCoordinator) stop(ctx context.Context) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if _, err := s.developmentActive(ctx); err != nil {
		return sessionResult{}, err
	}

	var inputErr error
	if s.remoteInput != nil {
		inputErr = s.remoteInput.Detach(ctx, "session_stop")
	}
	s.mu.Lock()
	hadMedia := s.mediaHandle != nil
	execution := s.execution
	failedWithoutHandle := !hadMedia && s.mediaState == "failed"
	s.mu.Unlock()
	var mediaErr error
	if hadMedia {
		mediaErr = s.stopMediaBounded(execution)
	}
	var st protocol.Status
	var serviceErr error
	if execution == fogcast.ExecutionFPGADevelopment {
		st, serviceErr = s.service.Stop(ctx)
	} else {
		st, serviceErr = s.stopServiceBounded()
	}
	if mediaErr != nil {
		return sessionResult{}, mediaErr
	}
	if serviceErr != nil {
		return sessionResult{}, serviceErr
	}
	if inputErr != nil {
		return sessionResult{}, remoteInputError()
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
	if execution == fogcast.ExecutionFPGADevelopment {
		s.mu.Lock()
		if s.execution == fogcast.ExecutionFPGADevelopment {
			s.execution = ""
			s.terminalStatus = nil
		}
		s.mu.Unlock()
	}
	s.record("session.stop", result, nil)
	return result, nil
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
	probe, ok := s.service.(sessionDevelopmentService)
	if !ok {
		return false, nil
	}
	development, err := probe.DevelopmentActive(ctx)
	if err != nil {
		return false, err
	}
	if !development {
		return false, nil
	}
	s.mu.Lock()
	if s.execution == "" {
		s.execution = fogcast.ExecutionFPGADevelopment
		s.terminalStatus = nil
	}
	development = s.execution == fogcast.ExecutionFPGADevelopment
	s.mu.Unlock()
	return development, nil
}

func (s *sessionCoordinator) restoreExecution(execution string) {
	s.mu.Lock()
	s.execution = execution
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
	if err := s.remoteInput.Detach(cleanupCtx, reason); err != nil {
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

func (s *sessionCoordinator) inputStatus() (host.RemoteInputStatus, bool) {
	if s.remoteInput == nil {
		return host.RemoteInputStatus{}, false
	}
	return s.remoteInput.Status(), true
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
	if st.State != protocol.StateActive {
		return sessionResult{}, remoteInputError()
	}
	core := sessionCore(st)
	if core == "" {
		return sessionResult{}, remoteInputError()
	}
	if err := s.remoteInput.Attach(ctx, core); err != nil {
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

	if err := s.remoteInput.Detach(ctx, "operator_detach"); err != nil {
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

func (s *sessionCoordinator) publicSession(st protocol.Status, progress *sessionProgress) sessionResult {
	result := publicSession(st, progress)
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
	result := sessionResult{State: st.State, GameID: gameID, System: system, Progress: progress}
	if st.State == protocol.StateActive && st.Development {
		result.Execution = fogcast.ExecutionFPGADevelopment
	} else if system != nil {
		result.Execution = fogcast.ExecutionFPGANative
	}
	return result
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
