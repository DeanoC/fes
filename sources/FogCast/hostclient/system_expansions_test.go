package hostclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSlotExpansionSelectionUsesExpectedCardAndChecksReturnedSocket(t *testing.T) {
	pkg, old, next := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	var body map[string]string
	wrong := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.URL.Path != "/api/v1/library/core-entries/my-apple2/expansions/4" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		id := next
		if wrong {
			id = old
		}
		json.NewEncoder(w).Encode(CoreEntrySlotExpansions{GameID: "my-apple2", PackageID: pkg, Sockets: []int{2, 4, 5, 7}, Expansions: []SlotExpansion{{Slot: 4, ExpansionID: id, Ready: true}}})
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	if _, err := client.SelectCoreEntrySlotExpansion(context.Background(), "my-apple2", pkg, 4, old, next); err != nil {
		t.Fatal(err)
	}
	if body["package_id"] != pkg || body["expected_expansion_id"] != old || body["expansion_id"] != next {
		t.Fatalf("missing compare-and-swap: %+v", body)
	}
	wrong = true
	if _, err := client.SelectCoreEntrySlotExpansion(context.Background(), "my-apple2", pkg, 4, old, next); err == nil {
		t.Fatal("accepted another selected card")
	}
}

func TestCoreEntrySlotViewRejectsMalformedSocketSelection(t *testing.T) {
	pkg, card := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, tc := range []CoreEntrySlotExpansions{
		{GameID: "other", PackageID: pkg, Sockets: []int{1}},
		{GameID: "game", PackageID: pkg, Sockets: []int{4, 2}},
		{GameID: "game", PackageID: pkg, Sockets: []int{2, 4}, Expansions: []SlotExpansion{{Slot: 7, ExpansionID: card}}},
		{GameID: "game", PackageID: pkg, Sockets: []int{2, 4}, Expansions: []SlotExpansion{{Slot: 2, ExpansionID: card}, {Slot: 2, ExpansionID: card}}},
	} {
		if err := validateCoreEntrySlots(tc, "game", pkg); err == nil {
			t.Errorf("accepted malformed view: %+v", tc)
		}
	}
}
