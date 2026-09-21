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

func TestBuiltinPongUnavailableRejectedBeforeMutation(t *testing.T) {
	client := &fakeServiceClient{}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
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

func TestRawBuiltinRejectedWithoutLegacyAvailability(t *testing.T) {
	client := &fakeServiceClient{}
	s := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	_, _, err := s.launchGame(context.Background(), catalog.Game{
		ID: "pong", System: protocol.SystemPong, Kind: catalog.SourceKindBuiltin,
	}, nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeUnsupportedOperation || api.Phase != "admission" {
		t.Fatalf("raw FPGA game must require a package: %v", err)
	}
	if client.nativeLaunchCalls != 0 {
		t.Fatal("retired launch was dispatched")
	}
}

func TestRetiredGameCannotStopActivePackage(t *testing.T) {
	client := &fakeServiceClient{}
	game := catalog.Game{ID: "pong", System: protocol.SystemPong, Kind: catalog.SourceKindBuiltin}
	s := newTestService(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{}, client)
	s.activePackageID, s.activePackageGeneration = "retained-package", 7
	s.activeExecution, s.activeTarget = ExecutionFPGANative, s.selectedTarget
	_, err := s.LaunchOn(context.Background(), game.ID, "", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeUnsupportedOperation || api.Phase != "admission" {
		t.Fatalf("raw FPGA game must fail admission: %v", err)
	}
	if client.stopCalls != 0 || client.nativeLaunchCalls != 0 || client.launchCalls != 0 || s.activePackageID != "retained-package" || s.activePackageGeneration != 7 {
		t.Fatalf("rejected launch changed active package or dispatched: stop=%d native=%d cached=%d id=%q generation=%d", client.stopCalls, client.nativeLaunchCalls, client.launchCalls, s.activePackageID, s.activePackageGeneration)
	}
}
