package fogcast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
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
	service := newService(Config{}, Paths{}, cat, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.corePackages = packages
	const kitID = "01234567-89ab-cdef-0123-456789abcdef"
	var kitReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kitReads.Add(1)
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
	if service.meshEnsure || service.meshPlacement || len(service.placementNodes) != 0 || len(service.libraryNodes) != 0 {
		t.Fatalf("ensure=%v placement=%v placement_cache=%d library_cache=%d", service.meshEnsure, service.meshPlacement, len(service.placementNodes), len(service.libraryNodes))
	}
	// Unseeded GET: placement is off and both caches are empty. The
	// configured kit answers, and the FPGA row is available.
	rows, skipped := service.MeshBackendLibrary(ctx)
	seeded := kitReads.Load()
	if seeded == 0 {
		t.Fatal("unseeded library read did not dial the configured origin")
	}
	if len(service.placementNodes) != 0 {
		t.Fatal("library read wrote placement's cache")
	}
	// A raw ROM without a package is skipped; the installed FPGA entry stays usable.
	if !skipHas(skipped, romID, "package abi is required") || len(rows) != 1 || coreID == romID || rows[0].TitleID != coreID {
		t.Fatalf("ids=%q/%q rows=%+v skipped=%+v", coreID, romID, rows, skipped)
	}
	row := rows[0]
	if row.System != "sms" || len(row.Options) != 1 || len(row.ContentIDs) != 1 || row.ContentIDs[0].String() != "sha256:"+media {
		t.Fatalf("row=%+v", row)
	}
	fpgaOpt := row.Options[0]
	if fpgaOpt.Entry.TitleID != coreID || fpgaOpt.Entry.Execute[0].Kind != meshcontent.ExecuteFPGANative || fpgaOpt.Reason != "" || len(fpgaOpt.Nodes) != 1 || !fpgaOpt.Nodes[0].Available {
		t.Fatalf("fpga option=%+v", fpgaOpt)
	}
	cachedRows, cachedSkipped := service.MeshBackendLibrary(ctx)
	if kitReads.Load() != seeded || !skipHas(cachedSkipped, romID, "package abi is required") || len(cachedRows) != 1 || !cachedRows[0].Options[0].Nodes[0].Available {
		t.Fatalf("library cache missed: reads=%d seeded=%d rows=%+v skipped=%+v", kitReads.Load(), seeded, cachedRows, cachedSkipped)
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
	discoveredAuth := atomic.Int32{}
	discovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			discoveredAuth.Add(1)
		}
		http.Error(w, "discovered origin", http.StatusForbidden)
	}))
	defer discovered.Close()
	service.meshNodes[0].Address = discovered.URL
	rows, skipped = service.MeshBackendLibrary(ctx)
	if discoveredAuth.Load() != 0 || kitReads.Load() != seeded || len(guard.denied) != 0 {
		t.Fatalf("library read dialed after discovery moved: discovered_auth=%d kit_reads=%d seeded=%d denied=%v", discoveredAuth.Load(), kitReads.Load(), seeded, guard.denied)
	}
	if moved := rows[0].Options[0]; !moved.Nodes[0].Available || moved.Nodes[0].NodeID != kitID {
		t.Fatalf("cached facts dropped: %+v", moved)
	}
	service.targets = nil
	rows, skipped = service.MeshBackendLibrary(ctx)
	if !skipHas(skipped, romID, "package abi is required") {
		t.Fatalf("without authenticated kit: skipped=%+v", skipped)
	}
	core := backendRow(t, rows, coreID)
	if core.Options[0].Reason != "no compatible executor in inventory" || len(core.Options[0].Nodes) != 1 || core.Options[0].Nodes[0].Reason != "package unavailable on node" {
		t.Fatalf("without package facts: core=%+v", core)
	}
}

