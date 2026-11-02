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
	for _, change := range []string{"none", "generation", "package", "core", "keyboard", "hid", "gamepad", "keypad", "mouse", "not_ready"} {
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
			case "hid":
				service.status.CorePackage.ActiveInterfaces = []protocol.RuntimeInterface{{ID: "fes.gamepad.ports", Major: 1}}
			case "gamepad":
				service.status.CorePackage.ActiveInterfaces = []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}}
			case "keypad":
				service.status.CorePackage.ActiveInterfaces = append(service.status.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: "fes.keypad.ports", Major: 1})
			case "mouse":
				service.status.CorePackage.ActiveInterfaces = append(service.status.CorePackage.ActiveInterfaces, protocol.RuntimeInterface{ID: "fes.mouse.relative", Major: 1})
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

func TestInputResponsesPreserveActiveExecutionButNotAfterExternalStop(t *testing.T) {
	for _, tc := range []struct{ name, abi, execution string }{
		{"native", "fes.computer", "fpga_native"},
		{"development", "unknown.development", "fpga_development"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core := "fes.c64"
			service := &fakeService{status: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
				CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 7, Gamepad: true,
					ABI: protocol.RuntimeContract{ID: tc.abi, Major: 1}, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard.hid", Major: 1}}}}}
			input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
			handler := hostapi.New(service, hostapi.WithRemoteInput(input))
			initial := serve(t, handler, http.MethodGet, "/api/v1/session")
			if initial.Code != http.StatusOK || !strings.Contains(initial.Body.String(), `"execution":"`+tc.execution+`"`) {
				t.Fatalf("initial session: %d %s", initial.Code, initial.Body)
			}
			for _, path := range []string{"/api/v1/session/input/attach", "/api/v1/session/input/detach", "/api/v1/session/input/attach"} {
				response := serve(t, handler, http.MethodPost, path)
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"execution":"`+tc.execution+`"`) {
					t.Fatalf("%s: %d %s", path, response.Code, response.Body)
				}
			}
			service.status = protocol.Status{State: protocol.StateIdle}
			response := serve(t, handler, http.MethodPost, "/api/v1/session/input/detach")
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"idle"`) || strings.Contains(response.Body.String(), `"execution"`) {
				t.Fatalf("detach after external stop: %d %s", response.Code, response.Body)
			}
		})
	}
}
