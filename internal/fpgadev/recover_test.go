//go:build fpgadev

package fpgadev

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
	"golang.org/x/sys/unix"
)

func TestInitializeOwnerAdoptsOnlyHealthyUninitializedMain(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	owner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	manager := &InstallManager{
		Journal:       NewInstallJournal(filepath.Join(root, "journal.json"), uid),
		InstallLocker: hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid),
		OwnerStore:    owner,
		OwnerLocker:   hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid),
		BootID:        func() (string, error) { return testBootID, nil },
		MainReadiness: func(context.Context) error { return nil },
	}
	if err := manager.InitializeOwner(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, exists, err := owner.Load()
	if err != nil || !exists {
		t.Fatalf("owner load exists=%v err=%v", exists, err)
	}
	if record.State != hardwareowner.StateNormalMain || record.ActiveOwner != hardwareowner.OwnerCompatMain || record.ActiveGeneration != 1 {
		t.Fatalf("owner=%#v", record)
	}
}

func TestInstallManagerRefusesUnprovenSourceTransition(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	source := SourceRecord{Path: "/etc/fogcast-start", Kind: "regular", Mode: 0o755, SHA256: strings.Repeat("a", 64), BackupPath: "/var/lib/fogcast/backup", BackupSHA256: strings.Repeat("b", 64), DisabledState: "approved_trampoline", DisabledSHA256: strings.Repeat("c", 64)}
	manager := &InstallManager{Journal: NewInstallJournal(filepath.Join(root, "journal.json"), uid), InstallLocker: hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid), OwnerStore: hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid), OwnerLocker: hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid), BootID: func() (string, error) { return testBootID, nil }, PackageSHA256: strings.Repeat("d", 64), PreviousConfigSHA256: strings.Repeat("e", 64), Inventory: InventoryV1{Schema: 1, MainExecutable: PathExpectation{Path: "/usr/bin/Main_MiSTer", Kind: "regular", SHA256: strings.Repeat("f", 64)}, MainFIFO: PathExpectation{Path: "/dev/MiSTer_cmd", Kind: "fifo"}, StartSources: []SourceRecord{source}}, Sources: []SourceRecord{source}, MainReadiness: func(context.Context) error { return nil }, StopAgent: func(context.Context) error { return nil }, ProveAgentAbsent: func(context.Context) error { return nil }, InstallTrampoline: func(context.Context) error { return nil }, DisableSources: func(context.Context) error { return nil }, InstallSupervisor: func(context.Context) error { return nil }}
	err := manager.Install(context.Background(), "")
	if err == nil {
		t.Fatal("install without a preexisting owner unexpectedly succeeded")
	}
	record, exists, loadErr := manager.Journal.Load()
	if loadErr != nil || !exists || record.State != InstallStatePrepared {
		t.Fatalf("journal adoption record=%#v exists=%v err=%v", record, exists, loadErr)
	}
}

func TestFailStopInstallPublishesTrampolineBeforePrepared(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	for path, data := range map[string][]byte{dispatcher: []byte("dispatcher"), legacy: []byte("legacy")} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := CaptureStartSourceInventory([]string{dispatcher, legacy}, filepath.Join(root, "backups"), dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(mainPath, []byte("main"), 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	main, err := inspectPathExpectation(mainPath, true)
	if err != nil {
		t.Fatal(err)
	}
	fifo, err := inspectPathExpectation(fifoPath, true)
	if err != nil {
		t.Fatal(err)
	}
	owner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	if err := owner.Replace(hardwareowner.Record{
		Schema: 1, State: hardwareowner.StateNormalMain, BootID: testBootID,
		GenerationHighWater: 1, ActiveSession: strings.Repeat("a", 32), ActiveGeneration: 1,
		ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain,
		ActiveLeases: hardwareowner.NormalLeases(), CandidateMode: hardwareowner.ModeNone,
		CandidateOwner: hardwareowner.OwnerNone, QuiescingOwner: hardwareowner.OwnerNone,
		RequestedResources: []string{}, FirstFailure: "",
	}); err != nil {
		t.Fatal(err)
	}
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	manager := &InstallManager{
		Journal: journal, InstallLocker: NewInstallLocker(filepath.Join(root, "install.lock"), uid),
		OwnerStore: owner, OwnerLocker: NewInstallLocker(filepath.Join(root, "owner.lock"), uid),
		BootID:        func() (string, error) { return testBootID, nil },
		PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64),
		Inventory: InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources}, Sources: sources,
		MainReadiness: func(context.Context) error { return nil },
		StopAgent:     func(context.Context) error { return nil }, ProveAgentAbsent: func(context.Context) error { return nil },
	}
	manager.InstallTrampoline = func(context.Context) error {
		_, exists, loadErr := journal.Load()
		if loadErr != nil {
			return loadErr
		}
		if exists {
			return errors.New("prepared journal was published before trampoline")
		}
		return nil
	}
	manager.DisableSources = func(context.Context) error { return nil }
	manager.InstallSupervisor = func(context.Context) error { return nil }
	if err := manager.Install(context.Background(), ""); err != nil && !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v", err)
	}
}

func TestFailStopInstallBeforeTrampolinePublicationLeavesOriginalSources(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	crash := errors.New("simulated crash before trampoline publication")
	fixture.manager.FailureHook = func(event string) error {
		if event == "before-trampoline-publication" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
		t.Fatalf("install error=%v, want trampoline seam", err)
	}
	if _, exists, err := fixture.manager.Journal.Load(); err != nil || exists {
		t.Fatalf("journal after pre-publication crash exists=%v err=%v", exists, err)
	}
	for path, want := range map[string][]byte{
		fixture.dispatcher: fixture.dispatcherOriginal,
		fixture.legacy:     fixture.legacyOriginal,
		fixture.legacy2:    fixture.legacy2Original,
	} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("source %s changed before trampoline publication: %q err=%v", path, got, readErr)
		}
	}
}

func TestFailStopInstallAuthorityIsDurableAtTrampolineBoundary(t *testing.T) {
	fixture, options := newProtectedAuthorityFixture(t)
	crash := errors.New("simulated crash at trampoline boundary")
	fixture.manager.FailureHook = func(event string) error {
		if event == "before-trampoline-publication" {
			if _, err := os.Stat(PreJournalRecoveryAuthorityPath(options.JournalPath)); err != nil {
				return fmt.Errorf("pre-journal authority was not durable: %w", err)
			}
			return crash
		}
		return nil
	}
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
		t.Fatalf("install error=%v, want trampoline boundary seam", err)
	}
	if got, err := os.ReadFile(fixture.dispatcher); err != nil || !bytes.Equal(got, fixture.dispatcherOriginal) {
		t.Fatalf("dispatcher changed before publication: %q err=%v", got, err)
	}
}

func TestFailStopRecoverAbsentChainsOriginalWithoutStartingChildren(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uint32(os.Getuid()))
	lock := NewInstallLocker(filepath.Join(root, "install.lock"), uint32(os.Getuid()))
	started := 0
	chained := 0
	manager := &InstallManager{Journal: journal, InstallLocker: lock, ChainOriginal: func(context.Context) error { chained++; return nil }, ExecSupervisor: func(context.Context, int) error { started++; return nil }}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatalf("absent recovery error=%v", err)
	}
	if chained != 1 || started != 0 {
		t.Fatalf("chain=%d supervisor=%d", chained, started)
	}
}

func TestFailStopRecoverAbsentRequiresOriginalChain(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	manager := &InstallManager{
		Journal:       NewInstallJournal(filepath.Join(root, "journal.json"), uint32(os.Getuid())),
		InstallLocker: NewInstallLocker(filepath.Join(root, "install.lock"), uint32(os.Getuid())),
	}
	if err := manager.Recover(context.Background()); !errors.Is(err, ErrRunnerConfiguration) {
		t.Fatalf("absent recovery error=%v, want fail-closed chain configuration", err)
	}
}

