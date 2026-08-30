//go:build linux && fpgadev

package fpgadev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

type task1AdmissionFixture struct {
	root       string
	owner      hardwareowner.Record
	ready      ReadyRecordV3
	readyStore ReadyStoreV3
	journal    *InstallJournalStore
	ownerStore *hardwareowner.Store
	install    *hardwareowner.Locker
	ownerLock  *hardwareowner.Locker
	profile    string
	starts     map[int]uint64
}

type task1ReadyStoreOverride struct {
	base   ReadyStoreV3
	record ReadyRecordV3
}

type task1JournalReaderOverride struct {
	record InstallJournalRecord
	exists bool
	err    error
}

func (r task1JournalReaderOverride) Load() (InstallJournalRecord, bool, error) {
	return r.record, r.exists, r.err
}

func (s task1ReadyStoreOverride) Load() (ReadyRecordV3, bool, error) {
	return s.record, true, nil
}

func (s task1ReadyStoreOverride) Replace(record ReadyRecordV3) error {
	return s.base.Replace(record)
}

func (s task1ReadyStoreOverride) Remove() error { return s.base.Remove() }

func newTask1AdmissionFixture(t *testing.T) task1AdmissionFixture {
	t.Helper()
	base := "/dev/shm/fogcast-task1"
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(base, "admission-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	journalRecord := testTerminalInstallJournal()
	journalRecord.InstallBootID = task7BootID
	if err := replaceInstallJournalForTest(journal, journalRecord); err != nil {
		t.Fatal(err)
	}
	owner := task7NormalMainRecord()
	ownerStore := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	if err := ownerStore.Replace(owner); err != nil {
		t.Fatal(err)
	}
	install := hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid)
	ownerLock := hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid)
	profile := strings.Repeat("b", 64)
	raw, err := journalRecord.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	ready := ReadyRecordV3{
		Schema:              3,
		BootID:              task7BootID,
		JournalSHA256:       sha256Hex(raw),
		OwnerSession:        owner.ActiveSession,
		OwnerGeneration:     owner.ActiveGeneration,
		ProfileSHA256:       profile,
		Capabilities:        DevelopmentCapabilities(),
		SupervisorPID:       101,
		SupervisorStartTime: 1001,
		MainPID:             102,
		MainStartTime:       1002,
		AgentPID:            103,
		AgentStartTime:      1003,
	}
	readyStore := NewReadyStoreV3(filepath.Join(root, "ready-v3.json"), uid)
	if err := readyStore.Replace(ready); err != nil {
		t.Fatal(err)
	}
	return task1AdmissionFixture{
		root: root, owner: owner, ready: ready, readyStore: readyStore,
		journal: journal, ownerStore: ownerStore, install: install, ownerLock: ownerLock,
		profile: profile, starts: map[int]uint64{101: 1001, 102: 1002, 103: 1003},
	}
}

