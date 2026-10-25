package tenfoot

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const (
	workshopStartGameID   = "fpga-fes-zx81-9c9571aad009"
	workshopStartTarget   = "kit-a"
	workshopStartTargetID = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
	workshopStartToken    = "12345678901234567890123456789012"
)

var (
	workshopStartPreviousPackage = strings.Repeat("7", 64)
	workshopStartPackage         = strings.Repeat("4", 64)
)

type workshopStartService struct {
	mu sync.Mutex

	selectedPackage string
	selections      [][3]string
	launches        []string
}

func newWorkshopStartService() *workshopStartService {
	return &workshopStartService{selectedPackage: workshopStartPreviousPackage}
}

func workshopStartGame() catalog.Game {
	return catalog.Game{
		ID: workshopStartGameID, Title: "Workshop ZX81", System: catalog.CorePlatform,
		Kind: catalog.SourceKindCorePackage, State: catalog.SourceStateAvailable, RootOnline: true,
	}
}

func (s *workshopStartService) Games(context.Context) ([]catalog.Game, error) {
	return []catalog.Game{workshopStartGame()}, nil
}

func (s *workshopStartService) Search(context.Context, string) ([]catalog.Game, error) {
	return []catalog.Game{workshopStartGame()}, nil
}

func (s *workshopStartService) Game(_ context.Context, id string) (catalog.Game, error) {
	if id != workshopStartGameID {
		return catalog.Game{}, errors.New("game not found")
	}
	return workshopStartGame(), nil
}

func (*workshopStartService) Health(context.Context) (protocol.Health, error) {
	return protocol.Health{Ready: true}, nil
}

func (s *workshopStartService) Status(context.Context) (protocol.Status, error) {
	s.mu.Lock()
	active := len(s.launches) != 0
	s.mu.Unlock()
	if active {
		return workshopStartActiveStatus(), nil
	}
	return protocol.Status{State: protocol.StateIdle}, nil
}

func (s *workshopStartService) Launch(ctx context.Context, gameID string, progress fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return s.LaunchOn(ctx, gameID, workshopStartTarget, progress)
}

func (s *workshopStartService) LaunchOn(_ context.Context, gameID, target string, _ fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.launches = append(s.launches, target+":"+gameID)
	if gameID != workshopStartGameID || target != workshopStartTarget || s.selectedPackage != workshopStartPackage {
		return protocol.CachedLaunchResponse{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "ZX81 package selection is not ready"}
	}
	return protocol.CachedLaunchResponse{Status: workshopStartActiveStatus()}, nil
}

func workshopStartActiveStatus() protocol.Status {
	gameID, system, coreID := workshopStartGameID, protocol.System("zx81"), "fes.zx81"
	return protocol.Status{
		State: protocol.StateActive, Development: true, GameID: &gameID, System: &system, ObservedCore: &coreID,
		CorePackage: &protocol.CorePackageStatus{
			PackageID: workshopStartPackage, Generation: 1,
			ABI: protocol.RuntimeContract{ID: "fes.computer", Major: 1}, BuildID: strings.Repeat("b", 32),
		},
	}
}

func (*workshopStartService) LoadDevelopmentRBF(context.Context, int64, io.Reader) (protocol.Status, error) {
	return protocol.Status{}, nil
}

func (s *workshopStartService) Stop(context.Context) (protocol.Status, error) {
	s.mu.Lock()
	s.launches = nil
	s.mu.Unlock()
	return protocol.Status{State: protocol.StateIdle}, nil
}

func (*workshopStartService) MeshBackendLibrary(context.Context) ([]fogcast.MeshBackendRow, []fogcast.MeshSkip) {
	return []fogcast.MeshBackendRow{}, []fogcast.MeshSkip{}
}

func (*workshopStartService) LibrarySettings() fogcast.LibraryConfig {
	return fogcast.LibraryConfig{
		SelectedTarget: workshopStartTarget,
		Targets:        []fogcast.TargetConfig{{Name: workshopStartTarget, TargetID: workshopStartTargetID, Enabled: true}},
	}
}

func (*workshopStartService) PairedTargetStatus(context.Context, string) (kitlease.Status, bool, bool, error) {
	return kitlease.Status{State: "free", Generation: "gen-1"}, true, true, nil
}

func (s *workshopStartService) Hardware(context.Context) ([]fogcast.HardwareMachine, error) {
	s.mu.Lock()
	selected := s.selectedPackage
	s.mu.Unlock()
	return []fogcast.HardwareMachine{{
		GameID: workshopStartGameID, Title: "Workshop ZX81", CoreID: "fes.zx81",
		PackageID: selected, LaunchPackageID: workshopStartPackage,
		PackageReady: true, FirmwareReady: true, Ready: true,
		Socket:  fogcast.HardwareSocket{ID: "rear", Label: "Rear expansion socket", Supported: true},
		Choices: []fogcast.HardwareExpansion{},
	}}, nil
}

func (*workshopStartService) CoreCompositions(_ context.Context, ids []string) (map[string]protocol.CoreComposition, error) {
	result := make(map[string]protocol.CoreComposition, len(ids))
	for _, id := range ids {
		if id == workshopStartGameID {
			result[id] = protocol.CoreComposition{FirmwareRequired: true, FirmwareReady: true, ROMRole: protocol.FirmwareRole, ROMID: "machine-rom", ROMSourceSize: 8192}
		}
	}
	return result, nil
}

func (*workshopStartService) CorePackages(context.Context) ([]fogcast.InstalledCorePackage, error) {
	return []fogcast.InstalledCorePackage{{Inspection: workshopStartInspection(workshopStartPackage)}}, nil
}

