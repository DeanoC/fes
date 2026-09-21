package hostapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
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
			service := &fakeService{execution: fogcast.ExecutionFPGANative,
				game:   catalog.Game{ID: game, Kind: catalog.SourceKindCorePackage},
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
				if response.Code != 200 || len(input.detach) != 1 || len(input.attach) != 1 || !strings.Contains(response.Body.String(), `"game_id":"core-pong"`) || !strings.Contains(response.Body.String(), `"execution":"fpga_native"`) {
					t.Fatalf("activated: %d %s detach=%v attach=%v", response.Code, response.Body.String(), input.detach, input.attach)
				}
			}
		})
	}
}

func TestLibraryPackageDefaultMediaFailureRetiresPriorOwnership(t *testing.T) {
	gameID, system, core := "prior-game", protocol.SystemSNES, "SNES"
	service := &fakeService{
		launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ObservedCore: &core}},
		status: protocol.Status{State: protocol.StateIdle},
	}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input), hostapi.WithMediaSession(media))
	if launched := launchSession(t, handler, gameID); launched.Code != http.StatusOK || len(input.attach) != 1 || len(media.start) != 1 {
		t.Fatalf("prior launch=%d %s attach=%v media=%v", launched.Code, launched.Body.String(), input.attach, media.start)
	}

	service.execution = fogcast.ExecutionFPGANative
	service.game = catalog.Game{ID: "core-coleco", Kind: catalog.SourceKindCorePackage}
	service.launch = protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateIdle}}
	service.launchErr = &protocol.APIError{Code: protocol.CodeBusy, Message: "diagnostic media rejected", Phase: "recovery"}
	service.status = protocol.Status{State: protocol.StateIdle}
	result := launchSession(t, handler, "core-coleco")
	if result.Code == http.StatusOK || len(input.detach) != 1 || len(media.stop) != 1 || input.status.State != host.RemoteInputDetached {
		t.Fatalf("stale ownership survived media failure: status=%d %s detach=%v media.stop=%v input=%+v", result.Code, result.Body.String(), input.detach, media.stop, input.status)
	}
}

func TestLibraryPackageIdentityFailureNeverAttachesInput(t *testing.T) {
	core := "unexpected.core"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("b", 64), Generation: 3, ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("c", 32), Gamepad: true}}
	s := &fakeService{execution: fogcast.ExecutionFPGANative, game: catalog.Game{ID: "core-pong", Kind: catalog.SourceKindCorePackage}, launch: protocol.CachedLaunchResponse{Status: active}, launchErr: &protocol.APIError{Code: protocol.CodeUnrecognizedCore, Message: "wrong package", Phase: "identity"}}
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

func TestLibraryPackageHostCleanupFailureRetainsMediaAndBlocksInput(t *testing.T) {
	game, system, core := "host-game", protocol.SystemSNES, "fes.pong"
	service := &fakeService{execution: fogcast.ExecutionHostOnly, launch: protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive, GameID: &game, System: &system}}, status: protocol.Status{State: protocol.StateActive, GameID: &game, System: &system}}
	input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
	media := &fakeMediaSession{}
	handler := hostapi.New(service, hostapi.WithRemoteInput(input), hostapi.WithMediaSession(media))
	if result := launchSession(t, handler, game); result.Code != http.StatusOK {
		t.Fatalf("host launch: %s", result.Body.String())
	}
	attached := len(input.attach)
	failure := &protocol.APIError{Code: protocol.CodeInternal, Phase: "recovery", Message: "host cleanup failed after core package activation"}
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, LastError: failure, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.simple-game", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true}}
	service.execution = fogcast.ExecutionFPGANative
	service.game = catalog.Game{ID: "core-pong", Kind: catalog.SourceKindCorePackage}
	service.launch = protocol.CachedLaunchResponse{Status: active}
	service.launchErr = failure
	service.status = active
	result := launchSession(t, handler, "core-pong")
	if result.Code == http.StatusOK || len(input.attach) != attached || len(media.stop) != 0 {
		t.Fatalf("cleanup owner retired: status=%d attach=%v media.stop=%v", result.Code, input.attach, media.stop)
	}
}

