package fogcast

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/internal/meshplace"
	"github.com/DeanoC/FogCast/protocol"
)

func TestDisplayMemoryEmptyDefaults(t *testing.T) {
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	opts := service.PlaceOptions()
	if opts != (meshplace.Options{}) {
		t.Fatalf("options %+v", opts)
	}
	service.SetDisplayPreference("   ")
	if service.PlaceOptions().DisplayPreference != "" {
		t.Fatalf("blank preference stored %+v", service.PlaceOptions())
	}
}

func TestDisplayPreferenceRoundTrip(t *testing.T) {
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.SetDisplayPreference("  kit-den ")
	opts := service.PlaceOptions()
	if opts.DisplayPreference != "kit-den" || opts.LastDisplaySink != "" || opts.OverrideNodeID != "" || opts.MissingRequiredSlot {
		t.Fatalf("options %+v", opts)
	}
	service.SetDisplayPreference("")
	if service.PlaceOptions().DisplayPreference != "" {
		t.Fatalf("cleared options %+v", service.PlaceOptions())
	}
}

func TestNonPlayPathsLeaveLastSinkUnset(t *testing.T) {
	observed := "DEVCORE"
	client := &fakeServiceClient{
		developmentLoad: func(context.Context, int64, io.Reader) (protocol.Status, error) {
			return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}, nil
		},
		statusResult: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed},
	}
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, client)
	service.targets[0].TargetID = "kit-living"
	service.SetDisplayPreference("kit-den")

	if _, err := service.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	opts := service.PlaceOptions()
	if opts.LastDisplaySink != "" || opts.DisplayPreference != "kit-den" {
		t.Fatalf("non-play options %+v", opts)
	}
}

func TestStatusAdoptDoesNotRecordLastSink(t *testing.T) {
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
	service.targets[0].TargetID = "kit-living"

	if _, err := service.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if service.activeExecution != ExecutionFPGANative {
		t.Fatalf("execution = %q", service.activeExecution)
	}
	if got := service.PlaceOptions().LastDisplaySink; got != "" {
		t.Fatalf("status recorded last sink %q", got)
	}
}

func TestFPGAPlayStartRecordsBoundDisplaySink(t *testing.T) {
	ctx := context.Background()
	raw := libraryPackageFixture(t, "0.1.0")
	service, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Standalone Pong")
	client.coreLoad = pongCoreLoad(inspection)
	service.targets = []TargetConfig{
		{Name: "den", TargetID: "kit-den", Enabled: true},
		{Name: "living", TargetID: "kit-living", Enabled: true},
	}
	service.selectedTarget = "den"
	service.targetClients = map[string]serviceClient{"den": client, "living": client}
	service.SetDisplayPreference("kit-den")

	if _, err := service.LaunchOn(ctx, entry.GameID, "living", nil); err != nil {
		t.Fatal(err)
	}
	opts := service.PlaceOptions()
	if opts.DisplayPreference != "kit-den" || opts.LastDisplaySink != "kit-living" || opts.OverrideNodeID != "" || opts.MissingRequiredSlot {
		t.Fatalf("options %+v", opts)
	}

	client.statusResult = protocol.Status{State: protocol.StateIdle}
	if _, err := service.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if got := service.PlaceOptions(); got.LastDisplaySink != "kit-living" || got.DisplayPreference != "kit-den" {
		t.Fatalf("status changed options %+v", got)
	}
}

func TestFPGAPlayWithoutNodeIDLeavesLastSinkEmpty(t *testing.T) {
	ctx := context.Background()
	raw := libraryPackageFixture(t, "0.1.0")
	service, client, entry, inspection := newCoreEntryLaunchFixture(t, raw, "Standalone Pong")
	client.coreLoad = pongCoreLoad(inspection)
	service.SetDisplayPreference("kit-den")
	if _, err := service.Launch(ctx, entry.GameID, nil); err != nil {
		t.Fatal(err)
	}
	if service.activeExecution != ExecutionFPGANative {
		t.Fatalf("execution = %q", service.activeExecution)
	}
	opts := service.PlaceOptions()
	if opts.LastDisplaySink != "" || opts.DisplayPreference != "kit-den" {
		t.Fatalf("options %+v", opts)
	}
}

