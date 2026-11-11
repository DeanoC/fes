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
	"github.com/DeanoC/FogCast/internal/meshcontent"
	"github.com/DeanoC/FogCast/protocol"
)

const twoNodeSoftwareGameID = "two-node-software"

func launchTwoNodeKit(t *testing.T, f *twoNodeFixture) {
	t.Helper()
	if _, err := f.service.LaunchOn(context.Background(), f.kitGame.GameID, "kit", nil); err != nil {
		t.Fatalf("LaunchOn kit: %v", err)
	}
}

type twoNodeFixture struct {
	service *Service
	kit     *packageLibraryClient
	kitGame catalog.CoreEntry
}

func newTwoNodeFixture(t *testing.T) *twoNodeFixture {
	t.Helper()
	ctx := context.Background()
	rootDir := t.TempDir()
	libraryDir := filepath.Join(rootDir, "library")
	if err := os.MkdirAll(libraryDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libraryDir, "software.sfc"), []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := catalog.Root{ID: "two-node-library", System: protocol.SystemSNES, Path: libraryDir}
	store, err := catalog.OpenContext(ctx, filepath.Join(rootDir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scan, err := store.BeginRootScan(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scan.Observe(ctx, catalog.Candidate{
		ID: twoNodeSoftwareGameID, System: protocol.SystemSNES, RelativePath: "software.sfc", Title: "Software title",
		Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable,
		Fingerprint: catalog.Fingerprint{SourceSize: 3, ModifiedNS: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := scan.Complete(ctx); err != nil {
		t.Fatal(err)
	}

	baseClient := &packageLibraryClient{fakeServiceClient: &fakeServiceClient{
		statusResult: protocol.Status{State: protocol.StateIdle},
		stopResult:   protocol.Status{State: protocol.StateIdle},
	}}
	kitConfig := TargetConfig{Name: "kit", TargetID: "kit", Enabled: true}
	service := newService(Config{Libraries: []catalog.Root{root}, Targets: []TargetConfig{kitConfig}, SelectedTarget: "kit", RequestTimeout: time.Second, UploadTimeout: 2 * time.Second}, Paths{Staging: filepath.Join(rootDir, "staging")}, store, &fakeServiceScanner{}, &fakeServicePreparer{}, baseClient)
	// The constructor normally dials configured clients; attach the fake kit
	// here because this test must exercise the service without a real target.
	service.targetClients["kit"] = baseClient
	service.pairedTargetClients["kit"] = baseClient
	service.corePackages, err = corepackage.NewStore(filepath.Join(rootDir, "packages"))
	if err != nil {
		t.Fatal(err)
	}
	raw := libraryPackageFixture(t, "0.1.0")
	inspection, created, err := service.ImportCorePackage(ctx, int64(len(raw)), bytes.NewReader(raw))
	if err != nil || !created {
		t.Fatalf("ImportCorePackage: created=%v err=%v", created, err)
	}
	baseClient.inspection = protocol.CoreInspection{PackageID: inspection.PackageID, Descriptor: inspection.Descriptor, Compatible: true}
	kitGame, err := service.CreateCoreEntry(ctx, "Kit Pong", inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	active := coreEntryActiveStatus(inspection, 9, false)
	baseClient.coreLoad = func(context.Context, int64, io.Reader) (protocol.Status, error) {
		baseClient.statusResult = active
		return active, nil
	}
	return &twoNodeFixture{service: service, kit: baseClient, kitGame: kitGame}
}

func TestRawCatalogLaunchDoesNotReplaceKitPlay(t *testing.T) {
	f := newTwoNodeFixture(t)
	launchTwoNodeKit(t, f)
	_, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil)
	assertServiceErrorCode(t, err, protocol.CodeUnsupportedOperation)
	plays := f.service.PlaySessions()
	if f.kit.coreCalls != 1 || f.kit.stopCalls != 0 || len(plays) != 1 || plays[0].Target != "kit" || plays[0].GameID != f.kitGame.GameID {
		t.Fatalf("kit calls=%d/%d plays=%+v", f.kit.coreCalls, f.kit.stopCalls, plays)
	}
	status, err := f.service.StatusTarget(context.Background(), "kit")
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("kit status=%+v err=%v", status, err)
	}
}

func TestPackageScopedStop(t *testing.T) {
	f := newTwoNodeFixture(t)
	launchTwoNodeKit(t, f)
	if _, err := f.service.StopTarget(context.Background(), "missing"); err == nil {
		t.Fatal("StopTarget of unknown target succeeded")
	}
	if f.kit.stopCalls != 0 {
		t.Fatalf("unknown target stopped kit %d times", f.kit.stopCalls)
	}
	if _, err := f.service.StopTarget(context.Background(), "kit"); err != nil {
		t.Fatalf("StopTarget kit: %v", err)
	}
	if f.kit.stopCalls != 1 {
		t.Fatalf("kit stops=%d", f.kit.stopCalls)
	}
}

func TestPackageKitStatusRecoversAfterDisconnect(t *testing.T) {
	f := newTwoNodeFixture(t)
	launchTwoNodeKit(t, f)
	f.kit.statusErr = errors.New("target transport unavailable")
	_, err := f.service.StatusTarget(context.Background(), "kit")
	assertServiceErrorCode(t, err, protocol.CodeMiSTerUnavailable)
	f.kit.statusErr = nil
	status, err := f.service.StatusTarget(context.Background(), "kit")
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("restored kit status=%+v err=%v", status, err)
	}
	if f.kit.coreCalls != 1 || f.kit.stopCalls != 0 {
		t.Fatalf("kit calls=%d/%d", f.kit.coreCalls, f.kit.stopCalls)
	}
}

func TestPackageKitBusyLeaseRefusesLaunch(t *testing.T) {
	f := newTwoNodeFixture(t)
	f.service.connection = TargetConnection{State: "busy", Owner: "foreign-shell"}
	_, err := f.service.LaunchOn(context.Background(), f.kitGame.GameID, "kit", nil)
	assertServiceErrorCode(t, err, protocol.CodeKitLeaseDenied)
	if f.kit.coreCalls != 0 || f.kit.stopCalls != 0 {
		t.Fatalf("foreign kit contacted: core loads=%d stops=%d", f.kit.coreCalls, f.kit.stopCalls)
	}
}

// A raw row must be refused before a synthetic mesh projection can pull it.
func TestRawCatalogLaunchDoesNotStartMeshEnsure(t *testing.T) {
	f := newTwoNodeFixture(t)
	entry, media := fpgaMeshEntry(twoNodeSoftwareGameID)
	exec := &meshLaunchExecutor{node: "kit", sources: map[string]bool{media.String(): true}, abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}}}
	f.service.SetMeshExecuteSession(MeshExecuteSession{BoundNode: "kit", Executor: exec, Entry: func(id string) (meshcontent.Entry, bool) { return entry, id == twoNodeSoftwareGameID }})
	_, err := f.service.Launch(context.Background(), twoNodeSoftwareGameID, nil)
	assertServiceErrorCode(t, err, protocol.CodeUnsupportedOperation)
	if len(exec.pulls) != 0 || len(exec.links) != 0 || f.kit.coreCalls != 0 || f.kit.stopCalls != 0 {
		t.Fatalf("raw launch mutated state: pulls=%v links=%v loads=%d stops=%d", exec.pulls, exec.links, f.kit.coreCalls, f.kit.stopCalls)
	}
}

type failingAdmissionCatalog struct {
	*fakeServiceCatalog
	coreEntryCatalog
	cancel context.CancelFunc
}

func (c *failingAdmissionCatalog) CoreEntry(context.Context, string) (catalog.CoreEntry, error) {
	c.cancel()
	return catalog.CoreEntry{}, errors.New("catalog read interrupted")
}

func TestCanceledCoreEntryAdmissionDoesNotStartMeshEnsure(t *testing.T) {
	entry, media := fpgaMeshEntry("coleco-frogger")
	exec := &meshLaunchExecutor{node: "node-a", sources: map[string]bool{media.String(): true}, abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}}}
	service := meshTargetService(exec, "node-a", entry)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.catalog = &failingAdmissionCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, cancel: cancel}
	_, err := service.Launch(ctx, entry.TitleID, nil)
	if !errors.Is(err, context.Canceled) || len(exec.pulls) != 0 || len(exec.links) != 0 {
		t.Fatalf("err=%v pulls=%v links=%v", err, exec.pulls, exec.links)
	}
}