func TestCoreSaveFailureRestoresInputOnlyForSameResumedGeneration(t *testing.T) {
	for _, kind := range []string{"resumed", "new generation", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			core, game := "fes.pong", "core-pong"
			active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, GameID: &game, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 7, Gamepad: true, PersistenceMode: "persistent"}}
			input := &fakeRemoteInput{status: host.RemoteInputStatus{State: host.RemoteInputDetached}}
			service := &fakeService{status: active, execution: fogcast.ExecutionFPGANative, game: catalog.Game{ID: game, Kind: catalog.SourceKindCorePackage}, launch: protocol.CachedLaunchResponse{Status: active}}
			handler := hostapi.New(service, hostapi.WithRemoteInput(input))
			if out := launchSession(t, handler, game); out.Code != 200 {
				t.Fatal(out.Body)
			}
			attached := len(input.attach)
			service.stopErr = &protocol.APIError{Code: protocol.CodeSaveFailed, Message: "save failed", Phase: "save"}
			failed := active
			copy := *active.CorePackage
			failed.CorePackage = &copy
			failed.LastError = service.stopErr.(*protocol.APIError)
			if kind == "new generation" {
				failed.CorePackage.Generation++
			}
			if kind == "recovery" {
				failed.State = protocol.StateFailed
				failed.LastError = &protocol.APIError{Code: protocol.CodeSaveFailed, Message: "recovery", Phase: "recovery"}
			}
			service.status = failed
			out := serve(t, handler, http.MethodPost, "/api/v1/session/stop")
			if out.Code == 200 {
				t.Fatal("failed save reported success")
			}
			want := attached
			if kind == "resumed" {
				want++
			}
			if len(input.attach) != want {
				t.Fatalf("attach=%v want count=%d body=%s", input.attach, want, out.Body)
			}
		})
	}
}

func TestNoABICorePackageLaunchRemainsDevelopment(t *testing.T) {
	core, game := "vendor.core", "core-unknown"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, GameID: &game,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("d", 64), Generation: 2,
			ABI: protocol.RuntimeContract{ID: "vendor.unknown", Major: 1}, BuildID: strings.Repeat("e", 32), Gamepad: true}}
	service := &fakeService{execution: fogcast.ExecutionFPGADevelopment,
		game:   catalog.Game{ID: game, Kind: catalog.SourceKindCorePackage},
		status: active, launch: protocol.CachedLaunchResponse{Status: active}}
	handler := hostapi.New(service)
	response := launchSession(t, handler, game)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"execution":"fpga_development"`) {
		t.Fatalf("no-ABI package launch = %d %s", response.Code, response.Body.String())
	}
}

func TestSessionCartridgeLaunchStopsRecognizedABIPackagePlay(t *testing.T) {
	core, packageGame := "fes.coleco", "core-coleco"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, GameID: &packageGame,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 3,
			ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true}}
	order := []string{}
	cartridgeID, system, cartCore := "snes-replacement", protocol.SystemSNES, "SNES"
	cartridge := protocol.Status{State: protocol.StateActive, GameID: &cartridgeID, System: &system, ExpectedCore: &cartCore, ObservedCore: &cartCore}
	service := &fakeService{
		execution: fogcast.ExecutionFPGANative,
		game:      catalog.Game{ID: packageGame, Kind: catalog.SourceKindCorePackage},
		status:    active,
		launch:    protocol.CachedLaunchResponse{Status: active},
		stopped:   protocol.Status{State: protocol.StateIdle},
		order:     &order,
	}
	handler := hostapi.New(service)
	if out := launchSession(t, handler, packageGame); out.Code != http.StatusOK {
		t.Fatalf("package launch = %d %s", out.Code, out.Body.String())
	}

	service.game = catalog.Game{ID: cartridgeID, Kind: catalog.SourceKindRaw}
	service.launch = protocol.CachedLaunchResponse{Status: cartridge}
	order = order[:0]
	service.order = &order
	out := launchSession(t, handler, cartridgeID)
	if out.Code != http.StatusOK || service.launchCalls != 2 {
		t.Fatalf("replacement launch = %d %s calls=%d", out.Code, out.Body.String(), service.launchCalls)
	}
	if got := strings.Join(order, ","); got != "service.stop" {
		t.Fatalf("replacement order = %q", got)
	}
	if !strings.Contains(out.Body.String(), `"execution":"fpga_native"`) {
		t.Fatalf("cartridge session = %s", out.Body.String())
	}
}

func TestSessionHostOnlyLaunchStopsRecognizedABIPackagePlay(t *testing.T) {
	core, packageGame := "fes.zx81", "core-zx81"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, GameID: &packageGame,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 5,
			ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, BuildID: strings.Repeat("b", 32)}}
	order := []string{}
	hostGame := "host-title"
	hostStatus := protocol.Status{State: protocol.StateActive, GameID: &hostGame, System: systemPtr(protocol.SystemSNES)}
	service := &fakeService{
		execution: fogcast.ExecutionFPGANative,
		game:      catalog.Game{ID: packageGame, Kind: catalog.SourceKindCorePackage},
		status:    active,
		launch:    protocol.CachedLaunchResponse{Status: active},
		stopped:   protocol.Status{State: protocol.StateIdle},
		order:     &order,
	}
	handler := hostapi.New(service)
	if out := launchSession(t, handler, packageGame); out.Code != http.StatusOK {
		t.Fatalf("package launch = %d %s", out.Code, out.Body.String())
	}

	service.execution = fogcast.ExecutionHostOnly
	service.game = catalog.Game{ID: hostGame, Kind: catalog.SourceKindRaw}
	service.launch = protocol.CachedLaunchResponse{Status: hostStatus}
	order = order[:0]
	service.order = &order
	out := launchSession(t, handler, hostGame)
	if out.Code != http.StatusOK || service.launchCalls != 2 {
		t.Fatalf("host-only replacement = %d %s calls=%d", out.Code, out.Body.String(), service.launchCalls)
	}
	if got := strings.Join(order, ","); got != "service.stop" {
		t.Fatalf("replacement order = %q", got)
	}
}

func TestSessionNoABIPackageMustStopBeforeCatalogLaunch(t *testing.T) {
	core, packageGame := "vendor.core", "core-unknown"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, GameID: &packageGame,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("d", 64), Generation: 2,
			ABI: protocol.RuntimeContract{ID: "vendor.unknown", Major: 1}, BuildID: strings.Repeat("e", 32)}}
	service := &fakeService{
		execution: fogcast.ExecutionFPGADevelopment,
		game:      catalog.Game{ID: packageGame, Kind: catalog.SourceKindCorePackage},
		status:    active,
		launch:    protocol.CachedLaunchResponse{Status: active},
		stopped:   protocol.Status{State: protocol.StateIdle},
	}
	handler := hostapi.New(service)
	if out := launchSession(t, handler, packageGame); out.Code != http.StatusOK {
		t.Fatalf("no-ABI launch = %d %s", out.Code, out.Body.String())
	}

	service.execution = fogcast.ExecutionFPGANative
	service.game = catalog.Game{ID: "snes-replacement", Kind: catalog.SourceKindRaw}
	service.launch = protocol.CachedLaunchResponse{Status: protocol.Status{State: protocol.StateActive}}
	out := launchSession(t, handler, "snes-replacement")
	if out.Code != http.StatusConflict || service.launchCalls != 1 || len(service.stopCtxErrs) != 0 {
		t.Fatalf("replacement launch = %d %s launches=%d stops=%d", out.Code, out.Body.String(), service.launchCalls, len(service.stopCtxErrs))
	}
}

func TestSessionStatusReconstructsRecognizedABIPackagePlayAsNative(t *testing.T) {
	core := "fes.coleco"
	status := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 11,
			ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, BuildID: strings.Repeat("b", 32), Gamepad: true}}
	response := serve(t, hostapi.New(&fakeService{status: status}), http.MethodGet, "/api/v1/session")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"execution":"fpga_native"`) {
		t.Fatalf("restart status = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"execution":"fpga_development"`) {
		t.Fatalf("recognized ABI reconstructed as Diagnostic: %s", response.Body.String())
	}
}

