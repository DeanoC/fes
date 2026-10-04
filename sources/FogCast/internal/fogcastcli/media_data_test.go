package fogcastcli

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSaveDiskCLIUsesBoundSessionAndSingleMutation(t *testing.T) {
	id := strings.Repeat("a", 64)
	unit := protocol.MediaUnitStatus{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: "ready", Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			if r.URL.Path != "/api/v1/session/disk/save" || r.Header.Get(protocol.MediaUnitHeader) != "0" || r.Header.Get(protocol.HostSessionIDHeader) != "session" {
				t.Error(r.URL, r.Header)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "session", "target": "dev", "state": "active", "core_package": map[string]any{"package_id": id, "generation": 9, "abi": map[string]any{"id": "fes.computer", "major": 1, "minor": 0}, "media_units": []protocol.MediaUnitStatus{unit}, "persistence_mode": "persistent", "active_interfaces": []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}}})
	}))
	defer server.Close()
	if result := runLiveMediaCommand(context.Background(), server.URL, []string{"save-disk"}); result.err != nil || posts != 1 {
		t.Fatal(result.err, posts)
	}
	unit.Persistence = nil
	if result := runLiveMediaCommand(context.Background(), server.URL, []string{"save-disk"}); result.err == nil || posts != 1 {
		t.Fatal("volatile disk dispatched")
	}
}
