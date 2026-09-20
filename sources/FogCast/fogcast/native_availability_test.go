package fogcast

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestNativeAdmissionWithQueuedTargetWriter(t *testing.T) {
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	target := targetByName(s.targets, s.selectedTarget)
	s.rememberNativeAvailability(target, protocol.Health{NativeCores: &protocol.NativeCoreAvailability{Version: 1, Systems: []protocol.System{}}}, nil)
	s.targetMu.RLock()
	defer s.targetMu.RUnlock()
	writerDone := make(chan struct{})
	go func() { s.targetMu.Lock(); s.targetMu.Unlock(); close(writerDone) }()
	deadline := time.Now().Add(time.Second)
	for s.targetMu.TryRLock() {
		s.targetMu.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("writer did not queue")
		}
		runtime.Gosched()
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := s.launchGame(context.Background(), catalog.Game{ID: "pong", System: protocol.SystemPong, Kind: catalog.SourceKindBuiltin}, nil)
		done <- err
	}()
	select {
	case err := <-done:
		var api *protocol.APIError
		if !errors.As(err, &api) || api.Code != protocol.CodeUnsupportedOperation {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("admission recursively acquired target read lock behind queued writer")
	}
}

func TestNativeAvailabilityProjectionAndIsolation(t *testing.T) {
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	target := targetByName(s.targets, s.selectedTarget)
	cores := &protocol.NativeCoreAvailability{Version: 1, Systems: []protocol.System{protocol.SystemPong}}
	s.rememberNativeAvailability(target, protocol.Health{NativeCores: cores}, nil)
	cores.Systems[0] = protocol.SystemSNES
	if !s.PlatformLaunchable(protocol.SystemPong) || s.PlatformLaunchable(protocol.SystemSNES) || !s.PlatformLaunchable(catalog.CorePlatform) {
		t.Fatal("projection lost copied capability or package exception")
	}
	s.rememberNativeAvailability(target, protocol.Health{}, errors.New("observation failed"))
	if s.PlatformLaunchable(protocol.SystemPong) {
		t.Fatal("failure retained positive capability")
	}
	for _, capability := range []*protocol.NativeCoreAvailability{
		{Version: 1, Systems: []protocol.System{}}, {Version: 2, Systems: []protocol.System{protocol.SystemPong}}, {Version: 1, Systems: nil},
	} {
		s.rememberNativeAvailability(target, protocol.Health{NativeCores: capability}, nil)
		if s.PlatformLaunchable(protocol.SystemPong) {
			t.Fatal("unknown/empty capability admitted native core")
		}
	}
	s.rememberNativeAvailability(target, protocol.Health{}, nil)
	if !s.PlatformLaunchable(protocol.SystemPong) {
		t.Fatal("older Main contract broken")
	}
	// A new endpoint must not inherit the previous endpoint's capability.
	s.targets[0].Address = "http://new-target.invalid"
	s.nativeAvailabilityMu.RLock()
	_, found := s.nativeAvailability[nativeKey(s.targets[0])]
	s.nativeAvailabilityMu.RUnlock()
	if found {
		t.Fatal("observation survived target rebinding")
	}
}

func TestBuiltinPongUnavailableRejectedBeforeMutation(t *testing.T) {
	client := &fakeServiceClient{}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	target := targetByName(s.targets, s.selectedTarget)
	s.rememberNativeAvailability(target, protocol.Health{NativeCores: &protocol.NativeCoreAvailability{Version: 1, Systems: []protocol.System{}}}, nil)
	_, _, err := s.launchGame(context.Background(), catalog.Game{ID: "pong", System: protocol.SystemPong, Kind: catalog.SourceKindBuiltin}, nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeUnsupportedOperation || api.Phase != "admission" {
		t.Fatalf("err=%v", err)
	}
	if client.nativeLaunchCalls != 0 {
		t.Fatal("missing core dispatched")
	}
}

func TestNativeAvailabilityOnlyResolvedHostExecutionBypasses(t *testing.T) {
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	s.hostExecutor = &fakeHostExecutor{}
	target := targetByName(s.targets, s.selectedTarget)
	s.rememberNativeAvailability(target, protocol.Health{NativeCores: &protocol.NativeCoreAvailability{Version: 1, Systems: []protocol.System{}}}, nil)
	game := catalog.Game{System: protocol.SystemSNES}
	if s.PlatformLaunchable(game.System) || s.nativeCatalogAdmission(context.Background(), game) == nil {
		t.Fatal("merely having a host executor bypassed native admission")
	}
	s.executionResolver = ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil })
	if !s.PlatformLaunchable(game.System) || s.nativeCatalogAdmission(context.Background(), game) != nil {
		t.Fatal("selected host-only execution was blocked")
	}
}

func TestNativeUnavailableDiagnosticIsSafe(t *testing.T) {
	for _, message := range []string{"legacy native core artifact is unavailable on this target", "legacy native core is unavailable on the selected target; choose an installed FPGA package"} {
		got, phase := SafeTargetDiagnostic(&protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: message, Phase: "admission"})
		if got != "Legacy core is unavailable on this target; choose an installed FPGA package." || phase != "admission" {
			t.Fatalf("%q %q", got, phase)
		}
	}
}
