package hostapi

import (
	"context"
	"io"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/protocol"
)

type placementHandoffService struct {
	state  protocol.Status
	target string
}

func (s *placementHandoffService) Game(context.Context, string) (catalog.Game, error) {
	return catalog.Game{ID: "placed-game", Kind: catalog.SourceKindRaw}, nil
}
func (s *placementHandoffService) Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return protocol.CachedLaunchResponse{Status: s.state}, nil
}
func (s *placementHandoffService) LaunchOn(context.Context, string, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.target = "kit-b"
	s.state = protocol.Status{State: protocol.StateActive, GameID: stringPointer("placed-game"), System: systemPointer(protocol.SystemSNES), ObservedCore: stringPointer("SNES")}
	return protocol.CachedLaunchResponse{Status: s.state}, nil
}
func (s *placementHandoffService) LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error) {
	return protocol.Status{}, nil
}
func (s *placementHandoffService) Stop(context.Context) (protocol.Status, error) {
	s.state = protocol.Status{State: protocol.StateIdle}
	return s.state, nil
}
func (s *placementHandoffService) Status(context.Context) (protocol.Status, error) {
	return s.state, nil
}
func (s *placementHandoffService) DevelopmentActive(context.Context) (bool, error) { return false, nil }
func (s *placementHandoffService) SessionTargetName() string                       { return s.target }

type handoffInput struct {
	attached int
	detached int
}

func (i *handoffInput) Attach(context.Context, string) error { i.attached++; return nil }
func (i *handoffInput) Detach(context.Context, string) error { i.detached++; return nil }
func (*handoffInput) Status() host.RemoteInputStatus {
	return host.RemoteInputStatus{State: host.RemoteInputDetached}
}

type handoffMedia struct{ handles []*handoffMediaHandle }

func (m *handoffMedia) Start(context.Context, string) (MediaHandle, error) {
	handle := &handoffMediaHandle{}
	m.handles = append(m.handles, handle)
	return handle, nil
}

type handoffMediaHandle struct{ stopped int }

func (h *handoffMediaHandle) Stop(context.Context) error { h.stopped++; return nil }

func TestOmittedPlacementHandoffMovesFlightAndStopOwnership(t *testing.T) {
	service := &placementHandoffService{target: "kit-a"}
	media := &handoffMedia{}
	root := newSessionCoordinator(service, nil, media)
	inputs := map[string]*handoffInput{}
	root.remoteInputFactory = func(target string) host.RemoteInputController {
		input := &handoffInput{}
		inputs[target] = input
		return input
	}
	a := root.forTarget("kit-a")
	a.execution, a.flightID = "previous-a", "flight-a"
	result, err := a.launch(context.Background(), "placed-game", "", clientStamp{})
	if err != nil {
		t.Fatal(err)
	}
	b := root.forTarget("kit-b")
	if result.Target != "kit-b" || b.execution != fogcast.ExecutionFPGANative || b.flightID == "" || b.flightID == "flight-a" {
		t.Fatalf("B coordinator result=%+v execution=%q flight=%q", result, b.execution, b.flightID)
	}
	if inputs["kit-b"].attached != 1 || b.mediaHandle == nil || len(media.handles) != 1 {
		t.Fatalf("resolved B resources: input=%+v media=%+v handles=%d", inputs["kit-b"], b.mediaHandle, len(media.handles))
	}
	if a.execution != "previous-a" || a.flightID != "flight-a" {
		t.Fatalf("provisional A state was not restored: execution=%q flight=%q", a.execution, a.flightID)
	}
	if _, err := b.stop(context.Background(), clientStamp{}, false, false, nil); err != nil {
		t.Fatalf("Stop B: %v", err)
	}
	if !b.nativeStoppedIdle || a.execution != "previous-a" || service.state.State != protocol.StateIdle || inputs["kit-b"].detached == 0 || media.handles[0].stopped != 1 {
		t.Fatalf("after Stop B: A=%q B=%q target=%s", a.execution, b.execution, service.state.State)
	}
}

func stringPointer(value string) *string                   { return &value }
func systemPointer(value protocol.System) *protocol.System { return &value }
