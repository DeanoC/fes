package hostapi

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/protocol"
)

type placementHandoffService struct {
	mu             sync.Mutex
	state          protocol.Status
	target         string
	stops          int
	stopTarget     string
	core           bool
	placed         chan struct{}
	continueLaunch chan struct{}
}

func (s *placementHandoffService) Game(context.Context, string) (catalog.Game, error) {
	kind := catalog.SourceKindRaw
	if s.core {
		kind = catalog.SourceKindCorePackage
	}
	return catalog.Game{ID: "placed-game", Kind: kind}, nil
}
func (s *placementHandoffService) Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return protocol.CachedLaunchResponse{Status: s.state}, nil
}
func (s *placementHandoffService) LaunchOn(context.Context, string, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.mu.Lock()
	s.target = "kit-b"
	s.state = protocol.Status{State: protocol.StateActive, GameID: stringPointer("placed-game"), System: systemPointer(protocol.SystemSNES), ObservedCore: stringPointer("SNES")}
	status := s.state
	placed, continueLaunch := s.placed, s.continueLaunch
	s.mu.Unlock()
	if placed != nil {
		close(placed)
	}
	if continueLaunch != nil {
		<-continueLaunch
	}
	return protocol.CachedLaunchResponse{Status: status}, nil
}
func (s *placementHandoffService) LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error) {
	return protocol.Status{}, nil
}
func (s *placementHandoffService) Stop(ctx context.Context) (protocol.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops++
	s.stopTarget = fogcast.SessionTargetFromContext(ctx)
	s.state = protocol.Status{State: protocol.StateIdle}
	return s.state, nil
}
func (s *placementHandoffService) Status(context.Context) (protocol.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

type blockingHandoffInput struct {
	detachStarted chan struct{}
	release       chan struct{}
	attached      int
}

func (i *blockingHandoffInput) Attach(context.Context, string) error { i.attached++; return nil }
func (i *blockingHandoffInput) Detach(context.Context, string) error {
	close(i.detachStarted)
	<-i.release
	return nil
}
func (*blockingHandoffInput) Status() host.RemoteInputStatus {
	return host.RemoteInputStatus{State: host.RemoteInputDetached}
}

type handoffMedia struct{ handles []*handoffMediaHandle }

func (m *handoffMedia) Start(context.Context, string) (MediaHandle, error) {
	handle := &handoffMediaHandle{}
	m.handles = append(m.handles, handle)
	return handle, nil
}

type handoffMediaHandle struct {
	stopped int
	err     error
}

func (h *handoffMediaHandle) Stop(context.Context) error { h.stopped++; return h.err }

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

func TestPlacementHandoffCleanupFailureRollsBackPlacedPlayAndKeepsOldMedia(t *testing.T) {
	service := &placementHandoffService{target: "kit-a"}
	root := newSessionCoordinator(service, nil, &handoffMedia{})
	root.remoteInputFactory = func(string) host.RemoteInputController { return &handoffInput{} }
	a, b := root.forTarget("kit-a"), root.forTarget("kit-b")
	old := &handoffMediaHandle{err: errors.New("stop failed")}
	b.execution, b.mediaHandle, b.mediaState = fogcast.ExecutionFPGANative, old, "active"
	_, err := a.launch(context.Background(), "placed-game", "", clientStamp{})
	if err == nil {
		t.Fatal("launch succeeded despite B media cleanup failure")
	}
	if b.execution != fogcast.ExecutionFPGANative || b.mediaHandle != old || b.mediaState != "active" {
		t.Fatalf("B ownership overwritten after failed cleanup: execution=%q handle=%p state=%q", b.execution, b.mediaHandle, b.mediaState)
	}
	if service.stops != 1 || service.stopTarget != "kit-b" || service.state.State != protocol.StateIdle {
		t.Fatalf("placed play rollback: stops=%d target=%q status=%s", service.stops, service.stopTarget, service.state.State)
	}
}

func TestPackagePlacementBusyCoordinatorStopsPlacedPlay(t *testing.T) {
	service := &placementHandoffService{target: "kit-a", core: true}
	root := newSessionCoordinator(service, nil, nil)
	root.remoteInputFactory = func(string) host.RemoteInputController { return &handoffInput{} }
	a, b := root.forTarget("kit-a"), root.forTarget("kit-b")
	b.busy = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := a.launch(ctx, "placed-game", "", clientStamp{})
	if err == nil || service.stops != 1 || service.stopTarget != "kit-b" || service.state.State != protocol.StateIdle {
		t.Fatalf("busy admission rollback: err=%v stops=%d target=%q status=%s", err, service.stops, service.stopTarget, service.state.State)
	}
}

func TestPlacementAdoptionRechecksAfterConcurrentStop(t *testing.T) {
	service := &placementHandoffService{target: "kit-b", state: protocol.Status{State: protocol.StateActive, GameID: stringPointer("placed-game")}}
	root := newSessionCoordinator(service, nil, nil)
	b := root.forTarget("kit-b")
	input := &blockingHandoffInput{detachStarted: make(chan struct{}), release: make(chan struct{})}
	b.remoteInput = input
	stopDone := make(chan error, 1)
	go func() { _, err := b.stop(context.Background(), clientStamp{}, false, false, nil); stopDone <- err }()
	<-input.detachStarted
	service.mu.Lock()
	placed := service.state
	service.mu.Unlock()
	installed := false
	done := make(chan error, 1)
	go func() {
		_, err := root.adoptPlacedPlay(context.Background(), b, placed, func() (sessionResult, error) {
			installed = true
			return sessionResult{}, nil
		})
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	close(input.release)
	if err := <-stopDone; err != nil {
		t.Fatalf("concurrent Stop: %v", err)
	}
	if err := <-done; err == nil {
		t.Fatal("adopted a play that Stop had already ended")
	}
	if installed || b.execution != "" || b.mediaHandle != nil || b.inputBinding != (sessionInputBinding{}) {
		t.Fatalf("stale adoption installed resources: installed=%v execution=%q", installed, b.execution)
	}
}

func TestPlacementAdoptionRejectsSameGameNewPackageGeneration(t *testing.T) {
	packageStatus := func(generation uint64) protocol.Status {
		return protocol.Status{State: protocol.StateActive, GameID: stringPointer("placed-game"), CorePackage: &protocol.CorePackageStatus{PackageID: "package-id", Generation: generation}}
	}
	service := &placementHandoffService{target: "kit-b", state: packageStatus(12)}
	root := newSessionCoordinator(service, nil, nil)
	b := root.forTarget("kit-b")
	installed := false
	_, err := root.adoptPlacedPlay(context.Background(), b, packageStatus(11), func() (sessionResult, error) {
		installed = true
		return sessionResult{}, nil
	})
	if err == nil || installed || service.stops != 0 {
		t.Fatalf("newer same-game play adopted or stopped: err=%v installed=%v stops=%d", err, installed, service.stops)
	}
	if samePlacedPlay(packageStatus(11), packageStatus(12)) {
		t.Fatal("different package generations identified as the same play")
	}
}

func TestPlacementRollbackPreservesSameGameNewPackageGeneration(t *testing.T) {
	placed := protocol.Status{State: protocol.StateActive, GameID: stringPointer("placed-game"), CorePackage: &protocol.CorePackageStatus{PackageID: "package-id", Generation: 11}}
	service := &placementHandoffService{target: "kit-b", state: protocol.Status{State: protocol.StateActive, GameID: stringPointer("placed-game"), CorePackage: &protocol.CorePackageStatus{PackageID: "package-id", Generation: 12}}}
	root := newSessionCoordinator(service, nil, nil)
	root.forTarget("kit-b").rollbackPlacedIfCurrent(context.Background(), placed)
	if service.stops != 0 || service.state.CorePackage.Generation != 12 {
		t.Fatalf("rollback stopped newer play: stops=%d generation=%d", service.stops, service.state.CorePackage.Generation)
	}
}

func TestStopWaitsForPlacementAdoptionAndCleansResources(t *testing.T) {
	service := &placementHandoffService{target: "kit-b", state: protocol.Status{State: protocol.StateActive, GameID: stringPointer("placed-game"), System: systemPointer(protocol.SystemSNES)}}
	root := newSessionCoordinator(service, nil, nil)
	b := root.forTarget("kit-b")
	entered, release := make(chan struct{}), make(chan struct{})
	service.mu.Lock()
	placed := service.state
	service.mu.Unlock()
	adopted := make(chan error, 1)
	go func() {
		_, err := root.adoptPlacedPlay(context.Background(), b, placed, func() (sessionResult, error) {
			b.mu.Lock()
			b.execution = fogcast.ExecutionFPGANative
			b.mu.Unlock()
			b.beginFlight()
			close(entered)
			<-release
			service.mu.Lock()
			status := service.state
			service.mu.Unlock()
			return b.publicSession(status, nil), nil
		})
		adopted <- err
	}()
	<-entered
	stopped := make(chan error, 1)
	go func() { _, err := b.stop(context.Background(), clientStamp{}, false, false, nil); stopped <- err }()
	select {
	case err := <-stopped:
		t.Fatalf("Stop overlapped adoption: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-adopted; err != nil {
		t.Fatalf("adoption: %v", err)
	}
	if err := <-stopped; err != nil {
		t.Fatalf("Stop after adoption: %v", err)
	}
	service.mu.Lock()
	state := service.state.State
	service.mu.Unlock()
	if state != protocol.StateIdle || !b.nativeStoppedIdle || b.execution != fogcast.ExecutionFPGANative {
		t.Fatalf("Stop did not clean adopted play: state=%s nativeIdle=%v execution=%q", state, b.nativeStoppedIdle, b.execution)
	}
}

func TestPlacementPathsDoNotInstallAfterStopWinsAdmission(t *testing.T) {
	for _, core := range []bool{false, true} {
		name := "ordinary"
		if core {
			name = "package"
		}
		t.Run(name, func(t *testing.T) {
			placed, continueLaunch := make(chan struct{}), make(chan struct{})
			service := &placementHandoffService{target: "kit-a", core: core,
				placed: placed, continueLaunch: continueLaunch}
			root := newSessionCoordinator(service, nil, &handoffMedia{})
			a := root.forTarget("kit-a")
			b := root.forTarget("kit-b")
			input := &blockingHandoffInput{detachStarted: make(chan struct{}), release: make(chan struct{})}
			b.remoteInput = input
			b.execution = fogcast.ExecutionFPGANative
			launchDone := make(chan error, 1)
			go func() { _, err := a.launch(context.Background(), "placed-game", "", clientStamp{}); launchDone <- err }()
			<-placed
			stopDone := make(chan error, 1)
			go func() { _, err := b.stop(context.Background(), clientStamp{}, false, false, nil); stopDone <- err }()
			<-input.detachStarted
			close(input.release)
			if err := <-stopDone; err != nil {
				t.Fatalf("Stop B: %v", err)
			}
			close(continueLaunch)
			if err := <-launchDone; err == nil {
				t.Fatal("launch adopted a play after Stop ended it")
			}
			service.mu.Lock()
			state := service.state.State
			service.mu.Unlock()
			if state != protocol.StateIdle || input.attached != 0 || b.mediaHandle != nil || b.inputBinding != (sessionInputBinding{}) {
				t.Fatalf("stale placement installed resources: state=%s attached=%d media=%v", state, input.attached, b.mediaHandle)
			}
		})
	}
}

func stringPointer(value string) *string                   { return &value }
func systemPointer(value protocol.System) *protocol.System { return &value }
