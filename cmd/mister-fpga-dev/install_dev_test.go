//go:build linux && fpgadev

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/fpgadev"
	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

type installFixtureRunner struct {
	commandRunner
	installs, recovers, uninstalls int
	packageRoot                    string
}

func (r *installFixtureRunner) Install(_ context.Context, packageRoot string) error {
	r.installs++
	r.packageRoot = packageRoot
	return nil
}
func (r *installFixtureRunner) Recover(context.Context) error   { r.recovers++; return nil }
func (r *installFixtureRunner) Uninstall(context.Context) error { r.uninstalls++; return nil }

func TestInstallCommandGrammarAndRootGate(t *testing.T) {
	runner := &installFixtureRunner{}
	for _, command := range []string{"install-profile", "recover-install", "uninstall-profile"} {
		var out, errOut bytes.Buffer
		if got := runInstallCommandWithPrivilege([]string{command}, &out, &errOut, runner, func() bool { return true }); got != exitOK {
			t.Fatalf("%s exit=%d out=%q err=%q", command, got, out.String(), errOut.String())
		}
	}
	if runner.installs != 1 || runner.recovers != 1 || runner.uninstalls != 1 {
		t.Fatalf("runner calls=%#v", runner)
	}
	var out, errOut bytes.Buffer
	if got := runInstallCommandWithPrivilege([]string{"install-profile"}, &out, &errOut, runner, func() bool { return false }); got != exitUsage {
		t.Fatalf("unprivileged exit=%d", got)
	}
	_ = fpgadev.ErrRebootRequested
}

func TestInstallCommandAcceptsWrappedRebootRequest(t *testing.T) {
	runner := &installFixtureRunner{}
	// The fixture's method cannot vary its return value, so use a focused
	// runner that wraps the manager's reboot sentinel.
	wrapper := &wrappedInstallRunner{commandRunner: runner}
	var out, errOut bytes.Buffer
	if got := runInstallCommandWithPrivilege([]string{"install-profile"}, &out, &errOut, wrapper, func() bool { return true }); got != exitOK {
		t.Fatalf("exit=%d out=%q err=%q", got, out.String(), errOut.String())
	}
}

func TestInstallCommandForwardsVerifiedPackageRoot(t *testing.T) {
	runner := &installFixtureRunner{}
	var out, errOut bytes.Buffer
	if got := runInstallCommandWithPrivilege([]string{"install-profile", "--package-root", "/tmp/private-package"}, &out, &errOut, runner, func() bool { return true }); got != exitOK {
		t.Fatalf("exit=%d out=%q err=%q", got, out.String(), errOut.String())
	}
	if runner.packageRoot != "/tmp/private-package" {
		t.Fatalf("package root=%q", runner.packageRoot)
	}
}

func TestProductionAgentStopperRequiresConclusiveAbsence(t *testing.T) {
	cases := []struct {
		name       string
		identities []fpgadev.ProcessIdentity
		scanErr    error
		wantErr    bool
	}{
		{name: "absent", wantErr: false},
		{name: "present", identities: []fpgadev.ProcessIdentity{{PID: 17}}, wantErr: true},
		{name: "ambiguous", scanErr: errors.New("process scan incomplete"), wantErr: true},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			stop := stopAgentOnlyWhenAbsent(fakeAgentObserver{identities: test.identities, err: test.scanErr})
			err := stop(context.Background())
			if (err != nil) != test.wantErr {
				t.Fatalf("stop error=%v wantErr=%t", err, test.wantErr)
			}
		})
	}
	if err := stopAgentOnlyWhenAbsent(nil)(context.Background()); err == nil {
		t.Fatal("nil observer unexpectedly proved agent absence")
	}
}

type fakeAgentObserver struct {
	identities []fpgadev.ProcessIdentity
	err        error
}

func (o fakeAgentObserver) SnapshotContext(context.Context) ([]fpgadev.ProcessIdentity, error) {
	return append([]fpgadev.ProcessIdentity(nil), o.identities...), o.err
}

// TestInitializeTaggedInstallCommandsUseConcreteManager exercises the tagged
// command dispatcher against the real journal/lock/source manager.  The
// command boundary is injected with a manager only so the test never invokes
// a target reboot or an ARM-only protected path; all three operations still
// execute the concrete InstallManager state machine and source plan.
func TestInitializeTaggedInstallCommandsUseConcreteManager(t *testing.T) {
	fixture := newConcreteInstallCommandFixture(t)
	runner := &concreteInstallCommandRunner{manager: fixture.manager}

	for _, command := range []string{"install-profile", "recover-install", "uninstall-profile"} {
		var out, errOut bytes.Buffer
		if got := runInstallCommandWithPrivilege([]string{command}, &out, &errOut, runner, func() bool { return true }); got != exitOK {
			t.Fatalf("%s exit=%d out=%q err=%q", command, got, out.String(), errOut.String())
		}
		want := "FOGCAST_FPGA_DEV_INSTALL command=" + command + " code=ok\n"
		if out.String() != want {
			t.Fatalf("%s output=%q want=%q", command, out.String(), want)
		}
		if errOut.Len() != 0 {
			t.Fatalf("%s stderr=%q", command, errOut.String())
		}
	}

	record, exists, err := fixture.manager.Journal.Load()
	if err != nil || !exists || record.State != fpgadev.InstallStateRestored {
		t.Fatalf("final journal=%#v exists=%v err=%v", record, exists, err)
	}
	for path, want := range map[string][]byte{
		fixture.dispatcher: fixture.dispatcherOriginal,
		fixture.legacy:     fixture.legacyOriginal,
	} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("restored source %s=%q err=%v want=%q", path, got, readErr, want)
		}
	}
	if _, err := os.Lstat(fixture.supervisor); !os.IsNotExist(err) {
		t.Fatalf("supervisor remains after uninstall: %v", err)
	}
}