func TestFailStopChainOriginalVerifiesDurableDispatcherBackup(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	original := []byte("original-dispatcher\n")
	if err := os.WriteFile(dispatcher, original, 0o755); err != nil {
		t.Fatal(err)
	}
	sources, err := CaptureStartSourceInventory([]string{dispatcher}, filepath.Join(root, "backups"), dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewSourceMutationPlan(sources, filepath.Join(root, "backups"))
	plan.ExpectedUID = uid
	if err := plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	called := false
	chain := chainOriginalFromBackupWithExec(dispatcher, sources[0], uid, func(path string, argv, env []string) error {
		called = true
		if path != dispatcher || len(argv) != 1 || argv[0] != dispatcher || len(env) == 0 {
			return errors.New("chain exec arguments were not exact")
		}
		return nil
	})
	if err := chain(context.Background()); err != nil {
		t.Fatalf("chain error=%v", err)
	}
	if !called {
		t.Fatal("original dispatcher was not chained")
	}
	if err := os.WriteFile(sources[0].BackupPath, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	called = false
	if err := chain(context.Background()); err == nil {
		t.Fatal("tampered durable backup unexpectedly chained")
	}
	if called {
		t.Fatal("tampered durable backup reached exec")
	}
}

func TestFailStopRestoredRecoveryRequiresConfiguredChain(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	fixture.manager.RequestReboot = func(context.Context) error { return nil }
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v", err)
	}
	if err := fixture.plan.DisableSupervisor(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.RestoreSources(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists {
		t.Fatalf("journal=%#v exists=%v err=%v", record, exists, err)
	}
	record.State = InstallStateUninstalling
	if err := fixture.manager.Journal.Replace(record); err != nil {
		t.Fatal(err)
	}
	record.State = InstallStateRestored
	if err := fixture.manager.Journal.Replace(record); err != nil {
		t.Fatal(err)
	}
	fixture.manager.ChainOriginal = nil
	if err := fixture.manager.Recover(context.Background()); !errors.Is(err, ErrRunnerConfiguration) {
		t.Fatalf("restored recovery error=%v, want fail-closed chain configuration", err)
	}
	if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
		t.Fatalf("stage should be removed before the missing chain configuration is reported: %v", err)
	}
}

func TestFailStopUninstallMakesRestoredDurableBeforeDispatcher(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	for path, data := range map[string][]byte{dispatcher: []byte("dispatcher"), legacy: []byte("legacy")} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := CaptureStartSourceInventory([]string{dispatcher, legacy}, filepath.Join(root, "backups"), dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewSourceMutationPlan(sources, filepath.Join(root, "backups"))
	plan.Supervisor = filepath.Join(root, "supervisor")
	plan.ExpectedUID = uid
	if err := plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	mainPath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(mainPath, []byte("main"), 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	main, err := inspectPathExpectation(mainPath, true)
	if err != nil {
		t.Fatal(err)
	}
	fifo, err := inspectPathExpectation(fifoPath, true)
	if err != nil {
		t.Fatal(err)
	}
	record := InstallJournalRecord{Schema: 1, State: InstallStateTerminal, InstallBootID: testBootID, PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64), Inventory: InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources}, Sources: sources}
	if err := replaceInstallJournalForTest(journal, record); err != nil {
		t.Fatal(err)
	}
	owner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	if err := owner.Replace(hardwareowner.Record{Schema: 1, State: hardwareowner.StateNormalMain, BootID: testBootID, GenerationHighWater: 1, ActiveSession: strings.Repeat("a", 32), ActiveGeneration: 1, ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain, ActiveLeases: hardwareowner.NormalLeases(), CandidateMode: hardwareowner.ModeNone, CandidateOwner: hardwareowner.OwnerNone, QuiescingOwner: hardwareowner.OwnerNone, RequestedResources: []string{}, FirstFailure: ""}); err != nil {
		t.Fatal(err)
	}
	manager := &InstallManager{Journal: journal, InstallLocker: NewInstallLocker(filepath.Join(root, "install.lock"), uid), OwnerStore: owner, OwnerLocker: NewInstallLocker(filepath.Join(root, "owner.lock"), uid), BootID: func() (string, error) { return testBootID, nil }, SourcePlan: plan, Sources: sources, Inventory: record.Inventory, PackageSHA256: record.PackageSHA256, PreviousConfigSHA256: record.PreviousConfigSHA256}
	manager.DisableSupervisor = func(context.Context) error { return nil }
	manager.DisableSources = func(context.Context) error { return nil }
	manager.RestoreSources = func(context.Context) error { return nil }
	manager.RemoveDispatcher = func(context.Context) error {
		current, exists, loadErr := journal.Load()
		if loadErr != nil {
			return loadErr
		}
		if !exists || current.State != InstallStateRestored {
			return errors.New("dispatcher restored before durable restored journal")
		}
		return nil
	}
	manager.ChainOriginal = func(context.Context) error { return nil }
	if err := manager.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall error=%v", err)
	}
}

func TestFailStopInstallDurablyStagesPackageBeforeFixedPathMutation(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	var rebootCalls int
	fixture.manager.RequestReboot = func(context.Context) error { rebootCalls++; return nil }
	err := fixture.manager.Install(context.Background(), fixture.packageRoot)
	if !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v, want reboot request", err)
	}
	if rebootCalls != 1 {
		t.Fatalf("reboot calls=%d, want 1", rebootCalls)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateTerminal {
		t.Fatalf("journal=%#v exists=%v err=%v", record, exists, err)
	}
	if _, err := os.Stat(fixture.stagePath); err != nil {
		t.Fatalf("persistent stage missing: %v", err)
	}
	trampoline, err := os.ReadFile(fixture.dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(trampoline, []byte(fixture.stagePath)) || !bytes.Contains(trampoline, []byte(fixture.helperHash)) {
		t.Fatalf("trampoline is not bound to persistent helper: %q", trampoline)
	}
	for member, target := range fixture.manager.FixedMembers {
		want := fixture.packageMembers[member]
		got, readErr := os.ReadFile(target)
		if readErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("fixed member %s=%q err=%v want=%q", member, got, readErr, want)
		}
	}
	if _, err := os.Lstat(fixture.legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy source remains err=%v", err)
	}
}

func TestFailStopPersistentStageUsesNonExecutable0600Members(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	pkg, err := ValidateInstallPackage(fixture.packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyInstallPackage(pkg, fixture.stagePath, fixture.uid); err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{"manifest.sha256", "deploy/fpgadev/agent.toml.example"} {
		info, statErr := os.Stat(filepath.Join(fixture.stagePath, filepath.FromSlash(member)))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("persistent non-executable %s mode=%o want 600", member, got)
		}
	}
	for _, member := range []string{"bin/mister-agent", "bin/mister-fpga-dev", "bin/fogcast-dev-supervisor", "deploy/fpgadev/start.sh"} {
		info, statErr := os.Stat(filepath.Join(fixture.stagePath, filepath.FromSlash(member)))
		if statErr != nil {
			t.Fatal(statErr)
		}
		if got := info.Mode().Perm(); got != 0o755 {
			t.Fatalf("persistent executable %s mode=%o want 755", member, got)
		}
	}
}

func TestFailStopPersistentStageRetryResyncsMatchingMembers(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	pkg, err := ValidateInstallPackage(fixture.packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyInstallPackage(pkg, fixture.stagePath, fixture.uid); err != nil {
		t.Fatal(err)
	}
	calls := make(map[string]int)
	if err := copyInstallPackageWithSync(pkg, fixture.stagePath, fixture.uid, func(path string) error {
		calls[path]++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := len(installPackageMembers) + 1
	if len(calls) != want {
		t.Fatalf("matching retry sync paths=%d want %d (%#v)", len(calls), want, calls)
	}
	for _, member := range append(append([]string(nil), installPackageMembers...), "manifest.sha256") {
		path := filepath.Join(fixture.stagePath, filepath.FromSlash(member))
		if calls[path] != 1 {
			t.Fatalf("matching retry sync %s calls=%d want 1", member, calls[path])
		}
	}
}

func TestFailStopRecoveryDiagnosticStaysOutsideMissingStage(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	manager := &InstallManager{Journal: NewInstallJournal(filepath.Join(root, "journal.json"), uint32(os.Getuid()))}
	stage := filepath.Join(root, "staging", strings.Repeat("a", 64))
	want := filepath.Join(root, "recovery.diagnostic")
	if got := manager.diagnosticPath(stage); got != want {
		t.Fatalf("diagnostic path=%q want %q", got, want)
	}
}

func TestFailStopMissingPersistentStageFencesWithStableDiagnostic(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	if err := fixture.plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := fixture.manager.journalRecord(InstallStatePrepared)
	if err := fixture.manager.Journal.Replace(prepared); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fixture.manager.StageRoot); err != nil {
		t.Fatal(err)
	}
	fixture.manager.PackageRoot = ""
	fixture.manager.StagePath = ""
	fixture.manager.StageManifestSHA256 = ""
	fixture.manager.ExecSupervisor = func(context.Context, int) error {
		return errors.New("supervisor must not start while stage is missing")
	}
	if err := fixture.manager.Recover(context.Background()); err == nil {
		t.Fatal("missing stage unexpectedly recovered")
	}
	diagnostic := filepath.Join(filepath.Dir(fixture.manager.Journal.Path), "recovery.diagnostic")
	info, err := os.Stat(diagnostic)
	if err != nil {
		t.Fatalf("stable recovery diagnostic missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("diagnostic mode=%o want 600", info.Mode().Perm())
	}
	if _, err := os.Lstat(fixture.manager.StageRoot); !os.IsNotExist(err) {
		t.Fatalf("missing stage was recreated: %v", err)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStatePrepared {
		t.Fatalf("fenced journal=%#v exists=%v err=%v", record, exists, err)
	}
}

func TestFailStopInstallRetryBindsDynamicTrampolineAfterPublicationCrash(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	crash := errors.New("simulated crash after trampoline publication")
	fixture.manager.FailureHook = func(event string) error {
		if event == "after-trampoline-publication" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
		t.Fatalf("install error=%v, want trampoline publication seam", err)
	}
	if _, exists, err := fixture.manager.Journal.Load(); err != nil || exists {
		t.Fatalf("journal after publication crash exists=%v err=%v", exists, err)
	}

	// A fresh manager has no dynamic bytes in memory. The package root and
	// stage are revalidated first, allowing Prepare to recognize the already
	// published trampoline and finish without overwriting an unknown source.
	plan := NewSourceMutationPlan(fixture.manager.SourcePlan.Sources, filepath.Join(fixture.root, "backups"))
	plan.Supervisor = fixture.supervisor
	manager := &InstallManager{
		Journal:       fixture.manager.Journal,
		InstallLocker: NewInstallLocker(filepath.Join(fixture.root, "install.lock-retry"), fixture.uid),
		OwnerStore:    fixture.manager.OwnerStore,
		OwnerLocker:   NewInstallLocker(filepath.Join(fixture.root, "owner.lock-retry"), fixture.uid),
		PackageSHA256: fixture.manager.PackageSHA256, PreviousConfigSHA256: fixture.manager.PreviousConfigSHA256,
		Inventory: fixture.manager.Inventory, Sources: fixture.manager.Sources, SourcePlan: plan,
		StageRoot: fixture.manager.StageRoot, FixedMembers: fixture.manager.FixedMembers,
		BootID:        func() (string, error) { return testBootID, nil },
		MainReadiness: func(context.Context) error { return nil },
		StopAgent:     func(context.Context) error { return nil }, ProveAgentAbsent: func(context.Context) error { return nil },
		RequestReboot: func(context.Context) error { return nil },
	}
	if err := manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("retry install error=%v, want reboot request", err)
	}
	record, exists, err := manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateTerminal {
		t.Fatalf("retry journal=%#v exists=%v err=%v", record, exists, err)
	}
}

func TestFailStopFreshProductionRecoveryUsesPreJournalAuthorityAfterPublication(t *testing.T) {
	fixture, options := newProtectedAuthorityFixture(t)
	crash := errors.New("simulated crash after trampoline publication")
	fixture.manager.FailureHook = func(event string) error {
		if event == "after-trampoline-publication" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
		t.Fatalf("install error=%v, want publication seam", err)
	}
	if _, exists, err := fixture.manager.Journal.Load(); err != nil || exists {
		t.Fatalf("journal after publication crash exists=%v err=%v", exists, err)
	}
	authorityPath := PreJournalRecoveryAuthorityPath(options.JournalPath)
	info, err := os.Stat(authorityPath)
	if err != nil {
		t.Fatalf("pre-journal authority missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("pre-journal authority mode=%o want 600", info.Mode().Perm())
	}

	// This is a fresh production composition: it has no source inventory or
	// trampoline bytes retained from the installing manager.
	fresh, err := NewProtectedInstallManager(options)
	if err != nil {
		t.Fatalf("fresh production composition: %v", err)
	}
	if fresh.SourcePlan == nil || len(fresh.SourcePlan.Sources) != 0 || len(fresh.Sources) != 0 || len(fresh.Inventory.StartSources) != 0 {
		t.Fatalf("fresh composition retained live source inventory: plan=%#v sources=%#v inventory=%#v", fresh.SourcePlan, fresh.Sources, fresh.Inventory.StartSources)
	}
	started := 0
	fresh.ExecSupervisor = func(context.Context, int) error {
		started++
		return errors.New("supervisor must not start for absent journal recovery")
	}
	chained := false
	fresh.ChainOriginal = func(context.Context) error {
		chained = true
		got, readErr := os.ReadFile(fixture.dispatcher)
		if readErr != nil || !bytes.Equal(got, fixture.dispatcherOriginal) {
			return fmt.Errorf("dispatcher was not restored before chain: %q (%v)", got, readErr)
		}
		return nil
	}
	if err := fresh.Recover(context.Background()); err != nil {
		t.Fatalf("fresh recovery error=%v", err)
	}
	if !chained || started != 0 {
		t.Fatalf("chain=%v supervisor starts=%d", chained, started)
	}
	if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
		t.Fatalf("persistent stage after pre-journal recovery=%v", err)
	}
	if _, err := os.Lstat(authorityPath); !os.IsNotExist(err) {
		t.Fatalf("pre-journal authority after recovery=%v", err)
	}
}

func TestFailStopFreshPreJournalRecoveryMissingStageFencesWithStableDiagnostic(t *testing.T) {
	fixture, options := newProtectedAuthorityFixture(t)
	crash := errors.New("simulated crash after trampoline publication")
	fixture.manager.FailureHook = func(event string) error {
		if event == "after-trampoline-publication" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
		t.Fatalf("install error=%v", err)
	}
	if err := os.RemoveAll(fixture.stagePath); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewProtectedInstallManager(options)
	if err != nil {
		t.Fatalf("fresh production composition: %v", err)
	}
	chained := 0
	fresh.ChainOriginal = func(context.Context) error { chained++; return nil }
	if err := fresh.Recover(context.Background()); err == nil {
		t.Fatal("missing pre-journal stage unexpectedly recovered")
	}
	if chained != 0 {
		t.Fatalf("missing stage reached original chain: %d", chained)
	}
	diagnostic := filepath.Join(fixture.root, "recovery.diagnostic")
	info, err := os.Stat(diagnostic)
	if err != nil {
		t.Fatalf("stable pre-journal diagnostic missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("pre-journal diagnostic mode=%o want 600", info.Mode().Perm())
	}
	if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
		t.Fatalf("missing pre-journal stage was recreated: %v", err)
	}
	if _, err := os.Stat(PreJournalRecoveryAuthorityPath(options.JournalPath)); err != nil {
		t.Fatalf("pre-journal authority was lost after fence: %v", err)
	}
	if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
		t.Fatalf("dispatcher escaped trampoline while stage was missing: %q err=%v", got, readErr)
	}
}

func TestFailStopFreshPreJournalRecoveryTamperedBackupFencesBeforeChain(t *testing.T) {
	fixture, options := newProtectedAuthorityFixture(t)
	crash := errors.New("simulated crash after trampoline publication")
	fixture.manager.FailureHook = func(event string) error {
		if event == "after-trampoline-publication" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
		t.Fatalf("install error=%v", err)
	}
	path := filepath.Join(fixture.root, "backups", "00-dispatcher")
	if err := os.WriteFile(path, []byte("tampered dispatcher\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewProtectedInstallManager(options)
	if err != nil {
		t.Fatalf("fresh production composition: %v", err)
	}
	chained := 0
	fresh.ChainOriginal = func(context.Context) error { chained++; return nil }
	if err := fresh.Recover(context.Background()); err == nil {
		t.Fatal("tampered pre-journal backup unexpectedly recovered")
	}
	if chained != 0 {
		t.Fatalf("tampered backup reached original chain: %d", chained)
	}
	if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
		t.Fatalf("dispatcher escaped trampoline after backup fence: %q err=%v", got, readErr)
	}
	if _, err := os.Stat(filepath.Join(fixture.root, "recovery.diagnostic")); err != nil {
		t.Fatalf("tampered backup diagnostic missing: %v", err)
	}
}

func TestFailStopRestoredCleanupCompletesBeforeOriginalChain(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	fixture.manager.RequestReboot = func(context.Context) error { return nil }
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v", err)
	}
	panicValue := any(nil)
	fixture.manager.ChainOriginal = func(context.Context) error {
		if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
			t.Fatalf("chain observed persistent stage=%v", err)
		}
		got, err := os.ReadFile(fixture.dispatcher)
		if err != nil || !bytes.Equal(got, fixture.dispatcherOriginal) {
			t.Fatalf("chain observed dispatcher=%q err=%v", got, err)
		}
		panic("chain callback reached after verified restored cleanup")
	}
	func() {
		defer func() { panicValue = recover() }()
		_ = fixture.manager.Uninstall(context.Background())
	}()
	if panicValue == nil {
		t.Fatal("original chain callback did not run after restored cleanup")
	}
	if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
		t.Fatalf("persistent stage after chain panic=%v", err)
	}
}

func TestFailStopRestoredCleanupFailureLeavesDurableRetry(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	fixture.manager.RequestReboot = func(context.Context) error { return nil }
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v", err)
	}
	cleanupErr := errors.New("simulated stage cleanup failure")
	chained := 0
	fixture.manager.ChainOriginal = func(context.Context) error { chained++; return nil }
	fixture.manager.FailureHook = func(event string) error {
		if event == "before-stage-removal" {
			return cleanupErr
		}
		return nil
	}
	if err := fixture.manager.Uninstall(context.Background()); !errors.Is(err, cleanupErr) {
		t.Fatalf("uninstall error=%v, want cleanup failure", err)
	}
	if chained != 0 {
		t.Fatalf("chain called despite cleanup failure: %d", chained)
	}
	restored, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || restored.State != InstallStateRestored {
		t.Fatalf("restored checkpoint=%#v exists=%v err=%v", restored, exists, err)
	}
	if _, err := os.Lstat(fixture.stagePath); err != nil {
		t.Fatalf("stage after cleanup failure=%v, want retained for retry", err)
	}
	if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
		t.Fatalf("dispatcher after cleanup failure=%q err=%v, want retry trampoline", got, readErr)
	}
	fixture.manager.FailureHook = nil
	if err := fixture.manager.Uninstall(context.Background()); err != nil {
		t.Fatalf("restored cleanup retry=%v", err)
	}
	if chained != 1 {
		t.Fatalf("chain calls after retry=%d want 1", chained)
	}
	if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
		t.Fatalf("stage after cleanup retry=%v", err)
	}
}

