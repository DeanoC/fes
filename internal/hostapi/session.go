package hostapi

import (
	"context"
	"sync"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type sessionService interface {
	Game(context.Context, string) (catalog.Game, error)
	Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error)
	Stop(context.Context) (protocol.Status, error)
	Status(context.Context) (protocol.Status, error)
}

// MediaSession is the session-owned seam for host-only media transport.
type MediaSession interface {
	Start(context.Context, string) (MediaHandle, error)
}

type MediaHandle interface {
	Stop(context.Context) error
}

type sessionExecutionService interface {
	SessionExecution(context.Context, string) (string, error)
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
	service     sessionService
	remoteInput host.RemoteInputController
	media       MediaSession
	mediaHandle MediaHandle
	execution   string
	mediaState  string
	mu          sync.Mutex
	busy        bool
	sequence    uint64
	events      []sessionEvent
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
	st, err := s.service.Status(ctx)
	result := s.publicSession(st, nil)
	s.mu.Lock()
	if s.execution != "" {
		result.Execution = s.execution
	}
	if s.mediaState != "" {
		result.Media = s.mediaState
	}
	s.mu.Unlock()
	if err == nil {
		s.record("session.status", result, nil)
	}
	return result, err
}

func (s *sessionCoordinator) launch(ctx context.Context, id string) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()

	if s.remoteInput != nil {
		if err := s.remoteInput.Detach(ctx, "session_replace"); err != nil {
			return sessionResult{}, remoteInputError()
		}
	}
	if err := s.stopMedia(ctx, "host_only"); err != nil {
		return sessionResult{}, err
	}

	execution := "fpga_native"
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
	if execution != "host_only" {
		s.mediaHandle = nil
		s.mediaState = ""
	}
	s.mu.Unlock()
	if execution == "host_only" && s.media != nil {
		handle, err := s.media.Start(ctx, id)
		if err != nil {
			s.record("session.media.failed", sessionResult{State: protocol.StateIdle, Execution: execution, Media: "failed"}, nil)
			return sessionResult{}, err
		}
		s.mu.Lock()
		s.execution = execution
		s.mediaHandle = handle
		s.mediaState = mediaState(handle)
		s.mu.Unlock()
		s.record("session.media.start", sessionResult{State: protocol.StateActive, Execution: execution, Media: mediaState(handle)}, nil)
	}

	var progress sessionProgress
	resp, err := s.service.Launch(ctx, id, func(value fogcast.Progress) {
		progress = sessionProgress{Stage: value.Stage, Message: value.Message}
	})
	if err != nil {
		_ = s.stopMedia(ctx, execution)
		return sessionResult{}, err
	}
	result := s.publicSession(resp.Status, &progress)
	result.Execution = execution
	if execution == "host_only" {
		if resp.Status.State != protocol.StateActive {
			if err := s.stopMedia(ctx, execution); err != nil {
				return sessionResult{}, err
			}
		}
		result.Media = s.currentMediaState()
	}
	if s.remoteInput != nil && resp.Status.State == protocol.StateActive {
		core := sessionCore(resp.Status)
		if core == "" {
			_, _ = s.service.Stop(ctx)
			s.stopMedia(ctx, execution)
			return sessionResult{}, remoteInputError()
		}
		if err := s.remoteInput.Attach(ctx, core); err != nil {
			_, _ = s.service.Stop(ctx)
			s.stopMedia(ctx, execution)
			return sessionResult{}, remoteInputError()
		}
		result = s.publicSession(resp.Status, &progress)
		result.Execution = execution
		if execution == "host_only" {
			result.Media = s.currentMediaState()
		}
	}
	s.record("session.launch", result, &progress)
	return result, nil
}

func (s *sessionCoordinator) stopMedia(ctx context.Context, execution string) error {
	s.mu.Lock()
	handle := s.mediaHandle
	s.mu.Unlock()
	if execution != "host_only" || handle == nil {
		return nil
	}
	cleanupCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		cleanupCtx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}
	if err := handle.Stop(cleanupCtx); err != nil {
		s.mu.Lock()
		s.mediaState = "active"
		s.mu.Unlock()
		s.record("session.media.stop_failed", sessionResult{State: protocol.StateActive, Execution: execution, Media: "active"}, nil)
		return &protocol.APIError{Code: protocol.CodeInternal, Message: "media session could not be stopped"}
	}
	s.mu.Lock()
	s.mediaHandle = nil
	s.mediaState = "stopped"
	s.mu.Unlock()
	s.record("session.media.stop", sessionResult{State: protocol.StateIdle, Execution: execution, Media: "stopped"}, nil)
	return nil
}

func (s *sessionCoordinator) stop(ctx context.Context) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()

	var inputErr error
	if s.remoteInput != nil {
		inputErr = s.remoteInput.Detach(ctx, "session_stop")
	}
	s.mu.Lock()
	hadMedia := s.mediaHandle != nil
	s.mu.Unlock()
	if hadMedia {
		if mediaErr := s.stopMedia(ctx, "host_only"); mediaErr != nil {
			return sessionResult{}, mediaErr
		}
	}
	st, err := s.service.Stop(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	if inputErr != nil {
		return sessionResult{}, remoteInputError()
	}
	result := s.publicSession(st, nil)
	if hadMedia {
		result.Execution = "host_only"
		result.Media = "stopped"
	}
	s.record("session.stop", result, nil)
	return result, nil
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
	if system != nil {
		result.Execution = "fpga_native"
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

func publicEvents(events []sessionEvent) []sessionEvent {
	result := make([]sessionEvent, len(events))
	copy(result, events)
	return result
}

type sessionEventsResult struct {
	Events []sessionEvent `json:"events"`
}
