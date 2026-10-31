package tenfoot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/misteross/expansion"
)

func TestSystemExpansionSelectsOnlyMatchingSocketWithSavedCardCAS(t *testing.T) {
	pkg, old, next, foreign := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("d", 64)
	var mu sync.Mutex
	var writes []map[string]string
	stale := true
	descriptor := corepackage.Descriptor{Core: corepackage.Core{ID: "fes.apple2"}, ABI: corepackage.Contract{ID: "fes.computer", Major: 1}, Interfaces: []corepackage.Interface{{ID: expansion.Apple2Slot, Major: 1}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/core-entries/apple2":
			json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "apple2", CoreID: "fes.apple2", PackageID: pkg})
		case "/api/v1/core-packages/" + pkg:
			json.NewEncoder(w).Encode(map[string]any{"package_id": pkg, "descriptor": descriptor})
		case "/api/v1/library/core-entries":
			json.NewEncoder(w).Encode(map[string]any{"entries": []hostclient.CoreEntry{}})
		case "/api/v1/core-packages":
			json.NewEncoder(w).Encode(map[string]any{"packages": []any{}})
		case "/api/v1/core-packages/" + pkg + "/media-capabilities":
			json.NewEncoder(w).Encode(map[string]any{"package_id": pkg, "media": []any{}})
		case "/api/v1/library/core-entries/apple2/expansions":
			json.NewEncoder(w).Encode(hostclient.CoreEntrySlotExpansions{GameID: "apple2", PackageID: pkg, Sockets: []int{2, 4, 5, 7}, Expansions: []hostclient.SlotExpansion{{Slot: 4, ExpansionID: old, Ready: true}}})
		case "/api/v1/core-expansions":
			json.NewEncoder(w).Encode([]hostclient.CoreExpansion{{ExpansionID: next, PackageID: pkg, Slot: 4, Label: "Sound card"}, {ExpansionID: foreign, PackageID: strings.Repeat("f", 64), Slot: 4, Label: "Other machine"}})
		case "/api/v1/library/core-entries/apple2/expansions/4":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			writes = append(writes, body)
			conflict := body["expansion_id"] == "" && stale
			if conflict {
				stale = false
			}
			mu.Unlock()
			if conflict {
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"error":{"code":"STALE_REVISION","message":"refresh saved setup"}}`))
				return
			}
			cards := []hostclient.SlotExpansion{}
			if body["expansion_id"] != "" {
				cards = append(cards, hostclient.SlotExpansion{Slot: 4, ExpansionID: body["expansion_id"], Ready: true})
			}
			json.NewEncoder(w).Encode(hostclient.CoreEntrySlotExpansions{GameID: "apple2", PackageID: pkg, Sockets: []int{2, 4, 5, 7}, Expansions: cards})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.ctx = context.Background()
	app.mu.Lock()
	app.openSystemExpansionsLocked(rooms.Action{GameID: "apple2", CoreID: "fes.apple2", PackageID: pkg})
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	app.mu.Lock()
	if app.coreLibrary.SystemExpansions.Selected[4] != old {
		t.Fatalf("selection not loaded: %+v", app.coreLibrary)
	}
	rows := app.systemExpansionRowsLocked()
	for _, row := range rows {
		if strings.Contains(row.Name, "Other machine") {
			t.Fatal("foreign shell card offered")
		}
	}
	app.handleSystemExpansionLocked(FirmwarePickerRow{Kind: "system-card:4:" + next, Selectable: true})
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	app.mu.Lock()
	app.handleSystemExpansionLocked(FirmwarePickerRow{Kind: "system-remove:4", Selectable: true})
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	app.mu.Lock()
	if app.coreLibrary.SystemExpansions.Selected[4] != next || !strings.Contains(app.coreLibrary.Status, "refresh saved setup") {
		t.Fatalf("stale write replaced saved view: %+v", app.coreLibrary)
	}
	app.handleSystemExpansionLocked(FirmwarePickerRow{Kind: "system-remove:4", Selectable: true})
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	mu.Lock()
	defer mu.Unlock()
	if len(writes) != 3 || writes[0]["expected_expansion_id"] != old || writes[0]["expansion_id"] != next || writes[1]["expected_expansion_id"] != next || writes[1]["expansion_id"] != "" || writes[2]["expected_expansion_id"] != next {
		t.Fatalf("wrong saved setup mutations: %+v", writes)
	}
}