func TestFailStopFreshRestoredCleanupFailureRearmsDynamicTrampoline(t *testing.T) {
	fixture, options := newProtectedAuthorityFixture(t)
	fixture.manager.RequestReboot = func(context.Context) error { return nil }
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v", err)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateTerminal {
		t.Fatalf("terminal journal=%#v exists=%v err=%v", record, exists, err)
	}
	record.State = InstallStateUninstalling
	if err := fixture.manager.Journal.Replace(record); err != nil {
		t.Fatal(err)
	}
	record.State = InstallStateRestored
	if err := fixture.manager.Journal.Replace(record); err != nil {
		t.Fatal(err)
	}
	// Simulate the dispatcher-restore boundary having completed before the
	// cleanup failure. The fresh manager must reconstruct the exact dynamic
	// trampoline from the persistent stage rather than its legacy fallback.
	if err := fixture.manager.SourcePlan.RemoveDispatcher(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewProtectedInstallManager(options)
	if err != nil {
		t.Fatalf("fresh production composition: %v", err)
	}
	fresh.RequestReboot = func(context.Context) error { return nil }
	cleanupErr := errors.New("simulated fresh cleanup failure")
	fresh.FailureHook = func(event string) error {
		if event == "before-stage-removal" {
			return cleanupErr
		}
		return nil
	}
	fresh.ChainOriginal = func(context.Context) error { return nil }
	if err := fresh.Uninstall(context.Background()); !errors.Is(err, cleanupErr) {
		t.Fatalf("fresh uninstall error=%v, want cleanup error", err)
	}
	got, err := os.ReadFile(fixture.dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(got, []byte(fixture.stagePath)) || !bytes.Contains(got, []byte(fixture.helperHash)) {
		t.Fatalf("fresh cleanup retry trampoline=%q, want stage/helper binding", got)
	}
	final, exists, err := fresh.Journal.Load()
	if err != nil || !exists || final.State != InstallStateRestored {
		t.Fatalf("fresh restored journal=%#v exists=%v err=%v", final, exists, err)
	}
}

func TestFailStopPreparedRecoveryDoesNotRepairCurrentBootOwner(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	if err := os.Remove(filepath.Join(fixture.root, "owner.json")); err != nil {
		t.Fatal(err)
	}
	crash := errors.New("simulated crash before owner initialization")
	initializerCalls := 0
	fixture.manager.OwnerInitializer = func(context.Context) error {
		initializerCalls++
		return fixture.manager.writeInitialOwner(testBootID)
	}
	fixture.manager.FailureHook = func(event string) error {
		if event == "before-owner-initialization" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
		t.Fatalf("install error=%v, want owner initialization seam", err)
	}
	prepared, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || prepared.State != InstallStatePrepared {
		t.Fatalf("prepared journal=%#v exists=%v err=%v", prepared, exists, err)
	}
	fixture.manager.FailureHook = nil
	if err := fixture.manager.Recover(context.Background()); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("recovery error=%v, want reboot request", err)
	}
	owner, exists, err := fixture.manager.OwnerStore.Load()
	if err != nil || exists {
		t.Fatalf("prepared recovery repaired owner=%#v exists=%v err=%v", owner, exists, err)
	}
	if initializerCalls != 0 {
		t.Fatalf("prepared recovery invoked owner initializer %d times", initializerCalls)
	}
	terminal, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || terminal.State != InstallStateTerminal {
		t.Fatalf("terminal journal=%#v exists=%v err=%v", terminal, exists, err)
	}
}

func TestFailStopCoarseJournalFsyncSeamsLeaveDurableResumableState(t *testing.T) {
	cases := []struct {
		name      string
		event     string
		wantState InstallState
	}{
		{name: "before-prepared", event: "before-prepared-fsync", wantState: ""},
		{name: "after-prepared", event: "after-prepared-fsync", wantState: InstallStatePrepared},
		{name: "before-installed", event: "before-installed-fsync", wantState: InstallStatePrepared},
		{name: "after-installed", event: "after-installed-fsync", wantState: InstallStateInstalled},
		{name: "before-terminal", event: "before-terminal-fsync", wantState: InstallStateInstalled},
		{name: "after-terminal", event: "after-terminal-fsync", wantState: InstallStateTerminal},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			fixture := newCoarseInstallFixture(t)
			crash := errors.New("simulated coarse journal fsync crash")
			fixture.manager.FailureHook = func(event string) error {
				if event == test.event {
					return crash
				}
				return nil
			}
			err := fixture.manager.Install(context.Background(), fixture.packageRoot)
			if !errors.Is(err, crash) {
				t.Fatalf("install error=%v, want %v", err, crash)
			}
			record, exists, loadErr := fixture.manager.Journal.Load()
			if loadErr != nil || (test.wantState == "" && exists) || (test.wantState != "" && (!exists || record.State != test.wantState)) {
				t.Fatalf("journal=%#v exists=%v err=%v want=%q", record, exists, loadErr, test.wantState)
			}
			fixture.manager.FailureHook = nil
			if test.wantState == "" {
				if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
					t.Fatalf("retry install error=%v, want reboot request", err)
				}
			} else if test.wantState != InstallStateTerminal {
				if err := fixture.manager.Recover(context.Background()); !errors.Is(err, ErrRebootRequested) {
					t.Fatalf("recovery error=%v, want reboot request", err)
				}
			}
		})
	}
}

func TestFailStopRecoverPreparedCompletesFromPersistentStageWithoutTransferRoot(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	pkg, err := ValidateInstallPackage(fixture.packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyInstallPackage(pkg, fixture.stagePath, fixture.uid); err != nil {
		t.Fatal(err)
	}
	fixture.manager.StagePath = fixture.stagePath
	fixture.manager.StageManifestSHA256 = pkg.ManifestSHA256
	if err := fixture.manager.setDynamicTrampoline(fixture.stagePath, fixture.helperHash); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.InstallTrampoline(context.Background()); err != nil {
		t.Fatal(err)
	}
	prepared := fixture.manager.journalRecord(InstallStatePrepared)
	if err := fixture.manager.Journal.Replace(prepared); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fixture.packageRoot); err != nil {
		t.Fatal(err)
	}
	var rebootCalls, starts int
	fixture.manager.PackageRoot = ""
	fixture.manager.RequestReboot = func(context.Context) error { rebootCalls++; return nil }
	fixture.manager.ExecSupervisor = func(context.Context, int) error { starts++; return nil }
	if err := fixture.manager.Recover(context.Background()); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("recover error=%v, want reboot request", err)
	}
	if starts != 0 || rebootCalls != 1 {
		t.Fatalf("starts=%d reboot=%d", starts, rebootCalls)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateTerminal {
		t.Fatalf("recovered journal=%#v exists=%v err=%v", record, exists, err)
	}
}

func TestFailStopRecoverPreparedValidHelperRollsBackDamagedPayload(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	pkg, err := ValidateInstallPackage(fixture.packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyInstallPackage(pkg, fixture.stagePath, fixture.uid); err != nil {
		t.Fatal(err)
	}
	fixture.manager.StagePath = fixture.stagePath
	fixture.manager.StageManifestSHA256 = pkg.ManifestSHA256
	if err := fixture.manager.setDynamicTrampoline(fixture.stagePath, fixture.helperHash); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.InstallTrampoline(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Journal.Replace(fixture.manager.journalRecord(InstallStatePrepared)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(fixture.stagePath, "bin", "mister-agent")); err != nil {
		t.Fatal(err)
	}
	var starts int
	fixture.manager.ExecSupervisor = func(context.Context, int) error { starts++; return nil }
	fixture.manager.RequestReboot = func(context.Context) error { return nil }
	if err := fixture.manager.Recover(context.Background()); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("recover error=%v, want reboot request", err)
	}
	if starts != 0 {
		t.Fatalf("supervisor started during rollback: %d", starts)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateRestored {
		t.Fatalf("rollback journal=%#v exists=%v err=%v", record, exists, err)
	}
	for path, want := range map[string][]byte{fixture.dispatcher: fixture.dispatcherOriginal, fixture.legacy: fixture.legacyOriginal} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("rollback source %s=%q err=%v want=%q", path, got, readErr, want)
		}
	}
	if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
		t.Fatalf("damaged persistent stage remains: %v", err)
	}
}