// An unseeded library read, with placement off and no cached node
// document, still loads package and ABI facts from the configured
// [[targets]] address. The discovered origin is a different server and
// receives no Authorization header.
func TestMeshBackendLibraryUnseededConfiguredRead(t *testing.T) {
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	const title = "Data Storm 1.00"
	const kitID = "01234567-89ab-cdef-0123-456789abcdef"
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
	coreGame := catalog.Game{ID: coreID, Title: title, System: catalog.CorePlatform, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true}
	cat := &backendCatalog{fakeServiceCatalog: &fakeServiceCatalog{games: []catalog.Game{coreGame}}, entry: catalog.CoreEntry{GameID: coreID, Title: title, CoreID: "fes.sms", PackageID: inspection.PackageID, MediaRole: "blob", MediaID: media}}
	service := newService(Config{}, Paths{}, cat, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.corePackages = packages
	var configuredHits atomic.Int32
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configuredHits.Add(1)
		if r.URL.Path != "/v1/mesh/content/node" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": kitID, "abis": []map[string]any{{"id": inspection.Descriptor.ABI.ID, "major": inspection.Descriptor.ABI.Major}}, "packages": []string{inspection.PackageID}})
	}))
	defer configured.Close()
	var discoveredAuth atomic.Int32
	discovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			discoveredAuth.Add(1)
		}
		http.Error(w, "discovered origin", http.StatusForbidden)
	}))
	defer discovered.Close()
	service.meshHTTP = &http.Client{Transport: &libraryHostSwitch{
		configuredHost: strings.TrimPrefix(configured.URL, "http://"),
		configured:     configured.Client().Transport,
		discoveredHost: strings.TrimPrefix(discovered.URL, "http://"),
		discovered:     discovered.Client().Transport,
		discoveredAuth: &discoveredAuth,
	}}
	service.targets = []TargetConfig{{Name: "kit", Enabled: true, TargetID: kitID, Address: configured.URL, Agent: "fixture-token"}}
	service.meshNodes = []MeshNode{{NodeID: kitID, TargetID: kitID, Address: discovered.URL, Mesh: discovery.MeshProtocol, Capabilities: discovery.KitCapabilities()}}
	if service.meshPlacement || len(service.placementNodes) != 0 || len(service.libraryNodes) != 0 {
		t.Fatalf("placement=%v placement_cache=%d library_cache=%d", service.meshPlacement, len(service.placementNodes), len(service.libraryNodes))
	}
	rows, skipped := service.MeshBackendLibrary(ctx)
	if configuredHits.Load() == 0 || discoveredAuth.Load() != 0 {
		t.Fatalf("configured_hits=%d discovered_auth=%d", configuredHits.Load(), discoveredAuth.Load())
	}
	if len(skipped) != 0 || len(rows) != 1 || len(rows[0].Options) != 1 || !rows[0].Options[0].Nodes[0].Available || rows[0].Options[0].Nodes[0].NodeID != kitID {
		t.Fatalf("unseeded rows=%+v skipped=%+v", rows, skipped)
	}
	if len(service.placementNodes) != 0 || len(service.libraryNodes) != 1 {
		t.Fatalf("placement cache=%d library cache=%d", len(service.placementNodes), len(service.libraryNodes))
	}
	hits := configuredHits.Load()
	if _, skipped = service.MeshBackendLibrary(ctx); configuredHits.Load() != hits || discoveredAuth.Load() != 0 || len(skipped) != 0 {
		t.Fatalf("second GET hammered the kit: configured=%d discovered_auth=%d skipped=%+v", configuredHits.Load(), discoveredAuth.Load(), skipped)
	}
}

