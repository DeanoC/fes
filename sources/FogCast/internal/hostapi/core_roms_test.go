package hostapi_test

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type romAPIService struct {
	coreLibraryAPIService
	bound []string
}

func (s *romAPIService) CoreEntryROM(_ context.Context, game string) (catalog.CoreEntryROM, error) {
	return catalog.CoreEntryROM{GameID: game}, nil
}
func (s *romAPIService) SelectCoreEntryROM(_ context.Context, game, pkg, rom, expected, media string) (catalog.CoreEntryROM, error) {
	s.bound = []string{game, pkg, rom, expected, media}
	return catalog.CoreEntryROM{GameID: game, PackageID: pkg, ROMID: rom, MediaID: media}, nil
}

type pairedROMAPIService struct {
	*romAPIService
	settings    fogcast.LibraryConfig
	entry       catalog.CoreEntry
	entryTarget string
}

func (s *pairedROMAPIService) LibrarySettings() fogcast.LibraryConfig { return s.settings }
func (s *pairedROMAPIService) CoreEntry(ctx context.Context, _ string) (catalog.CoreEntry, error) {
	s.entryTarget = fogcast.PairedTargetFromContext(ctx)
	return s.entry, nil
}

func TestPairedLauncherCanReadCoreEntryAndSelectROM(t *testing.T) {
	packageID := strings.Repeat("a", 64)
	mediaID := strings.Repeat("b", 64)
	service := &pairedROMAPIService{
		romAPIService: &romAPIService{coreLibraryAPIService: coreLibraryAPIService{
			fakeService: fakeService{connection: fogcast.TargetConnection{TargetID: launcherID}},
		}},
		settings: fogcast.LibraryConfig{SelectedTarget: "kit-a", Targets: []fogcast.TargetConfig{
			{Name: "kit-a", Enabled: true, TargetID: launcherID},
			{Name: "kit-b", Enabled: true, TargetID: launcherIDB},
		}},
		entry: catalog.CoreEntry{GameID: "fpga-zx81", PackageID: packageID},
	}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{
		{Token: launcherToken, TargetID: launcherID},
		{Token: launcherTokenB, TargetID: launcherIDB},
	}})
	if err != nil {
		t.Fatal(err)
	}
	pairedCall := func(method, path, token, targetID, body string) *httptest.ResponseRecorder {
		req := launcherRequest(method, "http://192.0.2.1:8789"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-FogCast-Target-ID", targetID)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	entry := pairedCall(http.MethodGet, "/api/v1/library/core-entries/fpga-zx81", launcherTokenB, launcherIDB, "")
	if entry.Code != http.StatusOK || !strings.Contains(entry.Body.String(), `"package_id":"`+packageID+`"`) {
		t.Fatalf("paired core-entry read: %d %s", entry.Code, entry.Body.String())
	}
	if service.entryTarget != launcherIDB {
		t.Fatalf("core-entry read target = %q, want paired kit %q", service.entryTarget, launcherIDB)
	}

	body := `{"package_id":"` + packageID + `","rom_id":"zx81-firmware","expected_media_id":"","media_id":"` + mediaID + `"}`
	selection := pairedCall(http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/rom", launcherTokenB, launcherIDB, body)
	if selection.Code != http.StatusOK {
		t.Fatalf("paired ROM selection: %d %s", selection.Code, selection.Body.String())
	}
	want := []string{"fpga-zx81", packageID, "zx81-firmware", "", mediaID}
	if len(service.bound) != len(want) {
		t.Fatalf("ROM selection = %v, want %v", service.bound, want)
	}
	for i := range want {
		if service.bound[i] != want[i] {
			t.Fatalf("ROM selection = %v, want %v", service.bound, want)
		}
	}

	wrongTarget := pairedCall(http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/rom", launcherTokenB, launcherID, body)
	if wrongTarget.Code != http.StatusForbidden {
		t.Fatalf("cross-target ROM selection: %d %s", wrongTarget.Code, wrongTarget.Body.String())
	}
}

func TestROMAPIRequiresNamedExactSelection(t *testing.T) {
	s := &romAPIService{}
	handler := hostapi.New(s)
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"package_id":"` + strings.Repeat("a", 64) + `","rom_id":"machine","expected_media_id":"","media_id":"` + strings.Repeat("b", 64) + `"}`, 200},
		{`{"package_id":"` + strings.Repeat("a", 64) + `","expected_media_id":"","media_id":""}`, 400},
		{`{"package_id":"` + strings.Repeat("a", 64) + `","rom_id":"machine","media_id":""}`, 400},
		{`{"package_id":"bad","rom_id":"machine","expected_media_id":"","media_id":""}`, 400},
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/library/core-entries/fpga-example/rom", strings.NewReader(tc.body))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	}
	if len(s.bound) != 5 || s.bound[2] != "machine" || s.bound[4] != strings.Repeat("b", 64) {
		t.Fatalf("%+v", s.bound)
	}
}
func TestROMReadinessBlocksLaunch(t *testing.T) {
	game := hostclient.Game{State: "available", RootOnline: true, Launchable: true, ROMRequired: true}
	if game.LaunchBlock() != hostclient.LaunchMissingROM {
		t.Fatalf("missing ROM ready: %+v", game)
	}
	game.ROMReady = true
	if !game.LaunchEligible() {
		t.Fatal("selected ROM blocked")
	}
}

func TestROMReadinessProjectsLibraryAndVariants(t *testing.T) {
	for _, ready := range []bool{false, true} {
		game := catalog.Game{ID: "fpga-rom", Title: "ROM title", System: protocol.System("zx81"), Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true, GroupKey: "rom-group"}
		variant := game
		variant.ID = "fpga-rom-variant"
		comp := protocol.CoreComposition{ROMRequired: true, ROMReady: ready, ROMID: "machine", ROMMediaID: strings.Repeat("a", 64)}
		service := &expansionReadinessService{groupedFake: groupedFake{fakeService: fakeService{games: []catalog.Game{game}, game: game}, variants: []catalog.Game{variant}}, compositions: map[string]protocol.CoreComposition{game.ID: comp, variant.ID: comp}}
		for _, path := range []string{"/api/v1/games", "/api/v1/games/" + game.ID} {
			response := serve(t, hostapi.New(service), http.MethodGet, path)
			if response.Code != 200 {
				t.Fatal(response.Body.String())
			}
			var games []hostclient.Game
			if path == "/api/v1/games" {
				var page struct {
					Games []hostclient.Game `json:"games"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				games = page.Games
			} else {
				var detail hostclient.Game
				if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
					t.Fatal(err)
				}
				games = append([]hostclient.Game{detail}, detail.Variants...)
			}
			if len(games) == 0 {
				t.Fatal("empty projection")
			}
			for _, g := range games {
				if !g.ROMRequired || g.ROMReady != ready || g.ROMID != comp.ROMID || g.ROMMediaID != comp.ROMMediaID {
					t.Fatalf("projection %+v", g)
				}
			}
		}
	}
}

