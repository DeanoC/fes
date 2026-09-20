package hostapi_test

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type expansionAPIService struct {
	coreLibraryAPIService
	bound []string
}

func (s *expansionAPIService) ImportCoreExpansion(context.Context, int64, io.Reader) (catalog.CoreExpansion, error) {
	s.imports++
	return catalog.CoreExpansion{ExpansionID: strings.Repeat("b", 64)}, nil
}
func (s *expansionAPIService) CoreExpansions(context.Context) ([]catalog.CoreExpansion, error) {
	return []catalog.CoreExpansion{}, nil
}
func (s *expansionAPIService) CoreEntryExpansion(_ context.Context, id string) (catalog.CoreEntryExpansion, error) {
	return catalog.CoreEntryExpansion{GameID: id}, nil
}
func (s *expansionAPIService) SelectCoreEntryExpansion(_ context.Context, game, base, expected, asset string) (catalog.CoreEntryExpansion, error) {
	s.bound = []string{game, base, expected, asset}
	return catalog.CoreEntryExpansion{GameID: game, ExpansionID: asset}, nil
}
func TestExpansionAPIRequiresExactSelection(t *testing.T) {
	s := &expansionAPIService{}
	handler := hostapi.New(s)
	for _, tc := range []struct {
		body string
		code int
	}{
		{`{"package_id":"` + strings.Repeat("a", 64) + `","expected_expansion_id":"","expansion_id":"` + strings.Repeat("b", 64) + `"}`, 200},
		{`{"package_id":"` + strings.Repeat("a", 64) + `","expansion_id":""}`, 400},
		{`{"package_id":"bad","expected_expansion_id":"","expansion_id":""}`, 400},
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/library/core-entries/fpga-example/expansion", strings.NewReader(tc.body))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.code {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if len(s.bound) != 4 || s.bound[0] != "fpga-example" || s.bound[2] != "" || s.bound[3] != strings.Repeat("b", 64) {
		t.Fatalf("selection %+v", s.bound)
	}
}

// Firmware-free shells still carry independent expansion readiness in every
// library projection consumed by host clients.
type expansionReadinessService struct {
	groupedFake
	compositions map[string]protocol.CoreComposition
}

func (s *expansionReadinessService) CoreCompositions(context.Context, []string) (map[string]protocol.CoreComposition, error) {
	return s.compositions, nil
}
func (s *expansionReadinessService) PlatformLaunchable(protocol.System) bool { return true }
func TestFirmwareFreeExpansionReadinessInLibraryResponses(t *testing.T) {
	selected := strings.Repeat("b", 64)
	for _, ready := range []bool{false, true} {
		game := catalog.Game{ID: "fpga-zx81", Title: "ZX81", System: protocol.System("zx81"), Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true, GroupKey: "zx81-group"}
		variant := game
		variant.ID = "fpga-zx81-variant"
		service := &expansionReadinessService{groupedFake: groupedFake{fakeService: fakeService{games: []catalog.Game{game}, game: game}, variants: []catalog.Game{variant}}, compositions: map[string]protocol.CoreComposition{
			game.ID: {ExpansionID: selected, ExpansionReady: ready}, variant.ID: {ExpansionID: selected, ExpansionReady: ready},
		}}
		check := func(g hostclient.Game, admission bool) {
			t.Helper()
			if g.FirmwareRequired || g.ExpansionID != selected || g.ExpansionReady != ready {
				t.Fatalf("ready=%v projection=%+v", ready, g)
			}
			if admission && g.LaunchEligible() != ready {
				t.Fatalf("ready=%v block=%s projection=%+v", ready, g.LaunchBlock(), g)
			}
			if admission && !ready && g.LaunchBlock() != hostclient.LaunchMissingExpansion {
				t.Fatalf("wrong refusal: %s", g.LaunchBlock())
			}
		}
		for _, path := range []string{"/api/v1/games", "/api/v1/games/" + game.ID} {
			response := serve(t, hostapi.New(service), http.MethodGet, path)
			if response.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
			}
			if path == "/api/v1/games" {
				var list struct {
					Games []hostclient.Game `json:"games"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
					t.Fatal(err)
				}
				if len(list.Games) != 1 {
					t.Fatal(list)
				}
				check(list.Games[0], true)
			} else {
				var detail hostclient.Game
				if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
					t.Fatal(err)
				}
				check(detail, true)
				if len(detail.Variants) != 1 {
					t.Fatal(detail)
				}
				check(detail.Variants[0], false)
			}
		}
	}
}
