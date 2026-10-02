package fogcast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/discovery"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

type backendCatalog struct {
	*fakeServiceCatalog
	entry catalog.CoreEntry
}

func (c *backendCatalog) Game(_ context.Context, id string) (catalog.Game, error) {
	for _, game := range c.games {
		if game.ID == id {
			return game, nil
		}
	}
	return catalog.Game{}, catalog.ErrInvalidCoreEntry
}

func (c *backendCatalog) CoreEntry(context.Context, string) (catalog.CoreEntry, error) {
	return c.entry, nil
}
func (c *backendCatalog) CoreEntries(context.Context) ([]catalog.CoreEntry, error) {
	return []catalog.CoreEntry{c.entry}, nil
}
func (c *backendCatalog) CreateCoreEntry(context.Context, string, string, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, catalog.ErrInvalidCoreEntry
}
func (c *backendCatalog) SelectCoreEntry(context.Context, string, string, string, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, catalog.ErrInvalidCoreEntry
}

func TestServiceMeshBackendLibraryRealCatalogIDs(t *testing.T) {
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	const title = "Data Storm 1.00"
	ctx := context.Background()
	archive := sizedSetupArchive(t, "fes.sms", 32768)
	packageRoot := t.TempDir()
	if err := os.Chmod(packageRoot, 0700); err != nil {
		t.Fatal(err)
	}
	packages, err := corepackage.NewStore(packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	inspection, _, err := packages.Import(ctx, int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	coreID := catalog.GameID(catalog.CorePlatform, "core-packages", "fes.sms\x00"+title, title)
	romID := catalog.GameID(protocol.SystemSMS, "sms-root", "Data Storm.sms", title)
	coreGame := catalog.Game{ID: coreID, Title: title, System: catalog.CorePlatform, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true}
	romGame := catalog.Game{ID: romID, Title: title, LibraryID: "sms-root", RelativePath: "Data Storm.sms", System: protocol.SystemSMS, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true, Content: &catalog.Content{SHA256: media, Size: 32768, Extension: "sms"}}
	cat := &backendCatalog{fakeServiceCatalog: &fakeServiceCatalog{games: []catalog.Game{coreGame, romGame}}, entry: catalog.CoreEntry{GameID: coreID, Title: title, CoreID: "fes.sms", PackageID: inspection.PackageID, MediaRole: "blob", MediaID: media}}
	service := newService(Config{}, Paths{}, cat, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{}, WithExecutionPolicy(ExecutionPolicy{Resolver: NewConfiguredExecutionResolver([]protocol.System{protocol.SystemSMS}, &fakeHostExecutor{}), Host: &fakeHostExecutor{}}))
	service.corePackages = packages
	const kitID = "01234567-89ab-cdef-0123-456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mesh/content/node" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": kitID, "abis": []map[string]any{{"id": inspection.Descriptor.ABI.ID, "major": inspection.Descriptor.ABI.Major}}, "packages": []string{inspection.PackageID}})
	}))
	defer server.Close()
	client := server.Client()
	guard := &originGuard{base: client.Transport, allow: strings.TrimPrefix(server.URL, "http://")}
	client.Transport = guard
	service.targets = []TargetConfig{{Name: "kit", Enabled: true, TargetID: kitID, Address: server.URL, Agent: "fixture-token"}}
	service.meshHTTP = client
	service.meshNodes = []MeshNode{{NodeID: kitID, TargetID: kitID, Address: server.URL, Mesh: discovery.MeshProtocol, Capabilities: discovery.KitCapabilities()}}
	if service.meshEnsure || service.meshPlacement {
		t.Fatalf("ensure=%v placement=%v", service.meshEnsure, service.meshPlacement)
	}
	rows, skipped := service.MeshBackendLibrary(ctx)
	// Same ROM sha256 and system: one row under the package game id, with the
	// kit FPGA option and the host-local emulator option.
	if len(skipped) != 0 || len(rows) != 1 || coreID == romID || rows[0].TitleID != coreID {
		t.Fatalf("ids=%q/%q rows=%+v skipped=%+v", coreID, romID, rows, skipped)
	}
	row := rows[0]
	if row.System != "sms" || len(row.Options) != 2 || len(row.ContentIDs) != 1 || row.ContentIDs[0].String() != "sha256:"+media {
		t.Fatalf("row=%+v", row)
	}
	fpgaOpt, emuOpt := row.Options[0], row.Options[1]
	if fpgaOpt.Entry.TitleID != coreID || fpgaOpt.Entry.Execute[0].Kind != meshcontent.ExecuteFPGANative || fpgaOpt.HostLocal || fpgaOpt.Reason != "" || len(fpgaOpt.Nodes) != 1 || !fpgaOpt.Nodes[0].Available {
		t.Fatalf("fpga option=%+v", fpgaOpt)
	}
	if emuOpt.Entry.TitleID != romID || emuOpt.Entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu || !emuOpt.HostLocal || emuOpt.Reason != "" {
		t.Fatalf("emulator option=%+v", emuOpt)
	}
	library, err := MeshLibraryTitles(rows)
	if err != nil {
		t.Fatal(err)
	}
	document, err := json.Marshal(library)
	if err != nil {
		t.Fatal(err)
	}
	assertKitOnlyLibraryDocument(t, document, coreID, romID, media, inspection.PackageID, inspection.Descriptor.ABI.ID, int(inspection.Descriptor.ABI.Major), kitID)
	const emuID = "fedcba98-7654-3210-fedc-ba9876543210"
	var runnerHits atomic.Int32
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runnerHits.Add(1)
		t.Errorf("runner dialed %s %s Authorization=%q", r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"))
		http.Error(w, "runner must not be dialed", http.StatusForbidden)
	}))
	defer runner.Close()
	service.meshNodes = append(service.meshNodes, MeshNode{
		NodeID: emuID, TargetID: emuID, Address: runner.URL, Mesh: discovery.MeshProtocol,
		Capabilities: discovery.Capabilities{Execute: []discovery.Execute{{Kind: meshcontent.ExecuteNativeEmu}}},
	})
	rows, skipped = service.MeshBackendLibrary(ctx)
	if len(skipped) != 0 || len(rows) != 1 || len(rows[0].Options) != 3 {
		t.Fatalf("with runner rows=%+v skipped=%+v", rows, skipped)
	}
	remote := rows[0].Options[2]
	if remote.HostLocal || remote.Entry.TitleID != romID || remote.Reason != "no compatible executor in inventory" || len(remote.Nodes) != 1 || remote.Nodes[0].Available || remote.Nodes[0].NodeID != emuID || remote.Nodes[0].Reason != "remote emulator system and version unverified" {
		t.Fatalf("remote option=%+v", remote)
	}
	if len(rows[0].ContentSources) != 1 || len(rows[0].ContentSources[0].NodeIDs) != 0 {
		t.Fatalf("provenance shipped: %+v", rows[0].ContentSources)
	}
	if hits := runnerHits.Load(); hits != 0 || len(guard.denied) != 0 {
		t.Fatalf("bearer left the configured kit origin: runner_hits=%d denied=%v", hits, guard.denied)
	}
	service.targets = nil
	rows, skipped = service.MeshBackendLibrary(ctx)
	if len(skipped) != 0 {
		t.Fatalf("without authenticated kit: skipped=%+v", skipped)
	}
	core := backendRow(t, rows, coreID)
	if core.Options[0].Reason != "no compatible executor in inventory" || len(core.Options[0].Nodes) != 1 || core.Options[0].Nodes[0].Reason != "package unavailable on node" {
		t.Fatalf("without package facts: core=%+v", core)
	}
}