type concreteInstallCommandRunner struct {
	commandRunner
	manager *fpgadev.InstallManager
}

func (r *concreteInstallCommandRunner) Install(ctx context.Context, packageRoot string) error {
	return r.manager.Install(ctx, packageRoot)
}

func (r *concreteInstallCommandRunner) Recover(ctx context.Context) error {
	return r.manager.Recover(ctx)
}

func (r *concreteInstallCommandRunner) Uninstall(ctx context.Context) error {
	return r.manager.Uninstall(ctx)
}

type concreteInstallCommandFixture struct {
	manager                            *fpgadev.InstallManager
	dispatcher, legacy, supervisor     string
	dispatcherOriginal, legacyOriginal []byte
}

func newConcreteInstallCommandFixture(t *testing.T) concreteInstallCommandFixture {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	dispatcher := filepath.Join(root, "dispatcher")
	legacy := filepath.Join(root, "legacy")
	supervisor := filepath.Join(root, "supervisor")
	dispatcherOriginal := []byte("dispatcher-original\n")
	legacyOriginal := []byte("legacy-original\n")
	for path, data := range map[string][]byte{dispatcher: dispatcherOriginal, legacy: legacyOriginal} {
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	backupDir := filepath.Join(root, "backups")
	sources, err := fpgadev.CaptureStartSourceInventory([]string{dispatcher, legacy}, backupDir, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	plan := fpgadev.NewSourceMutationPlan(sources, backupDir)
	plan.Supervisor = supervisor
	plan.ExpectedUID = uid

	mainPath := filepath.Join(root, "Main_MiSTer")
	mainBytes := []byte("main\n")
	if err := os.WriteFile(mainPath, mainBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(root, "MiSTer_cmd")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatal(err)
	}
	mainDigest := sha256.Sum256(mainBytes)
	main := fpgadev.PathExpectation{Path: mainPath, Kind: "regular", SHA256: hex.EncodeToString(mainDigest[:])}
	fifo := fpgadev.PathExpectation{Path: fifoPath, Kind: "fifo"}
	inventory := fpgadev.InventoryV1{Schema: 1, MainExecutable: main, MainFIFO: fifo, StartSources: sources}

	owner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	if err := owner.Replace(hardwareowner.Record{
		Schema: 1, State: hardwareowner.StateNormalMain, BootID: concreteInstallCommandBootID,
		GenerationHighWater: 1, ActiveSession: strings.Repeat("a", 32), ActiveGeneration: 1,
		ActiveMode: hardwareowner.ModeFPGANative, ActiveOwner: hardwareowner.OwnerCompatMain,
		ActiveLeases: hardwareowner.NormalLeases(), CandidateMode: hardwareowner.ModeNone,
		CandidateOwner: hardwareowner.OwnerNone, QuiescingOwner: hardwareowner.OwnerNone,
		RequestedResources: []string{}, FirstFailure: "",
	}); err != nil {
		t.Fatal(err)
	}
	manager := &fpgadev.InstallManager{
		Journal:       fpgadev.NewInstallJournal(filepath.Join(root, "journal.json"), uid),
		InstallLocker: hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid),
		OwnerStore:    owner,
		OwnerLocker:   hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid),
		BootID:        func() (string, error) { return concreteInstallCommandBootID, nil },
		PackageSHA256: strings.Repeat("1", 64), PreviousConfigSHA256: strings.Repeat("2", 64),
		Inventory: inventory, Sources: sources, SourcePlan: plan,
		MainReadiness:    func(context.Context) error { return nil },
		StopAgent:        func(context.Context) error { return nil },
		ProveAgentAbsent: func(context.Context) error { return nil },
	}
	manager.ExecSupervisor = func(context.Context, int) error { return nil }
	manager.ChainOriginal = func(context.Context) error { return nil }
	return concreteInstallCommandFixture{
		manager: manager, dispatcher: dispatcher, legacy: legacy, supervisor: supervisor,
		dispatcherOriginal: dispatcherOriginal, legacyOriginal: legacyOriginal,
	}
}

const concreteInstallCommandBootID = "12345678-1234-1234-1234-123456789abc"

type wrappedInstallRunner struct{ commandRunner }

func (r *wrappedInstallRunner) Install(context.Context, string) error {
	return fmt.Errorf("install reboot: %w", fpgadev.ErrRebootRequested)
}
func (r *wrappedInstallRunner) Recover(context.Context) error   { return nil }
func (r *wrappedInstallRunner) Uninstall(context.Context) error { return nil }