func sha256Hex(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func (f task1AdmissionFixture) verifier(t *testing.T) *DevelopmentAdmissionVerifier {
	t.Helper()
	return NewDevelopmentAdmissionVerifier(
		f.readyStore,
		f.journal,
		func() (string, error) { return task7BootID, nil },
		func(context.Context) (string, error) { return f.profile, nil },
		func(ctx context.Context, _ string, pid int) (uint64, error) {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			start, ok := f.starts[pid]
			if !ok {
				return 0, fmt.Errorf("pid %d is absent", pid)
			}
			return start, nil
		},
	)
}

func (f task1AdmissionFixture) gate(verifier *DevelopmentAdmissionVerifier) *hardwareowner.Gate {
	return func() *hardwareowner.Gate {
		gate := hardwareowner.NewGate(
			f.ownerStore,
			f.ownerLock,
			NewMaintenanceGate(f.journal, f.install),
			task7BootID,
		)
		gate.InstallLocker = f.install
		gate.AdmissionVerifier = verifier
		return gate
	}()
}

func TestDevelopmentAdmissionVerifierChecksReadyJournalProfileAndPIDStartTimesWhileLocksHeld(t *testing.T) {
	fixture := newTask1AdmissionFixture(t)
	verifier := fixture.verifier(t)
	var checked sync.Once
	var lockErrs [2]error
	verifier.ProcessStartTime = func(ctx context.Context, _ string, pid int) (uint64, error) {
		checked.Do(func() {
			probeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			_, lockErrs[0] = fixture.install.Lock(probeCtx)
			_, lockErrs[1] = fixture.ownerLock.Lock(probeCtx)
		})
		return fixture.starts[pid], ctx.Err()
	}
	gate := fixture.gate(verifier)
	unlock, err := gate.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if unlock == nil {
		t.Fatal("successful admission returned nil unlock")
	}
	if lockErrs[0] == nil || lockErrs[1] == nil {
		t.Fatalf("admission verifier ran without both locks held: install=%v owner=%v", lockErrs[0], lockErrs[1])
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
}

func TestReadyRecordQuiescenceRequiresAndUsesCompleteAdmissionVerifier(t *testing.T) {
	fixture := newTask1AdmissionFixture(t)
	status := MaintenanceStatus{TerminalJournalSHA256: fixture.ready.JournalSHA256, Inventory: testTerminalInstallJournal().Inventory}
	evidence := &productionQualificationEvidence{}
	if err := (&readyRecordQuiescence{evidence: evidence}).VerifyPreDispatch(context.Background(), status, fixture.owner); !errors.Is(err, ErrRunnerConfiguration) {
		t.Fatalf("missing complete admission verifier error = %v", err)
	}
	verifier := fixture.verifier(t)
	if err := (&readyRecordQuiescence{evidence: evidence, admission: verifier}).VerifyPreDispatch(context.Background(), status, fixture.owner); err != nil {
		t.Fatal(err)
	}
	intent := task7IntentRecord(hardwareowner.PhaseIntentCommitted)
	if err := (&readyRecordQuiescence{evidence: evidence, admission: verifier}).VerifyPreDispatch(context.Background(), status, intent); err != nil {
		t.Fatalf("durable intent pre-FIFO revalidation failed: %v", err)
	}
	_, retainedOwner, _, _ := evidence.snapshot()
	if retainedOwner.State != hardwareowner.StateRecoveringIntent || retainedOwner.Phase != hardwareowner.PhaseIntentCommitted {
		t.Fatalf("dynamic evidence lost actual recovering owner: %#v", retainedOwner)
	}
	fixture.ready.ProfileSHA256 = strings.Repeat("d", 64)
	if err := fixture.readyStore.Replace(fixture.ready); err != nil {
		t.Fatal(err)
	}
	if err := (&readyRecordQuiescence{evidence: evidence, admission: verifier}).VerifyPreDispatch(context.Background(), status, fixture.owner); err == nil {
		t.Fatal("profile mismatch bypassed complete admission verifier")
	}
}

func TestReadyRecordQuiescenceAdmitsPreviousBootNormalMainWithoutReadyRecord(t *testing.T) {
	fixture := newTask1AdmissionFixture(t)
	status := MaintenanceStatus{TerminalJournalSHA256: fixture.ready.JournalSHA256, Inventory: testTerminalInstallJournal().Inventory}
	owner := fixture.owner
	owner.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
	if err := fixture.readyStore.Remove(); err != nil {
		t.Fatal(err)
	}
	quiescence := &readyRecordQuiescence{evidence: &productionQualificationEvidence{}, admission: fixture.verifier(t)}

	if err := quiescence.VerifyPriorBoot(context.Background(), status, owner, task7BootID); err != nil {
		t.Fatalf("VerifyPriorBoot() = %v, want terminal journal to admit stale normal owner without boot-local ready record", err)
	}
	owner.State = hardwareowner.StateRecoveringIntent
	owner.Phase = hardwareowner.PhaseIntentCommitted
	owner.RunID = strings.Repeat("a", 32)
	owner.GenerationHighWater++
	owner.CandidateSession = strings.Repeat("3", 32)
	owner.CandidateGeneration = owner.GenerationHighWater
	owner.CandidateMode = hardwareowner.ModeUpdating
	owner.CandidateOwner = hardwareowner.OwnerFPGADev
	owner.QuiescingOwner = hardwareowner.OwnerCompatMain
	owner.RequestedResources = hardwareowner.DevelopmentLeases()
	if err := quiescence.VerifyPriorBoot(context.Background(), status, owner, task7BootID); err == nil {
		t.Fatal("prior-boot verifier admitted a stale development intent")
	}
}

func TestRunnerPreflightPriorBootSucceedsWithReadyAbsentAndRemainsReadOnly(t *testing.T) {
	admission := newTask1AdmissionFixture(t)
	if err := admission.readyStore.Remove(); err != nil {
		t.Fatal(err)
	}
	fixture := newTask7RunnerFixture(t)
	fixture.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
	status := MaintenanceStatus{TerminalJournalSHA256: admission.ready.JournalSHA256, Inventory: testTerminalInstallJournal().Inventory}
	deps := fixture.dependencies()
	deps.maintenance = &task7MaintenanceGate{status: status}
	deps.quiescence = &readyRecordQuiescence{
		evidence:  &productionQualificationEvidence{},
		admission: admission.verifier(t),
	}

	if err := newFixtureRunner(deps).Preflight(context.Background(), fixture.request); err != nil {
		t.Fatalf("prior-boot Preflight() required absent ready record: %v", err)
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 || fixture.mapper.openCalls != 0 {
		t.Fatalf("prior-boot preflight mutated state: replace=%d fifo=%d mappings=%d", fixture.store.replaceCalls, fixture.fifo.calls, fixture.mapper.openCalls)
	}
}

func TestReadyRecordQuiescencePriorBootPostMainDoesNotRequireReadyRecord(t *testing.T) {
	fixture := newProductionEvidenceFixture(t)
	journal := priorBootJournalForProductionEvidence(t, &fixture)
	raw, err := journal.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	status := MaintenanceStatus{TerminalJournalSHA256: sha256Hex(raw), Inventory: fixture.inventory}
	previous := task7NormalMainRecord()
	previous.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
	current := task7IntentRecord(hardwareowner.PhaseLoadAttempted)
	verifier := &DevelopmentAdmissionVerifier{
		BootID:  func() string { return task7BootID },
		Journal: task1JournalReaderOverride{record: journal, exists: true},
	}
	quiescence := &readyRecordQuiescence{evidence: fixture.evidence, admission: verifier}
	if err := fixture.evidence.ready.Remove(); err != nil {
		t.Fatal(err)
	}

	proof, err := quiescence.VerifyPriorBootPostMain(context.Background(), status, previous, current, task7BootID)
	if err != nil {
		t.Fatalf("prior-boot post-Main proof required absent ready record: %v", err)
	}
	if err := proof.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*MaintenanceStatus, *hardwareowner.Record, *hardwareowner.Record, *string)
	}{
		{name: "journal digest changed", mutate: func(status *MaintenanceStatus, _, _ *hardwareowner.Record, _ *string) {
			status.TerminalJournalSHA256 = strings.Repeat("9", 64)
		}},
		{name: "previous owner is not normal", mutate: func(_ *MaintenanceStatus, previous, _ *hardwareowner.Record, _ *string) {
			previous.State = hardwareowner.StateRecoveryRequired
		}},
		{name: "active tuple changed", mutate: func(_ *MaintenanceStatus, _ *hardwareowner.Record, current *hardwareowner.Record, _ *string) {
			current.ActiveSession = strings.Repeat("8", 32)
		}},
		{name: "current boot changed", mutate: func(_ *MaintenanceStatus, _, _ *hardwareowner.Record, currentBootID *string) {
			*currentBootID = "fedcba98-7654-3210-fedc-ba9876543210"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			badStatus, badPrevious, badCurrent, badBootID := status, previous, current, task7BootID
			test.mutate(&badStatus, &badPrevious, &badCurrent, &badBootID)
			if _, err := quiescence.VerifyPriorBootPostMain(context.Background(), badStatus, badPrevious, badCurrent, badBootID); err == nil {
				t.Fatal("invalid prior-boot post-Main proof was admitted")
			}
		})
	}
}

func TestRunnerRunCommandPriorBootDoesNotFailPostMainSolelyBecauseReadyIsAbsent(t *testing.T) {
	evidence := newProductionEvidenceFixture(t)
	journal := priorBootJournalForProductionEvidence(t, &evidence)
	raw, err := journal.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	status := MaintenanceStatus{TerminalJournalSHA256: sha256Hex(raw), Inventory: evidence.inventory}
	verifier := &DevelopmentAdmissionVerifier{
		BootID:  func() string { return task7BootID },
		Journal: task1JournalReaderOverride{record: journal, exists: true},
	}
	if err := evidence.evidence.ready.Remove(); err != nil {
		t.Fatal(err)
	}
	fixture := newTask7RunnerFixture(t)
	fixture.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
	deps := fixture.dependencies()
	deps.maintenance = &task7MaintenanceGate{status: status}
	deps.quiescence = &readyRecordQuiescence{evidence: evidence.evidence, admission: verifier}
	programmingCalls := 0
	evidence.evidence.observeProgramming = func(context.Context) (ProgrammingAbsenceProof, error) {
		programmingCalls++
		return ProgrammingAbsenceProof{NoProgrammingProcess: true, NoProgrammingMapping: true}, nil
	}
	qualification, _, _ := validQualificationDeps()
	qualification.Subsystems = evidence.evidence
	qualification.PressedInput = evidence.evidence
	qualification.Programming = evidence.evidence
	concreteQualifier := newFixtureQualifier(qualification)
	deps.qualifier = &concreteQualifier

	result, err := newFixtureRunner(deps).RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("prior-boot RunCommand() failed after FIFO because ready is absent: result=%#v err=%v", result, err)
	}
	if fixture.fifo.calls != 1 || programmingCalls != 1 || result.PrimaryCode != string(CodeOK) {
		t.Fatalf("prior-boot RunCommand() = result:%#v fifo:%d programming:%d, want complete successful qualification", result, fixture.fifo.calls, programmingCalls)
	}
}

