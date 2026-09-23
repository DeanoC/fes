package hostapi_test

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/catalog"
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
