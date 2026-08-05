package hostapi

import (
	"context"
	"sync"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
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
	State     protocol.State   `json:"state"`
	GameID    *string          `json:"game_id,omitempty"`
	System    *protocol.System `json:"system,omitempty"`
	Execution string           `json:"execution,omitempty"`
	Progress  *sessionProgress `json:"progress,omitempty"`
}

type sessionEvent struct {
	Sequence uint64           `json:"sequence"`
	Event    string           `json:"event"`
	State    protocol.State   `json:"state"`
	GameID   *string          `json:"game_id,omitempty"`
	System   *protocol.System `json:"system,omitempty"`
	Progress *sessionProgress `json:"progress,omitempty"`
}

type sessionCoordinator struct {
	service  sessionService
	mu       sync.Mutex
	busy     bool
	sequence uint64
	events   []sessionEvent
}

func newSessionCoordinator(service sessionService) *sessionCoordinator {
	return &sessionCoordinator{service: service}
}

func (s *sessionCoordinator) record(event string, result sessionResult, progress *sessionProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	s.events = append(s.events, sessionEvent{Sequence: s.sequence, Event: event, State: result.State, GameID: result.GameID, System: result.System, Progress: progress})
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
	result := publicSession(st, nil)
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

	var progress sessionProgress
	resp, err := s.service.Launch(ctx, id, func(value fogcast.Progress) {
		progress = sessionProgress{Stage: value.Stage, Message: value.Message}
	})
	if err != nil {
		return sessionResult{}, err
	}
	result := publicSession(resp.Status, &progress)
	s.record("session.launch", result, &progress)
	return result, nil
}

func (s *sessionCoordinator) stop(ctx context.Context) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	st, err := s.service.Stop(ctx)
	if err != nil {
		return sessionResult{}, err
	}
	result := publicSession(st, nil)
	s.record("session.stop", result, nil)
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

func busyError() error {
	return &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
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
