package fogcast

import (
	"context"
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

func TestNativeUnavailableDiagnosticIsSafe(t *testing.T) {
	for _, message := range []string{"legacy native core artifact is unavailable on this target", "legacy native core is unavailable on the selected target; choose an installed FPGA package"} {
		got, phase := SafeTargetDiagnostic(&protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: message, Phase: "admission"})
		if got != "Legacy core is unavailable on this target; choose an installed FPGA package." || phase != "admission" {
			t.Fatalf("%q %q", got, phase)
		}
	}
}

func TestRetiredGameCannotStopActivePackage(t *testing.T) {
	client := &fakeServiceClient{}
	game := catalog.Game{ID: "pong", System: protocol.SystemPong, Kind: catalog.SourceKindBuiltin}
	s := newTestService(&fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServicePreparer{}, client)
	s.plays[s.selectedTarget] = targetPlay{execution: ExecutionFPGANative, packageID: "retained-package", packageGeneration: 7}
	s.activeTarget = s.selectedTarget
	_, err := s.LaunchOn(context.Background(), game.ID, "", nil)
	var api *protocol.APIError
	if !errors.As(err, &api) || api.Code != protocol.CodeUnsupportedOperation || api.Phase != "admission" {
		t.Fatalf("raw FPGA game must fail admission: %v", err)
	}
	if client.stopCalls != 0 || client.nativeLaunchCalls != 0 || client.launchCalls != 0 || s.plays[s.selectedTarget].packageID != "retained-package" || s.plays[s.selectedTarget].packageGeneration != 7 {
		t.Fatalf("rejected launch changed kit package or dispatched: stop=%d native=%d cached=%d play=%+v", client.stopCalls, client.nativeLaunchCalls, client.launchCalls, s.plays[s.selectedTarget])
	}
}
