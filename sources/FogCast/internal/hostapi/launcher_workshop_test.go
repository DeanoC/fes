package hostapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type workshopLauncherService struct {
	coreLibraryAPIService
	settings     fogcast.LibraryConfig
	launchTarget string
}

func (s *workshopLauncherService) LibrarySettings() fogcast.LibraryConfig {
	return s.settings
}

func (s *workshopLauncherService) LaunchOn(_ context.Context, gameID, target string, _ fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.launchTarget = target
	system := protocol.System("zx81")
	return protocol.CachedLaunchResponse{Status: protocol.Status{
		State:  protocol.StateActive,
		GameID: &gameID,
		System: &system,
	}}, nil
}

// Tenfoot's Workshop selects the package before it posts the ordinary launch.
// Exercise both calls through the paired listener, including its target pin.
func TestTenfootWorkshopPackageSelectionSucceedsThroughPairedLauncher(t *testing.T) {
	gameID := "zx81-workshop"
	oldPackage, nextPackage := strings.Repeat("a", 64), strings.Repeat("b", 64)
	service := &workshopLauncherService{
		coreLibraryAPIService: coreLibraryAPIService{fakeService: fakeService{
			game: catalog.Game{ID: gameID, Title: "ZX81 Workshop", System: protocol.System("zx81"), Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true},
		}},
		settings: twoKitSettings("kit-a"),
	}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), distinctKitConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := pairedExpansionClient(server, launcherTokenB, launcherIDB)

	ctx := context.Background()
	entry, err := client.SelectCoreEntryPackage(ctx, gameID, oldPackage, nextPackage)
	if err != nil || entry.GameID != gameID || entry.PackageID != nextPackage {
		t.Fatalf("package selection = %+v, %v", entry, err)
	}
	result, err := client.LaunchStamped(ctx, gameID, hostclient.ClientStampNow())
	if err != nil || result.State != "active" || result.Target != "kit-b" {
		t.Fatalf("paired launch = %+v, %v", result, err)
	}
	if len(service.selection) != 3 || service.selection[0] != gameID || service.selection[1] != oldPackage || service.selection[2] != nextPackage {
		t.Fatalf("package selection forwarded as %v", service.selection)
	}
	if service.launchTarget != "kit-b" || service.settings.SelectedTarget != "kit-a" {
		t.Fatalf("launch target=%q foreground=%q", service.launchTarget, service.settings.SelectedTarget)
	}
}

type workshopCoreReadAPIService struct {
	coreLibraryAPIService
	packageID string
	settings  fogcast.LibraryConfig
	reads     []string
}

func (s *workshopCoreReadAPIService) LibrarySettings() fogcast.LibraryConfig { return s.settings }

func (s *workshopCoreReadAPIService) CorePackages(context.Context) ([]fogcast.InstalledCorePackage, error) {
	s.reads = append(s.reads, "packages")
	return []fogcast.InstalledCorePackage{{Inspection: corepackage.Inspection{PackageID: s.packageID, Descriptor: workshopZX81Descriptor()}}}, nil
}

func (s *workshopCoreReadAPIService) CorePackage(_ context.Context, id string) (corepackage.Inspection, error) {
	s.reads = append(s.reads, "package:"+id)
	return corepackage.Inspection{PackageID: id, Descriptor: workshopZX81Descriptor()}, nil
}

func (s *workshopCoreReadAPIService) CoreEntries(context.Context) ([]catalog.CoreEntry, error) {
	s.reads = append(s.reads, "entries")
	return []catalog.CoreEntry{{GameID: "zx81-workshop", Title: "ZX81 Workshop", CoreID: "fes.zx81", PackageID: s.packageID}}, nil
}

func (s *workshopCoreReadAPIService) CoreMediaCapabilities(_ context.Context, id string) (protocol.CoreMediaCapabilities, error) {
	s.reads = append(s.reads, "capabilities:"+id)
	return protocol.CoreMediaCapabilities{PackageID: id, Source: "declared-contract", Compatibility: "unknown", Media: []protocol.CoreMediaCapability{}}, nil
}

func workshopZX81Descriptor() corepackage.Descriptor {
	return corepackage.Descriptor{
		Format: 3,
		Core:   corepackage.Core{ID: "fes.zx81", Name: "FES ZX81", Version: "1.7.0"},
		ROM:    &corepackage.ROM{ID: "machine-rom", Role: protocol.FirmwareRole, SourceSize: 8192},
		ABI:    corepackage.Contract{ID: "fes.computer", Major: 1},
	}
}

func TestWorkshopCoreSetupReadsAndPackageSelectionAreAdmitted(t *testing.T) {
	packageID := strings.Repeat("c", 64)
	service := &workshopCoreReadAPIService{packageID: packageID, settings: twoKitSettings("kit-a")}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), distinctKitConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := pairedExpansionClient(server, launcherTokenB, launcherIDB)

	setup, err := client.InstalledCoreSetup(context.Background(), packageID)
	if err != nil || setup.CoreID != "fes.zx81" || setup.PackageID != packageID || len(setup.ROMs) != 1 || setup.ROMs[0].ID != "machine-rom" {
		t.Fatalf("installed ZX81 setup = %+v, %v", setup, err)
	}
	entry, err := client.SelectCoreEntryPackage(context.Background(), "zx81-workshop", strings.Repeat("a", 64), packageID)
	if err != nil || entry.PackageID != packageID {
		t.Fatalf("package selection = %+v, %v", entry, err)
	}

	want := []string{"package:" + packageID, "entries", "packages", "capabilities:" + packageID}
	if len(service.reads) != len(want) {
		t.Fatalf("launcher setup reads = %v, want %v", service.reads, want)
	}
	for i := range want {
		if service.reads[i] != want[i] {
			t.Fatalf("launcher setup reads = %v, want %v", service.reads, want)
		}
	}
	if len(service.selection) != 3 || service.selection[0] != "zx81-workshop" || service.selection[1] != strings.Repeat("a", 64) || service.selection[2] != packageID {
		t.Fatalf("package selection forwarded as %v", service.selection)
	}
}