func TestReadyRecordQuiescenceSameBootPostMainStillRequiresReadyRecord(t *testing.T) {
	fixture := newProductionEvidenceFixture(t)
	quiescence := &readyRecordQuiescence{evidence: fixture.evidence}
	if err := fixture.evidence.ready.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := quiescence.VerifyPostMain(context.Background(), fixture.status, fixture.owner); err == nil || !strings.Contains(err.Error(), "ready record is absent") {
		t.Fatalf("same-boot post-Main proof admitted without ready record: %v", err)
	}
}

func TestReadyRecordQuiescenceRejectsPreviousBootNormalWithoutCurrentJournalProof(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(*task1AdmissionFixture, *DevelopmentAdmissionVerifier, *MaintenanceStatus, *hardwareowner.Record)
		currentBootID string
	}{
		{name: "current boot changed", currentBootID: "fedcba98-7654-3210-fedc-ba9876543210"},
		{name: "current boot invalid", currentBootID: "not-a-boot-id", mutate: func(_ *task1AdmissionFixture, verifier *DevelopmentAdmissionVerifier, _ *MaintenanceStatus, _ *hardwareowner.Record) {
			verifier.BootID = func() string { return "not-a-boot-id" }
		}},
		{name: "owner is current boot", mutate: func(_ *task1AdmissionFixture, _ *DevelopmentAdmissionVerifier, _ *MaintenanceStatus, owner *hardwareowner.Record) {
			owner.BootID = task7BootID
		}},
		{name: "journal absent", mutate: func(_ *task1AdmissionFixture, verifier *DevelopmentAdmissionVerifier, _ *MaintenanceStatus, _ *hardwareowner.Record) {
			verifier.Journal = task1JournalReaderOverride{}
		}},
		{name: "journal nonterminal", mutate: func(_ *task1AdmissionFixture, verifier *DevelopmentAdmissionVerifier, _ *MaintenanceStatus, _ *hardwareowner.Record) {
			record := testTerminalInstallJournal()
			record.State = InstallStateInstalled
			verifier.Journal = task1JournalReaderOverride{record: record, exists: true}
		}},
		{name: "journal digest mismatch", mutate: func(_ *task1AdmissionFixture, _ *DevelopmentAdmissionVerifier, status *MaintenanceStatus, _ *hardwareowner.Record) {
			status.TerminalJournalSHA256 = strings.Repeat("c", 64)
		}},
		{name: "journal inventory mismatch", mutate: func(_ *task1AdmissionFixture, _ *DevelopmentAdmissionVerifier, status *MaintenanceStatus, _ *hardwareowner.Record) {
			status.Inventory.Identity = "different-terminal-inventory-v1"
		}},
		{name: "journal load error", mutate: func(_ *task1AdmissionFixture, verifier *DevelopmentAdmissionVerifier, _ *MaintenanceStatus, _ *hardwareowner.Record) {
			verifier.Journal = task1JournalReaderOverride{err: errors.New("journal unavailable")}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask1AdmissionFixture(t)
			verifier := fixture.verifier(t)
			status := MaintenanceStatus{TerminalJournalSHA256: fixture.ready.JournalSHA256, Inventory: testTerminalInstallJournal().Inventory}
			owner := fixture.owner
			owner.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
			currentBootID := test.currentBootID
			if currentBootID == "" {
				currentBootID = task7BootID
			}
			if test.mutate != nil {
				test.mutate(&fixture, verifier, &status, &owner)
			}
			quiescence := &readyRecordQuiescence{evidence: &productionQualificationEvidence{}, admission: verifier}
			if err := quiescence.VerifyPriorBoot(context.Background(), status, owner, currentBootID); err == nil {
				t.Fatal("previous-boot normal owner admitted without exact current journal proof")
			}
		})
	}
}