// A 302 from the configured kit must not be followed. Go would forward
// Authorization to the same hostname on another port. The redirect
// target receives nothing, and the FPGA row stays unavailable.
func TestMeshBackendLibraryRedirectDoesNotForwardBearer(t *testing.T) {
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	const title = "Data Storm 1.00"
	const kitID = "01234567-89ab-cdef-0123-456789abcdef"
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
	coreGame := catalog.Game{ID: coreID, Title: title, System: catalog.CorePlatform, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true}
	cat := &backendCatalog{fakeServiceCatalog: &fakeServiceCatalog{games: []catalog.Game{coreGame}}, entry: catalog.CoreEntry{GameID: coreID, Title: title, CoreID: "fes.sms", PackageID: inspection.PackageID, MediaRole: "blob", MediaID: media}}
	service := newService(Config{}, Paths{}, cat, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.corePackages = packages
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization forwarded to redirect target: %s", r.Header.Get("Authorization"))
		}
		http.Error(w, "redirect target", http.StatusForbidden)
	}))
	defer target.Close()
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL+"/v1/mesh/content/node")
		w.WriteHeader(http.StatusFound)
	}))
	defer configured.Close()
	shared := &http.Client{}
	service.meshHTTP = shared
	service.targets = []TargetConfig{{Name: "kit", Enabled: true, TargetID: kitID, Address: configured.URL, Agent: "fixture-token"}}
	service.meshNodes = []MeshNode{{NodeID: kitID, TargetID: kitID, Address: configured.URL, Mesh: discovery.MeshProtocol, Capabilities: discovery.KitCapabilities()}}
	rows, skipped := service.MeshBackendLibrary(ctx)
	if shared.CheckRedirect != nil {
		t.Fatal("library read mutated the shared HTTP client")
	}
	if targetHits.Load() != 0 {
		t.Fatalf("redirect target received %d requests", targetHits.Load())
	}
	if len(skipped) != 0 || len(rows) != 1 || len(rows[0].Options) != 1 {
		t.Fatalf("rows=%+v skipped=%+v", rows, skipped)
	}
	node := rows[0].Options[0].Nodes[0]
	if rows[0].Options[0].Reason == "" || node.Available || node.Reason != "package unavailable on node" {
		t.Fatalf("redirect counted as package facts: option=%+v", rows[0].Options[0])
	}
}