type setupCreateAPIService struct {
	coreLibraryAPIService
	settings  fogcast.LibraryConfig
	title     string
	packageID string
	target    string
	romReads  int
	romWrite  []string
}

func (s *setupCreateAPIService) LibrarySettings() fogcast.LibraryConfig { return s.settings }

func (s *setupCreateAPIService) CreateCoreEntry(ctx context.Context, title, id string) (catalog.CoreEntry, error) {
	s.title = title
	s.packageID = id
	s.target = fogcast.SessionTargetFromContext(ctx)
	return catalog.CoreEntry{GameID: "my-zx81", Title: title, CoreID: "fes.zx81", PackageID: id}, nil
}

func (s *setupCreateAPIService) CoreEntryROM(_ context.Context, game string) (catalog.CoreEntryROM, error) {
	s.romReads++
	return catalog.CoreEntryROM{GameID: game}, nil
}

func (s *setupCreateAPIService) SelectCoreEntryROM(_ context.Context, game, pkg, rom, expected, media string) (catalog.CoreEntryROM, error) {
	s.romWrite = []string{game, pkg, rom, expected, media}
	return catalog.CoreEntryROM{GameID: game, PackageID: pkg, ROMID: rom, MediaID: media}, nil
}

// Add game posts the collection, then reads and writes the named ROM binding.
// Other methods on the collection, and other core-entry routes, stay closed.
func TestLauncherAdmitsSetupRoomCoreEntryPost(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	rom := strings.Repeat("b", 64)
	service := &setupCreateAPIService{settings: twoKitSettings("kit-a")}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), distinctKitConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := pairedExpansionClient(server, launcherTokenB, launcherIDB)
	setup := hostclient.CoreSetup{
		CoreReference: hostclient.CoreReference{CoreID: "fes.zx81", PackageID: pkg},
		Descriptor:    corepackage.Descriptor{ABI: corepackage.Contract{ID: "fes.computer", Major: 1}},
		ROMs:          []hostclient.SetupROM{{ID: "machine-rom", Role: "firmware", SourceSize: 8192, Binding: "entry"}},
	}
	entry, err := client.CreateInstalledCoreEntry(context.Background(), setup, "My Zx81", map[string]string{"machine-rom": rom}, "", "")
	if err != nil || entry.GameID != "my-zx81" || entry.PackageID != pkg || entry.CoreID != "fes.zx81" {
		t.Fatalf("create = %+v, %v", entry, err)
	}
	if service.title != "My Zx81" || service.packageID != pkg || service.target != "kit-b" {
		t.Fatalf("forwarded create title=%q package=%q target=%q", service.title, service.packageID, service.target)
	}
	if service.romReads != 1 || len(service.romWrite) != 5 || service.romWrite[0] != "my-zx81" || service.romWrite[1] != pkg || service.romWrite[2] != "machine-rom" || service.romWrite[3] != "" || service.romWrite[4] != rom {
		t.Fatalf("rom read=%d write=%v", service.romReads, service.romWrite)
	}

	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/library/core-entries"},
		{http.MethodPatch, "/api/v1/library/core-entries"},
		{http.MethodDelete, "/api/v1/library/core-entries"},
		{http.MethodPost, "/api/v1/library/core-entries/"},
		{http.MethodPost, "/api/v1/library/core-entries/fpga-zx81"},
		{http.MethodPost, "/api/v1/library/core-entries/fpga-zx81/rom"},
		{http.MethodGet, "/api/v1/library/core-entries/Nope/rom"},
		{http.MethodGet, "/api/v1/library/core-entries/fpga-zx81/rom/extra"},
		{http.MethodGet, "/api/v1/library/core-entries/fpga-zx81/video"},
		{http.MethodPut, "/api/v1/library/core-entries/Nope/media"},
		{http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/media/extra"},
		{http.MethodPost, "/api/v1/library/settings"},
	} {
		response := kitCall(handler, tc.method, tc.path, launcherTokenB, launcherIDB, `{}`)
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "launcher operation is unavailable") {
			t.Fatalf("%s %s = %d %s", tc.method, tc.path, response.Code, response.Body.String())
		}
	}
}

func TestLauncherCorePackageReadsRejectUnlistedPaths(t *testing.T) {
	service := &workshopCoreReadAPIService{packageID: strings.Repeat("c", 64), settings: twoKitSettings("kit-a")}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), distinctKitConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/core-packages/bad",
		"/api/v1/core-packages/" + service.packageID + "/compatibility",
		"/api/v1/core-packages/" + service.packageID + "/media-capabilities/extra",
	} {
		request := launcherRequest(http.MethodGet, "http://192.0.2.1:8789"+path, nil)
		request.Header.Set("Authorization", "Bearer "+launcherTokenB)
		request.Header.Set("X-FogCast-Target-ID", launcherIDB)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "launcher operation is unavailable") {
			t.Fatalf("GET %s = %d %s", path, response.Code, response.Body.String())
		}
	}
}
