package hostapi_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

type pairedSystemSetupService struct {
	settingsFake
	packageID                   string
	catalogReads, setupReads    int
	installTarget, createTarget string
	created                     fogcast.CoreSetupRequest
	blank                       fogcast.BlankAtariStRequest
}

func (s *pairedSystemSetupService) AvailableCores(context.Context) ([]fogcast.AvailableCore, error) {
	s.catalogReads++
	return []fogcast.AvailableCore{}, nil
}
func (s *pairedSystemSetupService) InstallAvailableCore(ctx context.Context, source, core, pkg string) (fogcast.InstalledCorePackage, error) {
	s.installTarget = fogcast.SessionTargetFromContext(ctx)
	return fogcast.InstalledCorePackage{Inspection: corepackage.Inspection{PackageID: pkg}}, nil
}
func (s *pairedSystemSetupService) CoreSetup(_ context.Context, source, core, pkg string) (fogcast.CoreSetup, error) {
	s.setupReads++
	return fogcast.CoreSetup{LibrarySourceID: "household", SourceID: source, CoreID: core, PackageID: pkg}, nil
}
func (s *pairedSystemSetupService) CreateCoreSetupEntry(ctx context.Context, req fogcast.CoreSetupRequest) (catalog.CoreEntry, error) {
	s.createTarget = fogcast.SessionTargetFromContext(ctx)
	s.created = req
	return catalog.CoreEntry{GameID: "my-st", Title: req.Title, CoreID: req.CoreID, PackageID: req.PackageID, MediaRole: req.MediaRole, MediaID: req.MediaID}, nil
}
func (s *pairedSystemSetupService) CreateBlankAtariStDisk(_ context.Context, req fogcast.BlankAtariStRequest) (fogcast.BlankAtariStDisk, error) {
	s.blank = req
	return fogcast.BlankAtariStDisk{MediaID: strings.Repeat("d", 64), Name: req.Name, Tracks: req.Tracks, Heads: req.Heads, Sectors: req.Sectors, Size: int64(req.Tracks * req.Heads * req.Sectors * 512), Created: true}, nil
}

func TestPairedLauncherPublishedSetupAndBlankDiskUseCapturedKit(t *testing.T) {
	service := &pairedSystemSetupService{settingsFake: settingsFake{settings: twoKitSettings("kit-a")}, packageID: strings.Repeat("a", 64)}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(service), distinctKitConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := pairedExpansionClient(server, launcherTokenB, launcherIDB)
	ctx := context.Background()
	if _, err := client.AvailableCores(ctx); err != nil {
		t.Fatal(err)
	}
	ref := hostclient.CoreReference{LibrarySourceID: "household", SourceID: "fes-first-party", CoreID: "fes.atari-st", PackageID: service.packageID}
	if err := client.InstallAvailableCore(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreSetup(ctx, ref.SourceID, ref.CoreID, ref.PackageID); err != nil {
		t.Fatal(err)
	}
	disk, err := client.CreateBlankAtariStDisk(ctx, hostclient.BlankAtariStRequest{Name: "WORK"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.CreateCoreSetupEntry(ctx, hostclient.CoreSetupRequest{CoreReference: ref, Title: "My ST", MediaRole: "disk", MediaID: disk.MediaID})
	if err != nil || result.Entry.MediaID != disk.MediaID {
		t.Fatalf("create %+v err=%v", result, err)
	}
	if service.catalogReads != 1 || service.setupReads != 1 || service.installTarget != "kit-b" || service.createTarget != "kit-b" || service.settings.SelectedTarget != "kit-a" {
		t.Fatalf("catalog reads=%d setup reads=%d install=%s create=%s foreground=%s", service.catalogReads, service.setupReads, service.installTarget, service.createTarget, service.settings.SelectedTarget)
	}
	if service.blank.Name != "WORK" || service.blank.Tracks != 80 || service.blank.Heads != 2 || service.blank.Sectors != 9 || service.created.PackageID != ref.PackageID || service.created.MediaID != disk.MediaID {
		t.Fatalf("blank=%+v created=%+v", service.blank, service.created)
	}
}