// A configured hostname is pinned to the first lookup. A later lookup
// that no longer includes that IP is not dialed and sends no bearer.
// This host does not assign 127.0.0.2, so the second answer is ::1,
// another loopback address with its own server. An IP literal does not
// consult the resolver.
func TestMeshBackendLibraryPinsConfiguredHostname(t *testing.T) {
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	const title = "Data Storm 1.00"
	const kitID = "01234567-89ab-cdef-0123-456789abcdef"
	const token = "fixture-token"
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
	coreGame := catalog.Game{ID: coreID, Title: title, System: catalog.CorePlatform, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true}
	cat := &backendCatalog{fakeServiceCatalog: &fakeServiceCatalog{games: []catalog.Game{coreGame}}, entry: catalog.CoreEntry{GameID: coreID, Title: title, CoreID: "fes.sms", PackageID: inspection.PackageID, MediaRole: "blob", MediaID: media}}
	newLibrary := func() *Service {
		service := newService(Config{}, Paths{}, cat, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
		service.corePackages = packages
		return service
	}
	writeNode := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mesh/content/node" || r.Header.Get("Authorization") != "Bearer "+token {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":  kitID,
			"abis":     []map[string]any{{"id": inspection.Descriptor.ABI.ID, "major": inspection.Descriptor.ABI.Major}},
			"packages": []string{inspection.PackageID},
		})
	}

	ln1, ln2, port := listenLibraryPinPair(t)
	defer ln1.Close()
	defer ln2.Close()
	hostHeader := fmt.Sprintf("fes-kit-a:%d", port)
	var ip1Hits, ip2Hits atomic.Int32
	var ip1Auth, ip2Auth atomic.Bool
	var ip1Host atomic.Bool
	ip1 := serveLibraryPinListener(t, ln1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip1Hits.Add(1)
		if r.Header.Get("Authorization") == "Bearer "+token {
			ip1Auth.Store(true)
		}
		if r.Host == hostHeader {
			ip1Host.Store(true)
		}
		writeNode(w, r)
	}))
	defer ip1.Close()
	ip2 := serveLibraryPinListener(t, ln2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip2Hits.Add(1)
		if r.Header.Get("Authorization") != "" {
			ip2Auth.Store(true)
		}
		writeNode(w, r)
	}))
	defer ip2.Close()
	var discoveredHits atomic.Int32
	discovered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discoveredHits.Add(1)
		http.Error(w, "discovered origin", http.StatusForbidden)
	}))
	defer discovered.Close()

	service := newLibrary()
	service.meshHTTP = &http.Client{}
	service.targets = []TargetConfig{{Name: "kit", Enabled: true, TargetID: kitID, Address: "http://" + hostHeader, Agent: token}}
	service.meshNodes = []MeshNode{{NodeID: kitID, TargetID: kitID, Address: discovered.URL, Mesh: discovery.MeshProtocol, Capabilities: discovery.KitCapabilities()}}
	var resolveMu sync.Mutex
	answer := net.ParseIP("127.0.0.1")
	service.libraryResolve = func(_ context.Context, host string) ([]net.IP, error) {
		if host != "fes-kit-a" {
			t.Errorf("library resolved %q", host)
		}
		resolveMu.Lock()
		defer resolveMu.Unlock()
		return []net.IP{append(net.IP(nil), answer...)}, nil
	}

	rows, skipped := service.MeshBackendLibrary(ctx)
	if ip1Hits.Load() == 0 || !ip1Auth.Load() || !ip1Host.Load() || ip2Hits.Load() != 0 || discoveredHits.Load() != 0 {
		t.Fatalf("first pin read: ip1=%d auth=%v host=%v ip2=%d discovered=%d", ip1Hits.Load(), ip1Auth.Load(), ip1Host.Load(), ip2Hits.Load(), discoveredHits.Load())
	}
	if len(skipped) != 0 || len(rows) != 1 || !rows[0].Options[0].Nodes[0].Available || rows[0].Options[0].Nodes[0].NodeID != kitID {
		t.Fatalf("pinned rows=%+v skipped=%+v", rows, skipped)
	}
	if got := service.libraryPins["fes-kit-a"]; !got.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("pin = %v", got)
	}
	seeded := ip1Hits.Load()

	resolveMu.Lock()
	answer = net.ParseIP("::1")
	resolveMu.Unlock()
	service.meshMu.Lock()
	cached := service.libraryNodes[kitID]
	cached.at = time.Now().Add(-placementNodeTTL - time.Second)
	service.libraryNodes[kitID] = cached
	service.meshMu.Unlock()
	rows, skipped = service.MeshBackendLibrary(ctx)
	if ip2Hits.Load() != 0 || ip2Auth.Load() || ip1Hits.Load() != seeded || discoveredHits.Load() != 0 {
		t.Fatalf("moved read dialed: ip1=%d seeded=%d ip2=%d ip2_auth=%v discovered=%d", ip1Hits.Load(), seeded, ip2Hits.Load(), ip2Auth.Load(), discoveredHits.Load())
	}
	if len(skipped) != 0 || len(rows) != 1 || len(rows[0].Options) != 1 {
		t.Fatalf("moved rows=%+v skipped=%+v", rows, skipped)
	}
	moved := rows[0].Options[0]
	if moved.Available() || moved.Nodes[0].Available || moved.Nodes[0].Reason != LibraryNodeMovedReason {
		t.Fatalf("moved option=%+v", moved)
	}
	if got := service.libraryPins["fes-kit-a"]; !got.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("pin replaced after move: %v", got)
	}

	// A new process has no pin, so the current answer is the enrolled IP.
	restart := newLibrary()
	restart.meshHTTP = &http.Client{}
	restart.targets = service.targets
	restart.meshNodes = service.meshNodes
	restart.libraryResolve = func(_ context.Context, host string) ([]net.IP, error) {
		if host != "fes-kit-a" {
			t.Errorf("restart resolved %q", host)
		}
		return []net.IP{net.ParseIP("::1")}, nil
	}
	rows, skipped = restart.MeshBackendLibrary(ctx)
	if ip2Hits.Load() == 0 || !ip2Auth.Load() || len(skipped) != 0 || len(rows) != 1 || !rows[0].Options[0].Nodes[0].Available {
		t.Fatalf("restart did not re-pin: ip2=%d auth=%v rows=%+v skipped=%+v", ip2Hits.Load(), ip2Auth.Load(), rows, skipped)
	}
	if got := restart.libraryPins["fes-kit-a"]; !got.Equal(net.ParseIP("::1")) {
		t.Fatalf("restart pin = %v", got)
	}
	if got := service.libraryPins["fes-kit-a"]; !got.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("first process pin changed: %v", got)
	}

	literal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.NotFound(w, r)
			return
		}
		writeNode(w, r)
	}))
	defer literal.Close()
	plain := newLibrary()
	plain.meshHTTP = &http.Client{}
	plain.libraryResolve = func(_ context.Context, host string) ([]net.IP, error) {
		t.Errorf("IP literal resolved %q", host)
		return nil, fmt.Errorf("resolver must not be called")
	}
	plain.targets = []TargetConfig{{Name: "kit", Enabled: true, TargetID: kitID, Address: literal.URL, Agent: token}}
	plain.meshNodes = []MeshNode{{NodeID: kitID, TargetID: kitID, Address: discovered.URL, Mesh: discovery.MeshProtocol, Capabilities: discovery.KitCapabilities()}}
	before := discoveredHits.Load()
	rows, skipped = plain.MeshBackendLibrary(ctx)
	if discoveredHits.Load() != before || len(plain.libraryPins) != 0 || len(skipped) != 0 || len(rows) != 1 || !rows[0].Options[0].Nodes[0].Available {
		t.Fatalf("IP literal: discovered=%d pins=%v rows=%+v skipped=%+v", discoveredHits.Load(), plain.libraryPins, rows, skipped)
	}
}

