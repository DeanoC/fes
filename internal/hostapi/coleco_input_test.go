package hostapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type capabilityAttach struct {
	core     string
	keyboard bool
}

type portsRemoteInput struct {
	fakeRemoteInput
	keyboard, ports, keypad bool
}

func (r *portsRemoteInput) AttachWithControllerPorts(ctx context.Context, core string, keyboard, ports, keypad bool) error {
	r.keyboard, r.ports, r.keypad = keyboard, ports, keypad
	return r.fakeRemoteInput.Attach(ctx, core)
}

func TestSessionBindsColecoPortsWithoutKeyboardMapping(t *testing.T) {
	core := "fes.coleco"
	status := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 3, Gamepad: true, ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad.ports", Major: 1}, {ID: "fes.keypad.ports", Major: 1}}}}
	input := &portsRemoteInput{fakeRemoteInput: fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}}
	response := serve(t, hostapi.New(&fakeService{status: status}, hostapi.WithRemoteInput(input)), http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK || !input.ports || !input.keypad || input.keyboard {
		t.Fatalf("response=%d binding=%+v", response.Code, input)
	}
}

type capabilityRemoteInput struct {
	fakeRemoteInput
	capabilities []capabilityAttach
}

func (r *capabilityRemoteInput) AttachWithCapabilities(_ context.Context, core string, keyboard bool) error {
	r.capabilities = append(r.capabilities, capabilityAttach{core: core, keyboard: keyboard})
	return r.fakeRemoteInput.Attach(context.Background(), core)
}

func TestSessionPassesExactKeyboardCapabilityToRemoteInput(t *testing.T) {
	cases := []struct {
		name         string
		core         string
		interfaces   []protocol.RuntimeInterface
		gamepad      bool
		wantKeyboard bool
	}{
		{
			name:         "sms keyboard 1.0",
			core:         "fes.sms",
			interfaces:   []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1, Minor: 0}},
			wantKeyboard: true,
		},
		{
			name:       "sms wrong keyboard minor with gamepad",
			core:       "fes.sms",
			interfaces: []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1, Minor: 1}},
			gamepad:    true,
		},
		{
			name:    "sms without keyboard with gamepad",
			core:    "fes.sms",
			gamepad: true,
		},
		{
			name:         "coleco keyboard 1.0",
			core:         "fes.coleco",
			interfaces:   []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1, Minor: 0}},
			wantKeyboard: true,
		},
		{
			name:         "coleco wrong keyboard minor with gamepad",
			core:         "fes.coleco",
			interfaces:   []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1, Minor: 1}},
			gamepad:      true,
			wantKeyboard: false,
		},
		{
			name:         "zx81 keyboard 1.0",
			core:         "fes.zx81",
			interfaces:   []protocol.RuntimeInterface{{ID: "fes.keyboard", Major: 1, Minor: 0}},
			wantKeyboard: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := protocol.Status{State: protocol.StateActive, Development: true}
			status.ObservedCore = &tc.core
			status.CorePackage = &protocol.CorePackageStatus{
				PackageID:        strings.Repeat("a", 64),
				Generation:       1,
				ActiveInterfaces: tc.interfaces,
				Gamepad:          tc.gamepad,
			}
			input := &capabilityRemoteInput{fakeRemoteInput: fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}}
			response := serve(t, hostapi.New(&fakeService{status: status}, hostapi.WithRemoteInput(input)), http.MethodGet, "/api/v1/session")
			if response.Code != http.StatusOK || len(input.capabilities) != 1 {
				t.Fatalf("response=%d %s capabilities=%+v", response.Code, response.Body.String(), input.capabilities)
			}
			got := input.capabilities[0]
			if got.core != tc.core || got.keyboard != tc.wantKeyboard {
				t.Fatalf("capability attach=%+v, want core=%q keyboard=%v", got, tc.core, tc.wantKeyboard)
			}
		})
	}
}