func TestFailStopRecoverPreparedMismatchedHelperStaysFencedWithDiagnostic(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	pkg, err := ValidateInstallPackage(fixture.packageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := copyInstallPackage(pkg, fixture.stagePath, fixture.uid); err != nil {
		t.Fatal(err)
	}
	fixture.manager.StagePath = fixture.stagePath
	fixture.manager.StageManifestSHA256 = pkg.ManifestSHA256
	if err := fixture.manager.setDynamicTrampoline(fixture.stagePath, fixture.helperHash); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.InstallTrampoline(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Journal.Replace(fixture.manager.journalRecord(InstallStatePrepared)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.stagePath, "bin", "mister-fpga-dev"), []byte("wrong helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	var starts int
	fixture.manager.ExecSupervisor = func(context.Context, int) error { starts++; return nil }
	if err := fixture.manager.Recover(context.Background()); err == nil {
		t.Fatal("mismatched helper unexpectedly recovered")
	}
	if starts != 0 {
		t.Fatalf("supervisor started despite helper mismatch: %d", starts)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStatePrepared {
		t.Fatalf("fenced journal=%#v exists=%v err=%v", record, exists, err)
	}
	if _, err := os.Stat(fixture.dispatcher); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.manager.diagnosticPath(fixture.stagePath)); err != nil {
		t.Fatalf("recovery diagnostic missing: %v", err)
	}
}

func TestFailStopInstalledRecoveryUsesPublishedTrampolineAndOwnerOrder(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	crash := errors.New("simulated crash after installed fsync")
	fixture.manager.FailureHook = func(event string) error {
		if event == "after-installed-fsync" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
		t.Fatalf("install error=%v, want installed checkpoint seam", err)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateInstalled {
		t.Fatalf("installed journal=%#v exists=%v err=%v", record, exists, err)
	}

	// Simulate the rebooted helper: no transfer root or in-memory trampoline
	// bytes are supplied, so recovery must bind the published dispatcher and
	// still acquire install before owner before disabling sources.
	plan := NewSourceMutationPlan(record.Sources, filepath.Join(fixture.root, "backups"))
	plan.Supervisor = fixture.supervisor
	manager := &InstallManager{
		Journal:       fixture.manager.Journal,
		InstallLocker: NewInstallLocker(filepath.Join(fixture.root, "install.lock-recovery"), fixture.uid),
		OwnerLocker:   NewInstallLocker(filepath.Join(fixture.root, "owner.lock-recovery"), fixture.uid),
		SourcePlan:    plan,
		StageRoot:     filepath.Join(fixture.root, "staging"),
		FixedMembers:  fixture.manager.FixedMembers,
		BootID:        func() (string, error) { return testBootID, nil },
		RequestReboot: func(context.Context) error { return nil },
	}
	if err := manager.Recover(context.Background()); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("installed recovery error=%v, want reboot request", err)
	}
	record, exists, err = manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateTerminal {
		t.Fatalf("terminal journal=%#v exists=%v err=%v", record, exists, err)
	}
	if _, err := os.Lstat(fixture.legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy source was not disabled: %v", err)
	}
}

func TestFailStopUninstallReentersFromRestoredCheckpoint(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	fixture.manager.ChainOriginal = func(context.Context) error { return nil }
	fixture.manager.RequestReboot = func(context.Context) error { return nil }
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v", err)
	}
	crash := errors.New("simulated crash after restored fsync")
	fixture.manager.FailureHook = func(event string) error {
		if event == "after-restored-fsync" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Uninstall(context.Background()); !errors.Is(err, crash) {
		t.Fatalf("uninstall error=%v, want restored checkpoint seam", err)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateRestored {
		t.Fatalf("restored journal=%#v exists=%v err=%v", record, exists, err)
	}
	fixture.manager.FailureHook = nil
	if err := fixture.manager.Uninstall(context.Background()); err != nil {
		t.Fatalf("restored re-entry error=%v", err)
	}
	if got, err := os.ReadFile(fixture.dispatcher); err != nil || !bytes.Equal(got, fixture.dispatcherOriginal) {
		t.Fatalf("dispatcher after re-entry=%q err=%v", got, err)
	}
	if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
		t.Fatalf("persistent stage after re-entry=%v", err)
	}
}

func TestFailStopRecoverNextBootResumesUninstallingAndRestored(t *testing.T) {
	for _, targetState := range []InstallState{InstallStateUninstalling, InstallStateRestored} {
		targetState := targetState
		t.Run(string(targetState), func(t *testing.T) {
			fixture := newCoarseInstallFixture(t)
			fixture.manager.RequestReboot = func(context.Context) error { return nil }
			if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
				t.Fatalf("install error=%v", err)
			}
			record, exists, err := fixture.manager.Journal.Load()
			if err != nil || !exists || record.State != InstallStateTerminal {
				t.Fatalf("terminal journal=%#v exists=%v err=%v", record, exists, err)
			}
			record.State = InstallStateUninstalling
			if err := fixture.manager.Journal.Replace(record); err != nil {
				t.Fatal(err)
			}
			if targetState == InstallStateRestored {
				if err := fixture.plan.DisableSupervisor(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := fixture.plan.RestoreSources(context.Background()); err != nil {
					t.Fatal(err)
				}
				record.State = InstallStateRestored
				if err := fixture.manager.Journal.Replace(record); err != nil {
					t.Fatal(err)
				}
			}

			plan := NewSourceMutationPlan(nil, filepath.Join(fixture.root, "backups"))
			plan.Supervisor = fixture.supervisor
			plan.ExpectedUID = fixture.uid
			chained := false
			manager := &InstallManager{
				Journal: fixture.manager.Journal, InstallLocker: NewInstallLocker(filepath.Join(fixture.root, "install-next.lock"), fixture.uid),
				OwnerStore: fixture.manager.OwnerStore, OwnerLocker: NewInstallLocker(filepath.Join(fixture.root, "owner-next.lock"), fixture.uid),
				SourcePlan: plan, StageRoot: fixture.manager.StageRoot, FixedMembers: fixture.manager.FixedMembers,
				BootID: func() (string, error) { return testBootID, nil }, RequestReboot: func(context.Context) error { return nil },
				ChainOriginal: func(context.Context) error {
					chained = true
					got, readErr := os.ReadFile(fixture.dispatcher)
					if readErr != nil || !bytes.Equal(got, fixture.dispatcherOriginal) {
						return fmt.Errorf("dispatcher was not restored before chain: %q (%v)", got, readErr)
					}
					return nil
				},
			}
			if err := manager.Recover(context.Background()); err != nil {
				t.Fatalf("next-boot recovery error=%v", err)
			}
			if !chained {
				t.Fatal("next-boot recovery did not chain original dispatcher")
			}
			for path, want := range map[string][]byte{
				fixture.dispatcher: fixture.dispatcherOriginal,
				fixture.legacy:     fixture.legacyOriginal,
				fixture.legacy2:    fixture.legacy2Original,
			} {
				got, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(got, want) {
					t.Fatalf("restored source %s=%q err=%v want=%q", path, got, readErr, want)
				}
			}
			if _, err := os.Lstat(fixture.supervisor); !os.IsNotExist(err) {
				t.Fatalf("supervisor remains after recovery: %v", err)
			}
			if _, err := os.Lstat(fixture.stagePath); !os.IsNotExist(err) {
				t.Fatalf("persistent stage remains after recovery: %v", err)
			}
			final, exists, err := manager.Journal.Load()
			if err != nil || !exists || final.State != InstallStateRestored {
				t.Fatalf("final journal=%#v exists=%v err=%v", final, exists, err)
			}
		})
	}
}

func TestFailStopUninstallJournalFsyncCrashTable(t *testing.T) {
	cases := []struct {
		name      string
		event     string
		wantState InstallState
	}{
		{name: "before-uninstalling", event: "before-uninstalling-fsync", wantState: InstallStateTerminal},
		{name: "after-uninstalling", event: "after-uninstalling-fsync", wantState: InstallStateUninstalling},
		{name: "before-restored", event: "before-restored-fsync", wantState: InstallStateUninstalling},
		{name: "after-restored", event: "after-restored-fsync", wantState: InstallStateRestored},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			fixture := newCoarseInstallFixture(t)
			fixture.manager.ChainOriginal = func(context.Context) error { return nil }
			fixture.manager.RequestReboot = func(context.Context) error { return nil }
			if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
				t.Fatalf("install error=%v", err)
			}
			crash := errors.New("simulated uninstall journal fsync crash")
			fixture.manager.FailureHook = func(event string) error {
				if event == test.event {
					return crash
				}
				return nil
			}
			if err := fixture.manager.Uninstall(context.Background()); !errors.Is(err, crash) {
				t.Fatalf("uninstall error=%v, want %v", err, crash)
			}
			record, exists, err := fixture.manager.Journal.Load()
			if err != nil || !exists || record.State != test.wantState {
				t.Fatalf("journal=%#v exists=%v err=%v want=%q", record, exists, err, test.wantState)
			}
			if test.wantState != InstallStateRestored {
				if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
					t.Fatalf("dispatcher escaped trampoline on %s: %q err=%v", test.name, got, readErr)
				}
			}
		})
	}
}

func TestFailStopUninstallKeepsTrampolineUntilDispatcherRestore(t *testing.T) {
	fixture := newCoarseInstallFixture(t)
	fixture.manager.ChainOriginal = func(context.Context) error { return nil }
	fixture.manager.RequestReboot = func(context.Context) error { return nil }
	if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v", err)
	}
	crash := errors.New("simulated crash before dispatcher restore")
	fixture.manager.FailureHook = func(event string) error {
		if event == "before-dispatcher-restoration" {
			return crash
		}
		return nil
	}
	if err := fixture.manager.Uninstall(context.Background()); !errors.Is(err, crash) {
		t.Fatalf("uninstall error=%v, want %v", err, crash)
	}
	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateRestored {
		t.Fatalf("journal=%#v exists=%v err=%v", record, exists, err)
	}
	if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
		t.Fatalf("dispatcher escaped trampoline before restore: %q err=%v", got, readErr)
	}
	fixture.manager.FailureHook = nil
	if err := fixture.manager.Uninstall(context.Background()); err != nil {
		t.Fatalf("restored retry error=%v", err)
	}
	if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Equal(got, fixture.dispatcherOriginal) {
		t.Fatalf("dispatcher after retry=%q err=%v", got, readErr)
	}
}

func TestFailStopSourceAndSupervisorSeamsRetainTrampolineUntilRestore(t *testing.T) {
	t.Run("disable-source-midway", func(t *testing.T) {
		fixture := newCoarseInstallFixture(t)
		fixture.manager.ChainOriginal = func(context.Context) error { return nil }
		fixture.manager.RequestReboot = func(context.Context) error { return nil }
		crash := errors.New("simulated source-disable crash")
		fixture.manager.FailureHook = func(event string) error {
			if strings.HasPrefix(event, "after-source-disable:") {
				return crash
			}
			return nil
		}
		if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, crash) {
			t.Fatalf("install error=%v, want source-disable seam", err)
		}
		record, exists, err := fixture.manager.Journal.Load()
		if err != nil || !exists || record.State != InstallStateInstalled {
			t.Fatalf("journal=%#v exists=%v err=%v", record, exists, err)
		}
		if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
			t.Fatalf("trampoline after source seam=%q err=%v", got, readErr)
		}
		fixture.manager.FailureHook = nil
		if err := fixture.manager.Recover(context.Background()); !errors.Is(err, ErrRebootRequested) {
			t.Fatalf("recovery error=%v", err)
		}
		if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
			t.Fatalf("dispatcher escaped trampoline at terminal=%q err=%v", got, readErr)
		}
		for _, path := range []string{fixture.legacy, fixture.legacy2} {
			if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
				t.Fatalf("legacy source %s remains after disable recovery: %v", path, statErr)
			}
		}
		if err := fixture.manager.Uninstall(context.Background()); err != nil {
			t.Fatalf("uninstall after disable recovery error=%v", err)
		}
		for path, want := range map[string][]byte{
			fixture.dispatcher: fixture.dispatcherOriginal,
			fixture.legacy:     fixture.legacyOriginal,
			fixture.legacy2:    fixture.legacy2Original,
		} {
			got, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(got, want) {
				t.Fatalf("source %s after disable/uninstall=%q err=%v want=%q", path, got, readErr, want)
			}
		}
	})

	t.Run("supervisor-disable-before-and-after", func(t *testing.T) {
		for _, after := range []bool{false, true} {
			after := after
			t.Run(fmt.Sprintf("after=%t", after), func(t *testing.T) {
				fixture := newCoarseInstallFixture(t)
				if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
					t.Fatalf("install error=%v", err)
				}
				crash := errors.New("simulated supervisor-disable crash")
				fixture.manager.FailureHook = func(event string) error {
					if (after && event == "after-supervisor-disable") || (!after && event == "before-supervisor-disable") {
						return crash
					}
					return nil
				}
				if err := fixture.manager.Uninstall(context.Background()); !errors.Is(err, crash) {
					t.Fatalf("uninstall error=%v", err)
				}
				record, exists, err := fixture.manager.Journal.Load()
				if err != nil || !exists || record.State != InstallStateUninstalling {
					t.Fatalf("journal=%#v exists=%v err=%v", record, exists, err)
				}
				if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
					t.Fatalf("trampoline after supervisor seam=%q err=%v", got, readErr)
				}
			})
		}
	})

	t.Run("restore-source-midway", func(t *testing.T) {
		fixture := newCoarseInstallFixture(t)
		fixture.manager.ChainOriginal = func(context.Context) error { return nil }
		fixture.manager.RequestReboot = func(context.Context) error { return nil }
		if err := fixture.manager.Install(context.Background(), fixture.packageRoot); !errors.Is(err, ErrRebootRequested) {
			t.Fatalf("install error=%v", err)
		}
		crash := errors.New("simulated source-restore crash")
		fixture.manager.FailureHook = func(event string) error {
			if strings.HasPrefix(event, "after-source-restore:") {
				return crash
			}
			return nil
		}
		if err := fixture.manager.Uninstall(context.Background()); !errors.Is(err, crash) {
			t.Fatalf("uninstall error=%v", err)
		}
		record, exists, err := fixture.manager.Journal.Load()
		if err != nil || !exists || record.State != InstallStateUninstalling {
			t.Fatalf("journal=%#v exists=%v err=%v", record, exists, err)
		}
		if got, readErr := os.ReadFile(fixture.dispatcher); readErr != nil || !bytes.Contains(got, []byte("recover-install")) {
			t.Fatalf("trampoline after restore seam=%q err=%v", got, readErr)
		}
		fixture.manager.FailureHook = nil
		if err := fixture.manager.Uninstall(context.Background()); err != nil {
			t.Fatalf("restore recovery error=%v", err)
		}
		for path, want := range map[string][]byte{
			fixture.dispatcher: fixture.dispatcherOriginal,
			fixture.legacy:     fixture.legacyOriginal,
			fixture.legacy2:    fixture.legacy2Original,
		} {
			got, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(got, want) {
				t.Fatalf("source %s after restore recovery=%q err=%v want=%q", path, got, readErr, want)
			}
		}
	})
}