// HTTP_PROXY is set to a fake proxy. The configured kit is an IP
// literal. The library dials that address directly: the proxy receives
// nothing and the kit receives the bearer. ProxyFromEnvironment does
// not proxy loopback, so a second request to a non-loopback literal
// uses the same client and must also miss the proxy.
func TestMeshBackendLibraryIPLiteralSkipsProxy(t *testing.T) {
	service, kitID, writeNode := newLibraryKitService(t)
	var kitHits atomic.Int32
	var kitAuth atomic.Bool
	kit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kitHits.Add(1)
		if r.Header.Get("Authorization") == "Bearer fixture-token" {
			kitAuth.Store(true)
		}
		writeNode(w, r)
	}))
	defer kit.Close()
	if net.ParseIP(kit.Listener.Addr().(*net.TCPAddr).IP.String()) == nil {
		t.Fatalf("kit URL %s is not an IP literal", kit.URL)
	}
	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		http.Error(w, "proxy", http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("https_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	service.meshHTTP = &http.Client{}
	service.targets = []TargetConfig{{
		Name: "kit", Enabled: true, TargetID: kitID, Address: kit.URL, Agent: "fixture-token",
	}}
	service.meshNodes = []MeshNode{{
		NodeID: kitID, TargetID: kitID, Address: "http://203.0.113.5:8182",
		Mesh: discovery.MeshProtocol, Capabilities: discovery.KitCapabilities(),
	}}
	rows, skipped := service.MeshBackendLibrary(context.Background())
	if proxyHits.Load() != 0 || kitHits.Load() == 0 || !kitAuth.Load() {
		t.Fatalf("proxy=%d kit=%d auth=%v", proxyHits.Load(), kitHits.Load(), kitAuth.Load())
	}
	if len(skipped) != 0 || len(rows) != 1 || !rows[0].Options[0].Nodes[0].Available || rows[0].Options[0].Nodes[0].NodeID != kitID {
		t.Fatalf("rows=%+v skipped=%+v", rows, skipped)
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	probe, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "http://192.0.2.1:9/v1/mesh/content/node", nil)
	if err != nil {
		t.Fatal(err)
	}
	probe.Header.Set("Authorization", "Bearer fixture-token")
	_, probeErr := libraryReadClient(nil, nil).Do(probe)
	if proxyHits.Load() != 0 {
		t.Fatalf("proxy received %d requests", proxyHits.Load())
	}
	if probeErr == nil {
		t.Fatal("non-loopback literal was served without a direct refusal")
	}
}

// Hostname lookup shares the document-read deadline. A resolver that
// waits for cancellation must return with the rows unavailable, inside
// about placementNodeTimeout, and the attempt is not cached.
func TestMeshBackendLibraryHostnameLookupSharesReadDeadline(t *testing.T) {
	service, kitID, _ := newLibraryKitService(t)
	service.meshHTTP = &http.Client{}
	service.targets = []TargetConfig{{
		Name: "kit", Enabled: true, TargetID: kitID, Address: "http://fes-kit-a:8182", Agent: "fixture-token",
	}}
	service.meshNodes = []MeshNode{{
		NodeID: kitID, TargetID: kitID, Address: "http://203.0.113.5:8182",
		Mesh: discovery.MeshProtocol, Capabilities: discovery.KitCapabilities(),
	}}
	var sawHost atomic.Value
	service.libraryResolve = func(ctx context.Context, host string) ([]net.IP, error) {
		sawHost.Store(host)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	type outcome struct {
		rows    []MeshBackendRow
		skipped []MeshSkip
		elapsed time.Duration
	}
	done := make(chan outcome, 1)
	go func() {
		start := time.Now()
		rows, skipped := service.MeshBackendLibrary(context.Background())
		done <- outcome{rows, skipped, time.Since(start)}
	}()
	select {
	case got := <-done:
		if got.elapsed < placementNodeTimeout/2 || got.elapsed > placementNodeTimeout+2*time.Second {
			t.Fatalf("lookup duration %s, deadline %s", got.elapsed, placementNodeTimeout)
		}
		host, _ := sawHost.Load().(string)
		if host != "fes-kit-a" {
			t.Fatalf("resolver saw %q", host)
		}
		if len(got.skipped) != 0 || len(got.rows) != 1 || len(got.rows[0].Options) != 1 {
			t.Fatalf("rows=%+v skipped=%+v", got.rows, got.skipped)
		}
		node := got.rows[0].Options[0].Nodes[0]
		if got.rows[0].Options[0].Available() || node.Available || node.Reason != "package unavailable on node" {
			t.Fatalf("option=%+v", got.rows[0].Options[0])
		}
		if len(service.libraryNodes) != 0 {
			t.Fatalf("deadline cached: %+v", service.libraryNodes[kitID])
		}
	case <-time.After(placementNodeTimeout + 2*time.Second):
		t.Fatal("hostname lookup was not bounded by the library read deadline")
	}
}

func newLibraryKitService(t *testing.T) (*Service, string, func(http.ResponseWriter, *http.Request)) {
	t.Helper()
	const media = "4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
	const title = "Data Storm 1.00"
	const kitID = "01234567-89ab-cdef-0123-456789abcdef"
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
	coreGame := catalog.Game{ID: coreID, Title: title, System: catalog.CorePlatform, Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true}
	cat := &backendCatalog{fakeServiceCatalog: &fakeServiceCatalog{games: []catalog.Game{coreGame}}, entry: catalog.CoreEntry{GameID: coreID, Title: title, CoreID: "fes.sms", PackageID: inspection.PackageID, MediaRole: "blob", MediaID: media}}
	service := newService(Config{}, Paths{}, cat, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.corePackages = packages
	writeNode := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mesh/content/node" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":  kitID,
			"abis":     []map[string]any{{"id": inspection.Descriptor.ABI.ID, "major": inspection.Descriptor.ABI.Major}},
			"packages": []string{inspection.PackageID},
		})
	}
	return service, kitID, writeNode
}