func TestSessionCartridgeLaunchStopsReconstructedABIPackagePlayAfterRestart(t *testing.T) {
	core := "fes.zx81"
	active := protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 8,
			ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, BuildID: strings.Repeat("b", 32)}}
	order := []string{}
	cartridgeID, system, cartCore := "snes-replacement", protocol.SystemSNES, "SNES"
	cartridge := protocol.Status{State: protocol.StateActive, GameID: &cartridgeID, System: &system, ExpectedCore: &cartCore, ObservedCore: &cartCore}
	service := &fakeService{
		execution: fogcast.ExecutionFPGANative,
		game:      catalog.Game{ID: cartridgeID, Kind: catalog.SourceKindRaw},
		status:    active,
		launch:    protocol.CachedLaunchResponse{Status: cartridge},
		stopped:   protocol.Status{State: protocol.StateIdle},
		order:     &order,
	}
	out := launchSession(t, hostapi.New(service), cartridgeID)
	if out.Code != http.StatusOK || service.launchCalls != 1 {
		t.Fatalf("restart replacement = %d %s calls=%d", out.Code, out.Body.String(), service.launchCalls)
	}
	if got := strings.Join(order, ","); got != "service.stop" {
		t.Fatalf("replacement order = %q", got)
	}
	if service.stopHasDeadline {
		t.Fatal("package replacement used the bounded native stop deadline")
	}
}

func systemPtr(system protocol.System) *protocol.System {
	return &system
}