func (*workshopStartService) CorePackage(_ context.Context, id string) (corepackage.Inspection, error) {
	if id != workshopStartPackage {
		return corepackage.Inspection{}, errors.New("package not found")
	}
	return workshopStartInspection(id), nil
}

func (*workshopStartService) CorePackageCompatibility(context.Context, string) (fogcast.CoreCompatibility, error) {
	return fogcast.CoreCompatibility{}, nil
}

func (*workshopStartService) CoreEntries(context.Context) ([]catalog.CoreEntry, error) {
	return []catalog.CoreEntry{{
		GameID: workshopStartGameID, Title: "Workshop ZX81", CoreID: "fes.zx81", PackageID: workshopStartPreviousPackage,
	}}, nil
}

func (s *workshopStartService) CoreEntry(context.Context, string) (catalog.CoreEntry, error) {
	s.mu.Lock()
	selected := s.selectedPackage
	s.mu.Unlock()
	return catalog.CoreEntry{GameID: workshopStartGameID, Title: "Workshop ZX81", CoreID: "fes.zx81", PackageID: selected}, nil
}

func (*workshopStartService) CreateCoreEntry(context.Context, string, string) (catalog.CoreEntry, error) {
	return catalog.CoreEntry{}, errors.New("entry creation not used")
}

func (s *workshopStartService) SelectCoreEntry(_ context.Context, game, expected, pkg string) (catalog.CoreEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selections = append(s.selections, [3]string{game, expected, pkg})
	if game != workshopStartGameID || expected != s.selectedPackage || pkg != workshopStartPackage {
		return catalog.CoreEntry{}, &protocol.APIError{Code: protocol.CodeBadRequest, Phase: "admission", Message: "ZX81 package compare-and-swap failed"}
	}
	s.selectedPackage = pkg
	return catalog.CoreEntry{GameID: game, Title: "Workshop ZX81", CoreID: "fes.zx81", PackageID: pkg}, nil
}

func (*workshopStartService) ImportCorePackage(context.Context, int64, io.Reader) (corepackage.Inspection, bool, error) {
	return corepackage.Inspection{}, false, errors.New("package import not used")
}

func (*workshopStartService) CoreMediaCapabilities(_ context.Context, id string) (protocol.CoreMediaCapabilities, error) {
	return protocol.CoreMediaCapabilities{PackageID: id, Source: "declared-contract", Compatibility: "unknown", Media: []protocol.CoreMediaCapability{}}, nil
}

func workshopStartInspection(id string) corepackage.Inspection {
	return corepackage.Inspection{PackageID: id, Descriptor: corepackage.Descriptor{
		Format: 3,
		Core:   corepackage.Core{ID: "fes.zx81", Name: "FES ZX81", Version: "1.7.0"},
		ROM:    &corepackage.ROM{ID: "machine-rom", Role: protocol.FirmwareRole, SourceSize: 8192},
		ABI:    corepackage.Contract{ID: "fes.computer", Major: 1},
	}}
}

func workshopStartHasText(snapshot Snapshot, text string) bool {
	for _, op := range snapshot.Room.Frame.Ops {
		if op.Kind == rooms.OpText && op.Text == text {
			return true
		}
	}
	return false
}

func TestWorkshopStartMachineSucceedsThroughPairedHostAPI(t *testing.T) {
	service := newWorkshopStartService()
	api, err := hostapi.NewLauncherHandler(hostapi.New(service), hostapi.LauncherConfig{Pairings: []hostapi.LauncherPairing{{Token: workshopStartToken, TargetID: workshopStartTargetID}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := NewClient(server.URL, launcherHTTPClient(workshopStartToken, workshopStartTargetID))

	setup, err := client.InstalledZX81Setup(t.Context(), workshopStartPackage)
	if err != nil || setup.PackageID != workshopStartPackage || len(setup.Entries) != 1 || setup.ROMs[0].ID != "machine-rom" {
		t.Fatalf("installed ZX81 setup over host API = %+v, %v", setup, err)
	}

	app := NewApp(client, 1280, 720, 20)
	app.SetPrefsPath(t.TempDir() + "/tenfoot.json")
	app.SetRooms(rooms.NewIndex(rooms.Examples()), "")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.openRoomLocked(hardwareRoomID)
	app.mu.Unlock()
	waitFor(t, app, "ready ZX81 Start machine button", func(s Snapshot) bool {
		return s.Room.Open && workshopStartHasText(s, "Start machine") && !workshopStartHasText(s, "Checking...")
	})

	app.mu.Lock()
	app.room.Activate("power")
	app.applyRoomActionsLocked()
	app.mu.Unlock()

	started := waitFor(t, app, "Start machine host response", func(s Snapshot) bool {
		return s.Launch.HTTPStatus != 0 || s.Launch.Phase == "host" || s.Launch.Phase == "error"
	})
	if started.Launch.HTTPStatus != 200 || started.Launch.State != "active" || started.Launch.ErrorCode != "" || started.FirmwarePicker.Open {
		t.Fatalf("Start machine did not receive a successful launch response without a firmware prompt: launch=%+v firmware=%+v", started.Launch, started.FirmwarePicker)
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	if len(service.selections) != 1 || service.selections[0] != [3]string{workshopStartGameID, workshopStartPreviousPackage, workshopStartPackage} {
		t.Fatalf("Start machine package selection = %v", service.selections)
	}
	if len(service.launches) != 1 || service.launches[0] != workshopStartTarget+":"+workshopStartGameID {
		t.Fatalf("Start machine launches = %v", service.launches)
	}
}

var _ hostapi.Service = (*workshopStartService)(nil)