func listenLibraryPinPair(t *testing.T) (net.Listener, net.Listener, int) {
	t.Helper()
	var last error
	for i := 0; i < 8; i++ {
		v4, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := v4.Addr().(*net.TCPAddr).Port
		v6, err := net.Listen("tcp6", net.JoinHostPort("::1", fmt.Sprintf("%d", port)))
		if err == nil {
			return v4, v6, port
		}
		last = err
		v4.Close()
	}
	t.Fatalf("listen ::1: %v", last)
	return nil, nil, 0
}

func serveLibraryPinListener(t *testing.T, ln net.Listener, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	if err := srv.Listener.Close(); err != nil {
		t.Fatal(err)
	}
	srv.Listener = ln
	srv.Start()
	return srv
}

func TestMeshBackendLibraryLinksMatchingFPGAPackages(t *testing.T) {
	media := strings.Repeat("42", 32)
	first := meshCoreTitle("First", "fes.sms", strings.Repeat("ab", 32), media, false)
	second := meshCoreTitle("Second", "fes.sms", strings.Repeat("cd", 32), media, false)
	differentDigest := meshCoreTitle("Different digest", "fes.sms", strings.Repeat("ef", 32), strings.Repeat("43", 32), false)
	differentSystem := meshCoreTitle("Different system", "fes.coleco", strings.Repeat("12", 32), media, false)
	wantID := first.Game.ID
	if second.Game.ID < wantID {
		wantID = second.Game.ID
	}
	var original []MeshBackendRow
	for i, titles := range [][]MeshTitle{
		{first, second, differentDigest, differentSystem},
		{differentSystem, second, differentDigest, first},
	} {
		rows, skipped := ProjectMeshBackendLibrary(MeshLibrary{Titles: titles}, nil, false, nil, nil)
		if len(skipped) != 0 || len(rows) != 3 {
			t.Fatalf("rows=%+v skipped=%+v", rows, skipped)
		}
		linked := backendRow(t, rows, wantID)
		if len(linked.Options) != 2 || len(linked.ContentIDs) != 1 || linked.ContentIDs[0].String() != "sha256:"+media {
			t.Fatalf("linked FPGA packages=%+v", linked)
		}
		if linked.Options[0].Entry.TitleID >= linked.Options[1].Entry.TitleID {
			t.Fatalf("option order=%+v", linked.Options)
		}
		backendRow(t, rows, differentDigest.Game.ID)
		backendRow(t, rows, differentSystem.Game.ID)
		if i == 0 {
			original = rows
		} else if !reflect.DeepEqual(rows, original) {
			t.Fatalf("input order changed projection: %+v vs %+v", rows, original)
		}
	}
}

