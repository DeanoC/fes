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

type sessionCoordinator struct {
	service sessionService
	mu      sync.Mutex
	busy    bool
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

func newSessionCoordinator(service sessionService) *sessionCoordinator {
	return &sessionCoordinator{service: service}
}
func (s *sessionCoordinator) status(ctx context.Context) (sessionResult, error) {
	st, err := s.service.Status(ctx)
	return publicSession(st, nil), err
}
func (s *sessionCoordinator) launch(ctx context.Context, id string) (sessionResult, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	s.busy = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	var p sessionProgress
	resp, err := s.service.Launch(ctx, id, func(v fogcast.Progress) {
		s.mu.Lock()
		p = sessionProgress{Stage: v.Stage, Message: v.Message}
		s.mu.Unlock()
	})
	if err != nil {
		return sessionResult{}, err
	}
	return publicSession(resp.Status, &p), nil
}
func (s *sessionCoordinator) stop(ctx context.Context) (sessionResult, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeBusy, Message: "another launch or stop transition is running"}
	}
	s.busy = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.busy = false; s.mu.Unlock() }()
	st, err := s.service.Stop(ctx)
	return publicSession(st, nil), err
}
func publicSession(st protocol.Status, p *sessionProgress) sessionResult {
	gameID := st.GameID
	system := st.System
	if st.State != protocol.StateActive {
		gameID = nil
		system = nil
	}
	r := sessionResult{State: st.State, GameID: gameID, System: system, Progress: p}
	if system != nil {
		r.Execution = "fpga_native"
	}
	return r
}