func TestHostOnlyPlayDoesNotRecordDisplaySink(t *testing.T) {
	dir := t.TempDir()
	cuePath := filepath.Join(dir, "game.cue")
	binPath := filepath.Join(dir, "game.bin")
	if err := os.WriteFile(cuePath, []byte("FILE \"game.bin\" BINARY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte("track-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := serviceGame(catalog.Content{})
	game.RelativePath = "game.cue"
	game.Content = nil
	game.Fingerprint = fileServiceFingerprint(t, cuePath)
	adapter := &fakePathHostExecutor{}
	service := newService(
		Config{
			Libraries:      []catalog.Root{{ID: "snes-main", System: protocol.SystemSNES, Path: dir}},
			Targets:        []TargetConfig{{Name: "den", TargetID: "kit-den", Enabled: true}},
			SelectedTarget: "den",
			RequestTimeout: time.Second,
			UploadTimeout:  2 * time.Second,
		},
		Paths{Staging: filepath.Join(t.TempDir(), "staging")},
		&fakeServiceCatalog{games: []catalog.Game{game}},
		&fakeServiceScanner{},
		&fakeServicePreparer{},
		&fakeServiceClient{},
		WithExecutionPolicy(ExecutionPolicy{
			Resolver: ExecutionResolverFunc(func(context.Context, catalog.Game) (string, error) { return ExecutionHostOnly, nil }),
			Host:     adapter,
		}),
	)
	service.SetDisplayPreference("kit-living")
	if _, err := service.Launch(context.Background(), game.ID, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if adapter.cleanup != nil {
			adapter.cleanup()
		}
	})
	if service.activeExecution != ExecutionHostOnly {
		t.Fatalf("execution = %q", service.activeExecution)
	}
	if _, err := service.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	opts := service.PlaceOptions()
	if opts.LastDisplaySink != "" || opts.DisplayPreference != "kit-living" || opts.OverrideNodeID != "" {
		t.Fatalf("options %+v", opts)
	}
}

func TestBoundPlaySinkFeedsPlace(t *testing.T) {
	service := newTestService(&fakeServiceCatalog{}, &fakeServicePreparer{}, &fakeServiceClient{})
	service.targets = []TargetConfig{
		{Name: "den", TargetID: "kit-den", Enabled: true},
		{Name: "living", TargetID: "kit-living", Enabled: true},
	}
	service.selectedTarget = "den"
	service.SetDisplayPreference("kit-den")
	service.targetMu.Lock()
	service.executionMu.Lock()
	service.activeExecution = ExecutionFPGANative
	service.activeTarget = "living"
	service.notePlayDisplaySinkLocked()
	service.executionMu.Unlock()
	service.targetMu.Unlock()

	opts := service.PlaceOptions()
	got := meshplace.Place(displayMemoryFPGAEntry(), []meshplace.Candidate{
		displayMemoryFPGACandidate("kit-living"),
		displayMemoryFPGACandidate("kit-den"),
	}, opts)
	if got.Outcome != meshplace.OutcomeSelected || got.Choice.Execute != "kit-den" {
		t.Fatalf("place %+v opts %+v", got, opts)
	}
	if opts.LastDisplaySink != "kit-living" {
		t.Fatalf("last sink %q", opts.LastDisplaySink)
	}
}

func pongCoreLoad(inspection corepackage.Inspection) func(context.Context, int64, io.Reader) (protocol.Status, error) {
	return func(context.Context, int64, io.Reader) (protocol.Status, error) {
		core := "fes.pong"
		return protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &core, CorePackage: &protocol.CorePackageStatus{
			PackageID: inspection.PackageID, Generation: 4,
			ABI: protocol.RuntimeContract{ID: inspection.Descriptor.ABI.ID, Major: 1}, BuildID: inspection.Descriptor.Build.ID, Gamepad: true,
		}}, nil
	}
}

func displayMemoryFPGAEntry() meshcontent.Entry {
	return meshcontent.Entry{
		TitleID:    "coleco-frogger",
		System:     "coleco",
		Launchable: true,
		Execute:    []meshcontent.Execute{{Kind: meshcontent.ExecuteFPGANative}},
		Slots: []meshcontent.Slot{
			meshcontent.PackageSlot(meshcontent.PackageABI{
				PackageID: strings.Repeat("ab", 32),
				ABI:       "fes.application",
				Major:     1,
			}),
			meshcontent.PrimaryMediaSlot(meshcontent.SumSHA256([]byte("cart"))),
		},
	}
}

func displayMemoryFPGACandidate(id string) meshplace.Candidate {
	return meshplace.Candidate{
		NodeID:      id,
		MeshMajorOK: true,
		Execute:     []string{meshcontent.ExecuteFPGANative},
		DisplaySink: true,
		InputSource: true,
		ABIs:        []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}},
	}
}