func TestMeshBackendLibraryRejectsNativeEmulation(t *testing.T) {
	pkg := strings.Repeat("ab", 32)
	fpga := meshCoreTitle("Frogger", "fes.coleco", pkg, "", false)
	legacy := meshNativeTitle("Old emulator", protocol.SystemSNES, strings.Repeat("44", 32))
	kitID := "01234567-89ab-cdef-0123-456789abcdef"
	kit := MeshNode{NodeID: kitID, TargetID: kitID, Mesh: discovery.MeshProtocol, Address: "http://192.0.2.1:8182", Capabilities: discovery.KitCapabilities()}
	rows, skipped := ProjectMeshBackendLibrary(MeshLibrary{Titles: []MeshTitle{fpga, legacy}}, []MeshNode{kit}, false,
		map[string][]string{kitID: {pkg}}, map[string][]meshcontent.EligibleABI{kitID: {{ID: "fes.application", Major: 1}}})
	if len(rows) != 1 || rows[0].TitleID != fpga.Game.ID || len(rows[0].Options) != 1 || !rows[0].Options[0].Nodes[0].Available {
		t.Fatalf("FPGA projection=%+v", rows)
	}
	if !skipHas(skipped, legacy.Game.ID, "execution is not a mesh catalog kind") {
		t.Fatalf("legacy projection skips=%+v", skipped)
	}
}