func TestFirmwareROMReadinessProjectsAsFirmwareAndClearsAfterSelection(t *testing.T) {
	for _, ready := range []bool{false, true} {
		game := catalog.Game{ID: "fpga-zx81-rom", Title: "ZX81 BASIC", System: protocol.System("zx81"), Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true}
		comp := protocol.CoreComposition{FirmwareRequired: true, FirmwareReady: ready, ROMRole: protocol.FirmwareRole, ROMID: "zx81-machine-rom", ROMSourceSize: 8192}
		service := &expansionReadinessService{groupedFake: groupedFake{fakeService: fakeService{games: []catalog.Game{game}, game: game}}, compositions: map[string]protocol.CoreComposition{game.ID: comp}}
		response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games/"+game.ID)
		if response.Code != http.StatusOK {
			t.Fatal(response.Body.String())
		}
		var projected hostclient.Game
		if err := json.Unmarshal(response.Body.Bytes(), &projected); err != nil {
			t.Fatal(err)
		}
		if !projected.FirmwareRequired || projected.FirmwareReady != ready || projected.ROMRequired || projected.ROMRole != protocol.FirmwareRole || projected.ROMID != comp.ROMID || projected.ROMSourceSize != comp.ROMSourceSize {
			t.Fatalf("firmware ROM projection: %+v", projected)
		}
		want := hostclient.LaunchMissingFirmware
		if ready {
			want = ""
		}
		if projected.LaunchBlock() != want {
			t.Fatalf("ready=%v block=%q want %q", ready, projected.LaunchBlock(), want)
		}
	}
}
