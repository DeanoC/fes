package hostapi_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
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