// libraryHostSwitch sends a library dial to the server for that host.
// A discovered origin and the configured origin stay distinct, so a
// bearer sent to the wrong one is visible on discoveredAuth.
type libraryHostSwitch struct {
	configuredHost string
	configured     http.RoundTripper
	discoveredHost string
	discovered     http.RoundTripper
	discoveredAuth *atomic.Int32
}

func (h *libraryHostSwitch) RoundTrip(r *http.Request) (*http.Response, error) {
	switch r.URL.Host {
	case h.configuredHost:
		if h.configured == nil {
			return nil, fmt.Errorf("no configured transport")
		}
		return h.configured.RoundTrip(r)
	case h.discoveredHost:
		if r.Header.Get("Authorization") != "" && h.discoveredAuth != nil {
			h.discoveredAuth.Add(1)
		}
		if h.discovered == nil {
			return nil, fmt.Errorf("no discovered transport")
		}
		return h.discovered.RoundTrip(r)
	default:
		return nil, fmt.Errorf("refusing %s", r.URL.Host)
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
	for _, absent := range []string{"ready_here", "host_only", "host_local", "Data Storm.sms", "logical/", `"path"`, `"nodes":null`, `"node_ids":null`, `"titles":null`} {
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
	if len(title.Options) != 1 {
		t.Fatalf("options %+v", title.Options)
	}
	fpga := title.Options[0]
	if fpga.SourceGameID != coreID || fpga.Execution != "fpga_native" || !fpga.Available || fpga.Reason != "" || fpga.CoreID != "fes.sms" || fpga.Package == nil || fpga.Package.PackageID != packageID || fpga.Package.ABI != abi || fpga.Package.Major != major || len(fpga.Nodes) != 1 || fpga.Nodes[0].NodeID != kitID || !fpga.Nodes[0].Available || fpga.Nodes[0].Reason != "" {
		t.Fatalf("fpga %+v", fpga)
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
	// Exercise the production monitor but allow for a delayed health tick on
	// a busy host before asserting that its observation was published.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.MeshNodes()) == 1 {
			return
		}
		// A bounded wait avoids depending on DNS-SD or a real target.
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("observer-only monitor did not populate inventory")
}

func TestMeshMonitorBoundsBrowseWithoutSlowingHealth(t *testing.T) {
	client := &fakeServiceClient{healthErr: fmt.Errorf("target unavailable")}
	s := newService(Config{}, Paths{}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &fakeServicePreparer{}, client)
	if s.discoveryEnabled() {
		t.Fatal("fixture unexpectedly has a Phase 0 bind")
	}
	id := "01234567-89ab-cdef-0123-456789abcdef"
	browses := 0
	s.collectNodes = func(context.Context) ([]discovery.ObservedNode, error) {
		browses++
		if browses%2 == 0 {
			return nil, nil
		}
		return []discovery.ObservedNode{{NodeID: id, TargetID: id}}, nil
	}
	var next time.Time
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for second := 0; second <= 20; second++ {
		now := start.Add(time.Duration(second) * time.Second)
		s.monitorTargetTick(context.Background(), &next, func() time.Time { return now })
		if want := second/10 + 1; browses != want {
			t.Fatalf("second %d: browses = %d, want %d", second, browses, want)
		}
		if client.healthCalls != second+1 {
			t.Fatalf("second %d: health calls = %d, want %d", second, client.healthCalls, second+1)
		}
		if want := browses % 2; len(s.MeshNodes()) != want {
			t.Fatalf("second %d: inventory = %+v, want %d rows", second, s.MeshNodes(), want)
		}
	}
}

// Same-id host and sourced options must retain their distinct availability.
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
