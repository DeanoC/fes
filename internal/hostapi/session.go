package hostapi

import (
	"context"
	"sync"

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

type sessionProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type sessionResult struct {
	State     protocol.State          `json:"state"`
	GameID    *string                 `json:"game_id,omitempty"`
	System    *protocol.System        `json:"system,omitempty"`
	Execution string                  `json:"execution,omitempty"`
	Progress  *sessionProgress        `json:"progress,omitempty"`
	Input     *host.RemoteInputStatus `json:"input,omitempty"`
}

type sessionEvent struct {
	Sequence uint64                  `json:"sequence"`
	Event    string                  `json:"event"`
	State    protocol.State          `json:"state"`
	GameID   *string                 `json:"game_id,omitempty"`
	System   *protocol.System        `json:"system,omitempty"`
	Progress *sessionProgress        `json:"progress,omitempty"`
	Input    *host.RemoteInputStatus `json:"input,omitempty"`
}

type sessionCoordinator struct {
	service     sessionService
	remoteInput host.RemoteInputController
	mu          sync.Mutex
	busy        bool
	sequence    uint64
	events      []sessionEvent
}

func newSessionCoordinator(service sessionService, remoteInput host.RemoteInputController) *sessionCoordinator {
	return &sessionCoordinator{service: service, remoteInput: remoteInput}
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
		// A new target session must never inherit the prior session's bridge
		// identity or virtual-device state.
		if err := s.remoteInput.Detach(ctx, "session_replace"); err != nil {
			return sessionResult{}, remoteInputError()
		}
	}

	var progress sessionProgress
	resp, err := s.service.Launch(ctx, id, func(value fogcast.Progress) {
		progress = sessionProgress{Stage: value.Stage, Message: value.Message}
	})
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicSession(resp.Status, &progress)
	if s.remoteInput != nil && resp.Status.State == protocol.StateActive {
		core := sessionCore(resp.Status)
		if core == "" {
			_, _ = s.service.Stop(ctx)
			return sessionResult{}, remoteInputError()
		}
		if err := s.remoteInput.Attach(ctx, core); err != nil {
			_, _ = s.service.Stop(ctx)
			return sessionResult{}, remoteInputError()
		}
		result = s.publicSession(resp.Status, &progress)
	}
	s.record("session.launch", result, &progress)
	return result, nil
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
	st, err := s.service.Stop(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	if inputErr != nil {
		return sessionResult{}, remoteInputError()
	}
	result := s.publicSession(st, nil)
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

func publicEvents(events []sessionEvent) []sessionEvent {
	result := make([]sessionEvent, len(events))
	copy(result, events)
	return result
}

type sessionEventsResult struct {
	Events []sessionEvent `json:"events"`
}
