package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

// A paired kit relaunches its running Coleco with another cartridge. Only the
// exact running cartridge machine generation admits it, the launch is the
// same title on the same kit, and the swap cartridge rides only that launch.
func TestPairedCartridgeSwapRelaunchesTheRunningTitle(t *testing.T) {
	id, game := strings.Repeat("c", 64), "coleco-cart"
	cart := strings.Repeat("d", 64)
	status := protocol.Status{State: protocol.StateActive, Development: true, GameID: &game,
		CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.application", Major: 1},
			ROMLink:          &corepackage.ROMLinkIdentity{ROMID: "cartridge-rom", Role: "cartridge", SourceSize: 16384},
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad.ports", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}}
	s := &displayHostService{fakeService: fakeService{status: status, game: catalog.Game{ID: game, Kind: catalog.SourceKindCorePackage},
		launch: protocol.CachedLaunchResponse{Status: status}},
		settings: fogcast.LibraryConfig{SelectedTarget: "kit-a", Targets: []fogcast.TargetConfig{{Name: "kit-a", TargetID: launcherID, Enabled: true}}}}
	var swapped []string
	s.launchHook = func(ctx context.Context) {
		media, ok := fogcast.CartridgeFromContext(ctx, game)
		if !ok {
			media = "(none)"
		}
		swapped = append(swapped, media)
	}
	api := hostapi.New(s)
	launcher, err := hostapi.NewLauncherHandler(api, hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	launcher.ServeHTTP(w, launcherRequest(http.MethodGet, "http://192.0.2.1:8789/api/v1/session", nil))
	var observed struct {
		ID string `json:"id"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &observed) != nil || observed.ID == "" {
		t.Fatalf("observe: %d %s", w.Code, w.Body)
	}
	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: "kit-a", TargetID: launcherID}
	swap := func(session string, binding protocol.DevelopmentMediaBinding, body string) int {
		r := launcherRequest(http.MethodPost, "http://192.0.2.1:8789/api/v1/session/cartridge", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(protocol.HostSessionIDHeader, session)
		binding.SetHeaders(r.Header)
		out := httptest.NewRecorder()
		launcher.ServeHTTP(out, r)
		return out.Code
	}
	good := `{"media_id":"` + cart + `","name":"Donkey Kong.col"}`
	for _, tc := range []struct {
		name    string
		session string
		binding protocol.DevelopmentMediaBinding
		body    string
		want    int
	}{
		{"stale session", "stale-session", b, good, 409},
		{"replaced generation", observed.ID, protocol.DevelopmentMediaBinding{PackageID: id, Generation: 10, Target: "kit-a", TargetID: launcherID}, good, 409},
		{"another kit", observed.ID, protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: "kit-b", TargetID: launcherID}, good, 403},
		{"not a cartridge", observed.ID, b, `{"media_id":"` + cart + `","name":"tape.p"}`, 400},
		{"bad media id", observed.ID, b, `{"media_id":"xyz","name":"game.col"}`, 400},
	} {
		if got := swap(tc.session, tc.binding, tc.body); got != tc.want {
			t.Fatalf("%s: status=%d want=%d", tc.name, got, tc.want)
		}
	}
	if len(swapped) != 0 {
		t.Fatalf("refused swap launched: %v", swapped)
	}
	if got := swap(observed.ID, b, good); got != 200 {
		t.Fatalf("swap status=%d", got)
	}
	if len(swapped) != 1 || swapped[0] != cart || s.launchTarget != "kit-a" {
		t.Fatalf("swap launch = %v on %q", swapped, s.launchTarget)
	}
	// A machine without a linked cartridge has nothing to swap.
	tape := status
	pkg := *status.CorePackage
	pkg.ROMLink = nil
	tape.CorePackage = &pkg
	s.status, s.launch.Status = tape, tape
	if got := swap(observed.ID, b, good); got != 409 || len(swapped) != 1 {
		t.Fatalf("cartridge-less machine swap status=%d launches=%v", got, swapped)
	}
}

func TestPairedCartridgeSwapHoldsAdmissionFromStatusThroughLaunch(t *testing.T) {
	id, game := strings.Repeat("c", 64), "coleco-cart"
	cart := strings.Repeat("d", 64)
	status := protocol.Status{State: protocol.StateActive, Development: true, GameID: &game,
		CorePackage: &protocol.CorePackageStatus{PackageID: id, Generation: 9, ABI: protocol.RuntimeContract{ID: "fes.application", Major: 1},
			ROMLink:          &corepackage.ROMLinkIdentity{ROMID: "cartridge-rom", Role: "cartridge", SourceSize: 16384},
			ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad.ports", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}}}
	s := &displayHostService{fakeService: fakeService{status: status, game: catalog.Game{ID: game, Kind: catalog.SourceKindCorePackage},
		launch: protocol.CachedLaunchResponse{Status: status}},
		settings: fogcast.LibraryConfig{SelectedTarget: "kit-a", Targets: []fogcast.TargetConfig{{Name: "kit-a", TargetID: launcherID, Enabled: true}}}}
	var swapped []string
	s.launchHook = func(ctx context.Context) {
		media, ok := fogcast.CartridgeFromContext(ctx, game)
		if !ok {
			media = "(none)"
		}
		swapped = append(swapped, media)
	}
	api := hostapi.New(s)
	launcher, err := hostapi.NewLauncherHandler(api, hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	launcher.ServeHTTP(w, launcherRequest(http.MethodGet, "http://192.0.2.1:8789/api/v1/session", nil))
	var observed struct {
		ID string `json:"id"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &observed) != nil || observed.ID == "" {
		t.Fatalf("observe: %d %s", w.Code, w.Body)
	}

	started, releaseStatus := make(chan struct{}), make(chan struct{})
	var statusGate, releaseGate sync.Once
	s.statusTargetHook = func(_ context.Context, target string) (protocol.Status, error) {
		s.target = target
		statusGate.Do(func() {
			close(started)
			<-releaseStatus
		})
		return s.status, nil
	}
	release := func() { releaseGate.Do(func() { close(releaseStatus) }) }
	defer release()

	b := protocol.DevelopmentMediaBinding{PackageID: id, Generation: 9, Target: "kit-a", TargetID: launcherID}
	swapRequest := launcherRequest(http.MethodPost, "http://192.0.2.1:8789/api/v1/session/cartridge", strings.NewReader(`{"media_id":"`+cart+`","name":"Donkey Kong.col"}`))
	swapRequest.Header.Set("Content-Type", "application/json")
	swapRequest.Header.Set(protocol.HostSessionIDHeader, observed.ID)
	b.SetHeaders(swapRequest.Header)
	swapDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		out := httptest.NewRecorder()
		launcher.ServeHTTP(out, swapRequest)
		swapDone <- out
	}()
	<-started

	competing := httptest.NewRecorder()
	launcher.ServeHTTP(competing, launcherRequest(http.MethodPost, "http://192.0.2.1:8789/api/v1/session/launch", strings.NewReader(`{"game_id":"another-title"}`)))
	if competing.Code != http.StatusConflict {
		release()
		t.Fatalf("launch admitted during cartridge replacement: %d %s", competing.Code, competing.Body)
	}
	release()
	if out := <-swapDone; out.Code != http.StatusOK {
		t.Fatalf("cartridge swap: %d %s", out.Code, out.Body)
	}
	if s.launchCalls != 1 || len(swapped) != 1 || swapped[0] != cart {
		t.Fatalf("launches=%d cartridge contexts=%v", s.launchCalls, swapped)
	}
}