func TestDevelopmentAdmissionVerifierRejectsReadyRecordMismatchOrAbsence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*task1AdmissionFixture)
	}{
		{name: "missing ready", mutate: func(f *task1AdmissionFixture) { _ = f.readyStore.Remove() }},
		{name: "boot", mutate: func(f *task1AdmissionFixture) { f.ready.BootID = "fedcba98-7654-3210-fedc-ba9876543210" }},
		{name: "journal", mutate: func(f *task1AdmissionFixture) { f.ready.JournalSHA256 = strings.Repeat("c", 64) }},
		{name: "owner session", mutate: func(f *task1AdmissionFixture) { f.ready.OwnerSession = strings.Repeat("3", 32) }},
		{name: "owner generation", mutate: func(f *task1AdmissionFixture) { f.ready.OwnerGeneration++ }},
		{name: "profile", mutate: func(f *task1AdmissionFixture) { f.ready.ProfileSHA256 = strings.Repeat("d", 64) }},
		{name: "capabilities", mutate: func(f *task1AdmissionFixture) { f.ready.Capabilities[0] = "wrong" }},
		{name: "supervisor pid", mutate: func(f *task1AdmissionFixture) { f.ready.SupervisorPID++ }},
		{name: "supervisor start", mutate: func(f *task1AdmissionFixture) { f.ready.SupervisorStartTime++ }},
		{name: "main pid", mutate: func(f *task1AdmissionFixture) { f.ready.MainPID++ }},
		{name: "main start", mutate: func(f *task1AdmissionFixture) { f.ready.MainStartTime++ }},
		{name: "agent pid", mutate: func(f *task1AdmissionFixture) { f.ready.AgentPID++ }},
		{name: "agent start", mutate: func(f *task1AdmissionFixture) { f.ready.AgentStartTime++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask1AdmissionFixture(t)
			test.mutate(&fixture)
			if test.name == "capabilities" {
				fixture.readyStore = task1ReadyStoreOverride{base: fixture.readyStore, record: fixture.ready}
			} else if test.name != "missing ready" {
				if err := fixture.readyStore.Replace(fixture.ready); err != nil {
					t.Fatal(err)
				}
			}
			gate := fixture.gate(fixture.verifier(t))
			if unlock, err := gate.Enter(context.Background()); err == nil || unlock != nil {
				t.Fatalf("mismatched ready record admitted: unlock=%v err=%v", unlock, err)
			}
		})
	}
}

