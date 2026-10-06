package hostclient

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/protocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiskClientBindsCapturedSessionAndNeverReplays(t *testing.T) {
	id := strings.Repeat("a", 64)
	u := protocol.MediaUnitStatus{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: "ready", Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "st-desktop", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}
	prior := SessionResult{ID: "session", Target: "dev", State: "active", CorePackage: &SessionCorePackage{PackageID: id, Generation: 9, ABI: SessionCoreABI{ID: "fes.computer", Major: 1}, MediaUnits: []protocol.MediaUnitStatus{u}, PersistenceMode: "persistent", ActiveInterfaces: []SessionCoreInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}}}
	calls := 0
	bad := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get(protocol.HostSessionIDHeader) != prior.ID || r.Header.Get(protocol.MediaUnitHeader) != "0" || r.Header.Get("X-FogCast-Core-Generation") != "9" {
			t.Error(r.Header)
		}
		generation := 9
		if bad {
			generation = 10
		}
		json.NewEncoder(w).Encode(map[string]any{"id": prior.ID, "target": "dev", "state": "active", "core_package": map[string]any{"package_id": id, "generation": generation, "abi": map[string]any{"id": "fes.computer", "major": 1, "minor": 0}, "media_units": []protocol.MediaUnitStatus{u}, "persistence_mode": "persistent", "active_interfaces": []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}}})
	}))
	defer server.Close()
	c := NewClient(server.URL, server.Client())
	if _, err := c.SaveDiskForSession(context.Background(), prior); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if _, err := c.InsertLibraryDiskForSession(context.Background(), prior, "st-desktop", u.Persistence.BaseMediaID); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	bad = true
	if _, err := c.SaveDiskForSession(context.Background(), prior); err == nil || calls != 3 {
		t.Fatal("changed generation/replay")
	}
	prior.CorePackage.MediaUnits[0].Persistence = nil
	if _, err := c.SaveDiskForSession(context.Background(), prior); err == nil || calls != 3 {
		t.Fatal("volatile save dispatched")
	}
}
