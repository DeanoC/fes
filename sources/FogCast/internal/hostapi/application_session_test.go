package hostapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestApplicationSessionAttachesOnlyDeclaredInput(t *testing.T) {
	for _, gamepad := range []bool{false, true} {
		t.Run(map[bool]string{false: "video only", true: "controller and media"}[gamepad], func(t *testing.T) {
			core, game := "example.palette", "core-application"
			interfaces := []protocol.RuntimeInterface{{ID: "fes.video.fixed-720p60", Major: 1}}
			if gamepad {
				interfaces = append(interfaces, protocol.RuntimeInterface{ID: "fes.gamepad", Major: 1}, protocol.RuntimeInterface{ID: "fes.media.blob", Major: 1})
			}
			active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, GameID: &game,
				CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9,
					ABI: protocol.RuntimeContract{ID: "fes.application", Major: 1}, BuildID: strings.Repeat("b", 32),
					Gamepad: gamepad, ActiveInterfaces: interfaces}}
			input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
			service := &fakeService{execution: fogcast.ExecutionFPGADevelopment,
				status: active, launch: protocol.CachedLaunchResponse{Status: active}}
			handler := hostapi.New(service, hostapi.WithRemoteInput(input))
			response := launchSession(t, handler, game)
			want := 0
			if gamepad {
				want = 1
			}
			if response.Code != http.StatusOK || len(input.attach) != want || len(input.detach) != 1 {
				t.Fatalf("response=%d %s input attaches=%v", response.Code, response.Body.String(), input.attach)
			}
		})
	}
}
