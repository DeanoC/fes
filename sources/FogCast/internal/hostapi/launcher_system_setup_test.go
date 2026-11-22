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
	shelfTargets                []string
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

// The shelf answers for the kit the launcher is paired with.
func (s *pairedSystemSetupService) shelfDisk(ctx context.Context, title string) fogcast.AtariStShelfDisk {
	s.shelfTargets = append(s.shelfTargets, fogcast.SessionTargetFromContext(ctx))
	return fogcast.AtariStShelfDisk{GameID: "new-disk", Title: title, PackageID: s.packageID, MediaID: strings.Repeat("d", 64), Size: 737280, Made: true, Kept: fogcast.ShelfKeptNone}
}
func (s *pairedSystemSetupService) AtariStShelf(ctx context.Context) (fogcast.AtariStShelf, error) {
	s.shelfDisk(ctx, "")
	return fogcast.AtariStShelf{Target: "kit-b", KeptKnown: true, Disks: []fogcast.AtariStShelfDisk{}}, nil
}
func (s *pairedSystemSetupService) NewAtariStShelfDisk(ctx context.Context, req fogcast.AtariStShelfNew) (fogcast.AtariStShelfDisk, error) {
	return s.shelfDisk(ctx, req.Title), nil
}
func (s *pairedSystemSetupService) CopyAtariStShelfDisk(ctx context.Context, _, title string) (fogcast.AtariStShelfDisk, error) {
	return s.shelfDisk(ctx, title), nil
}
func (s *pairedSystemSetupService) RevertAtariStShelfDisk(ctx context.Context, game, _ string) (fogcast.AtariStShelfDisk, error) {
	disk := s.shelfDisk(ctx, "Work")
	disk.GameID = game
	return disk, nil
}
func (s *pairedSystemSetupService) RenameAtariStShelfDisk(ctx context.Context, _, _, title string) (fogcast.AtariStShelfDisk, error) {
	return s.shelfDisk(ctx, title), nil
}
func (s *pairedSystemSetupService) DeleteAtariStShelfDisk(ctx context.Context, _, _ string) error {
	s.shelfDisk(ctx, "")
	return nil
}

func TestPairedLauncherPublishedSetupAndDiskShelfUseCapturedKit(t *testing.T) {
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
	if _, err := client.AtariStShelf(ctx); err != nil {
		t.Fatal(err)
	}
	disk, err := client.NewAtariStShelfDisk(ctx, "Work", ref.PackageID, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CopyAtariStShelfDisk(ctx, "work", "Spare"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RevertAtariStShelfDisk(ctx, "work", strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RenameAtariStShelfDisk(ctx, "work", "", "Letters"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteAtariStShelfDisk(ctx, "work", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Join(service.shelfTargets, ",") != "kit-b,kit-b,kit-b,kit-b,kit-b,kit-b" {
		t.Fatalf("shelf targets %v", service.shelfTargets)
	}
	result, err := client.CreateCoreSetupEntry(ctx, hostclient.CoreSetupRequest{CoreReference: ref, Title: "My ST", MediaRole: "disk", MediaID: disk.MediaID})
	if err != nil || result.Entry.MediaID != disk.MediaID {
		t.Fatalf("create %+v err=%v", result, err)
	}
	if service.catalogReads != 1 || service.setupReads != 1 || service.installTarget != "kit-b" || service.createTarget != "kit-b" || service.settings.SelectedTarget != "kit-a" {
		t.Fatalf("catalog reads=%d setup reads=%d install=%s create=%s foreground=%s", service.catalogReads, service.setupReads, service.installTarget, service.createTarget, service.settings.SelectedTarget)
	}
	if service.created.PackageID != ref.PackageID || service.created.MediaID != disk.MediaID {
		t.Fatalf("created=%+v", service.created)
	}
}