func TestDevelopmentAdmissionVerifierHonorsContextCancellationAndInstallerContention(t *testing.T) {
	fixture := newTask1AdmissionFixture(t)
	gate := fixture.gate(fixture.verifier(t))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if unlock, err := gate.Enter(canceled); err == nil || unlock != nil {
		t.Fatalf("canceled admission returned unlock=%v err=%v", unlock, err)
	}
	held, err := fixture.install.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held() }()
	contention, cancelContention := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelContention()
	if unlock, err := gate.Enter(contention); err == nil || unlock != nil {
		t.Fatalf("installer contention was admitted: unlock=%v err=%v", unlock, err)
	}
}

func TestDevelopmentAdmissionDefaultProcReaderRejectsDeadStatesForEveryRole(t *testing.T) {
	roles := []struct {
		name string
		pid  int
	}{
		{name: "supervisor", pid: 101},
		{name: "Main", pid: 102},
		{name: "agent", pid: 103},
	}
	states := []struct {
		name  string
		state string
		dead  bool
	}{
		{name: "live", state: "R"},
		{name: "zombie", state: "Z", dead: true},
		{name: "dead upper", state: "X", dead: true},
		{name: "dead lower", state: "x", dead: true},
	}
	for _, role := range roles {
		for _, state := range states {
			t.Run(role.name+"/"+state.name, func(t *testing.T) {
				fixture := newTask1AdmissionFixture(t)
				procRoot, err := os.MkdirTemp("/dev/shm/fogcast-task1", "proc-reader-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(procRoot) })
				for _, process := range roles {
					processState := "R"
					if process.pid == role.pid {
						processState = state.state
					}
					writeProcStatFixture(t, procRoot, process.pid, fixture.starts[process.pid], processState)
				}
				verifier := fixture.verifier(t)
				verifier.ProcRoot = procRoot
				verifier.ProcessStartTime = nil
				unlock, err := fixture.gate(verifier).Enter(context.Background())
				if state.dead {
					if err == nil || unlock != nil {
						t.Fatalf("dead %s state was admitted: unlock=%v err=%v", state.state, unlock, err)
					}
					return
				}
				if err != nil || unlock == nil {
					t.Fatalf("live process state was rejected: unlock=%v err=%v", unlock, err)
				}
				if err := unlock(); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestProductionQualificationEvidenceUsesResolvedProcessDescriptorAndSocketState(t *testing.T) {
	fixture := newProductionEvidenceFixture(t)
	proof, err := fixture.evidence.Verify(context.Background())
	if err != nil {
		t.Fatalf("production evidence error=%v, want ReadyRecord/inventory success", err)
	}
	if err := proof.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProductionQualificationEvidenceAcceptsEachAbsentNetworkRoute(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(*InventoryV1)
	}{
		{name: "input_listen", set: func(inv *InventoryV1) {
			inv.InputListen = &NetworkExpectation{Network: "tcp", Address: "127.0.0.1:8080"}
		}},
		{name: "cast_rtp", set: func(inv *InventoryV1) { inv.CastRTP = &NetworkExpectation{Network: "udp", Address: "127.0.0.1:9000"} }},
		{name: "cast_control", set: func(inv *InventoryV1) {
			inv.CastControl = &NetworkExpectation{Network: "tcp", Address: "127.0.0.1:8081"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProductionEvidenceFixture(t)
			test.set(&fixture.inventory)
			fixture.status.Inventory = fixture.inventory
			fixture.evidence.setStatus(fixture.status, fixture.owner)
			if _, err := fixture.evidence.Verify(context.Background()); err != nil {
				t.Fatalf("configured endpoint absence rejected: %v", err)
			}
		})
	}
}

func TestProductionQualificationEvidenceRejectsEachPresentNetworkRoute(t *testing.T) {
	for _, test := range []struct {
		name    string
		network string
		address string
		file    string
		local   string
		set     func(*InventoryV1, *NetworkExpectation)
	}{
		{name: "input_listen", network: "tcp", address: "127.0.0.1:8080", file: "tcp", local: "0100007F:1F90", set: func(inv *InventoryV1, endpoint *NetworkExpectation) { inv.InputListen = endpoint }},
		{name: "cast_rtp", network: "udp", address: "127.0.0.1:9000", file: "udp", local: "0100007F:2328", set: func(inv *InventoryV1, endpoint *NetworkExpectation) { inv.CastRTP = endpoint }},
		{name: "cast_control", network: "tcp", address: "127.0.0.1:8081", file: "tcp", local: "0100007F:1F91", set: func(inv *InventoryV1, endpoint *NetworkExpectation) { inv.CastControl = endpoint }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProductionEvidenceFixture(t)
			test.set(&fixture.inventory, &NetworkExpectation{Network: test.network, Address: test.address})
			fixture.status.Inventory = fixture.inventory
			fixture.evidence.setStatus(fixture.status, fixture.owner)
			row := "sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n  0: " + test.local + " 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 777\n"
			if err := os.WriteFile(filepath.Join(fixture.procRoot, "net", test.file), []byte(row), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("socket:[777]", filepath.Join(fixture.procRoot, "60", "fd", "9")); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.evidence.Verify(context.Background()); err == nil || !strings.Contains(err.Error(), "unexpectedly present") {
				t.Fatalf("present endpoint was accepted: %v", err)
			}
		})
	}
}

func TestProductionQualificationEvidenceRejectsInventoriedWorkerDescriptor(t *testing.T) {
	fixture := newProductionEvidenceFixture(t)
	workerPath := filepath.Join(fixture.root, "input-uinput")
	if err := os.WriteFile(workerPath, []byte("uinput fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.inventory.InputUInput = &PathExpectation{Path: workerPath, Kind: "regular"}
	fixture.status.Inventory = fixture.inventory
	fixture.evidence.setStatus(fixture.status, fixture.owner)
	if err := os.MkdirAll(filepath.Join(fixture.procRoot, "61", "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(workerPath, filepath.Join(fixture.procRoot, "61", "fd", "5")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.evidence.Verify(context.Background()); err == nil {
		t.Fatal("inventoried worker descriptor holder was accepted after Main absence")
	}
}

func TestObserveLinuxProgrammingAbsence(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(root, "mister-fpga-dev")
	other := filepath.Join(root, "other")
	for _, path := range []string{tool, other} {
		if err := os.WriteFile(path, []byte(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeProcess := func(pid int, executable string, descriptors map[string]string, maps string) {
		t.Helper()
		process := filepath.Join(root, fmt.Sprint(pid))
		if err := os.MkdirAll(filepath.Join(process, "fd"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(executable, filepath.Join(process, "exe")); err != nil {
			t.Fatal(err)
		}
		for name, target := range descriptors {
			if err := os.Symlink(target, filepath.Join(process, "fd", name)); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(process, "maps"), []byte(maps), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	const selfPID = 41
	writeProcess(selfPID, tool, map[string]string{"3": "/dev/mem"}, "1000-2000 rw-s 00000000 00:00 0 /dev/mem\n")
	writeProcess(42, other, nil, "1000-2000 r-xp 00000000 00:00 0 "+other+"\n")
	proof, err := observeLinuxProgrammingAbsence(context.Background(), root, tool, selfPID)
	if err != nil || !proof.NoProgrammingProcess || !proof.NoProgrammingMapping {
		t.Fatalf("clean observation = %#v, %v", proof, err)
	}

	t.Run("other protected tool process", func(t *testing.T) {
		writeProcess(43, tool, nil, "")
		proof, err := observeLinuxProgrammingAbsence(context.Background(), root, tool, selfPID)
		if err != nil || proof.NoProgrammingProcess || !proof.NoProgrammingMapping {
			t.Fatalf("tool conflict = %#v, %v", proof, err)
		}
		if err := os.RemoveAll(filepath.Join(root, "43")); err != nil {
			t.Fatal(err)
		}
	})

	for _, test := range []struct {
		name        string
		descriptors map[string]string
		maps        string
	}{
		{name: "devmem descriptor", descriptors: map[string]string{"7": "/dev/mem"}},
		{name: "devmem mapping", maps: "3000-4000 rw-s 00000000 00:00 0 /dev/mem\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeProcess(44, other, test.descriptors, test.maps)
			proof, err := observeLinuxProgrammingAbsence(context.Background(), root, tool, selfPID)
			if err != nil || !proof.NoProgrammingProcess || proof.NoProgrammingMapping {
				t.Fatalf("mapping conflict = %#v, %v", proof, err)
			}
			if err := os.RemoveAll(filepath.Join(root, "44")); err != nil {
				t.Fatal(err)
			}
		})
	}

	t.Run("malformed maps fail closed", func(t *testing.T) {
		writeProcess(45, other, nil, "malformed\n")
		if _, err := observeLinuxProgrammingAbsence(context.Background(), root, tool, selfPID); err == nil {
			t.Fatal("malformed maps were accepted")
		}
	})

	t.Run("unavailable proc fails closed", func(t *testing.T) {
		if _, err := observeLinuxProgrammingAbsence(context.Background(), filepath.Join(root, "missing"), tool, selfPID); err == nil {
			t.Fatal("unavailable proc population was accepted")
		}
	})
}

func TestProductionQualificationEvidenceUsesIndependentProgrammingObservation(t *testing.T) {
	fixture := newProductionEvidenceFixture(t)
	want := ProgrammingAbsenceProof{NoProgrammingProcess: false, NoProgrammingMapping: true}
	calls := 0
	fixture.evidence.observeProgramming = func(context.Context) (ProgrammingAbsenceProof, error) {
		calls++
		return want, nil
	}
	got, err := fixture.evidence.VerifyProgramming(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != want || calls != 1 {
		t.Fatalf("programming observation = %#v calls=%d, want %#v/1", got, calls, want)
	}
}

func TestProductionQualificationEvidenceRejectsEachMissingLiveIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		pid  uint64
	}{
		{name: "supervisor", pid: 101},
		{name: "agent", pid: 102},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProductionEvidenceFixture(t)
			fixture.evidence.readProcessIdentity = func(ctx context.Context, _ string, pid int) (ProcessIdentity, error) {
				if err := ctx.Err(); err != nil {
					return ProcessIdentity{}, err
				}
				if uint64(pid) == test.pid {
					return ProcessIdentity{}, fmt.Errorf("%s process disappeared", test.name)
				}
				return fixture.processIdentity(pid), nil
			}
			if _, err := fixture.evidence.Verify(context.Background()); err == nil {
				t.Fatal("missing live identity was accepted")
			}
		})
	}
}

func TestProductionQualificationEvidenceRequiresValidatedRetainedBindingForStaticFields(t *testing.T) {
	fixture := newProductionEvidenceFixture(t)
	fixture.evidence.setBinding(ArtifactBinding{})
	if _, err := fixture.evidence.Verify(context.Background()); err == nil {
		t.Fatal("static resource proof was fabricated without a retained binding")
	}

	policy, err := (staticPolicyVerifier{}).Verify(context.Background(), fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	policy.NoForbiddenResources = false
	if _, err := fixture.evidence.VerifyWithPolicy(context.Background(), fixture.binding, policy); err == nil {
		t.Fatal("invalid retained static policy was accepted")
	}

	closed := fixture.binding
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.evidence.VerifyWithPolicy(context.Background(), closed, policy); err == nil {
		t.Fatal("closed retained artifact binding was accepted")
	}
}

type productionEvidenceFixture struct {
	root         string
	procRoot     string
	inventory    InventoryV1
	status       MaintenanceStatus
	owner        hardwareowner.Record
	binding      ArtifactBinding
	evidence     *productionQualificationEvidence
	identities   map[int]ProcessIdentity
	readIdentity func(context.Context, string, int) (ProcessIdentity, error)
}

func priorBootJournalForProductionEvidence(t *testing.T, fixture *productionEvidenceFixture) InstallJournalRecord {
	t.Helper()
	source := &fixture.inventory.StartSources[0]
	trampoline := BuildApprovedRecoveryTrampoline()
	if err := os.WriteFile(source.Path, trampoline, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(trampoline)
	source.DisabledState = "approved_trampoline"
	source.DisabledSHA256 = hex.EncodeToString(digest[:])
	fixture.status.Inventory = fixture.inventory
	fixture.evidence.setStatus(fixture.status, fixture.owner)
	journal := testTerminalInstallJournal()
	journal.Inventory = fixture.inventory
	journal.Sources = append([]SourceRecord(nil), fixture.inventory.StartSources...)
	return journal
}

func newProductionEvidenceFixture(t *testing.T) productionEvidenceFixture {
	t.Helper()
	paths := newInventoryResolutionFixture(t)
	procRoot := filepath.Join(paths.root, "proc")
	if err := os.MkdirAll(filepath.Join(procRoot, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "net", "tcp"), []byte("sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "net", "udp"), []byte("sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(procRoot, "60", "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveInventoryAt(context.Background(), paths.inventory, InventoryResolutionOptions{
		ProcRoot:               procRoot,
		Phase:                  InventoryPhaseMainAbsent,
		ScanProcessDescriptors: true,
		RequireNetworkEvidence: true,
		RequireMainFIFOOwner:   false,
	})
	if err != nil {
		t.Fatalf("resolve evidence fixture inventory: %v", err)
	}
	_ = resolved
	owner := task7NormalMainRecord()
	status := MaintenanceStatus{TerminalJournalSHA256: strings.Repeat("e", 64), Inventory: paths.inventory}
	identities := map[int]ProcessIdentity{
		101: {PID: 101, StartTime: 1001, Device: 501, Inode: 601, SHA256: strings.Repeat("1", 64)},
		102: {PID: 102, StartTime: 1002, Device: 502, Inode: 602, SHA256: strings.Repeat("2", 64)},
	}
	readyStore := NewReadyStoreV3(filepath.Join(paths.root, "ready", "ready-v3.json"), uint32(os.Getuid()))
	if err := readyStore.Replace(ReadyRecordV3{Schema: 3, BootID: task7BootID, JournalSHA256: status.TerminalJournalSHA256, OwnerSession: owner.ActiveSession, OwnerGeneration: owner.ActiveGeneration, ProfileSHA256: strings.Repeat("4", 64), Capabilities: DevelopmentCapabilities(), SupervisorPID: 101, SupervisorStartTime: 1001, MainPID: 103, MainStartTime: 1003, AgentPID: 102, AgentStartTime: 1002}); err != nil {
		t.Fatalf("write evidence fixture ready record: %v", err)
	}
	binding := qualificationArtifactBinding()
	mainExpected := ExecutableIdentity{Device: 901, Inode: 902, SHA256: strings.Repeat("5", 64)}
	observer := NewObserver(mainExpected, procRoot)
	observer.Scanner = &fakeProcessScanner{scans: [][]ProcessRecord{{}}}
	fixture := productionEvidenceFixture{root: paths.root, procRoot: procRoot, inventory: paths.inventory, status: status, owner: owner, binding: binding, identities: identities}
	fixture.readIdentity = func(ctx context.Context, _ string, pid int) (ProcessIdentity, error) {
		if err := ctx.Err(); err != nil {
			return ProcessIdentity{}, err
		}
		return fixture.processIdentity(pid), nil
	}
	fixture.evidence = newProductionQualificationEvidenceWithOptions(productionQualificationEvidenceOptions{
		observer: observer, ready: readyStore, bootID: func() (string, error) { return task7BootID, nil },
		procRoot: procRoot, processRoot: "/fixture-proc", allowNonRootFIFO: true, readProcessIdentity: func(ctx context.Context, _ string, pid int) (ProcessIdentity, error) {
			return fixture.readIdentity(ctx, "", pid)
		},
	})
	fixture.evidence.setStatus(status, owner)
	fixture.evidence.setBinding(binding)
	return fixture
}

func (f productionEvidenceFixture) processIdentity(pid int) ProcessIdentity {
	if identity, ok := f.identities[pid]; ok {
		return identity
	}
	return ProcessIdentity{PID: pid, StartTime: 9000, Device: 9001, Inode: 9002, SHA256: strings.Repeat("9", 64)}
}
