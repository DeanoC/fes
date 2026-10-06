package hostapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestExplicitInputAttachPreservesOnlyReadyMatchingBinding(t *testing.T) {
	for _, change := range []string{"none", "generation", "package", "core", "keyboard", "not_ready"} {
		t.Run(change, func(t *testing.T) {
			core := "fes.c64"
			status := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
				CorePackage: &protocol.CorePackageStatus{
					PackageID: strings.Repeat("a", 64), Generation: 7, Gamepad: true,
					ABI:              protocol.RuntimeContract{ID: "fes.computer", Major: 1},
					ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}, {ID: "fes.gamepad.ports", Major: 1}},
				}}
			service := &fakeService{status: status}
			input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
			handler := hostapi.New(service, hostapi.WithRemoteInput(input))
			// This follows the same automatic binding path as a library launch.
			initial := serve(t, handler, http.MethodGet, "/api/v1/session")
			if initial.Code != http.StatusOK || len(input.attach) != 1 {
				t.Fatalf("initial=%d %s attachments=%v", initial.Code, initial.Body.String(), input.attach)
			}
			input.status.SessionID = "existing-stream"
			switch change {
			case "generation":
				service.status.CorePackage.Generation++
			case "package":
				service.status.CorePackage.PackageID = strings.Repeat("b", 64)
			case "core":
				other := "fes.apple2"
				service.status.ObservedCore = &other
			case "keyboard":
				service.status.CorePackage.ActiveInterfaces = append(service.status.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: "fes.keyboard", Major: 1})
			case "not_ready":
				input.status.Ready = false
			}
			response := serve(t, handler, http.MethodPost, "/api/v1/session/input/attach")
			if change == "none" {
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "existing-stream") {
					t.Fatalf("duplicate attach=%d %s", response.Code, response.Body.String())
				}
			} else if response.Code == http.StatusOK {
				t.Fatalf("stale/unready binding accepted: %s", response.Body.String())
			}
			if len(input.attach) != 1 || len(input.detach) != 0 || input.status.SessionID != "existing-stream" {
				t.Fatalf("duplicate attach mutated stream: %+v", input)
			}
		})
	}
}