type malformedAdmissionCatalog struct {
	*fakeServiceCatalog
	coreEntryCatalog
	entry catalog.CoreEntry
}

func (c *malformedAdmissionCatalog) CoreEntry(context.Context, string) (catalog.CoreEntry, error) {
	return c.entry, nil
}

func TestMalformedCoreEntryDoesNotStartMeshEnsure(t *testing.T) {
	entry, media := fpgaMeshEntry("coleco-frogger")
	for name, malformed := range map[string]catalog.CoreEntry{
		"package digest":  {GameID: entry.TitleID, Title: "Frogger", CoreID: "fes.coleco", PackageID: "invalid"},
		"media selection": {GameID: entry.TitleID, Title: "Frogger", CoreID: "fes.coleco", PackageID: strings.Repeat("a", 64), MediaRole: "blob", MediaID: "invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			exec := &meshLaunchExecutor{node: "node-a", sources: map[string]bool{media.String(): true}, abis: []meshcontent.EligibleABI{{ID: "fes.application", Major: 1}}}
			service := meshTargetService(exec, "node-a", entry)
			service.catalog = &malformedAdmissionCatalog{fakeServiceCatalog: &fakeServiceCatalog{}, entry: malformed}
			_, err := service.Launch(context.Background(), entry.TitleID, nil)
			assertServiceErrorCode(t, err, protocol.CodeInternal)
			if len(exec.pulls) != 0 || len(exec.links) != 0 {
				t.Fatalf("malformed entry started mesh work: pulls=%v links=%v", exec.pulls, exec.links)
			}
		})
	}
}
