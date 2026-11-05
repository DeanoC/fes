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
		resultUnit := u
		resultPersistence := *u.Persistence
		if strings.HasSuffix(r.URL.Path, "/save") {
			resultPersistence.Revision = strings.Repeat("c", 64)
		}
		resultUnit.Persistence = &resultPersistence
		json.NewEncoder(w).Encode(map[string]any{"id": prior.ID, "target": "dev", "state": "active", "core_package": map[string]any{"package_id": id, "generation": generation, "abi": map[string]any{"id": "fes.computer", "major": 1, "minor": 0}, "media_units": []protocol.MediaUnitStatus{resultUnit}, "persistence_mode": "persistent", "active_interfaces": []protocol.RuntimeInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}}})
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
	prior.CorePackage.MediaUnits[0].Persistence = u.Persistence
	no := false
	prior.CorePackage.MediaUnits[0].Saveable = &no
	if _, err := c.SaveDiskForSession(context.Background(), prior); err == nil || calls != 3 {
		t.Fatal("non-saveable media dispatched")
	}
}

func TestPcxtDiskSessionRequiresPairedWriteAndKeepsBinding(t *testing.T) {
	id := strings.Repeat("a", 64)
	u := protocol.MediaUnitStatus{Interface: protocol.PcxtFloppyInterface(), MinBytes: 368640, MaxBytes: 368640, ChunkBytes: 512, State: "ready", Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "dos-game", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}
	p := &SessionCorePackage{PackageID: id, Generation: 9, ABI: SessionCoreABI{ID: "fes.computer", Major: 1, Minor: 1}, MediaUnits: []protocol.MediaUnitStatus{u}, PersistenceMode: "persistent", ActiveInterfaces: []SessionCoreInterface{{ID: protocol.PcxtFloppyInterface().ID, Major: 1}, {ID: protocol.PcxtFloppyWriteInterface().ID, Major: 1}}}
	prior := SessionResult{ID: "session", Target: "dev", State: "active", CorePackage: p}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		result := *p
		result.MediaUnits = append([]protocol.MediaUnitStatus(nil), p.MediaUnits...)
		binding := *result.MediaUnits[0].Persistence
		if strings.HasSuffix(r.URL.Path, "/save") {
			binding.Revision = strings.Repeat("c", 64)
		}
		result.MediaUnits[0].Persistence = &binding
		json.NewEncoder(w).Encode(map[string]any{"id": prior.ID, "target": prior.Target, "state": "active", "core_package": result})
	}))
	defer server.Close()
	c := NewClient(server.URL, server.Client())
	if _, err := c.SaveDiskForSession(context.Background(), prior); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InsertLibraryDiskForSession(context.Background(), prior, u.Persistence.GameID, u.Persistence.BaseMediaID); err != nil {
		t.Fatal(err)
	}
	p.ActiveInterfaces = p.ActiveInterfaces[:1]
	if _, err := c.SaveDiskForSession(context.Background(), prior); err == nil || calls != 2 {
		t.Fatal("unpaired write dispatched")
	}
	p.ActiveInterfaces = append(p.ActiveInterfaces, SessionCoreInterface{ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1})
	if _, err := c.InsertLibraryDiskForSession(context.Background(), prior, u.Persistence.GameID, u.Persistence.BaseMediaID); err == nil || calls != 2 {
		t.Fatal("wrong write extension dispatched")
	}
	p.ActiveInterfaces = p.ActiveInterfaces[:1]
	p.ActiveInterfaces = append(p.ActiveInterfaces, SessionCoreInterface{ID: protocol.PcxtFloppyWriteInterface().ID, Major: 1})
	p.ABI.Minor = 0
	if _, err := c.SaveDiskForSession(context.Background(), prior); err == nil || calls != 2 {
		t.Fatal("ABI 1.0 dispatched")
	}
}