func TestMeshBackendLibraryTwoNodeProjection(t *testing.T) {
	// This synthetic same-id composition exercises projection only. The
	// catalog gives a core entry and a raw ROM different game ids.
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
	emuOnly := meshNativeTitle("Emulator only", protocol.SystemSMS, strings.Repeat("01", 32))
	lib := MeshLibrary{Titles: []MeshTitle{fpga, native, pong, emuOnly, native}}
	packages := map[string][]string{kitID: {pkgID}}
	abis := map[string][]meshcontent.EligibleABI{kitID: {{ID: "fes.application", Major: 1}}}
	nodes := []MeshNode{kit, emu}
	rows, skipped := ProjectMeshBackendLibrary(lib, nodes, false, packages, abis)
	if len(skipped) != 0 || len(rows) != 3 {
		t.Fatalf("rows=%+v skipped=%+v", rows, skipped)
	}
	dual := backendRow(t, rows, fpga.Game.ID)
	if len(dual.Options) != 3 || len(dual.ContentIDs) != 1 || dual.ContentIDs[0].String() != "sha256:"+media {
		t.Fatalf("dual=%+v", dual)
	}
	if dual.Options[0].Entry.Execute[0].Kind != meshcontent.ExecuteFPGANative || !dual.Options[0].Nodes[0].Available {
		t.Fatalf("fpga=%+v", dual.Options[0])
	}
	if dual.Options[1].Entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu || !dual.Options[1].HostLocal || dual.Options[1].Nodes[0].Available || dual.Options[1].Nodes[0].Reason == "" {
		t.Fatalf("emu=%+v", dual.Options[1])
	}
	if opt := dual.Options[2]; opt.HostLocal || opt.Reason != "no compatible executor in inventory" || len(opt.Nodes) != 1 || opt.Nodes[0].Available || opt.Nodes[0].NodeID != emuID || opt.Nodes[0].Reason != "remote emulator system and version unverified" {
		t.Fatalf("phase-2 option=%+v", opt)
	}
	if got := backendRow(t, rows, pong.Game.ID); len(got.Options) != 1 || got.Options[0].Entry.Execute[0].Kind != meshcontent.ExecuteFPGANative {
		t.Fatalf("pong=%+v", got)
	}
	if got := backendRow(t, rows, emuOnly.Game.ID); len(got.Options) != 2 || !got.Options[0].HostLocal || got.Options[0].Entry.Execute[0].Kind != meshcontent.ExecuteNativeEmu || got.Options[1].HostLocal || got.Options[1].Nodes[0].Available {
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

type originGuard struct {
	base   http.RoundTripper
	allow  string
	denied []string
}

func (g *originGuard) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != g.allow {
		g.denied = append(g.denied, r.URL.String()+" "+r.Header.Get("Authorization"))
		return nil, fmt.Errorf("refused %s", r.URL.Host)
	}
	return g.base.RoundTrip(r)
}

func assertKitOnlyLibraryDocument(t *testing.T, raw []byte, coreID, romID, media, packageID, abi string, major int, kitID string) {
	t.Helper()
	var doc struct {
		Titles []struct {
			TitleID        string   `json:"title_id"`
			System         string   `json:"system"`
			ContentIDs     []string `json:"content_ids"`
			ContentSources []struct {
				ContentID string   `json:"content_id"`
				NodeIDs   []string `json:"node_ids"`
			} `json:"content_sources"`
			Options []struct {
				SourceGameID string `json:"source_game_id"`
				Execution    string `json:"execution"`
				HostLocal    bool   `json:"host_local"`
				Available    bool   `json:"available"`
				Reason       string `json:"reason"`
				CoreID       string `json:"core_id"`
				Package      *struct {
					PackageID string `json:"package_id"`
					ABI       string `json:"abi"`
					Major     int    `json:"major"`
				} `json:"package"`
				Nodes []struct {
					NodeID    string `json:"node_id"`
					Available bool   `json:"available"`
					Reason    string `json:"reason"`
				} `json:"nodes"`
			} `json:"options"`
		} `json:"titles"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, absent := range []string{"ready_here", "host_only", "Data Storm.sms", "logical/", `"path"`, `"nodes":null`, `"node_ids":null`, `"titles":null`} {
		if strings.Contains(body, absent) {
			t.Fatalf("document contains %q: %s", absent, body)
		}
	}
	if len(doc.Titles) != 1 {
		t.Fatalf("titles %+v", doc)
	}
	title := doc.Titles[0]
	if title.TitleID != coreID || title.System != "sms" || len(title.ContentIDs) != 1 || title.ContentIDs[0] != "sha256:"+media {
		t.Fatalf("title %+v", title)
	}
	if len(title.ContentSources) != 1 || title.ContentSources[0].ContentID != title.ContentIDs[0] || len(title.ContentSources[0].NodeIDs) != 0 {
		t.Fatalf("sources %+v", title.ContentSources)
	}
	if len(title.Options) != 2 {
		t.Fatalf("options %+v", title.Options)
	}
	fpga, emu := title.Options[0], title.Options[1]
	if fpga.SourceGameID != coreID || fpga.Execution != "fpga_native" || fpga.HostLocal || !fpga.Available || fpga.Reason != "" || fpga.CoreID != "fes.sms" || fpga.Package == nil || fpga.Package.PackageID != packageID || fpga.Package.ABI != abi || fpga.Package.Major != major || len(fpga.Nodes) != 1 || fpga.Nodes[0].NodeID != kitID || !fpga.Nodes[0].Available || fpga.Nodes[0].Reason != "" {
		t.Fatalf("fpga %+v", fpga)
	}
	if emu.SourceGameID != romID || emu.Execution != "native_emu" || !emu.HostLocal || !emu.Available || emu.Reason != "" || emu.CoreID != "" || emu.Package != nil || len(emu.Nodes) != 0 {
		t.Fatalf("emu %+v", emu)
	}
	for _, option := range title.Options {
		if !option.HostLocal && option.Execution == "native_emu" {
			t.Fatalf("kit-only remote option %+v", option)
		}
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

// Same-id host and sourced options must retain their distinct availability.
func TestMeshBackendLibraryHostAndSourcedCollision(t *testing.T) {
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	local := meshNativeTitle("Data Storm 1.00", protocol.SystemSMS, media)
	remote := local
	remote.Execute = meshcontent.ExecuteNativeEmu
	rows, skipped := ProjectMeshBackendLibrary(MeshLibrary{Titles: []MeshTitle{local, remote}}, nil, false, nil, nil)
	if len(skipped) != 0 || len(rows) != 1 {
		t.Fatalf("rows=%+v skipped=%+v", rows, skipped)
	}
	if options := rows[0].Options; len(options) != 2 || !options[0].HostLocal || options[0].Reason != "" || options[1].HostLocal || options[1].Reason != "no advertised executor in inventory" {
		t.Fatalf("options=%+v", options)
	}

	// A remote-sourced emulator title (not host-local) is listed but
	// unavailable, with explicit node and option reasons.
	kitID, emuID := "01234567-89ab-cdef-0123-456789abcdef", "fedcba98-7654-3210-fedc-ba9876543210"
	kit := MeshNode{NodeID: kitID, TargetID: kitID, Mesh: discovery.MeshProtocol, Address: "http://192.0.2.1:8182", Capabilities: discovery.KitCapabilities()}
	packages := map[string][]string{}
	abis := map[string][]meshcontent.EligibleABI{}
	emu := MeshNode{NodeID: emuID, TargetID: emuID, Mesh: discovery.MeshProtocol, Address: "http://192.0.2.2:8182", Capabilities: discovery.Capabilities{Execute: []discovery.Execute{{Kind: meshcontent.ExecuteNativeEmu}}}}
	rows, _ = ProjectMeshBackendLibrary(MeshLibrary{Titles: []MeshTitle{remote}}, []MeshNode{kit, emu}, false, packages, abis)
	got := backendRow(t, rows, remote.Game.ID)
	if len(got.Options) != 1 {
		t.Fatalf("remote=%+v", got)
	}
	opt := got.Options[0]
	if opt.HostLocal || opt.Reason != "no compatible executor in inventory" || len(opt.Nodes) != 1 ||
		opt.Nodes[0].Available || opt.Nodes[0].Reason != "remote emulator system and version unverified" {
		t.Fatalf("remote option=%+v", opt)
	}
	rows, _ = ProjectMeshBackendLibrary(MeshLibrary{Titles: []MeshTitle{remote}}, []MeshNode{kit}, false, packages, abis)
	if opt := backendRow(t, rows, remote.Game.ID).Options[0]; opt.Reason != "no advertised executor in inventory" {
		t.Fatalf("remote without runner=%+v", opt)
	}

	// An offline or unavailable local source is not a usable host-local
	// option, matching the launch check.
	offline := local
	offline.Game.RootOnline = false
	unavailable := local
	unavailable.Game.State = catalog.SourceStateMissing
	for name, title := range map[string]MeshTitle{"offline": offline, "missing": unavailable} {
		rows, _ = ProjectMeshBackendLibrary(MeshLibrary{Titles: []MeshTitle{title}}, nil, false, nil, nil)
		if opt := backendRow(t, rows, title.Game.ID).Options[0]; !opt.HostLocal || opt.Reason != "local source unavailable" {
			t.Fatalf("%s local option=%+v", name, opt)
		}
	}
}

// Bob's #380 join rule: link only on an exact ROM sha256 AND system match;
// the package row's game id is canonical; collisions resolve package first,
// then lowest game id, independent of input order.
func TestMeshBackendLibraryTitleLink(t *testing.T) {
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	patched := strings.Repeat("5a", 32)
	pkgID := strings.Repeat("ab", 32)
	pkg := meshCoreTitle("Data Storm 1.00", "fes.sms", pkgID, media, false)

	// Hash mismatch (patched ROM): two rows.
	hack := meshNativeTitle("Data Storm 1.00", protocol.SystemSMS, patched)
	rows, skipped := ProjectMeshBackendLibrary(MeshLibrary{Titles: []MeshTitle{pkg, hack}}, nil, false, nil, nil)
	if len(skipped) != 0 || len(rows) != 2 {
		t.Fatalf("hash mismatch rows=%+v skipped=%+v", rows, skipped)
	}
	backendRow(t, rows, pkg.Game.ID)
	backendRow(t, rows, hack.Game.ID)

	// Same hash, different system: separate rows.
	gg := meshNativeTitle("Data Storm 1.00", protocol.SystemGameGear, media)
	rows, skipped = ProjectMeshBackendLibrary(MeshLibrary{Titles: []MeshTitle{pkg, gg}}, nil, false, nil, nil)
	if len(skipped) != 0 || len(rows) != 2 {
		t.Fatalf("system mismatch rows=%+v skipped=%+v", rows, skipped)
	}
	if got := backendRow(t, rows, gg.Game.ID); got.System == "sms" || len(got.Options) != 1 {
		t.Fatalf("system mismatch row=%+v", got)
	}

	// Three-plus collision: two package rows and two raw ROM rows with the
	// same hash and system. The lowest package game id is canonical, and
	// the result is identical for every input order.
	pkg2 := meshCoreTitle("Data Storm (alt package)", "fes.sms", strings.Repeat("cd", 32), media, false)
	romA := meshNativeTitle("Data Storm A", protocol.SystemSMS, media)
	romB := meshNativeTitle("Data Storm B", protocol.SystemSMS, media)
	titles := []MeshTitle{romB, pkg2, romA, pkg}
	wantID := pkg.Game.ID
	if pkg2.Game.ID < wantID {
		wantID = pkg2.Game.ID
	}
	var first []MeshBackendRow
	orders := [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}, {1, 3, 0, 2}}
	for n, order := range orders {
		in := make([]MeshTitle, 0, len(order))
		for _, i := range order {
			in = append(in, titles[i])
		}
		rows, skipped := ProjectMeshBackendLibrary(MeshLibrary{Titles: in}, nil, false, nil, nil)
		if len(skipped) != 0 || len(rows) != 1 || rows[0].TitleID != wantID || len(rows[0].Options) != 4 || len(rows[0].ContentIDs) != 1 {
			t.Fatalf("order %v rows=%+v skipped=%+v", order, rows, skipped)
		}
		options := rows[0].Options
		for i, option := range options {
			_, isPackage := meshEntryPackage(option.Entry)
			if isPackage != (i < 2) {
				t.Fatalf("order %v: package options must come first: %+v", order, options)
			}
		}
		if options[0].Entry.TitleID > options[1].Entry.TitleID || options[2].Entry.TitleID > options[3].Entry.TitleID {
			t.Fatalf("order %v: options not ordered by game id: %+v", order, options)
		}
		if n == 0 {
			first = rows
		} else if !reflect.DeepEqual(rows, first) {
			t.Fatalf("order %v differs:\n%+v\n%+v", order, rows, first)
		}
	}
}

// Kit plus a runner: one merged title per content id, per-node availability,
// and a malformed content id recorded as a skip.
func TestMeshBackendLibraryCombinedTwoNode(t *testing.T) {
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	kitID, emuID := "01234567-89ab-cdef-0123-456789abcdef", "fedcba98-7654-3210-fedc-ba9876543210"
	kit := MeshNode{NodeID: kitID, TargetID: kitID, Mesh: discovery.MeshProtocol, Address: "http://192.0.2.1:8182", Capabilities: discovery.KitCapabilities()}
	emu := MeshNode{NodeID: emuID, TargetID: emuID, Mesh: discovery.MeshProtocol, Address: "http://192.0.2.2:8182", Capabilities: discovery.Capabilities{Execute: []discovery.Execute{{Kind: meshcontent.ExecuteNativeEmu}}}}
	pkgID := strings.Repeat("ab", 32)
	fpga := meshCoreTitle("Data Storm 1.00", "fes.sms", pkgID, media, false)
	native := meshNativeTitle("Data Storm 1.00", protocol.SystemSMS, media)
	other := meshNativeTitle("Other Cart", protocol.SystemSMS, strings.Repeat("11", 32))
	gear := meshNativeTitle("Data Storm 1.00", protocol.SystemGameGear, media)
	malformedText := "sha256:" + strings.ToUpper(media)
	if _, err := meshcontent.ParseContentID(malformedText); err == nil {
		t.Fatal("uppercase wire id parsed")
	}
	if _, err := meshcontent.ParseContentID("/tmp/Data Storm.sms"); err == nil {
		t.Fatal("path parsed as a content id")
	}
	bad := meshNativeTitle("Broken", protocol.SystemSMS, malformedText)
	pathed := meshNativeTitle("Pathed", protocol.SystemSMS, "/tmp/Data Storm.sms")
	prefixed := meshNativeTitle("Prefixed", protocol.SystemSMS, "sha256:"+media)
	dev := meshNativeTitle("Development", protocol.SystemSMS, strings.Repeat("22", 32))
	dev.Execute = "development"
	lib := MeshLibrary{Titles: []MeshTitle{native, fpga, native, other, gear, bad, pathed, prefixed, dev}}
	packages := map[string][]string{kitID: {pkgID}}
	abis := map[string][]meshcontent.EligibleABI{kitID: {{ID: "fes.application", Major: 1}}}

	kitOnly, kitSkipped := ProjectMeshBackendLibrary(lib, []MeshNode{kit}, false, packages, abis)
	mergedKit := backendRow(t, kitOnly, fpga.Game.ID)
	if len(mergedKit.Options) != 2 || mergedKit.TitleID != fpga.Game.ID {
		t.Fatalf("kit-only merge %+v", mergedKit)
	}
	for _, option := range mergedKit.Options {
		if !option.HostLocal && len(option.Entry.Execute) == 1 && option.Entry.Execute[0].Kind == meshcontent.ExecuteNativeEmu {
			t.Fatalf("kit-only remote option %+v", option)
		}
	}
	const storedDigestSkip = "primary media digest is not a stored sha256"
	if !skipHas(kitSkipped, bad.Game.ID, storedDigestSkip) || !skipHas(kitSkipped, pathed.Game.ID, storedDigestSkip) || !skipHas(kitSkipped, prefixed.Game.ID, storedDigestSkip) || !skipHas(kitSkipped, dev.Game.ID, "execution is not a mesh catalog kind") {
		t.Fatalf("kit-only skipped=%+v", kitSkipped)
	}

	rows, skipped := ProjectMeshBackendLibrary(lib, []MeshNode{kit, emu}, false, packages, abis)
	if len(rows) != 3 {
		t.Fatalf("rows=%d %+v skipped=%+v", len(rows), rows, skipped)
	}
	if !skipHas(skipped, bad.Game.ID, storedDigestSkip) || !skipHas(skipped, pathed.Game.ID, storedDigestSkip) || !skipHas(skipped, prefixed.Game.ID, storedDigestSkip) || !skipHas(skipped, dev.Game.ID, "execution is not a mesh catalog kind") || len(skipped) != 4 {
		t.Fatalf("skipped=%+v", skipped)
	}
	for _, skip := range skipped {
		if skip.TitleID == "" || skip.Reason == "" {
			t.Fatalf("silent skip %+v", skip)
		}
		for _, row := range rows {
			if row.TitleID == skip.TitleID {
				t.Fatalf("skipped title was projected: %+v", row)
			}
		}
	}

	merged := backendRow(t, rows, fpga.Game.ID)
	if merged.System != "sms" || len(merged.ContentIDs) != 1 || merged.ContentIDs[0].String() != "sha256:"+media {
		t.Fatalf("merged content %+v", merged)
	}
	if len(merged.ContentSources) != 1 || merged.ContentSources[0].ContentID != merged.ContentIDs[0] || len(merged.ContentSources[0].NodeIDs) != 0 {
		t.Fatalf("sources %+v", merged.ContentSources)
	}
	if len(merged.Options) != 3 {
		t.Fatalf("options %+v", merged.Options)
	}
	fpgaOpt, hostOpt, remoteOpt := merged.Options[0], merged.Options[1], merged.Options[2]
	if fpgaOpt.CoreID != "fes.sms" || !fpgaOpt.Nodes[0].Available || fpgaOpt.Nodes[0].NodeID != kitID || len(fpgaOpt.Nodes) != 1 {
		t.Fatalf("kit node %+v", fpgaOpt)
	}
	if !hostOpt.HostLocal || hostOpt.Reason != "" || !hostOpt.Available() || hostOpt.Entry.TitleID != native.Game.ID || len(hostOpt.Nodes) != 1 || hostOpt.Nodes[0].Available || hostOpt.Nodes[0].NodeID != emuID || hostOpt.Nodes[0].Reason != "remote emulator system and version unverified" {
		t.Fatalf("host-local %+v", hostOpt)
	}
	if remoteOpt.HostLocal || remoteOpt.Entry.TitleID != native.Game.ID || remoteOpt.Reason != "no compatible executor in inventory" || remoteOpt.Available() || len(remoteOpt.Nodes) != 1 || remoteOpt.Nodes[0].Available || remoteOpt.Nodes[0].NodeID != emuID || remoteOpt.Nodes[0].Reason != "remote emulator system and version unverified" {
		t.Fatalf("remote %+v", remoteOpt)
	}
	otherRow := backendRow(t, rows, other.Game.ID)
	if otherRow.TitleID == merged.TitleID || len(otherRow.ContentIDs) != 1 || otherRow.ContentIDs[0] == merged.ContentIDs[0] {
		t.Fatalf("distinct hash collapsed %+v", otherRow)
	}
	gearRow := backendRow(t, rows, gear.Game.ID)
	if gearRow.System == "sms" || gearRow.TitleID == merged.TitleID {
		t.Fatalf("system mismatch linked %+v", gearRow)
	}

	library, err := MeshLibraryTitles([]MeshBackendRow{merged})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(library)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, absent := range []string{"ready_here", "host_only", "roms/", ".sfc", "/tmp/", "logical", malformedText, `"node_ids":["`} {
		if strings.Contains(body, absent) {
			t.Fatalf("document contains %q: %s", absent, body)
		}
	}
	if !strings.Contains(body, `"reason":"no compatible executor in inventory"`) || !strings.Contains(body, `"reason":"remote emulator system and version unverified"`) || !strings.Contains(body, `"host_local":false`) || !strings.Contains(body, `"available":false`) {
		t.Fatalf("phase-2 option missing from %s", body)
	}
}

func TestMeshLibraryTitlesRejectsMalformedContentID(t *testing.T) {
	bad := meshcontent.ContentID{Algorithm: "sha256", Digest: "ZZ"}
	_, err := MeshLibraryTitles([]MeshBackendRow{{
		TitleID:        "sms-cart",
		System:         "sms",
		ContentIDs:     []meshcontent.ContentID{bad},
		ContentSources: []MeshContentSource{{ContentID: bad, NodeIDs: []string{}}},
		Options: []MeshBackendOption{{
			Entry: meshcontent.Entry{
				TitleID: "sms-cart",
				Execute: []meshcontent.Execute{{Kind: meshcontent.ExecuteNativeEmu}},
			},
			HostLocal: true,
		}},
	}})
	if err == nil || !strings.Contains(err.Error(), MeshSkipMalformedContentID) {
		t.Fatalf("malformed content id: %v", err)
	}
	if _, parseErr := meshcontent.ParseContentID("fpga-data-storm-1-00-39c4d68f01fa"); parseErr == nil {
		t.Fatal("title id parsed as a content id")
	}
}

func skipHas(skipped []MeshSkip, titleID, reason string) bool {
	for _, skip := range skipped {
		if skip.TitleID == titleID && skip.Reason == reason {
			return true
		}
	}
	return false
}
