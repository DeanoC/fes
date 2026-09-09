package hostapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestLibraryPackageLaunchPreservesInputUntilAdmission(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "activate", true: "reject"}[reject], func(t *testing.T) {
			core, game := "fes.pong", "core-pong"
			active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, GameID: &game,
				CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9,
					ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true}}
			input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
			service := &fakeService{execution: fogcast.ExecutionFPGADevelopment,
				status: active, launch: protocol.CachedLaunchResponse{Status: active}}
			service.launchHook = func(context.Context) {
				if len(input.detach) != 0 {
					t.Fatal("detached prior input before admission")
				}
			}
			if reject {
				service.launchErr = &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "unsupported", Phase: "compatibility"}
			}
			handler := hostapi.New(service, hostapi.WithRemoteInput(input))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"core-pong"}`))
			req.Host = "127.0.0.1"
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if service.launchCalls != 1 {
				t.Fatalf("calls=%d response=%d %s", service.launchCalls, response.Code, response.Body.String())
			}
			if reject {
				if response.Code == 200 || len(input.detach) != 0 || len(input.attach) != 0 {
					t.Fatalf("rejected: %d detach=%v attach=%v", response.Code, input.detach, input.attach)
				}
			} else {
				if response.Code != 200 || len(input.detach) != 1 || len(input.attach) != 1 || !strings.Contains(response.Body.String(), `"game_id":"core-pong"`) {
					t.Fatalf("activated: %d %s detach=%v attach=%v", response.Code, response.Body.String(), input.detach, input.attach)
				}
			}
		})
	}
}

func TestLibraryPackageIdentityFailureNeverAttachesInput(t *testing.T) {
	core := "unexpected.core"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("b", 64), Generation: 3, ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("c", 32), Gamepad: true}}
	s := &fakeService{execution: fogcast.ExecutionFPGADevelopment, launch: protocol.CachedLaunchResponse{Status: active}, launchErr: &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "wrong package", Phase: "identity"}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true}}
	handler := hostapi.New(s, hostapi.WithRemoteInput(input))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"core-pong"}`))
	req.Host = "127.0.0.1"
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code == 200 || len(input.attach) != 0 || len(input.detach) != 1 {
		t.Fatalf("response=%d attach=%v detach=%v", response.Code, input.attach, input.detach)
	}
}