func TestReadOnlyDiskSessionInsertRemainsVolatile(t *testing.T) {
	for _, iface := range []protocol.RuntimeContract{protocol.AtariStFloppyInterface(), protocol.PcxtFloppyInterface()} {
		t.Run(iface.ID, func(t *testing.T) {
			size := uint32(protocol.AtariStFloppyBytes)
			minor := uint16(0)
			if iface == protocol.PcxtFloppyInterface() {
				size = uint32(protocol.PcxtFloppyBytes)
				minor = 1
			}
			unit := protocol.MediaUnitStatus{Interface: iface, MinBytes: size, MaxBytes: size, ChunkBytes: 512, State: "ready"}
			p := &SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: SessionCoreABI{ID: "fes.computer", Major: 1, Minor: minor}, MediaUnits: []protocol.MediaUnitStatus{unit}, PersistenceMode: "volatile", ActiveInterfaces: []SessionCoreInterface{{ID: iface.ID, Major: 1}}}
			prior := SessionResult{ID: "session", Target: "dev", State: "active", CorePackage: p}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				json.NewEncoder(w).Encode(map[string]any{"id": prior.ID, "target": prior.Target, "state": "active", "core_package": p})
			}))
			defer server.Close()
			c := NewClient(server.URL, server.Client())
			if _, err := c.InsertLibraryDiskForSession(context.Background(), prior, "game", strings.Repeat("b", 64)); err != nil || calls != 1 {
				t.Fatal("read-only insert rejected", err)
			}
			if _, err := c.SaveDiskForSession(context.Background(), prior); err == nil || calls != 1 {
				t.Fatal("read-only save dispatched")
			}
		})
	}
}

func TestDiskCheckpointRejectsChangedGeometryAndUnconfirmedSave(t *testing.T) {
	unit := protocol.MediaUnitStatus{Interface: protocol.AtariStFloppyInterface(), MinBytes: 737280, MaxBytes: 737280, ChunkBytes: 512, State: "ready", Persistence: &protocol.MediaDataStatus{Mode: "persistent", GameID: "game", BaseMediaID: strings.Repeat("b", 64), Revision: "absent"}}
	p := &SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 9, ABI: SessionCoreABI{ID: "fes.computer", Major: 1}, MediaUnits: []protocol.MediaUnitStatus{unit}, PersistenceMode: "persistent", ActiveInterfaces: []SessionCoreInterface{{ID: protocol.AtariStFloppyInterface().ID, Major: 1}, {ID: protocol.AtariStFloppyWriteInterface().ID, Major: 1}}}
	prior := SessionResult{ID: "session", Target: "dev", State: "active", CorePackage: p}
	mode := "geometry"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := *p
		result.MediaUnits = append([]protocol.MediaUnitStatus(nil), p.MediaUnits...)
		binding := *unit.Persistence
		binding.Revision = strings.Repeat("c", 64)
		if mode == "revision" {
			binding.Revision = "absent"
		}
		result.MediaUnits[0].Persistence = &binding
		if mode == "geometry" {
			result.MediaUnits[0].MinBytes, result.MediaUnits[0].MaxBytes = uint32(protocol.AtariStFloppyGeometryMinBytes), uint32(protocol.AtariStFloppyGeometryMaxBytes)
			result.ActiveInterfaces = append(append([]SessionCoreInterface(nil), p.ActiveInterfaces...), SessionCoreInterface{ID: protocol.AtariStFloppyGeometryInterface().ID, Major: 1})
		}
		if mode == "saveable" {
			no := false
			result.MediaUnits[0].Saveable = &no
		}
		json.NewEncoder(w).Encode(map[string]any{"id": prior.ID, "target": prior.Target, "state": "active", "core_package": result})
	}))
	defer server.Close()
	c := NewClient(server.URL, server.Client())
	for _, mode = range []string{"geometry", "revision", "saveable"} {
		if _, err := c.SaveDiskForSession(context.Background(), prior); err == nil {
			t.Fatal("unconfirmed save accepted", mode)
		}
	}
}
