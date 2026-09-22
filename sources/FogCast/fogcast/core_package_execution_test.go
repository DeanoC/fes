package fogcast

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
)

func TestRecognizedPlayABI(t *testing.T) {
	tests := []struct {
		id         string
		major      int64
		minor      int64
		recognized bool
	}{
		{id: "fes.simple-computer", major: 1, recognized: true},
		{id: "fes.simple-game", major: 1, recognized: true},
		{id: "fes.application", major: 1, recognized: true},
		{id: "fes.simple-computer", major: 1, minor: 1},
		{id: "fes.simple-computer", major: 2},
		{id: "vendor.unknown", major: 1},
		{id: ""},
	}
	for _, test := range tests {
		if got := RecognizedPlayABI(test.id, test.major, test.minor); got != test.recognized {
			t.Fatalf("%s %d.%d = %v, want %v", test.id, test.major, test.minor, got, test.recognized)
		}
	}
}

func TestResolveExecutionCorePackageRecognizedABIIsNativePlay(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name  string
		title string
		raw   []byte
	}{
		{name: "simple-game pong", title: "Standalone Pong", raw: libraryPackageFixture(t, "0.1.0")},
		{name: "zx81 simple-computer", title: "ZX81", raw: colecoLibraryPackageFixture(t)},
		{name: "coleco simple-computer", title: "ColecoVision", raw: colecoLibraryPackageFixture(t)},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _, entry, _ := newCoreEntryLaunchFixture(t, test.raw, test.title)
			got, err := service.SessionExecution(ctx, entry.GameID)
			if err != nil || got != ExecutionFPGANative {
				t.Fatalf("SessionExecution = %q, %v, want %q", got, err, ExecutionFPGANative)
			}
		})
	}
}

