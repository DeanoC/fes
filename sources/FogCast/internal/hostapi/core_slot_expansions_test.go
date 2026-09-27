package hostapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type slotExpansionAPIService struct {
	expansionAPIService
	selected []any
}

func (s *slotExpansionAPIService) CoreEntrySlotExpansions(_ context.Context, id string) (fogcast.CoreEntrySlotExpansions, error) {
	return fogcast.CoreEntrySlotExpansions{GameID: id, PackageID: strings.Repeat("a", 64), Sockets: []int{2, 4, 5, 7},
		Expansions: []protocol.SlotExpansionStatus{{Slot: 2, ExpansionID: strings.Repeat("c", 64), Ready: true}}, Ready: true}, nil
}
func (s *slotExpansionAPIService) SelectCoreEntrySlotExpansion(_ context.Context, game, pkg string, slot int, expected, id string) (fogcast.CoreEntrySlotExpansions, error) {
	s.selected = []any{game, pkg, slot, expected, id}
	if slot == 6 {
		return fogcast.CoreEntrySlotExpansions{}, &protocol.APIError{Code: protocol.CodeStaleRevision, Message: "stale"}
	}
	return fogcast.CoreEntrySlotExpansions{GameID: game, PackageID: pkg, Sockets: []int{2, 4, 5, 7}, Expansions: []protocol.SlotExpansionStatus{}}, nil
}

func TestSlotExpansionAPIValidatesPathAndBody(t *testing.T) {
	s := &slotExpansionAPIService{}
	handler := hostapi.New(s)
	a, c := strings.Repeat("a", 64), strings.Repeat("c", 64)
	for _, tc := range []struct {
		slot, body string
		code       int
	}{
		{"4", `{"package_id":"` + a + `","expected_expansion_id":"","expansion_id":"` + c + `"}`, 200},
		{"4", `{"package_id":"` + a + `","expected_expansion_id":"` + c + `","expansion_id":""}`, 200},
		{"6", `{"package_id":"` + a + `","expected_expansion_id":"","expansion_id":""}`, 409},
		{"4", `{"package_id":"` + a + `","expansion_id":""}`, 400},
		{"4", `{"package_id":"` + a + `","expected_expansion_id":"","expansion_id":"","extra":1}`, 400},
		{"4", `{"package_id":"bad","expected_expansion_id":"","expansion_id":""}`, 400},
		{"0", `{"package_id":"` + a + `","expected_expansion_id":"","expansion_id":""}`, 400},
		{"8", `{"package_id":"` + a + `","expected_expansion_id":"","expansion_id":""}`, 400},
		{"04", `{"package_id":"` + a + `","expected_expansion_id":"","expansion_id":""}`, 400},
		{"x", `{"package_id":"` + a + `","expected_expansion_id":"","expansion_id":""}`, 400},
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/library/core-entries/fpga-apple2/expansions/"+tc.slot, strings.NewReader(tc.body))
		req.Host = "127.0.0.1"
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.code {
			t.Fatalf("slot %s body %s: status=%d %s", tc.slot, tc.body, response.Code, response.Body.String())
		}
	}
	if len(s.selected) != 5 || s.selected[0] != "fpga-apple2" || s.selected[2] != 6 {
		t.Fatalf("last selection %+v", s.selected)
	}
	response := serve(t, handler, http.MethodGet, "/api/v1/library/core-entries/fpga-apple2/expansions")
	var view fogcast.CoreEntrySlotExpansions
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &view) != nil || len(view.Sockets) != 4 || len(view.Expansions) != 1 || !view.Expansions[0].Ready {
		t.Fatalf("GET %d %s", response.Code, response.Body.String())
	}
}

func TestSlotExpansionReadinessBlocksLaunchEligibility(t *testing.T) {
	for _, ready := range []bool{false, true} {
		game := catalog.Game{ID: "fpga-apple2", Title: "Apple II", System: protocol.System("apple2"), Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true, GroupKey: "apple2"}
		service := &expansionReadinessService{groupedFake: groupedFake{fakeService: fakeService{games: []catalog.Game{game}, game: game}}, compositions: map[string]protocol.CoreComposition{
			game.ID: {FirmwareReady: true, SlotExpansions: []protocol.SlotExpansionStatus{{Slot: 2, ExpansionID: strings.Repeat("c", 64), Ready: true}, {Slot: 7, ExpansionID: strings.Repeat("d", 64), Ready: ready}}},
		}}
		response := serve(t, hostapi.New(service), http.MethodGet, "/api/v1/games")
		var list struct {
			Games []hostclient.Game `json:"games"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || len(list.Games) != 1 {
			t.Fatalf("games %d %s", response.Code, response.Body.String())
		}
		g := list.Games[0]
		if len(g.SlotExpansions) != 2 || g.SlotExpansions[1].Slot != 7 || g.LaunchEligible() != ready ||
			(!ready && g.LaunchBlock() != hostclient.LaunchMissingExpansion) {
			t.Fatalf("ready=%v projection=%+v block=%s", ready, g, g.LaunchBlock())
		}
	}
}
