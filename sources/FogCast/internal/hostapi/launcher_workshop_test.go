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

	setup, err := client.InstalledZX81Setup(context.Background(), packageID)
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
