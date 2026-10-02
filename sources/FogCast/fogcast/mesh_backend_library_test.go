package fogcast

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

func TestMeshBackendLibraryTwoNodeProjection(t *testing.T) {
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	kitID, emuID := "01234567-89ab-cdef-0123-456789abcdef", "fedcba98-7654-3210-fedc-ba9876543210"
	kit := MeshNode{NodeID: kitID, TargetID: kitID, Mesh: discovery.MeshProtocol, Address: "http://192.0.2.1:8182", Capabilities: discovery.KitCapabilities()}
	emu := MeshNode{NodeID: emuID, TargetID: emuID, Mesh: discovery.MeshProtocol, Address: "http://192.0.2.2:8182", Capabilities: discovery.Capabilities{Execute: []discovery.Execute{{Kind: meshcontent.ExecuteNativeEmu}}}}
	pkgID := strings.Repeat("ab", 32)
	fpga := meshCoreTitle("Data Storm 1.00", "fes.sms", pkgID, media, false)
	native := meshNativeTitle("Data Storm 1.00", protocol.SystemSMS, media)
	native.Game.ID = fpga.Game.ID
	native.Game.Content.Size = 32768
	native.Game.Content.Extension = "sms"
	pong := meshCoreTitle("Pong", "fes.pong", pkgID, "", false)
	emuOnly := meshNativeTitle("Emulator only", protocol.SystemSMS, media)
	lib := MeshLibrary{Titles: []MeshTitle{fpga, native, pong, emuOnly, native}}
	packages := map[string][]string{kitID: {pkgID}}
	abis := map[string][]meshcontent.EligibleABI{kitID: {{ID: "fes.application", Major: 1}}}
	nodes := []MeshNode{kit, emu}
	rows, skipped := ProjectMeshBackendLibrary(lib, nodes, false, packages, abis)
	if len(skipped) != 0 || len(rows) != 3 {
		t.Fatalf("rows=%+v skipped=%+v", rows, skipped)
	}
	dual := backendRow(t, rows, fpga.Game.ID)
	if len(dual.Options) != 2 || len(dual.ContentIDs) != 1 || dual.ContentIDs[0].String() != "sha256:"+media {
		t.Fatalf("dual=%+v", dual)
	}
	if dual.Options[0].Entry.Execute[0].Kind != meshcontent.ExecuteFPGANative || !dual.Options[0].Nodes[0].Available {
		t.Fatalf("fpga=%+v", dual.Options[0])
	}
	if dual.Options[1].Entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu || !dual.Options[1].HostLocal || dual.Options[1].Nodes[0].Available || dual.Options[1].Nodes[0].Reason == "" {
		t.Fatalf("emu=%+v", dual.Options[1])
	}
	if got := backendRow(t, rows, pong.Game.ID); len(got.Options) != 1 || got.Options[0].Entry.Execute[0].Kind != meshcontent.ExecuteFPGANative {
		t.Fatalf("pong=%+v", got)
	}
	if got := backendRow(t, rows, emuOnly.Game.ID); len(got.Options) != 1 || got.Options[0].Entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu {
		t.Fatalf("emu-only=%+v", got)
	}

	// Silence removes the executor candidate, not the title, identity, or lease.
	silent, _ := ProjectMeshBackendLibrary(lib, []MeshNode{emu}, false, packages, abis)
	if got := backendRow(t, silent, fpga.Game.ID); got.Options[0].Reason == "" || len(got.Options[0].Nodes) != 0 || got.ContentIDs[0] != dual.ContentIDs[0] {
		t.Fatalf("silent=%+v", got)
	}
	retained, _ := ProjectMeshBackendLibrary(lib, nodes, true, packages, abis)
	if got := backendRow(t, retained, fpga.Game.ID); got.Options[0].Nodes[0].Available || got.Options[0].Nodes[0].Reason == "" {
		t.Fatalf("retained=%+v", got)
	}

	// Mismatch remains visible; a later browse with the same node id restores it.
	wrongMajor := kit
	wrongMajor.Mesh = "2.0"
	mismatch, _ := ProjectMeshBackendLibrary(lib, []MeshNode{wrongMajor, emu}, false, packages, abis)
	if got := backendRow(t, mismatch, fpga.Game.ID); got.Options[0].Nodes[0].Available || got.Options[0].Nodes[0].Reason == "" {
		t.Fatalf("major=%+v", got)
	}
	badABI, _ := ProjectMeshBackendLibrary(lib, nodes, false, packages, map[string][]meshcontent.EligibleABI{kitID: {{ID: "fes.application", Major: 2}}})
	if got := backendRow(t, badABI, fpga.Game.ID); got.Options[0].Nodes[0].Available || got.Options[0].Nodes[0].Reason == "" {
		t.Fatalf("abi=%+v", got)
	}
	reconnected := kit
	reconnected.Address = "http://192.0.2.3:8182"
	again, _ := ProjectMeshBackendLibrary(lib, []MeshNode{reconnected, emu}, false, packages, abis)
	if got := backendRow(t, again, fpga.Game.ID); !got.Options[0].Nodes[0].Available || got.ContentIDs[0] != dual.ContentIDs[0] {
		t.Fatalf("reconnect=%+v", got)
	}
	duplicate, _ := ProjectMeshBackendLibrary(lib, []MeshNode{kit, reconnected, emu}, false, packages, abis)
	if got := backendRow(t, duplicate, fpga.Game.ID); got.Options[0].Nodes[0].Available || got.Options[0].Nodes[1].Available {
		t.Fatalf("duplicate=%+v", got)
	}
}

func backendRow(t *testing.T, rows []MeshBackendRow, id string) MeshBackendRow {
	t.Helper()
	for _, row := range rows {
		if row.TitleID == id {
			return row
		}
	}
	t.Fatalf("missing title %s", id)
	return MeshBackendRow{}
}

func TestMeshInventoryObservesWithoutBoundTarget(t *testing.T) {
	s := newService(Config{}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	id := "01234567-89ab-cdef-0123-456789abcdef"
	s.collectNodes = func(context.Context) ([]discovery.ObservedNode, error) {
		return []discovery.ObservedNode{{NodeID: id, TargetID: id}}, nil
	}
	if s.discoveryEnabled() {
		t.Fatal("fixture unexpectedly has a Phase 0 bind")
	}
	s.startTargetMonitor()
	t.Cleanup(func() { s.monitorCancel(); <-s.monitorDone })
	// The monitor tick is intentionally the same path production uses.
	for i := 0; i < 30; i++ {
		if len(s.MeshNodes()) == 1 {
			return
		}
		// A bounded wait avoids depending on DNS-SD or a real target.
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("observer-only monitor did not populate inventory")
}
