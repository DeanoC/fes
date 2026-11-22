package hostapi

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type foreignStatusService struct {
	sessionService
	statusValue  protocol.Status
	observations int
}

func (s *foreignStatusService) Status(context.Context) (protocol.Status, error) {
	return s.statusValue, nil
}
func (*foreignStatusService) KitHostMode() bool { return true }
func (s *foreignStatusService) KitLocalForeignLease(context.Context, string) (bool, error) {
	s.observations++
	return true, nil
}

type endedLocalMedia struct {
	done  chan struct{}
	stops atomic.Int32
}

func (m *endedLocalMedia) Stop(context.Context) error { m.stops.Add(1); return nil }
func (m *endedLocalMedia) Done() <-chan struct{}      { return m.done }

func TestForeignStatusStillCleansTerminalLocalMedia(t *testing.T) {
	core := "C64"
	svc := &foreignStatusService{statusValue: protocol.Status{State: protocol.StateActive, ObservedCore: &core}}
	coordinator := newSessionCoordinator(svc, nil, nil)
	coordinator.target = fogcast.KitHostTargetName
	media := &endedLocalMedia{done: make(chan struct{})}
	close(media.done)
	coordinator.mediaHandle = media
	coordinator.mediaState = "active"
	coordinator.execution = "stale"
	result, err := coordinator.status(context.Background())
	if err != nil || media.stops.Load() != 1 || result.Input == nil || result.Input.Ready || result.Execution == "stale" || result.Media == "active" {
		t.Fatalf("foreign status=%+v err=%v local media stops=%d", result, err, media.stops.Load())
	}
}
func TestForeignStatusExceptionRejectsRuntimeRecovery(t *testing.T) {
	core := "C64"
	for _, st := range []protocol.Status{
		{State: protocol.StateActive, ObservedCore: &core, Recovery: "required"},
		{State: protocol.StateActive, ObservedCore: &core, LastError: &protocol.APIError{Code: protocol.CodeBusy}},
	} {
		svc := &foreignStatusService{statusValue: st}
		coordinator := newSessionCoordinator(svc, nil, nil)
		coordinator.target = fogcast.KitHostTargetName
		_, _ = coordinator.status(context.Background())
		if svc.observations != 0 {
			t.Fatal("recovery status took foreign input exception")
		}
	}
}