type coarseInstallFixture struct {
	uid                                                                   uint32
	root, packageRoot, stagePath, dispatcher, legacy, legacy2, supervisor string
	dispatcherOriginal, legacyOriginal, legacy2Original                   []byte
	packageMembers                                                        map[string][]byte
	helperHash                                                            string
	plan                                                                  *SourceMutationPlan
	manager                                                               *InstallManager
}

func newCoarseInstallFixture(t *testing.T) *coarseInstallFixture {
	t.Helper()
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	legacy2 := filepath.Join(root, "legacy2")
	supervisor := filepath.Join(root, "supervisor")
	dispatcherOriginal := []byte("dispatcher-original\n")
	legacyOriginal := []byte("legacy-original\n")
	legacy2Original := []byte("legacy2-original\n")
	for path, data := range map[string][]byte{dispatcher: dispatcherOriginal, legacy: legacyOriginal, legacy2: legacy2Original} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	backupDir := filepath.Join(root, "backups")
	sources, err := CaptureStartSourceInventory([]string{dispatcher, legacy, legacy2}, backupDir, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewSourceMutationPlan(sources, backupDir)
	plan.Supervisor = supervisor
	plan.ExpectedUID = uid
	mainPath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(mainPath, []byte("main\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	main, err := inspectPathExpectation(mainPath, true)
	if err != nil {
		t.Fatal(err)
	}
	fifo, err := inspectPathExpectation(fifoPath, true)
	if err != nil {
		t.Fatal(err)
	}
	owner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	if err := owner.Replace(hardwareowner.Record{Schema: 1, State: hardwareowner.StateNormalMain, BootID: testBootID, GenerationHighWater: 1, ActiveSession: strings.Repeat("a", 32), ActiveGeneration: 1, ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain, ActiveLeases: hardwareowner.NormalLeases(), CandidateMode: hardwareowner.ModeNone, CandidateOwner: hardwareowner.OwnerNone, QuiescingOwner: hardwareowner.OwnerNone, RequestedResources: []string{}, FirstFailure: ""}); err != nil {
		t.Fatal(err)
	}
	packageRoot := filepath.Join(root, "package")
	if err := os.Mkdir(packageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	members := map[string][]byte{
		"bin/fogcast-dev-supervisor":        []byte("supervisor binary\n"),
		"bin/mister-agent":                  []byte("agent binary\n"),
		"bin/mister-fpga-dev":               []byte("recovery helper\n"),
		"deploy/fpgadev/agent.toml.example": []byte("profile = \"development\"\n"),
		"deploy/fpgadev/start.sh":           []byte("#!/bin/sh\nexit 0\n"),
	}
	for member, data := range members {
		path := filepath.Join(packageRoot, filepath.FromSlash(member))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(member, "bin/") || member == "deploy/fpgadev/start.sh" {
			mode = 0o755
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	var manifest strings.Builder
	for _, member := range installPackageMembers {
		digest := sha256.Sum256(members[member])
		fmt.Fprintf(&manifest, "%x  %s\n", digest, member)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "manifest.sha256"), []byte(manifest.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	helperDigest := sha256.Sum256(members["bin/mister-fpga-dev"])
	stagePath := filepath.Join(root, "staging", fmt.Sprintf("%x", sha256.Sum256([]byte(manifest.String()))))
	inventory := InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources}
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	manager := &InstallManager{Journal: journal, InstallLocker: NewInstallLocker(filepath.Join(root, "install.lock"), uid), OwnerStore: owner, OwnerLocker: NewInstallLocker(filepath.Join(root, "owner.lock"), uid), BootID: func() (string, error) { return testBootID, nil }, PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64), Inventory: inventory, Sources: sources, SourcePlan: plan, StageRoot: filepath.Join(root, "staging"), FixedMembers: map[string]string{"bin/mister-agent": filepath.Join(root, "fixed-agent"), "bin/mister-fpga-dev": filepath.Join(root, "fixed-helper")}, MainReadiness: func(context.Context) error { return nil }, StopAgent: func(context.Context) error { return nil }, ProveAgentAbsent: func(context.Context) error { return nil }}
	return &coarseInstallFixture{uid: uid, root: root, packageRoot: packageRoot, stagePath: stagePath, dispatcher: dispatcher, legacy: legacy, legacy2: legacy2, supervisor: supervisor, dispatcherOriginal: dispatcherOriginal, legacyOriginal: legacyOriginal, legacy2Original: legacy2Original, packageMembers: members, helperHash: fmt.Sprintf("%x", helperDigest), plan: plan, manager: manager}
}

func newProtectedAuthorityFixture(t *testing.T) (*coarseInstallFixture, ProtectedInstallManagerOptions) {
	t.Helper()
	fixture := newCoarseInstallFixture(t)
	inputPath := filepath.Join(fixture.root, "uinput")
	if err := os.WriteFile(inputPath, []byte("input-fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(fixture.root, "agent.toml")
	config := []byte("listen_address = \"127.0.0.1:8182\"\n" +
		"token = \"authority-test-token\"\n" +
		"mister_process_comm = \"MiSTer\"\n" +
		"command_pipe = \"" + filepath.Join(fixture.root, "MiSTer_cmd") + "\"\n" +
		"core_name_file = \"" + filepath.Join(fixture.root, "CORENAME") + "\"\n" +
		"menu_rbf = \"" + filepath.Join(fixture.root, "menu.rbf") + "\"\n" +
		"mgl_directory = \"" + filepath.Join(fixture.root, "mister-remote") + "\"\n" +
		"build_profile = \"development\"\n" +
		"development_profile = true\n" +
		"hardware_owner_path = \"" + filepath.Join(fixture.root, "owner.json") + "\"\n" +
		"hardware_owner_lock = \"" + filepath.Join(fixture.root, "owner.lock") + "\"\n" +
		"designation_path = \"" + filepath.Join(fixture.root, "designation") + "\"\n" +
		"target_identity_path = \"" + filepath.Join(fixture.root, "identity") + "\"\n" +
		"fpgadev_boot_dispatcher = \"" + fixture.dispatcher + "\"\n" +
		"fpgadev_start_sources = [\"" + fixture.dispatcher + "\", \"" + fixture.legacy + "\", \"" + fixture.legacy2 + "\"]\n" +
		"input_uinput_path = \"" + inputPath + "\"\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(fixture.root, "MiSTer_cmd"), 0o600); err != nil {
		// The FIFO is already protected by mkfifoRuntimeTest; this keeps the
		// fixture setup explicit without replacing its special file.
		t.Fatal(err)
	}
	options := ProtectedInstallManagerOptions{
		ConfigPath:       configPath,
		MainExecutable:   filepath.Join(fixture.root, "Main_MiSTer"),
		MainFIFO:         filepath.Join(fixture.root, "MiSTer_cmd"),
		BackupDir:        filepath.Join(fixture.root, "backups"),
		JournalPath:      filepath.Join(fixture.root, "journal.json"),
		InstallLockPath:  filepath.Join(fixture.root, "install.lock"),
		OwnerPath:        filepath.Join(fixture.root, "owner.json"),
		OwnerLockPath:    filepath.Join(fixture.root, "owner.lock"),
		SupervisorSource: fixture.supervisor,
		StageRoot:        filepath.Join(fixture.root, "staging"),
		FixedMembers:     cloneStringMap(fixture.manager.FixedMembers),
		ExpectedUID:      fixture.uid,
		PackageSHA256:    strings.Repeat("1", 64),
		BootID:           func() (string, error) { return testBootID, nil },
		MainReadiness:    func(context.Context) error { return nil },
		MainObserver:     func(context.Context) ([]ProcessIdentity, error) { return []ProcessIdentity{{PID: 1}}, nil },
		StopAgent:        func(context.Context) error { return nil },
		ProveAgentAbsent: func(context.Context) error { return nil },
		RequestReboot:    func(context.Context) error { return nil },
	}
	manager, err := NewProtectedInstallManager(options)
	if err != nil {
		t.Fatalf("compose protected authority manager: %v", err)
	}
	fixture.manager = manager
	return fixture, options
}

const testBootID = "00000000-0000-0000-0000-000000000001"

func TestCaptureSourceInventoryAndProtectedBackups(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(root, "sources")
	backupDir := filepath.Join(root, "backups")
	if err := os.Mkdir(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dispatcher := filepath.Join(sourceDir, "dispatcher")
	legacy := filepath.Join(sourceDir, "legacy")
	dispatcherBytes := []byte("#!/bin/sh\nold-dispatcher\n")
	legacyBytes := []byte("#!/bin/sh\nold-agent\n")
	for path, data := range map[string][]byte{dispatcher: dispatcherBytes, legacy: legacyBytes} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := CaptureStartSourceInventory([]string{legacy, dispatcher}, backupDir, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 || sources[0].Path != dispatcher || sources[1].Path != legacy {
		t.Fatalf("sources=%#v", sources)
	}
	if sources[0].DisabledState != "approved_trampoline" || sources[1].DisabledState != "absent" {
		t.Fatalf("source states=%#v", sources)
	}
	plan := NewSourceMutationPlan(sources, backupDir)
	plan.ExpectedUID = uint32(os.Getuid())
	if err := plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	for index, want := range [][]byte{dispatcherBytes, legacyBytes} {
		backup := sources[index].BackupPath
		got, err := os.ReadFile(backup)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("backup %q=%q want=%q", backup, got, want)
		}
		info, err := os.Stat(backup)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("backup mode=%o", info.Mode().Perm())
		}
		digest := sha256.Sum256(got)
		if sources[index].BackupSHA256 != hex.EncodeToString(digest[:]) {
			t.Fatalf("backup hash=%q", sources[index].BackupSHA256)
		}
	}
	if !bytes.Equal(BuildApprovedRecoveryTrampoline(), []byte("#!/bin/sh\nset -eu\nexec /usr/bin/mister-fpga-dev recover-install\n")) {
		t.Fatalf("trampoline changed: %q", BuildApprovedRecoveryTrampoline())
	}
}

func TestReadRegularFileNoFollowUsesProtectedRegularBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "protected-regular")
	accepted := bytes.Repeat([]byte{0xa5}, InstallJournalMaxBytes+1)
	if err := os.WriteFile(path, accepted, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readRegularFileNoFollow(path, info)
	if err != nil {
		t.Fatalf("read %d-byte protected regular: %v", len(accepted), err)
	}
	if !bytes.Equal(got, accepted) {
		t.Fatal("protected regular bytes changed")
	}

	if err := os.WriteFile(path, bytes.Repeat([]byte{0x5a}, ProtectedRegularMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err = os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readRegularFileNoFollow(path, info); err == nil {
		t.Fatal("oversized protected regular accepted")
	}
}

func TestSourcePlanRejectsSwapSymlinkTypeOwnerModeAndLink(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	source := filepath.Join(root, "source")
	backupDir := filepath.Join(root, "backups")
	if err := os.WriteFile(source, []byte("original"), 0o755); err != nil {
		t.Fatal(err)
	}
	sources, err := CaptureStartSourceInventory([]string{source}, backupDir, source)
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name   string
		mutate func() error
	}{
		{name: "content swap", mutate: func() error { return os.WriteFile(source, []byte("changed"), 0o755) }},
		{name: "mode swap", mutate: func() error { return os.Chmod(source, 0o700) }},
		{name: "symlink swap", mutate: func() error {
			if err := os.Remove(source); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(root, "elsewhere"), source)
		}},
		{name: "fifo swap", mutate: func() error {
			if err := os.Remove(source); err != nil {
				return err
			}
			return mkfifoRuntimeTest(source)
		}},
		{name: "owner mismatch", mutate: func() error { return nil }},
		{name: "link count", mutate: func() error {
			if err := os.Remove(source); err != nil {
				return err
			}
			if err := os.WriteFile(source, []byte("original"), 0o755); err != nil {
				return err
			}
			_, err := os.Create(filepath.Join(root, "hardlink"))
			if err == nil {
				_ = os.Remove(filepath.Join(root, "hardlink"))
			}
			return os.Link(source, filepath.Join(root, "hardlink"))
		}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(source)
			if err := os.WriteFile(source, []byte("original"), 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.name == "owner mismatch" {
				plan := NewSourceMutationPlan(sources, backupDir)
				plan.ExpectedUID = uint32(os.Getuid()) + 1
				if err := plan.Prepare(context.Background()); err == nil {
					t.Fatal("owner mismatch accepted")
				}
				return
			}
			if err := tc.mutate(); err != nil {
				t.Fatal(err)
			}
			plan := NewSourceMutationPlan(sources, filepath.Join(root, tc.name+"-backups"))
			plan.ExpectedUID = uint32(os.Getuid())
			if err := plan.Prepare(context.Background()); err == nil {
				t.Fatal("hostile source mutation accepted")
			}
		})
	}
}

func TestSourcePlanRejectsCorruptBackupAndPerformsAtomicDisable(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	if err := os.WriteFile(dispatcher, []byte("dispatcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(root, "backups")
	sources, err := CaptureStartSourceInventory([]string{dispatcher, legacy}, backupDir, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewSourceMutationPlan(sources, backupDir)
	plan.ExpectedUID = uint32(os.Getuid())
	if err := plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sources[1].BackupPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := plan.Prepare(context.Background()); err == nil {
		t.Fatal("corrupt backup accepted")
	}
	// Recreate a valid plan/backup before testing the atomic source operation.
	if err := os.WriteFile(sources[1].BackupPath, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := plan.InstallTrampoline(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := plan.DisableSources(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy source remains err=%v", err)
	}
	trampoline, err := os.ReadFile(dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(trampoline, BuildApprovedRecoveryTrampoline()) {
		t.Fatalf("dispatcher=%q", trampoline)
	}
}

func TestInventoryFromProtectedConfigAcceptsPlayKitMainAndRejectsInvalidMain(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	mainPath := filepath.Join(root, "Main_MiSTer")
	mainBytes := bytes.Repeat([]byte{0xa5}, 1059560)
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	dispatcher := filepath.Join(root, "dispatcher")
	input := filepath.Join(root, "uinput")
	legacy := filepath.Join(root, "legacy")
	for path, data := range map[string][]byte{mainPath: mainBytes, dispatcher: []byte("dispatcher"), input: []byte("uinput fixture"), legacy: []byte("legacy")} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "agent.toml")
	configBytes := []byte("listen_address = \"127.0.0.1:8182\"\n" +
		"token = \"test-token\"\n" +
		"mister_process_comm = \"MiSTer\"\n" +
		"command_pipe = \"" + fifoPath + "\"\n" +
		"core_name_file = \"" + filepath.Join(root, "CORENAME") + "\"\n" +
		"menu_rbf = \"" + filepath.Join(root, "menu.rbf") + "\"\n" +
		"mgl_directory = \"" + filepath.Join(root, "mister-remote") + "\"\n" +
		"build_profile = \"development\"\n" +
		"development_profile = true\n" +
		"hardware_owner_path = \"" + filepath.Join(root, "owner.json") + "\"\n" +
		"hardware_owner_lock = \"" + filepath.Join(root, "owner.lock") + "\"\n" +
		"designation_path = \"" + filepath.Join(root, "designation") + "\"\n" +
		"target_identity_path = \"" + filepath.Join(root, "identity") + "\"\n" +
		"fpgadev_boot_dispatcher = \"" + dispatcher + "\"\n" +
		"fpgadev_start_sources = [\"" + dispatcher + "\", \"" + legacy + "\"]\n" +
		"input_uinput_path = \"" + input + "\"\n")
	if err := os.WriteFile(config, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	mainExpectation, err := inspectPathExpectation(mainPath, true)
	if err != nil {
		t.Fatalf("inspect %d-byte Main: %v", len(mainBytes), err)
	}
	inventory, err := InventoryFromProtectedConfig(config, mainPath, fifoPath, filepath.Join(root, "backups"), dispatcher, []string{dispatcher, legacy})
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Schema != 1 || inventory.MainExecutable.Path != mainPath || inventory.MainFIFO.Kind != "fifo" || len(inventory.StartSources) != 2 || inventory.InputListen == nil {
		t.Fatalf("inventory=%#v", inventory)
	}
	mainHash := sha256.Sum256(mainBytes)
	if mainExpectation.SHA256 != hex.EncodeToString(mainHash[:]) || inventory.MainExecutable.SHA256 != hex.EncodeToString(mainHash[:]) {
		t.Fatalf("Main hash=%q want=%x", inventory.MainExecutable.SHA256, mainHash)
	}

	if err := os.Remove(mainPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, bytes.Repeat([]byte{0x5a}, ProtectedRegularMaxBytes+1), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InventoryFromProtectedConfig(config, mainPath, fifoPath, filepath.Join(root, "backups"), dispatcher, []string{dispatcher, legacy}); err == nil {
		t.Fatal("oversized Main executable accepted")
	}

	if err := os.Remove(mainPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dispatcher, mainPath); err != nil {
		t.Fatal(err)
	}
	if _, err := InventoryFromProtectedConfig(config, mainPath, fifoPath, filepath.Join(root, "backups"), dispatcher, []string{dispatcher, legacy}); err == nil {
		t.Fatal("symlinked Main executable accepted")
	}

	if err := os.Remove(mainPath); err != nil {
		t.Fatal(err)
	}
	if err := mkfifoRuntimeTest(mainPath); err != nil {
		t.Fatal(err)
	}
	if _, err := InventoryFromProtectedConfig(config, mainPath, fifoPath, filepath.Join(root, "backups"), dispatcher, []string{dispatcher, legacy}); err == nil {
		t.Fatal("non-regular Main executable accepted")
	}
}

func TestInventoryFromProtectedConfigCapturesInputUInputWithoutCast(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	mainPath := filepath.Join(root, "Main_MiSTer")
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	dispatcher := filepath.Join(root, "dispatcher")
	input := filepath.Join(root, "uinput")
	legacy := filepath.Join(root, "legacy")
	for path, data := range map[string][]byte{
		mainPath:   []byte("main"),
		dispatcher: []byte("dispatcher"),
		legacy:     []byte("legacy"),
		input:      []byte("uinput fixture"),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "agent.toml")
	configBytes := []byte("listen_address = \"127.0.0.1:8182\"\n" +
		"token = \"test-token\"\n" +
		"mister_process_comm = \"MiSTer\"\n" +
		"command_pipe = \"" + fifoPath + "\"\n" +
		"core_name_file = \"" + filepath.Join(root, "CORENAME") + "\"\n" +
		"menu_rbf = \"" + filepath.Join(root, "menu.rbf") + "\"\n" +
		"mgl_directory = \"" + filepath.Join(root, "mister-remote") + "\"\n" +
		"build_profile = \"development\"\n" +
		"development_profile = true\n" +
		"hardware_owner_path = \"" + filepath.Join(root, "owner.json") + "\"\n" +
		"hardware_owner_lock = \"" + filepath.Join(root, "owner.lock") + "\"\n" +
		"designation_path = \"" + filepath.Join(root, "designation") + "\"\n" +
		"target_identity_path = \"" + filepath.Join(root, "identity") + "\"\n" +
		"fpgadev_boot_dispatcher = \"" + dispatcher + "\"\n" +
		"fpgadev_start_sources = [\"" + dispatcher + "\", \"" + legacy + "\"]\n" +
		"input_uinput_path = \"" + input + "\"\n")
	if err := os.WriteFile(config, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := InventoryFromProtectedConfig(config, mainPath, fifoPath, filepath.Join(root, "backups"), dispatcher, []string{dispatcher, legacy})
	if err != nil {
		t.Fatal(err)
	}
	if inventory.InputUInput == nil {
		t.Fatal("configured input-uinput path was omitted from the closed inventory")
	}
	if inventory.InputUInput.Path != input || inventory.InputUInput.Kind != "regular" || inventory.InputUInput.SHA256 == "" {
		t.Fatalf("input-uinput expectation=%#v", inventory.InputUInput)
	}
}

func TestInventoryFromProtectedConfigCapturesEveryConfiguredEndpoint(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	mainPath := filepath.Join(root, "Main_MiSTer")
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	input := filepath.Join(root, "uinput")
	cast := filepath.Join(root, "cast")
	framebuffer := filepath.Join(root, "framebuffer")
	native := filepath.Join(root, "native-cmd")
	tokenFile := filepath.Join(root, "cast-token")
	for path, data := range map[string][]byte{
		mainPath:    []byte("main"),
		dispatcher:  []byte("dispatcher"),
		legacy:      []byte("legacy"),
		input:       []byte("uinput"),
		cast:        []byte("cast executable"),
		framebuffer: []byte("framebuffer"),
		native:      []byte("native command"),
		tokenFile:   []byte("token file"),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "agent.toml")
	configBytes := []byte("listen_address = \"127.0.0.1:8182\"\n" +
		"token = \"test-token\"\n" +
		"mister_process_comm = \"MiSTer\"\n" +
		"command_pipe = \"" + fifoPath + "\"\n" +
		"core_name_file = \"" + filepath.Join(root, "CORENAME") + "\"\n" +
		"menu_rbf = \"" + filepath.Join(root, "menu.rbf") + "\"\n" +
		"mgl_directory = \"" + filepath.Join(root, "mister-remote") + "\"\n" +
		"build_profile = \"development\"\n" +
		"development_profile = true\n" +
		"hardware_owner_path = \"" + filepath.Join(root, "owner.json") + "\"\n" +
		"hardware_owner_lock = \"" + filepath.Join(root, "owner.lock") + "\"\n" +
		"designation_path = \"" + filepath.Join(root, "designation") + "\"\n" +
		"target_identity_path = \"" + filepath.Join(root, "identity") + "\"\n" +
		"fpgadev_boot_dispatcher = \"" + dispatcher + "\"\n" +
		"fpgadev_start_sources = [\"" + dispatcher + "\", \"" + legacy + "\"]\n" +
		"input_uinput_path = \"" + input + "\"\n" +
		"input_listen_address = \"127.0.0.1:18183\"\n" +
		"cast_binary = \"" + cast + "\"\n" +
		"cast_rtp_address = \"127.0.0.1:19000\"\n" +
		"cast_control_address = \"127.0.0.1:19001\"\n" +
		"cast_framebuffer = \"" + framebuffer + "\"\n" +
		"cast_native_cmd = \"" + native + "\"\n" +
		"cast_native_mode = \"fixture\"\n" +
		"cast_token_file = \"" + tokenFile + "\"\n")
	if err := os.WriteFile(config, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := InventoryFromProtectedConfig(config, mainPath, fifoPath, filepath.Join(root, "backups"), dispatcher, []string{dispatcher, legacy})
	if err != nil {
		t.Fatal(err)
	}
	if inventory.InputUInput == nil || inventory.CastExecutable == nil || inventory.CastFramebuffer == nil || inventory.CastNativeCommand == nil || inventory.CastTokenFile == nil {
		t.Fatalf("path endpoint inventory omitted a configured field: %#v", inventory)
	}
	if inventory.InputListen == nil || inventory.CastRTP == nil || inventory.CastControl == nil {
		t.Fatalf("network endpoint inventory omitted a configured field: %#v", inventory)
	}
	if inventory.InputListen.Network != "tcp" || inventory.CastRTP.Network != "udp" || inventory.CastControl.Network != "tcp" {
		t.Fatalf("network endpoint protocol mapping=%#v/%#v/%#v", inventory.InputListen, inventory.CastRTP, inventory.CastControl)
	}
}

func TestInventoryFromProtectedConfigRejectsUntrustedSourceAuthority(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	mainPath := filepath.Join(root, "Main_MiSTer")
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	other := filepath.Join(root, "other")
	for path, data := range map[string][]byte{mainPath: []byte("main"), dispatcher: []byte("dispatcher"), legacy: []byte("legacy"), other: []byte("other")} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "agent.toml")
	configBytes := []byte("listen_address = \"127.0.0.1:8182\"\n" +
		"token = \"test-token\"\n" +
		"mister_process_comm = \"MiSTer\"\n" +
		"command_pipe = \"" + fifoPath + "\"\n" +
		"core_name_file = \"" + filepath.Join(root, "CORENAME") + "\"\n" +
		"menu_rbf = \"" + filepath.Join(root, "menu.rbf") + "\"\n" +
		"mgl_directory = \"" + filepath.Join(root, "mister-remote") + "\"\n" +
		"build_profile = \"development\"\n" +
		"development_profile = true\n" +
		"hardware_owner_path = \"" + filepath.Join(root, "owner.json") + "\"\n" +
		"hardware_owner_lock = \"" + filepath.Join(root, "owner.lock") + "\"\n" +
		"designation_path = \"" + filepath.Join(root, "designation") + "\"\n" +
		"target_identity_path = \"" + filepath.Join(root, "identity") + "\"\n" +
		"fpgadev_boot_dispatcher = \"" + dispatcher + "\"\n" +
		"fpgadev_start_sources = [\"" + dispatcher + "\", \"" + legacy + "\"]\n")
	if err := os.WriteFile(config, configBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InventoryFromProtectedConfig(config, dispatcher, fifoPath, filepath.Join(root, "backups"), dispatcher, []string{dispatcher, other}); err == nil {
		t.Fatal("caller-supplied source authority was accepted over the protected config")
	}
}

func TestInventoryFromProtectedConfigRejectsSymlinkConfig(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	target := filepath.Join(root, "real-agent.toml")
	link := filepath.Join(root, "agent.toml")
	if err := os.WriteFile(target, []byte("not a valid config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := InventoryFromProtectedConfig(link, "/bin/true", filepath.Join(root, "fifo"), filepath.Join(root, "backups"), filepath.Join(root, "dispatcher"), []string{filepath.Join(root, "dispatcher")}); err == nil {
		t.Fatal("symlinked prior config accepted")
	}
}

func TestInstallManagerUsesConcretePlanInDurableOrder(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	supervisor := filepath.Join(root, "supervisor")
	for path, data := range map[string][]byte{dispatcher: []byte("dispatcher"), legacy: []byte("legacy")} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := CaptureStartSourceInventory([]string{dispatcher, legacy}, filepath.Join(root, "backups"), dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewSourceMutationPlan(sources, filepath.Join(root, "backups"))
	plan.Supervisor = supervisor
	plan.ExpectedUID = uid
	mainPath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(mainPath, []byte("main"), 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	main, err := inspectPathExpectation(mainPath, true)
	if err != nil {
		t.Fatal(err)
	}
	fifo, err := inspectPathExpectation(fifoPath, true)
	if err != nil {
		t.Fatal(err)
	}
	owner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	if err := owner.Replace(hardwareowner.Record{
		Schema: 1, State: hardwareowner.StateNormalMain, BootID: testBootID,
		GenerationHighWater: 1, ActiveSession: strings.Repeat("a", 32), ActiveGeneration: 1,
		ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain,
		ActiveLeases: hardwareowner.NormalLeases(), CandidateMode: hardwareowner.ModeNone,
		CandidateOwner: hardwareowner.OwnerNone, QuiescingOwner: hardwareowner.OwnerNone,
		RequestedResources: []string{}, FirstFailure: "",
	}); err != nil {
		t.Fatal(err)
	}
	manager := &InstallManager{
		Journal:       NewInstallJournal(filepath.Join(root, "journal.json"), uid),
		InstallLocker: hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid),
		OwnerStore:    owner, OwnerLocker: hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid),
		BootID:        func() (string, error) { return testBootID, nil },
		PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64),
		Inventory: InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources},
		Sources:   sources, SourcePlan: plan,
		MainReadiness: func(context.Context) error { return nil },
		StopAgent:     func(context.Context) error { return nil }, ProveAgentAbsent: func(context.Context) error { return nil },
	}
	if err := manager.Install(context.Background(), ""); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("Install error=%v, want reboot request", err)
	}
	record, exists, err := manager.Journal.Load()
	if err != nil || !exists || record.State != InstallStateTerminal {
		t.Fatalf("journal=%#v exists=%v err=%v", record, exists, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy source err=%v", err)
	}
	if got, err := os.ReadFile(dispatcher); err != nil || !bytes.Equal(got, BuildApprovedRecoveryTrampoline()) {
		t.Fatalf("dispatcher=%q err=%v", got, err)
	}
	if _, err := os.Stat(supervisor); err != nil {
		t.Fatalf("supervisor was not installed: %v", err)
	}
}

func TestConcretePlanUninstallRestoresOriginalSourcesAfterSupervisorRemoval(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	supervisor := filepath.Join(root, "supervisor")
	dispatcherOriginal := []byte("dispatcher-original")
	legacyOriginal := []byte("legacy-original")
	for path, data := range map[string][]byte{dispatcher: dispatcherOriginal, legacy: legacyOriginal} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := CaptureStartSourceInventory([]string{dispatcher, legacy}, filepath.Join(root, "backups"), dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewSourceMutationPlan(sources, filepath.Join(root, "backups"))
	plan.Supervisor = supervisor
	plan.ExpectedUID = uid
	if err := plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := plan.InstallTrampoline(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := plan.DisableSources(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := plan.InstallSupervisor(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager := &InstallManager{
		Journal:       NewInstallJournal(filepath.Join(root, "journal.json"), uid),
		InstallLocker: hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid),
		OwnerStore:    hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid),
		OwnerLocker:   hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid),
		BootID:        func() (string, error) { return testBootID, nil },
		PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64),
		Inventory: InventoryV1{Schema: 1, MainExecutable: PathExpectation{Path: "/bin/true", Kind: "regular", SHA256: strings.Repeat("3", 64)}, MainFIFO: PathExpectation{Path: "/dev/MiSTer_cmd", Kind: "fifo"}, StartSources: sources},
		Sources:   sources, SourcePlan: plan,
	}
	manager.ChainOriginal = func(context.Context) error { return nil }
	terminal := InstallJournalRecord{Schema: 1, State: InstallStateTerminal, InstallBootID: testBootID, PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64), Inventory: manager.Inventory, Sources: sources}
	if err := replaceInstallJournalForTest(manager.Journal, terminal); err != nil {
		t.Fatal(err)
	}
	if err := manager.OwnerStore.Replace(hardwareowner.Record{
		Schema: 1, State: hardwareowner.StateNormalMain, BootID: testBootID,
		GenerationHighWater: 1, ActiveSession: strings.Repeat("a", 32), ActiveGeneration: 1,
		ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain,
		ActiveLeases: hardwareowner.NormalLeases(), CandidateMode: hardwareowner.ModeNone,
		CandidateOwner: hardwareowner.OwnerNone, QuiescingOwner: hardwareowner.OwnerNone,
		RequestedResources: []string{}, FirstFailure: "",
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	if record, exists, err := manager.Journal.Load(); err != nil || !exists || record.State != InstallStateRestored {
		t.Fatalf("restored journal=%#v exists=%v err=%v", record, exists, err)
	}
	if got, err := os.ReadFile(dispatcher); err != nil || !bytes.Equal(got, dispatcherOriginal) {
		t.Fatalf("dispatcher restore=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(legacy); err != nil || !bytes.Equal(got, legacyOriginal) {
		t.Fatalf("legacy restore=%q err=%v", got, err)
	}
	if _, err := os.Lstat(supervisor); !os.IsNotExist(err) {
		t.Fatalf("supervisor remains err=%v", err)
	}
}

func TestProtectedInstallManagerCompositionExecutesConcreteInstallRecoverUninstall(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	mainPath := filepath.Join(root, "Main_MiSTer")
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	input := filepath.Join(root, "uinput")
	supervisor := filepath.Join(root, "supervisor")
	for path, data := range map[string][]byte{
		mainPath:   []byte("main"),
		dispatcher: []byte("dispatcher-original"),
		legacy:     []byte("legacy-original"),
		input:      []byte("input-fixture"),
	} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "agent.toml")
	config := []byte("listen_address = \"127.0.0.1:8182\"\n" +
		"token = \"composition-test-token\"\n" +
		"mister_process_comm = \"MiSTer\"\n" +
		"command_pipe = \"" + fifoPath + "\"\n" +
		"core_name_file = \"" + filepath.Join(root, "CORENAME") + "\"\n" +
		"menu_rbf = \"" + filepath.Join(root, "menu.rbf") + "\"\n" +
		"mgl_directory = \"" + filepath.Join(root, "mister-remote") + "\"\n" +
		"build_profile = \"development\"\n" +
		"development_profile = true\n" +
		"hardware_owner_path = \"" + filepath.Join(root, "owner.json") + "\"\n" +
		"hardware_owner_lock = \"" + filepath.Join(root, "owner.lock") + "\"\n" +
		"designation_path = \"" + filepath.Join(root, "designation") + "\"\n" +
		"target_identity_path = \"" + filepath.Join(root, "identity") + "\"\n" +
		"fpgadev_boot_dispatcher = \"" + dispatcher + "\"\n" +
		"fpgadev_start_sources = [\"" + dispatcher + "\", \"" + legacy + "\"]\n" +
		"input_uinput_path = \"" + input + "\"\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	options := ProtectedInstallManagerOptions{
		ConfigPath:       configPath,
		MainExecutable:   mainPath,
		MainFIFO:         fifoPath,
		JournalPath:      filepath.Join(root, "journal.json"),
		BackupDir:        filepath.Join(root, "backups"),
		InstallLockPath:  filepath.Join(root, "install.lock"),
		SupervisorSource: supervisor,
		ExpectedUID:      uid,
		PackageSHA256:    strings.Repeat("1", 64),
		BootID:           func() (string, error) { return testBootID, nil },
		MainReadiness:    func(context.Context) error { return nil },
		MainObserver:     func(context.Context) ([]ProcessIdentity, error) { return []ProcessIdentity{{PID: 1}}, nil },
		StopAgent:        func(context.Context) error { return nil },
		ProveAgentAbsent: func(context.Context) error { return nil },
	}
	manager, err := NewProtectedInstallManager(options)
	if err != nil {
		t.Fatalf("compose manager: %v", err)
	}
	if manager.SourcePlan == nil || len(manager.SourcePlan.Sources) != 0 || len(manager.Sources) != 0 || len(manager.Inventory.StartSources) != 0 {
		t.Fatalf("production composition recaptured live launch sources: plan=%#v managerSources=%#v inventorySources=%#v", manager.SourcePlan, manager.Sources, manager.Inventory.StartSources)
	}
	if err := manager.Install(context.Background(), ""); !errors.Is(err, ErrRebootRequested) {
		t.Fatalf("install error=%v, want reboot request", err)
	}
	terminal, exists, err := manager.Journal.Load()
	if err != nil || !exists || terminal.State != InstallStateTerminal {
		t.Fatalf("terminal journal=%#v exists=%v err=%v", terminal, exists, err)
	}
	if got, err := os.ReadFile(dispatcher); err != nil || !bytes.Equal(got, BuildApprovedRecoveryTrampoline()) {
		t.Fatalf("dispatcher after install=%q err=%v", got, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy source after install err=%v", err)
	}
	if _, err := os.Stat(supervisor); err != nil {
		t.Fatalf("supervisor after install: %v", err)
	}
	fresh, err := NewProtectedInstallManager(options)
	if err != nil {
		t.Fatalf("recompose manager after source mutation: %v", err)
	}
	if fresh.SourcePlan == nil || len(fresh.SourcePlan.Sources) != 0 || len(fresh.Sources) != 0 || len(fresh.Inventory.StartSources) != 0 {
		t.Fatalf("recovery composition recaptured mutated launch sources: plan=%#v managerSources=%#v inventorySources=%#v", fresh.SourcePlan, fresh.Sources, fresh.Inventory.StartSources)
	}
	if fresh.ChainOriginal == nil {
		t.Fatal("recovery composition did not bind durable original-dispatcher chain")
	}
	fresh.ExecSupervisor = func(ctx context.Context, fd int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if fd < 0 {
			return errors.New("recovery did not retain install lock")
		}
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil {
			return err
		}
		if flags&unix.FD_CLOEXEC != 0 {
			return errors.New("fixture callback expected CLOEXEC cleared for handoff")
		}
		return nil
	}
	fresh.ChainOriginal = func(context.Context) error { return nil }
	if err := fresh.Recover(context.Background()); err != nil {
		t.Fatalf("recover error=%v", err)
	}
	if err := fresh.Uninstall(context.Background()); err != nil {
		t.Fatalf("uninstall error=%v", err)
	}
	manager = fresh
	restored, exists, err := manager.Journal.Load()
	if err != nil || !exists || restored.State != InstallStateRestored {
		t.Fatalf("restored journal=%#v exists=%v err=%v", restored, exists, err)
	}
	for path, want := range map[string][]byte{
		dispatcher: []byte("dispatcher-original"),
		legacy:     []byte("legacy-original"),
	} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("restored %s=%q err=%v want=%q", path, got, err, want)
		}
	}
	if _, err := os.Lstat(supervisor); !os.IsNotExist(err) {
		t.Fatalf("supervisor after uninstall err=%v", err)
	}
}

func TestRecoverResumesConcretePlanFromPreparedJournal(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	for path, data := range map[string][]byte{dispatcher: []byte("dispatcher"), legacy: []byte("legacy")} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	backupDir := filepath.Join(root, "backups")
	sources, err := CaptureStartSourceInventory([]string{dispatcher, legacy}, backupDir, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewSourceMutationPlan(sources, backupDir)
	plan.Supervisor = filepath.Join(root, "supervisor")
	plan.ExpectedUID = uid
	if err := plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(mainPath, []byte("main"), 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	main, err := inspectPathExpectation(mainPath, true)
	if err != nil {
		t.Fatal(err)
	}
	fifo, err := inspectPathExpectation(fifoPath, true)
	if err != nil {
		t.Fatal(err)
	}
	inventory := InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources}
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	prepared := InstallJournalRecord{Schema: 1, State: InstallStatePrepared, InstallBootID: testBootID, PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64), Inventory: inventory, Sources: sources}
	if err := journal.Replace(prepared); err != nil {
		t.Fatal(err)
	}
	manager := &InstallManager{
		Journal: journal, InstallLocker: hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid),
		OwnerLocker: hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid),
		SourcePlan:  plan, OwnerInitializer: func(context.Context) error { return nil },
		BootID: func() (string, error) { return testBootID, nil },
	}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, exists, err := journal.Load()
	if err != nil || !exists || record.State != InstallStateTerminal {
		t.Fatalf("recovered journal=%#v exists=%v err=%v", record, exists, err)
	}
}

func TestRecoverExecutesWithRetainedInstallLockAndRejectsWrongFD(t *testing.T) {
	root := t.TempDir()
	_ = os.Chmod(root, 0o700)
	uid := uint32(os.Getuid())
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	record := testTerminalInstallJournal()
	if err := replaceInstallJournalForTest(journal, record); err != nil {
		t.Fatal(err)
	}
	lock := NewInstallLocker(filepath.Join(root, "install.lock"), uid)
	called := false
	manager := &InstallManager{Journal: journal, InstallLocker: lock, ExecSupervisor: func(context.Context, int) error {
		called = true
		return nil
	}}
	if err := manager.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("retained-lock supervisor exec was not called")
	}
	wrong, err := os.Open(filepath.Join(root, "wrong-fd"))
	if err != nil {
		wrong, err = os.Create(filepath.Join(root, "wrong-fd"))
		if err != nil {
			t.Fatal(err)
		}
	}
	defer wrong.Close()
	called = false
	manager.InheritedLockFD = int(wrong.Fd())
	if err := manager.Recover(context.Background()); err == nil {
		t.Fatal("wrong inherited fd accepted")
	}
	if called {
		t.Fatal("wrong inherited fd reached supervisor exec")
	}
	_ = time.Second
	_ = unix.FD_CLOEXEC
}

func TestRecoverResumesConcretePlanAfterEveryDurablePhase(t *testing.T) {
	states := []InstallState{
		InstallStatePrepared,
		InstallStateTrampolineInstalled,
		InstallStateOwnerInitialized,
		InstallStateSourcesDisabled,
		InstallStateSupervisorInstalled,
	}
	for _, state := range states {
		state := state
		t.Run(string(state), func(t *testing.T) {
			fixture := newConcreteRecoveryFixture(t)
			if installStates[state] >= installStates[InstallStateTrampolineInstalled] {
				if err := fixture.plan.InstallTrampoline(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if installStates[state] >= installStates[InstallStateSourcesDisabled] {
				if err := fixture.plan.DisableSources(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if installStates[state] >= installStates[InstallStateSupervisorInstalled] {
				if err := fixture.plan.InstallSupervisor(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			fixture.record.State = state
			if err := replaceInstallJournalForTest(fixture.journal, fixture.record); err != nil {
				t.Fatal(err)
			}
			if err := fixture.manager.Recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			got, exists, err := fixture.journal.Load()
			if err != nil || !exists || got.State != InstallStateTerminal {
				t.Fatalf("recovered journal=%#v exists=%v err=%v", got, exists, err)
			}
			if gotBytes, err := os.ReadFile(fixture.dispatcher); err != nil || !bytes.Equal(gotBytes, BuildApprovedRecoveryTrampoline()) {
				t.Fatalf("dispatcher=%q err=%v", gotBytes, err)
			}
			if _, err := os.Lstat(fixture.legacy); !os.IsNotExist(err) {
				t.Fatalf("legacy source remains err=%v", err)
			}
			if _, err := os.Stat(fixture.supervisor); err != nil {
				t.Fatalf("supervisor missing: %v", err)
			}
		})
	}
}

type concreteRecoveryFixture struct {
	root, dispatcher, legacy, supervisor string
	plan                                 *SourceMutationPlan
	journal                              *InstallJournalStore
	record                               InstallJournalRecord
	manager                              *InstallManager
}

func newConcreteRecoveryFixture(t *testing.T) concreteRecoveryFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	supervisor := filepath.Join(root, "supervisor")
	for path, data := range map[string][]byte{dispatcher: []byte("dispatcher"), legacy: []byte("legacy")} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	backupDir := filepath.Join(root, "backups")
	sources, err := CaptureStartSourceInventory([]string{dispatcher, legacy}, backupDir, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewSourceMutationPlan(sources, backupDir)
	plan.Supervisor = supervisor
	plan.ExpectedUID = uid
	if err := plan.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "Main_MiSTer")
	if err := os.WriteFile(mainPath, []byte("main"), 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := mkfifoRuntimeTest(fifoPath); err != nil {
		t.Fatal(err)
	}
	main, err := inspectPathExpectation(mainPath, true)
	if err != nil {
		t.Fatal(err)
	}
	fifo, err := inspectPathExpectation(fifoPath, true)
	if err != nil {
		t.Fatal(err)
	}
	inventory := InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources}
	record := InstallJournalRecord{Schema: 1, State: InstallStatePrepared, InstallBootID: testBootID, PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64), Inventory: inventory, Sources: sources}
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	manager := &InstallManager{Journal: journal, InstallLocker: NewInstallLocker(filepath.Join(root, "install.lock"), uid), OwnerLocker: NewInstallLocker(filepath.Join(root, "owner.lock"), uid), SourcePlan: plan, OwnerInitializer: func(context.Context) error { return nil }, ChainOriginal: func(context.Context) error { return nil }, BootID: func() (string, error) { return testBootID, nil }}
	return concreteRecoveryFixture{root: root, dispatcher: dispatcher, legacy: legacy, supervisor: supervisor, plan: plan, journal: journal, record: record, manager: manager}
}

func TestInstallLockContentionAndCrashRelease(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	path := filepath.Join(root, "install.lock")
	lock := NewInstallLocker(path, uid)
	first, firstUnlock, err := lock.LockFile(context.Background())
	if err != nil || first == nil || firstUnlock == nil {
		t.Fatalf("first lock file=%v unlock=%v err=%v", first, firstUnlock, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	second, secondUnlock, err := lock.LockFile(ctx)
	cancel()
	if second != nil {
		_ = second.Close()
	}
	if secondUnlock != nil {
		_ = secondUnlock()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended lock err=%v, want deadline", err)
	}
	if err := firstUnlock(); err != nil {
		t.Fatal(err)
	}
	third, thirdUnlock, err := lock.LockFile(context.Background())
	if err != nil || third == nil || thirdUnlock == nil {
		t.Fatalf("released lock file=%v unlock=%v err=%v", third, thirdUnlock, err)
	}
	if err := thirdUnlock(); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestInstallLockCrashHelper$")
	command.Env = append(os.Environ(), "FOGCAST_LOCK_CRASH_HELPER=1", "FOGCAST_LOCK_CRASH_PATH="+path)
	if err := command.Run(); err == nil {
		t.Fatal("crash helper returned normally")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 42 {
		t.Fatalf("crash helper exit=%v, want 42", err)
	}
	afterCrash, afterCrashUnlock, err := lock.LockFile(context.Background())
	if err != nil || afterCrash == nil || afterCrashUnlock == nil {
		t.Fatalf("lock after crash file=%v unlock=%v err=%v", afterCrash, afterCrashUnlock, err)
	}
	if err := afterCrashUnlock(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallLockCrashHelper(t *testing.T) {
	if os.Getenv("FOGCAST_LOCK_CRASH_HELPER") != "1" {
		return
	}
	lock := NewInstallLocker(os.Getenv("FOGCAST_LOCK_CRASH_PATH"), uint32(os.Getuid()))
	file, _, err := lock.LockFile(context.Background())
	if err != nil || file == nil {
		os.Exit(41)
	}
	os.Exit(42)
}

func TestRecoverExecFailureReleasesInstallLock(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	record := testTerminalInstallJournal()
	if err := replaceInstallJournalForTest(journal, record); err != nil {
		t.Fatal(err)
	}
	lock := NewInstallLocker(filepath.Join(root, "install.lock"), uid)
	manager := &InstallManager{Journal: journal, InstallLocker: lock, SupervisorExecutable: filepath.Join(root, "does-not-exist")}
	if err := manager.Recover(context.Background()); err == nil {
		t.Fatal("missing supervisor executable unexpectedly succeeded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	deferred, err := lock.Lock(ctx)
	cancel()
	if err != nil || deferred == nil {
		t.Fatalf("install lock remained held after exec failure: unlock=%v err=%v", deferred, err)
	}
	if err := deferred(); err != nil {
		t.Fatal(err)
	}
}

func TestConcreteUninstallResumesAfterDispatcherRestoreBeforeJournal(t *testing.T) {
	fixture := newConcreteRecoveryFixture(t)
	if err := fixture.plan.InstallTrampoline(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.DisableSources(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.plan.InstallSupervisor(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.record.State = InstallStateTerminal
	if err := replaceInstallJournalForTest(fixture.journal, fixture.record); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	owner := hardwareowner.NewStore(filepath.Join(fixture.root, "owner.json"), uid)
	if err := owner.Replace(hardwareowner.Record{
		Schema: 1, State: hardwareowner.StateNormalMain, BootID: testBootID,
		GenerationHighWater: 1, ActiveSession: strings.Repeat("a", 32), ActiveGeneration: 1,
		ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain,
		ActiveLeases: hardwareowner.NormalLeases(), CandidateMode: hardwareowner.ModeNone,
		CandidateOwner: hardwareowner.OwnerNone, QuiescingOwner: hardwareowner.OwnerNone,
		RequestedResources: []string{}, FirstFailure: "",
	}); err != nil {
		t.Fatal(err)
	}
	fixture.manager.OwnerStore = owner
	fixture.manager.OwnerLocker = NewInstallLocker(filepath.Join(fixture.root, "owner.lock"), uid)
	fixture.manager.RemoveDispatcher = func(ctx context.Context) error {
		if err := fixture.plan.RemoveDispatcher(ctx); err != nil {
			return err
		}
		return errors.New("simulated crash after dispatcher restore")
	}
	if err := fixture.manager.Uninstall(context.Background()); err == nil {
		t.Fatal("uninstall crash fixture unexpectedly succeeded")
	}
	interrupted, exists, err := fixture.journal.Load()
	if err != nil || !exists || interrupted.State != InstallStateRestored {
		t.Fatalf("interrupted journal=%#v exists=%v err=%v", interrupted, exists, err)
	}
	fixture.manager.RemoveDispatcher = nil
	if err := fixture.manager.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, exists, err := fixture.journal.Load()
	if err != nil || !exists || restored.State != InstallStateRestored {
		t.Fatalf("restored journal=%#v exists=%v err=%v", restored, exists, err)
	}
}