func TestResolveExecutionCorePackageUnknownABIIsDevelopment(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := catalog.OpenContext(ctx, filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	packages, err := corepackage.NewStore(filepath.Join(root, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	service := newService(Config{RequestTimeout: time.Second, UploadTimeout: time.Second}, Paths{}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.corePackages = packages
	raw := colecoLibraryPackageContractsFixture(t, "vendor.unknown", "")
	inspection, created, err := service.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil || !created {
		t.Fatalf("import: %v new=%v", err, created)
	}
	entry, err := store.CreateCoreEntry(ctx, "Unknown ABI Core", inspection.Descriptor.Core.ID, inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.SessionExecution(ctx, entry.GameID)
	if err != nil || got != ExecutionFPGADevelopment {
		t.Fatalf("SessionExecution = %q, %v, want %q", got, err, ExecutionFPGADevelopment)
	}
}

func TestLibraryCoreEntryLaunchRecordsNativePlayExecution(t *testing.T) {
	ctx := context.Background()
	raw := libraryPackageFixture(t, "0.1.0")
	service, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Standalone Pong")
	client.coreLoad = func(_ context.Context, n int64, r io.Reader) (protocol.Status, error) {
		core := "fes.pong"
		return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{
			PackageID: inspection.PackageID, Generation: 4,
			ABI: protocol.RuntimeContract{ID: inspection.Descriptor.ABI.ID, Major: 1}, BuildID: inspection.Descriptor.Build.ID, Gamepad: true,
		}}, nil
	}
	if got, err := service.SessionExecution(ctx, entry.GameID); err != nil || got != ExecutionFPGANative {
		t.Fatalf("SessionExecution = %q, %v", got, err)
	}
	if _, err := service.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	if service.activeExecution != ExecutionFPGANative {
		t.Fatalf("activeExecution = %q, want %q", service.activeExecution, ExecutionFPGANative)
	}
}

func TestDevelopmentCoreLoadKeepsFPGADevelopment(t *testing.T) {
	ctx := context.Background()
	raw := libraryPackageFixture(t, "0.1.0")
	path := filepath.Join(t.TempDir(), "core.fcore")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(path)
	if err != nil {
		t.Fatal(err)
	}
	client := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{}}
	client.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		core := "fes.pong"
		return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{
			PackageID: inspection.PackageID, Generation: 2,
			ABI: protocol.RuntimeContract{ID: inspection.Descriptor.ABI.ID, Major: 1}, BuildID: inspection.Descriptor.Build.ID, Gamepad: true,
		}}, nil
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	if _, err := service.LoadCore(ctx, int64(len(raw)), bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if service.activeExecution != ExecutionFPGADevelopment {
		t.Fatalf("activeExecution = %q, want %q", service.activeExecution, ExecutionFPGADevelopment)
	}
}

func TestStatusReconstructsRecognizedABIPackagePlayAsNative(t *testing.T) {
	packageID := strings.Repeat("f", 64)
	observed := "fes.zx81"
	client := &fakeServiceClient{statusResult: protocol.Status{
		State: protocol.StateActive, Development: true, ObservedCore: &observed,
		CorePackage: &protocol.CorePackageStatus{
			PackageID: packageID, Generation: 2,
			ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, BuildID: strings.Repeat("1", 32),
		},
	}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if service.activeExecution != ExecutionFPGANative || service.activePackageID != packageID || service.activePackageGeneration != 2 {
		t.Fatalf("execution=%q package=%q gen=%d", service.activeExecution, service.activePackageID, service.activePackageGeneration)
	}
	if status.Development != true {
		t.Fatal("target development transport flag was lost")
	}
}

func TestDevelopmentSessionStateReconstructsRecognizedABIPackagePlayAsNative(t *testing.T) {
	packageID := strings.Repeat("a", 64)
	observed := "fes.coleco"
	client := &fakeServiceClient{statusResult: protocol.Status{
		State: protocol.StateActive, Development: true, ObservedCore: &observed,
		CorePackage: &protocol.CorePackageStatus{
			PackageID: packageID, Generation: 9,
			ABI: protocol.RuntimeContract{ID: "fes.simple-computer", Major: 1}, BuildID: strings.Repeat("b", 32),
		},
	}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	development, execution, err := service.DevelopmentSessionState(context.Background())
	if err != nil || development || execution != ExecutionFPGANative {
		t.Fatalf("development=%t execution=%q err=%v", development, execution, err)
	}
	if service.activeExecution != ExecutionFPGANative || service.activePackageID != packageID {
		t.Fatalf("execution=%q package=%q", service.activeExecution, service.activePackageID)
	}
}

func TestDevelopmentSessionStateReconstructsUnknownABIPackageAsDevelopment(t *testing.T) {
	packageID := strings.Repeat("a", 64)
	observed := "vendor.core"
	client := &fakeServiceClient{statusResult: protocol.Status{
		State: protocol.StateActive, Development: true, ObservedCore: &observed,
		CorePackage: &protocol.CorePackageStatus{
			PackageID: packageID, Generation: 1,
			ABI: protocol.RuntimeContract{ID: "vendor.unknown", Major: 1}, BuildID: strings.Repeat("b", 32),
		},
	}}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

	development, execution, err := service.DevelopmentSessionState(context.Background())
	if err != nil || !development || execution != ExecutionFPGADevelopment {
		t.Fatalf("development=%t execution=%q err=%v", development, execution, err)
	}
}

func TestDevelopmentSessionStateReconstructsNonIdleDevelopmentWithoutLocalMarker(t *testing.T) {
	observed := "DEVCORE"
	for _, state := range []protocol.State{protocol.StateLaunching, protocol.StateFailed} {
		t.Run(string(state), func(t *testing.T) {
			client := &fakeServiceClient{statusResult: protocol.Status{
				State: state, Development: true, ObservedCore: &observed,
			}}
			service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)

			development, execution, err := service.DevelopmentSessionState(context.Background())
			if err != nil || !development || execution != ExecutionFPGADevelopment {
				t.Fatalf("development=%t execution=%q err=%v", development, execution, err)
			}
			if service.activeExecution != ExecutionFPGADevelopment {
				t.Fatalf("activeExecution = %q", service.activeExecution)
			}
		})
	}
}

func TestCatalogLaunchBlockedByNonIdleDevelopmentWithoutLocalMarker(t *testing.T) {
	game := serviceGame(catalog.Content{})
	observed := "DEVCORE"
	for _, state := range []protocol.State{protocol.StateLaunching, protocol.StateFailed} {
		t.Run(string(state), func(t *testing.T) {
			client := &fakeServiceClient{
				statusResult: protocol.Status{State: state, Development: true, ObservedCore: &observed},
			}
			service := newService(
				Config{
					Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: "/private/library"}},
					RequestTimeout: time.Second,
					UploadTimeout:  2 * time.Second,
				},
				Paths{Staging: "/private/staging"}, &fakeServiceCatalog{games: []catalog.Game{game}}, &fakeServiceScanner{}, &fakeServicePreparer{}, client,
			)
			development, execution, err := service.DevelopmentSessionState(context.Background())
			if err != nil || !development || execution != ExecutionFPGADevelopment {
				t.Fatalf("development=%t execution=%q err=%v", development, execution, err)
			}

			_, err = service.Launch(context.Background(), game.ID, nil)
			var apiErr *protocol.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeUnsupportedOperation {
				t.Fatalf("launch error = %v", err)
			}
			if client.stopCalls != 0 || client.nativeLaunchCalls != 0 {
				t.Fatalf("stops=%d launches=%d", client.stopCalls, client.nativeLaunchCalls)
			}
			if service.activeExecution != ExecutionFPGADevelopment {
				t.Fatalf("activeExecution = %q", service.activeExecution)
			}
		})
	}
}
