//go:build linux && fpgadev

package fpgadev

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

func TestProductionRunnerConstructorRejectsMissingAdapterBeforePreflight(t *testing.T) {
	missing := []struct {
		name  string
		clear func(*runnerDependencies)
	}{
		{name: "maintenance", clear: func(d *runnerDependencies) { d.maintenance = nil }},
		{name: "quiescence", clear: func(d *runnerDependencies) { d.quiescence = nil }},
		{name: "designation", clear: func(d *runnerDependencies) { d.designation = Designation{} }},
		{name: "profile", clear: func(d *runnerDependencies) { d.profile = nil }},
		{name: "store", clear: func(d *runnerDependencies) { d.store = nil }},
		{name: "locker", clear: func(d *runnerDependencies) { d.locker = nil }},
		{name: "artifact", clear: func(d *runnerDependencies) { d.artifact = nil }},
		{name: "bind", clear: func(d *runnerDependencies) { d.bind = nil }},
		{name: "fifo", clear: func(d *runnerDependencies) { d.fifo = nil }},
		{name: "observer", clear: func(d *runnerDependencies) { d.observer = nil }},
		{name: "qualifier", clear: func(d *runnerDependencies) { d.qualifier = nil }},
		{name: "mapper", clear: func(d *runnerDependencies) { d.mapper = nil }},
		{name: "results", clear: func(d *runnerDependencies) { d.results = nil }},
		{name: "readiness", clear: func(d *runnerDependencies) { d.readiness = nil }},
		{name: "reboot", clear: func(d *runnerDependencies) { d.reboot = nil }},
		{name: "clock", clear: func(d *runnerDependencies) { d.clock = nil }},
		{name: "boot id", clear: func(d *runnerDependencies) { d.bootID = nil }},
		{name: "privilege", clear: func(d *runnerDependencies) { d.privilege = nil }},
		{name: "tool", clear: func(d *runnerDependencies) { d.tool = nil }},
		{name: "revalidate", clear: func(d *runnerDependencies) { d.revalidate = nil }},
		{name: "dispatch path", clear: func(d *runnerDependencies) { d.dispatchPath = nil }},
		{name: "mailbox", clear: func(d *runnerDependencies) { d.mailbox, d.mailboxProgress = nil, nil }},
		{name: "new session", clear: func(d *runnerDependencies) { d.newSession = nil }},
	}
	for _, test := range missing {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			deps := fixture.dependencies()
			test.clear(&deps)
			runner, err := newProductionRunnerWithOptions(productionRunnerOptions{dependencies: deps})
			if err == nil || !errors.Is(err, ErrRunnerConfiguration) {
				t.Fatalf("constructor error = %v, want ErrRunnerConfiguration", err)
			}
			if runner != nil {
				t.Fatal("constructor returned a runner for incomplete adapters")
			}
		})
	}
}

func TestProductionRunnerOptionsPreflightAndRunReachAllAdapterBoundaries(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	runner, err := newProductionRunnerWithOptions(productionRunnerOptions{dependencies: fixture.dependencies()})
	if err != nil {
		t.Fatalf("construct production runner from host options: %v", err)
	}
	if runner.ConstructionError() != nil {
		t.Fatalf("ConstructionError() = %v", runner.ConstructionError())
	}
	if err := runner.Preflight(context.Background(), fixture.request); err != nil {
		t.Fatalf("Preflight() = %v", err)
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 || fixture.mapper.openCalls != 0 {
		t.Fatalf("preflight crossed mutating boundary: replaces=%d fifo=%d mappings=%d", fixture.store.replaceCalls, fixture.fifo.calls, fixture.mapper.openCalls)
	}
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if result.PrimaryCode != string(CodeOK) || fixture.fifo.calls != 1 || fixture.mapper.openCalls != 1 || fixture.results.createCalls != 1 || fixture.reboot.calls != 1 {
		t.Fatalf("run boundaries/result = code=%q fifo=%d mappings=%d results=%d reboot=%d", result.PrimaryCode, fixture.fifo.calls, fixture.mapper.openCalls, fixture.results.createCalls, fixture.reboot.calls)
	}
}

func TestRunnerPreflightIsReadOnlyAndUsesExactSuccessContract(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	runner := newFixtureRunner(fixture.dependencies())
	request := fixture.request

	if err := runner.Preflight(context.Background(), request); err != nil {
		t.Fatalf("Preflight() = %v", err)
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 || fixture.mapper.openCalls != 0 {
		t.Fatalf("preflight mutated state: replace=%d fifo=%d mappings=%d", fixture.store.replaceCalls, fixture.fifo.calls, fixture.mapper.openCalls)
	}
	if got := PreflightLine(CodeOK); got != "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n" {
		t.Fatalf("preflight success line = %q", got)
	}
}

func TestRunnerPreflightAcceptsProvenPreviousBootNormalMainReadOnly(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
	deps := fixture.dependencies()
	proof := &task7PriorBootQuiescence{}
	deps.quiescence = proof
	runner := newFixtureRunner(deps)

	if err := runner.Preflight(context.Background(), fixture.request); err != nil {
		t.Fatalf("Preflight() = %v, want proven previous-boot normal_main admission", err)
	}
	if proof.priorCalls != 1 || proof.currentCalls != 0 {
		t.Fatalf("quiescence proof calls = prior:%d current:%d, want 1/0", proof.priorCalls, proof.currentCalls)
	}
	if fixture.readiness.calls != 1 {
		t.Fatalf("Main readiness calls = %d, want current live proof", fixture.readiness.calls)
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 || fixture.mapper.openCalls != 0 {
		t.Fatalf("stale-boot preflight mutated state: replace=%d fifo=%d mappings=%d", fixture.store.replaceCalls, fixture.fifo.calls, fixture.mapper.openCalls)
	}
}

func TestRunnerPreflightAcceptsPreviousBootCompatMainRecoveryReadOnly(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	recovery := recoveryRecord(task7IntentRecord(hardwareowner.PhaseLoadAttempted), CodeMainHandoffTimeout)
	recovery.BootID = "40506b2a-7382-435d-a8f0-442689dcc288"
	fixture.store.record = recovery
	deps := fixture.dependencies()
	proof := &task7PriorBootQuiescence{}
	deps.quiescence = proof
	runner := newFixtureRunner(deps)

	if err := runner.Preflight(context.Background(), fixture.request); err != nil {
		t.Fatalf("Preflight() = %v, want proven previous-boot recovery_required/compat_main admission", err)
	}
	if proof.priorCalls != 1 || proof.currentCalls != 0 {
		t.Fatalf("quiescence proof calls = prior:%d current:%d, want 1/0", proof.priorCalls, proof.currentCalls)
	}
	if fixture.readiness.calls != 1 {
		t.Fatalf("Main readiness calls = %d, want current live proof", fixture.readiness.calls)
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 || fixture.mapper.openCalls != 0 {
		t.Fatalf("previous-boot recovery preflight mutated state: replace=%d fifo=%d mappings=%d", fixture.store.replaceCalls, fixture.fifo.calls, fixture.mapper.openCalls)
	}
}

func TestRunnerRunCommandReclaimsAttestedPreviousBootAtIntentBoundary(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	previousBoot := "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
	fixture.store.record.BootID = previousBoot
	previousSession := fixture.store.record.ActiveSession
	deps := fixture.dependencies()
	proof := &task7PriorBootQuiescence{}
	deps.quiescence = proof
	runner := newFixtureRunner(deps)

	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = result:%#v err:%v", result, err)
	}
	if len(fixture.store.history) == 0 {
		t.Fatal("RunCommand did not commit owner intent")
	}
	intent := fixture.store.history[0]
	if intent.State != hardwareowner.StateRecoveringIntent || intent.Phase != hardwareowner.PhaseIntentCommitted || intent.BootID != task7BootID {
		t.Fatalf("first durable owner = %#v, want current-boot recovering intent", intent)
	}
	if intent.ActiveSession != previousSession || intent.ActiveGeneration != 42 || intent.ActiveOwner != hardwareowner.OwnerCompatMain {
		t.Fatalf("reclaimed intent did not preserve attested compat owner: %#v", intent)
	}
	if proof.priorCalls != 2 || proof.currentCalls != 0 {
		t.Fatalf("quiescence proof calls = prior:%d current:%d, want prior admission and narrow pre-FIFO revalidation", proof.priorCalls, proof.currentCalls)
	}
	if proof.priorPostCalls != 1 || proof.postCalls != 0 {
		t.Fatalf("post-Main proof calls = prior:%d ordinary:%d, want migration-only proof", proof.priorPostCalls, proof.postCalls)
	}
	if fixture.readiness.calls != 2 {
		t.Fatalf("Main readiness calls = %d, want admission and immediate pre-FIFO revalidation", fixture.readiness.calls)
	}
}

func TestRunnerRunCommandReclaimsPreviousBootCompatMainRecoveryAtIntentBoundary(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	previousBoot := "40506b2a-7382-435d-a8f0-442689dcc288"
	recovery := recoveryRecord(task7IntentRecord(hardwareowner.PhaseLoadAttempted), CodeMainHandoffTimeout)
	recovery.BootID = previousBoot
	previousSession := recovery.ActiveSession
	previousGeneration := recovery.ActiveGeneration
	previousHighWater := recovery.GenerationHighWater
	previousCandidate := recovery.CandidateSession
	fixture.store.record = recovery
	deps := fixture.dependencies()
	proof := &task7PriorBootQuiescence{}
	deps.quiescence = proof
	deps.newSession = func() (string, error) { return strings.Repeat("4", 32), nil }
	runner := newFixtureRunner(deps)

	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = result:%#v err:%v", result, err)
	}
	if len(fixture.store.history) == 0 {
		t.Fatal("RunCommand did not commit owner intent")
	}
	intent := fixture.store.history[0]
	if intent.State != hardwareowner.StateRecoveringIntent || intent.Phase != hardwareowner.PhaseIntentCommitted || intent.BootID != task7BootID {
		t.Fatalf("first durable owner = %#v, want current-boot recovering intent", intent)
	}
	if intent.ActiveSession != previousSession || intent.ActiveGeneration != previousGeneration || intent.ActiveOwner != hardwareowner.OwnerCompatMain {
		t.Fatalf("reclaimed intent did not preserve attested compat owner: %#v", intent)
	}
	if intent.CandidateSession == previousCandidate || intent.CandidateGeneration != previousHighWater+1 || intent.FirstFailure != "" {
		t.Fatalf("reclaimed intent did not allocate a fresh development candidate: %#v", intent)
	}
	for i, record := range fixture.store.history {
		if record.BootID != task7BootID {
			t.Fatalf("later fence %d retained stale boot ID: %#v", i, record)
		}
	}
	if proof.priorCalls != 2 || proof.currentCalls != 0 || proof.priorPostCalls != 1 || proof.postCalls != 0 {
		t.Fatalf("prior-boot proofs = admission:%d current:%d post:%d ordinary-post:%d", proof.priorCalls, proof.currentCalls, proof.priorPostCalls, proof.postCalls)
	}
}

func TestRunnerRechecksPriorBootMainReadinessImmediatelyBeforeFIFO(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
	fixture.readiness.err = errors.New("Main readiness changed before dispatch")
	fixture.readiness.failAt = 2
	deps := fixture.dependencies()
	proof := &task7PriorBootQuiescence{}
	deps.quiescence = proof

	result, err := newFixtureRunner(deps).RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v, want handled post-intent recovery", err)
	}
	if proof.priorCalls != 2 || fixture.readiness.calls != 2 {
		t.Fatalf("prior-boot pre-FIFO proofs = journal:%d readiness:%d, want 2/2", proof.priorCalls, fixture.readiness.calls)
	}
	if fixture.fifo.calls != 0 || result.Phase != ResultPhaseIntentCommitted || result.PrimaryCode != string(CodeLoadDispatchFailed) {
		t.Fatalf("lost live-Main proof reached FIFO/result=%#v fifo=%d", result, fixture.fifo.calls)
	}
	fixture.store.mu.Lock()
	final := cloneTask7Record(fixture.store.record)
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired || final.Phase != hardwareowner.PhaseIntentCommitted {
		t.Fatalf("lost live-Main proof final owner = %#v, want fenced intent recovery", final)
	}
}

func TestRunnerPreflightRejectsUnreclaimableOwnerRecords(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*task7RunnerFixture)
	}{
		{name: "absent", configure: func(f *task7RunnerFixture) { f.store.record = hardwareowner.Record{} }},
		{name: "invalid", configure: func(f *task7RunnerFixture) { f.store.record.ActiveOwner = hardwareowner.OwnerNone }},
		{name: "previous boot fpgadev active", configure: func(f *task7RunnerFixture) {
			f.store.record = task7ActiveRecord(hardwareowner.PhaseLeaseActive)
			f.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
		}},
		{name: "previous boot recovering intent", configure: func(f *task7RunnerFixture) {
			f.store.record = task7IntentRecord(hardwareowner.PhaseIntentCommitted)
			f.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
		}},
		{name: "previous boot compat recovery without development candidate", configure: func(f *task7RunnerFixture) {
			f.store.record = recoveryRecord(task7NormalMainRecord(), CodeMainHandoffTimeout)
			f.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
		}},
		{name: "previous boot compat recovery reuses active session", configure: func(f *task7RunnerFixture) {
			f.store.record = recoveryRecord(task7IntentRecord(hardwareowner.PhaseLoadAttempted), CodeMainHandoffTimeout)
			f.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
			f.store.record.CandidateSession = f.store.record.ActiveSession
		}},
		{name: "previous boot compat recovery candidate is not newer", configure: func(f *task7RunnerFixture) {
			f.store.record = recoveryRecord(task7IntentRecord(hardwareowner.PhaseLoadAttempted), CodeMainHandoffTimeout)
			f.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
			f.store.record.CandidateGeneration = f.store.record.ActiveGeneration
			f.store.record.GenerationHighWater = f.store.record.ActiveGeneration
		}},
		{name: "same boot fpgadev active", configure: func(f *task7RunnerFixture) {
			f.store.record = task7ActiveRecord(hardwareowner.PhaseLeaseActive)
		}},
		{name: "same boot recovering intent", configure: func(f *task7RunnerFixture) {
			f.store.record = task7IntentRecord(hardwareowner.PhaseIntentCommitted)
		}},
		{name: "same boot compat Main recovery required", configure: func(f *task7RunnerFixture) {
			f.store.record = recoveryRecord(task7IntentRecord(hardwareowner.PhaseLoadAttempted), CodeMainHandoffTimeout)
		}},
		{name: "previous boot Main readiness missing", configure: func(f *task7RunnerFixture) {
			f.store.record.BootID = "222959a0-e21d-4d8b-a217-a6bd6367b2fb"
			f.readiness.err = errors.New("Main readiness is absent")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			test.configure(fixture)
			deps := fixture.dependencies()
			proof := &task7PriorBootQuiescence{}
			deps.quiescence = proof
			err := newFixtureRunner(deps).Preflight(context.Background(), fixture.request)
			if err == nil || !errors.Is(err, ErrRunPreflight) {
				t.Fatalf("Preflight() = %v, want ownership_conflict", err)
			}
			failure, ok := err.(*Failure)
			if !ok || failure.Code != CodeOwnershipConflict {
				t.Fatalf("Preflight failure = %#v, want ownership_conflict", err)
			}
			if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 {
				t.Fatalf("rejected owner mutated state: replace=%d fifo=%d", fixture.store.replaceCalls, fixture.fifo.calls)
			}
		})
	}
}

func TestRunnerRunCommandRejectsSameBootCompatMainRecoveryBeforeIntent(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.store.record = recoveryRecord(task7IntentRecord(hardwareowner.PhaseLoadAttempted), CodeMainHandoffTimeout)

	result, err := newFixtureRunner(fixture.dependencies()).RunCommand(context.Background(), fixture.request)
	if err == nil || !errors.Is(err, ErrRunPreflight) {
		t.Fatalf("RunCommand() = result:%#v err:%v, want pre-intent ownership conflict", result, err)
	}
	failure, ok := err.(*Failure)
	if !ok || failure.Code != CodeOwnershipConflict {
		t.Fatalf("RunCommand failure = %#v, want ownership_conflict", err)
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 || fixture.mapper.openCalls != 0 || fixture.results.createCalls != 0 {
		t.Fatalf("same-boot conflict crossed intent boundary: replace=%d fifo=%d mappings=%d results=%d", fixture.store.replaceCalls, fixture.fifo.calls, fixture.mapper.openCalls, fixture.results.createCalls)
	}
}

func TestRunnerPreflightRejectsPreviousBootRecoveryRunIDReuseReadOnly(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	recovery := recoveryRecord(task7IntentRecord(hardwareowner.PhaseLoadAttempted), CodeMainHandoffTimeout)
	recovery.BootID = "40506b2a-7382-435d-a8f0-442689dcc288"
	fixture.store.record = recovery
	fixture.manifest.RunID = recovery.RunID
	deps := fixture.dependencies()
	deps.quiescence = &task7PriorBootQuiescence{}

	err := newFixtureRunner(deps).Preflight(context.Background(), fixture.request)
	if err == nil || !errors.Is(err, ErrRunPreflight) {
		t.Fatalf("Preflight() = %v, want stale RunID rejection", err)
	}
	failure, ok := err.(*Failure)
	if !ok || failure.Code != CodeResultConflict {
		t.Fatalf("Preflight failure = %#v, want result_conflict", err)
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 || fixture.mapper.openCalls != 0 || fixture.results.createCalls != 0 {
		t.Fatalf("stale RunID crossed a mutation boundary: replace=%d fifo=%d mappings=%d results=%d", fixture.store.replaceCalls, fixture.fifo.calls, fixture.mapper.openCalls, fixture.results.createCalls)
	}
}

func TestMaintenanceGateAndQuiescenceAreSemanticBoundaries(t *testing.T) {
	var _ MaintenanceGate = semanticMaintenanceFixture{}
	var _ QuiescenceVerifier = semanticQuiescenceFixture{}
}

func TestRunnerRejectsUnattestedMaintenanceStatusBeforeOwnerLock(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	deps := fixture.dependencies()
	deps.maintenance = emptyMaintenanceFixture{}
	runner := newFixtureRunner(deps)
	_, err := runner.RunCommand(context.Background(), fixture.request)
	if err == nil || !errors.Is(err, ErrRunnerConfiguration) {
		t.Fatalf("RunCommand() = %v, want configuration failure", err)
	}
	if fixture.locker.lockCalls != 0 {
		t.Fatalf("owner lock calls = %d, want zero", fixture.locker.lockCalls)
	}
}

func TestMaintenanceStatusRejectsMissingInventoryAndNoncanonicalJournalDigest(t *testing.T) {
	missing := MaintenanceStatus{TerminalJournalSHA256: strings.Repeat("a", 64), Inventory: InventoryV1{}}
	if err := missing.Validate(); err == nil {
		t.Fatal("empty inventory status was accepted")
	}
	upper := task7ValidMaintenanceStatus()
	upper.TerminalJournalSHA256 = strings.Repeat("A", 64)
	if err := upper.Validate(); err == nil {
		t.Fatal("uppercase journal digest was accepted")
	}
}

func TestRunnerCleansMaintenanceUnlockReturnedAlongsideEnterError(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	enterErr := errors.New("maintenance enter failed")
	unlockErr := errors.New("maintenance unlock after enter failure")
	gate := &task7MaintenanceGate{status: task7ValidMaintenanceStatus(), enterErr: enterErr, unlockErr: unlockErr}
	deps := fixture.dependencies()
	deps.maintenance = gate
	runner := newFixtureRunner(deps)
	_, err := runner.RunCommand(context.Background(), fixture.request)
	if !errors.Is(err, enterErr) || !errors.Is(err, unlockErr) {
		t.Fatalf("RunCommand() = %v, want joined maintenance enter/unlock errors", err)
	}
	if gate.unlockCalls != 1 {
		t.Fatalf("maintenance unlock calls = %d, want one", gate.unlockCalls)
	}
}

func TestRunnerPreservesMaintenanceUnlockErrorAfterStatusValidationFailure(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	unlockErr := errors.New("maintenance unlock failed")
	gate := &task7MaintenanceGate{status: MaintenanceStatus{}, unlockErr: unlockErr}
	deps := fixture.dependencies()
	deps.maintenance = gate
	runner := newFixtureRunner(deps)
	_, err := runner.RunCommand(context.Background(), fixture.request)
	if !errors.Is(err, unlockErr) {
		t.Fatalf("RunCommand() = %v, want maintenance unlock error", err)
	}
	if gate.unlockCalls != 1 {
		t.Fatalf("maintenance unlock calls = %d, want one", gate.unlockCalls)
	}
}

func TestRunnerRechecksQuiescenceImmediatelyBeforeFIFO(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	proofLost := errors.New("agent became live again")
	var calls []hardwareowner.Record
	deps := fixture.dependencies()
	deps.quiescence = task7Quiescence{
		pre: func(_ context.Context, status MaintenanceStatus, record hardwareowner.Record) error {
			want := task7ValidMaintenanceStatus()
			if status.TerminalJournalSHA256 != want.TerminalJournalSHA256 || !status.Inventory.Equal(want.Inventory) {
				return errors.New("maintenance status changed")
			}
			calls = append(calls, record)
			if len(calls) == 2 {
				return proofLost
			}
			return nil
		},
	}
	deps.revalidate = func(context.Context, *ArtifactBinding) error { return nil }
	runner := newFixtureRunner(deps)
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("pre-dispatch proof calls = %d, want initial admission and immediately-before-FIFO checks", len(calls))
	}
	if calls[0].State != hardwareowner.StateNormalMain || calls[1].State != hardwareowner.StateRecoveringIntent || calls[1].Phase != hardwareowner.PhaseIntentCommitted {
		t.Fatalf("semantic proof records = %#v, want normal_main then intent_committed", calls)
	}
	if fixture.fifo.calls != 0 || result.Phase != ResultPhaseIntentCommitted || result.PrimaryCode != string(CodeLoadDispatchFailed) {
		t.Fatalf("invalidated pre-dispatch proof reached FIFO/result=%#v fifo=%d", result, fixture.fifo.calls)
	}
	fixture.store.mu.Lock()
	final := fixture.store.record
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired || final.Phase != hardwareowner.PhaseIntentCommitted {
		t.Fatalf("invalidated proof final owner = %#v", final)
	}
}

func TestRunnerRejectsHostileOwnerAndSemanticProofsAtTheirAdmissionGates(t *testing.T) {
	staleSupervisor := errors.New("supervisor is no longer quiescent")
	staleAgent := errors.New("agent is no longer quiescent")
	inventoryMismatch := errors.New("inventory identity does not match")
	tests := []struct {
		name           string
		configure      func(*task7RunnerFixture, *task7MaintenanceGate)
		semanticReject func(MaintenanceStatus, hardwareowner.Record) error
		wantQuiescence int
		wantOwnerLock  int
	}{
		{
			name: "absent owner record",
			configure: func(f *task7RunnerFixture, _ *task7MaintenanceGate) {
				f.store.record = hardwareowner.Record{}
			},
			wantOwnerLock: 1,
		},
		{
			name: "nonterminal maintenance",
			configure: func(_ *task7RunnerFixture, gate *task7MaintenanceGate) {
				gate.enterErr = errors.New("install journal is nonterminal")
			},
		},
		{
			name: "wrong boot without prior-boot proof",
			configure: func(f *task7RunnerFixture, _ *task7MaintenanceGate) {
				f.store.record.BootID = "fedcba98-7654-3210-fedc-ba9876543210"
			},
			wantOwnerLock: 1,
		},
		{
			name: "non-normal owner after semantic admission",
			configure: func(f *task7RunnerFixture, _ *task7MaintenanceGate) {
				f.store.record = task7RecoveryRequiredRecord()
			},
			wantQuiescence: 1, wantOwnerLock: 1,
		},
		{
			name:           "stale supervisor proof",
			semanticReject: func(MaintenanceStatus, hardwareowner.Record) error { return staleSupervisor },
			wantQuiescence: 1, wantOwnerLock: 1,
		},
		{
			name:           "stale agent proof",
			semanticReject: func(MaintenanceStatus, hardwareowner.Record) error { return staleAgent },
			wantQuiescence: 1, wantOwnerLock: 1,
		},
		{
			name: "inventory identity mismatch",
			configure: func(_ *task7RunnerFixture, gate *task7MaintenanceGate) {
				gate.status.Inventory = InventoryV1{Identity: "different-opaque-inventory"}
			},
			semanticReject: func(got MaintenanceStatus, _ hardwareowner.Record) error {
				if !got.Inventory.Equal(task7ValidMaintenanceStatus().Inventory) {
					return inventoryMismatch
				}
				return nil
			},
			wantQuiescence: 1, wantOwnerLock: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			gate := &task7MaintenanceGate{status: task7ValidMaintenanceStatus()}
			if test.configure != nil {
				test.configure(fixture, gate)
			}
			preCalls := 0
			deps := fixture.dependencies()
			deps.maintenance = gate
			deps.quiescence = task7Quiescence{pre: func(_ context.Context, status MaintenanceStatus, record hardwareowner.Record) error {
				preCalls++
				if test.semanticReject != nil {
					return test.semanticReject(status, record)
				}
				return nil
			}}
			runner := newFixtureRunner(deps)
			result, err := runner.RunCommand(context.Background(), fixture.request)
			if err == nil || !errors.Is(err, ErrRunPreflight) || result.PrimaryCode != string(CodeOwnershipConflict) {
				t.Fatalf("RunCommand() = result:%#v err:%v, want pre-intent ownership rejection", result, err)
			}
			if fixture.fifo.calls != 0 || fixture.store.replaceCalls != 0 {
				t.Fatalf("hostile %s reached durable intent/FIFO: replaces=%d fifo=%d", test.name, fixture.store.replaceCalls, fixture.fifo.calls)
			}
			if preCalls != test.wantQuiescence {
				t.Fatalf("hostile %s semantic proof calls=%d, want %d", test.name, preCalls, test.wantQuiescence)
			}
			if fixture.locker.lockCalls != test.wantOwnerLock {
				t.Fatalf("hostile %s owner lock calls=%d, want %d", test.name, fixture.locker.lockCalls, test.wantOwnerLock)
			}
			if gate.enterCalls != 1 || gate.unlockCalls != 1 {
				t.Fatalf("hostile %s maintenance lifecycle=%d enters/%d releases, want one each", test.name, gate.enterCalls, gate.unlockCalls)
			}
		})
	}
}

func TestTask7RunnerLifecycleAbruptCrashBeforeAndAfterEachDurableReplacement(t *testing.T) {
	transitions := []string{"intent", "load-attempted", "no-owner", "lease-active", "hello", "data", "end", "done", "recovery"}
	for replacement, name := range transitions {
		replacement, name := replacement+1, name
		for _, boundary := range []string{"before", "after"} {
			boundary := boundary
			t.Run(name+"/"+boundary, func(t *testing.T) {
				root := t.TempDir()
				if err := os.Chmod(root, 0o700); err != nil {
					t.Fatal(err)
				}
				owner := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uint32(os.Getuid()))
				if err := owner.Replace(task7NormalMainRecord()); err != nil {
					t.Fatalf("seed real owner store: %v", err)
				}
				point := boundary + "-" + strconv.Itoa(replacement)
				command := exec.Command(os.Args[0], "-test.run=^TestTask7RunnerLifecycleCrashHelper$")
				command.Env = append(os.Environ(), "FOGCAST_TASK7_RUNNER_CRASH_HELPER=1", "FOGCAST_TASK7_RUNNER_CRASH_ROOT="+root, "FOGCAST_TASK7_RUNNER_CRASH_POINT="+point)
				if err := command.Run(); err == nil {
					t.Fatal("crash helper returned normally")
				} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 73 {
					t.Fatalf("crash helper exit = %v, want abrupt exit 73", err)
				}

				got, exists, err := owner.Load()
				if err != nil || !exists {
					t.Fatalf("load real owner store after crash = %#v exists=%v err=%v", got, exists, err)
				}
				states := task7CrashLifecycleRecords(task7CrashRunID())
				wantIndex := replacement - 1
				if boundary == "after" {
					wantIndex = replacement
				}
				if want := states[wantIndex]; !reflect.DeepEqual(got, want) {
					t.Fatalf("durable owner after %s = %#v, want %#v", point, got, want)
				}

				results := NewResultStore(filepath.Join(root, "results"), uint32(os.Getuid()))
				if _, statErr := os.Stat(results.PathFor(task7CrashRunID())); !os.IsNotExist(statErr) {
					t.Fatalf("abrupt lifecycle crash created a result: %v", statErr)
				}
				if _, statErr := os.Stat(filepath.Join(root, "reboot.marker")); !os.IsNotExist(statErr) {
					t.Fatalf("abrupt lifecycle crash requested reboot: %v", statErr)
				}

				locker := hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uint32(os.Getuid()))
				lockCtx, cancel := context.WithTimeout(context.Background(), time.Second)
				unlock, lockErr := locker.Lock(lockCtx)
				cancel()
				if lockErr != nil || unlock == nil {
					t.Fatalf("kernel owner lock remained held after %s: unlock=%v err=%v", point, unlock, lockErr)
				}
				if err := unlock(); err != nil {
					t.Fatalf("release probe owner lock: %v", err)
				}

				probe := newTask7CrashLifecycleRunner(t, root, "")
				preflightErr := probe.Preflight(context.Background(), Request{ManifestPath: "/anonymous/manifest.json", ArtifactPath: "/anonymous/top.rbf"})
				if replacement == 1 && boundary == "before" {
					if preflightErr != nil {
						t.Fatalf("pre-intent crash did not permit fresh admission: %v", preflightErr)
					}
				} else if preflightErr == nil || !errors.Is(preflightErr, ErrRunPreflight) {
					t.Fatalf("post-intent crash admitted a fresh run: %v", preflightErr)
				}
			})
		}
	}
}

func TestTask7RunnerLifecycleCrashHelper(t *testing.T) {
	if os.Getenv("FOGCAST_TASK7_RUNNER_CRASH_HELPER") != "1" {
		return
	}
	root, point := os.Getenv("FOGCAST_TASK7_RUNNER_CRASH_ROOT"), os.Getenv("FOGCAST_TASK7_RUNNER_CRASH_POINT")
	if root == "" || point == "" {
		os.Exit(72)
	}
	runner := newTask7CrashLifecycleRunner(t, root, point)
	_, err := runner.RunCommand(context.Background(), Request{ManifestPath: "/anonymous/manifest.json", ArtifactPath: "/anonymous/top.rbf"})
	t.Fatalf("crash point %q returned from real Runner lifecycle: %v", point, err)
}

func task7CrashRunID() string { return strings.Repeat("1", 32) }

func task7CrashLifecycleRecords(runID string) []hardwareowner.Record {
	normal := task7NormalMainRecord()
	intent := task7IntentRecord(hardwareowner.PhaseIntentCommitted)
	intent.RunID = runID
	loadAttempted := intent
	loadAttempted.Phase = hardwareowner.PhaseLoadAttempted
	noOwner := task7NoOwnerRecord()
	noOwner.RunID = runID
	lease := task7ActiveRecord(hardwareowner.PhaseLeaseActive)
	lease.RunID = runID
	hello := task7ActiveRecord(hardwareowner.PhaseHelloObserved)
	hello.RunID = runID
	data := task7ActiveRecord(hardwareowner.PhaseMessagePartial)
	data.RunID = runID
	end := task7ActiveRecord(hardwareowner.PhaseEndAckWritten)
	end.RunID = runID
	done := task7ActiveRecord(hardwareowner.PhaseDoneObserved)
	done.RunID = runID
	recovery := recoveryRecord(done, CodeOK)
	return []hardwareowner.Record{normal, intent, loadAttempted, noOwner, lease, hello, data, end, done, recovery}
}

type task7CrashLifecycleStore struct {
	store *hardwareowner.Store
	point string
	calls int
}

func (s *task7CrashLifecycleStore) Load() (hardwareowner.Record, bool, error) {
	return s.store.Load()
}

func (s *task7CrashLifecycleStore) Replace(record hardwareowner.Record) error {
	next := s.calls + 1
	if s.point == "before-"+strconv.Itoa(next) {
		os.Exit(73)
	}
	err := s.store.Replace(record)
	if err != nil {
		return err
	}
	s.calls = next
	if s.point == "after-"+strconv.Itoa(next) {
		os.Exit(73)
	}
	return nil
}

type task7CrashMarkerRebooter struct{ path string }

func (r task7CrashMarkerRebooter) Request(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(r.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString("reboot requested\n")
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func newTask7CrashLifecycleRunner(t *testing.T, root, point string) *Runner {
	t.Helper()
	fixture := newTask7RunnerFixture(t)
	uid := uint32(os.Getuid())
	deps := fixture.dependencies()
	deps.store = &task7CrashLifecycleStore{store: hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid), point: point}
	deps.locker = hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid)
	deps.results = NewResultStore(filepath.Join(root, "results"), uid)
	deps.reboot = task7CrashMarkerRebooter{path: filepath.Join(root, "reboot.marker")}
	return newFixtureRunner(deps)
}

type semanticMaintenanceFixture struct{}
type emptyMaintenanceFixture struct{}
type semanticUnlockFixture struct{}

type task7MaintenanceGate struct {
	mu          sync.Mutex
	status      MaintenanceStatus
	enterErr    error
	unlockErr   error
	enterCalls  int
	unlockCalls int
	events      *task7EventLog
}

type task7PriorBootQuiescence struct {
	priorCalls     int
	priorPostCalls int
	currentCalls   int
	postCalls      int
	postErr        error
}

func (q *task7PriorBootQuiescence) VerifyPriorBoot(context.Context, MaintenanceStatus, hardwareowner.Record, string) error {
	q.priorCalls++
	return nil
}

func (q *task7PriorBootQuiescence) VerifyPreDispatch(context.Context, MaintenanceStatus, hardwareowner.Record) error {
	q.currentCalls++
	return nil
}

func (q *task7PriorBootQuiescence) VerifyPostMain(context.Context, MaintenanceStatus, hardwareowner.Record) (PolicySubsystemProof, error) {
	q.postCalls++
	return PolicySubsystemProof{}, q.postErr
}

func (q *task7PriorBootQuiescence) VerifyPriorBootPostMain(context.Context, MaintenanceStatus, hardwareowner.Record, hardwareowner.Record, string) (PolicySubsystemProof, error) {
	q.priorPostCalls++
	return semanticQuiescenceFixture{}.VerifyPostMain(context.Background(), MaintenanceStatus{}, hardwareowner.Record{})
}

func (*task7PriorBootQuiescence) VerifyPressedInput(context.Context, MaintenanceStatus, hardwareowner.Record) error {
	return nil
}

func (g *task7MaintenanceGate) Enter(context.Context) (MaintenanceStatus, MaintenanceUnlock, error) {
	g.mu.Lock()
	g.enterCalls++
	status, enterErr := g.status, g.enterErr
	g.mu.Unlock()
	if g.events != nil {
		g.events.add("maintenance.enter")
	}
	return status, &task7MaintenanceUnlock{gate: g}, enterErr
}

type task7MaintenanceUnlock struct {
	gate *task7MaintenanceGate
	once sync.Once
}

func (u *task7MaintenanceUnlock) Unlock() error {
	var err error
	u.once.Do(func() {
		u.gate.mu.Lock()
		u.gate.unlockCalls++
		err = u.gate.unlockErr
		u.gate.mu.Unlock()
		if u.gate.events != nil {
			u.gate.events.add("maintenance.release")
		}
	})
	return err
}

func task7ValidMaintenanceStatus() MaintenanceStatus {
	return MaintenanceStatus{TerminalJournalSHA256: strings.Repeat("a", 64), Inventory: InventoryV1{Identity: "fixture-terminal-inventory-v1"}}
}

func (semanticUnlockFixture) Unlock() error { return nil }
func (semanticMaintenanceFixture) Enter(context.Context) (MaintenanceStatus, MaintenanceUnlock, error) {
	return task7ValidMaintenanceStatus(), semanticUnlockFixture{}, nil
}
func (emptyMaintenanceFixture) Enter(context.Context) (MaintenanceStatus, MaintenanceUnlock, error) {
	return MaintenanceStatus{}, semanticUnlockFixture{}, nil
}

type semanticQuiescenceFixture struct{}

type task7Quiescence struct {
	pre     func(context.Context, MaintenanceStatus, hardwareowner.Record) error
	post    func(context.Context, MaintenanceStatus, hardwareowner.Record) (PolicySubsystemProof, error)
	pressed func(context.Context, MaintenanceStatus, hardwareowner.Record) error
}

func (q task7Quiescence) VerifyPreDispatch(ctx context.Context, status MaintenanceStatus, record hardwareowner.Record) error {
	if q.pre == nil {
		return nil
	}
	return q.pre(ctx, status, record)
}

func (q task7Quiescence) VerifyPostMain(ctx context.Context, status MaintenanceStatus, record hardwareowner.Record) (PolicySubsystemProof, error) {
	if q.post == nil {
		return semanticQuiescenceFixture{}.VerifyPostMain(ctx, status, record)
	}
	return q.post(ctx, status, record)
}

func (q task7Quiescence) VerifyPressedInput(ctx context.Context, status MaintenanceStatus, record hardwareowner.Record) error {
	if q.pressed == nil {
		return nil
	}
	return q.pressed(ctx, status, record)
}

func (semanticQuiescenceFixture) VerifyPreDispatch(context.Context, MaintenanceStatus, hardwareowner.Record) error {
	return nil
}
func (semanticQuiescenceFixture) VerifyPostMain(context.Context, MaintenanceStatus, hardwareowner.Record) (PolicySubsystemProof, error) {
	return PolicySubsystemProof{MainAbsent: true, InputWorkersAbsent: true, OffloadWorkersAbsent: true, PresentationWorkersAbsent: true, DeviceDescriptorsAbsent: true, PressedInputCleared: true, NoInput: true, NoOffload: true, NoPresentation: true, NoSave: true, NoStorage: true, NoVideo: true, NoAudio: true, NoPLL: true, NoSDRAM: true, NoExternalOutput: true, NoSharedMemory: true}, nil
}
func (semanticQuiescenceFixture) VerifyPressedInput(context.Context, MaintenanceStatus, hardwareowner.Record) error {
	return nil
}

func TestRunnerPreflightDoesNotCaptureTheRunBaseline(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	deps := fixture.dependencies()
	deps.observer = nil
	runner := newFixtureRunner(deps)
	if err := runner.Preflight(context.Background(), fixture.request); err != nil {
		t.Fatalf("Preflight() = %v, want success without a run baseline", err)
	}
}

func TestRunnerUsesDescriptorBinderWhenFixtureBindSeamIsAbsent(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	manifest := fixture.manifest
	root := task7ProductionStaging(t, &manifest)
	manifestPath := filepath.Join(root, "manifest.json")
	raw, err := manifest.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	binding := fixture.binding
	binding.state.manifest = manifest
	binding.state.metadata.Path = filepath.Join(root, ManifestArtifact)
	deps := fixture.dependencies()
	deps.bind = nil
	deps.manifestUID = uint32(os.Getuid())
	deps.artifact = &task7ManifestArtifactBinder{manifest: manifest, binding: binding}
	qDeps, _, _ := validQualificationDeps()
	receipt, err := newFixtureQualifier(qDeps).Qualify(context.Background(), binding)
	if err != nil {
		t.Fatalf("qualification fixture = %v", err)
	}
	deps.qualifier = &task7Qualification{receipt: receipt}
	runner := newFixtureRunner(deps)
	result, err := runner.RunCommand(context.Background(), Request{ManifestPath: manifestPath, ArtifactPath: binding.Path()})
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if result.PrimaryCode != string(CodeOK) {
		t.Fatalf("primary_code = %q, want %q", result.PrimaryCode, CodeOK)
	}
}

func TestProductionBindRejectsManifestWithWrongOwner(t *testing.T) {
	manifest := qualificationArtifactBinding().state.manifest
	staging := task7ProductionStaging(t, &manifest)
	manifestPath := filepath.Join(staging, "manifest.json")
	task7WriteManifest(t, manifestPath, manifest)
	wrongUID := uint32(os.Getuid())
	if wrongUID == 0 {
		wrongUID = 65534
		if err := os.Chown(manifestPath, int(wrongUID), -1); err != nil {
			t.Fatalf("chown hostile manifest: %v", err)
		}
	}
	binding := qualificationArtifactBinding()
	binding.state.manifest = manifest
	binding.state.metadata.Path = filepath.Join(staging, ManifestArtifact)
	_, returned, err := productionBind(&task7ManifestArtifactBinder{manifest: manifest, binding: binding})(context.Background(), Request{
		ManifestPath: manifestPath,
		ArtifactPath: binding.state.metadata.Path,
	})
	if returned.state != nil {
		_ = returned.Close()
	}
	if err == nil {
		t.Fatal("productionBind accepted a manifest not owned by UID 0")
	}
}

func TestProductionManifestDescriptorRejectsWrongFileOwner(t *testing.T) {
	manifest := qualificationArtifactBinding().state.manifest
	staging := task7ProductionStaging(t, &manifest)
	manifestPath := filepath.Join(staging, "manifest.json")
	task7WriteManifest(t, manifestPath, manifest)
	file, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	wrongUID := uint32(os.Getuid()) + 1
	if uint32(os.Getuid()) == ^uint32(0) {
		wrongUID = 0
	}
	if _, _, err := inspectProductionManifest(file, wrongUID, nil); err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("inspectProductionManifest() = %v, want exact file-owner rejection", err)
	}
}

func TestProductionBindRejectsOversizedManifestBeforeParsing(t *testing.T) {
	manifest := qualificationArtifactBinding().state.manifest
	staging := task7ProductionStaging(t, &manifest)
	manifestPath := filepath.Join(staging, "manifest.json")
	if err := os.WriteFile(manifestPath, bytes.Repeat([]byte{'x'}, maxStagedManifestBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	binding := qualificationArtifactBinding()
	binding.state.manifest = manifest
	binding.state.metadata.Path = filepath.Join(staging, ManifestArtifact)
	binder := &task7ManifestArtifactBinder{manifest: manifest, binding: binding}
	_, returned, err := productionBind(binder, productionBindOptions{ExpectedUID: uint32(os.Getuid())})(context.Background(), Request{
		ManifestPath: manifestPath,
		ArtifactPath: binding.state.metadata.Path,
	})
	if returned.state != nil {
		_ = returned.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "manifest size") {
		t.Fatalf("productionBind() = %v, want bounded manifest-size rejection", err)
	}
	if binder.seen != (Manifest{}) {
		t.Fatalf("artifact binder saw oversized manifest: %#v", binder.seen)
	}
}

func TestProductionBindRejectsSymlinkedStagingAncestor(t *testing.T) {
	manifest := qualificationArtifactBinding().state.manifest
	staging := task7ProductionStagingPath(t, &manifest)
	realStaging := t.TempDir()
	if err := os.Chmod(realStaging, 0o700); err != nil {
		t.Fatal(err)
	}
	task7WriteManifest(t, filepath.Join(realStaging, "manifest.json"), manifest)
	if err := os.Symlink(realStaging, staging); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(staging) })
	binding := qualificationArtifactBinding()
	binding.state.manifest = manifest
	binding.state.metadata.Path = filepath.Join(staging, ManifestArtifact)
	_, returned, err := productionBind(&task7ManifestArtifactBinder{manifest: manifest, binding: binding}, productionBindOptions{ExpectedUID: uint32(os.Getuid())})(context.Background(), Request{
		ManifestPath: filepath.Join(staging, "manifest.json"),
		ArtifactPath: binding.state.metadata.Path,
	})
	if returned.state != nil {
		_ = returned.Close()
	}
	if err == nil {
		t.Fatal("productionBind accepted a staging-directory symlink")
	}
}

func TestProductionBindRejectsMultiplyLinkedManifest(t *testing.T) {
	manifest := qualificationArtifactBinding().state.manifest
	staging := task7ProductionStaging(t, &manifest)
	manifestPath := filepath.Join(staging, "manifest.json")
	task7WriteManifest(t, manifestPath, manifest)
	if err := os.Link(manifestPath, filepath.Join(staging, "manifest.alias")); err != nil {
		t.Fatal(err)
	}
	binding := qualificationArtifactBinding()
	binding.state.manifest = manifest
	binding.state.metadata.Path = filepath.Join(staging, ManifestArtifact)
	_, returned, err := productionBind(&task7ManifestArtifactBinder{manifest: manifest, binding: binding}, productionBindOptions{ExpectedUID: uint32(os.Getuid())})(context.Background(), Request{
		ManifestPath: manifestPath,
		ArtifactPath: binding.state.metadata.Path,
	})
	if returned.state != nil {
		_ = returned.Close()
	}
	if err == nil {
		t.Fatal("productionBind accepted a multiply-linked manifest")
	}
}

func TestProductionBindRejectsManifestPathReplacementAfterValidation(t *testing.T) {
	manifest := qualificationArtifactBinding().state.manifest
	staging := task7ProductionStaging(t, &manifest)
	manifestPath := filepath.Join(staging, "manifest.json")
	task7WriteManifest(t, manifestPath, manifest)
	binding := qualificationArtifactBinding()
	binding.state.manifest = manifest
	binding.state.metadata.Path = filepath.Join(staging, ManifestArtifact)
	binder := task7ManifestArtifactBinder{manifest: manifest, binding: binding}
	bind := productionBind(&binder, productionBindOptions{
		ExpectedUID: uint32(os.Getuid()),
		AfterManifestOpen: func() {
			if err := os.Rename(manifestPath, filepath.Join(staging, "manifest.opened")); err != nil {
				t.Fatalf("retain opened manifest: %v", err)
			}
			if err := os.WriteFile(manifestPath, []byte("hostile replacement\n"), 0o600); err != nil {
				t.Fatalf("replace manifest path: %v", err)
			}
		},
	})
	_, returned, err := bind(context.Background(), Request{ManifestPath: manifestPath, ArtifactPath: binding.state.metadata.Path})
	if returned.state != nil {
		defer returned.Close()
	}
	if err == nil {
		t.Fatal("productionBind accepted a manifest path replaced after descriptor validation")
	}
	if binder.seen != (Manifest{}) {
		t.Fatalf("artifact binder saw replaced manifest boundary: %#v", binder.seen)
	}
}

func TestProductionBindKeepsManifestAndArtifactInOneValidatedStagingDirectory(t *testing.T) {
	manifest := qualificationArtifactBinding().state.manifest
	staging := task7ProductionStaging(t, &manifest)
	originalPayload := []byte("original descriptor-bound artifact")
	originalDigest := sha256.Sum256(originalPayload)
	manifest.ArtifactSize = uint64(len(originalPayload))
	manifest.ArtifactSHA256 = hex.EncodeToString(originalDigest[:])
	manifestPath := filepath.Join(staging, "manifest.json")
	task7WriteManifest(t, manifestPath, manifest)
	if err := os.WriteFile(filepath.Join(staging, ManifestArtifact), originalPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	evidence := validResourceEvidenceV2()
	evidence.SourceCommit = manifest.SourceCommit
	evidence.ArtifactSHA256 = manifest.ArtifactSHA256
	evidenceRaw, err := evidence.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, ManifestEvidence), evidenceRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rewriteBundleChecksums(t, staging); err != nil {
		t.Fatal(err)
	}

	stagedOriginal := staging + ".original"
	t.Cleanup(func() { _ = os.RemoveAll(stagedOriginal) })
	swapped := false
	bind := productionBind(ArtifactAccess{ExpectedUID: uint32(os.Getuid())}, productionBindOptions{
		ExpectedUID: uint32(os.Getuid()),
		AfterManifestOpen: func() {
			if swapped {
				t.Fatal("manifest-open hook ran more than once")
			}
			if err := os.Rename(staging, stagedOriginal); err != nil {
				t.Fatalf("move original staging directory: %v", err)
			}
			if err := os.Mkdir(staging, 0o700); err != nil {
				t.Fatalf("create replacement staging directory: %v", err)
			}
			replacementPayload := []byte("replacement artifact from a different directory")
			replacementDigest := sha256.Sum256(replacementPayload)
			replacementManifest := manifest
			replacementManifest.ArtifactSize = uint64(len(replacementPayload))
			replacementManifest.ArtifactSHA256 = hex.EncodeToString(replacementDigest[:])
			task7WriteManifest(t, filepath.Join(staging, "manifest.json"), replacementManifest)
			if err := os.WriteFile(filepath.Join(staging, ManifestArtifact), replacementPayload, 0o600); err != nil {
				t.Fatalf("write replacement artifact: %v", err)
			}
			swapped = true
		},
	})

	gotManifest, binding, err := bind(context.Background(), Request{
		ManifestPath: manifestPath,
		ArtifactPath: filepath.Join(staging, ManifestArtifact),
	})
	if err != nil {
		t.Fatalf("productionBind() after staging-path replacement: %v", err)
	}
	defer func() {
		if err := binding.Close(); err != nil {
			t.Errorf("close retained binding: %v", err)
		}
	}()
	if !swapped {
		t.Fatal("manifest-open hook did not replace the staging pathname")
	}
	if gotManifest != manifest {
		t.Fatalf("manifest = %#v, want descriptor-bound manifest %#v", gotManifest, manifest)
	}
	if metadata := binding.Metadata(); metadata.SHA256 != manifest.ArtifactSHA256 || metadata.Size != int64(len(originalPayload)) {
		t.Fatalf("binding metadata = %#v, want original descriptor-bound artifact", metadata)
	}
	artifact, err := binding.OpenArtifact()
	if err != nil {
		t.Fatalf("open retained artifact: %v", err)
	}
	payload, readErr := io.ReadAll(artifact)
	closeErr := artifact.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read retained artifact: %v", err)
	}
	if !bytes.Equal(payload, originalPayload) {
		t.Fatalf("retained artifact = %q, want original descriptor-bound bytes %q", payload, originalPayload)
	}
}

func task7ProductionStagingPath(t *testing.T, manifest *Manifest) string {
	t.Helper()
	seed := sha256.Sum256([]byte(t.TempDir()))
	manifest.RunID = hex.EncodeToString(seed[:16])
	return filepath.Join("/tmp", "misteross-fpgadev-"+manifest.RunID)
}

func task7ProductionStaging(t *testing.T, manifest *Manifest) string {
	t.Helper()
	staging := task7ProductionStagingPath(t, manifest)
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(staging) })
	return staging
}

func task7WriteManifest(t *testing.T, path string, manifest Manifest) {
	t.Helper()
	raw, err := manifest.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProductionPreflightRejectsUnverifiedResourceBundleBeforeMutation(t *testing.T) {
	tests := map[string]func(*testing.T, string){
		"missing evidence": func(t *testing.T, staging string) {
			if err := os.Remove(filepath.Join(staging, ManifestEvidence)); err != nil {
				t.Fatal(err)
			}
		},
		"corrupt evidence": func(t *testing.T, staging string) {
			if err := os.WriteFile(filepath.Join(staging, ManifestEvidence), []byte("not-json\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := rewriteBundleChecksums(t, staging); err != nil {
				t.Fatal(err)
			}
		},
		"mismatched evidence": func(t *testing.T, staging string) {
			path := filepath.Join(staging, ManifestEvidence)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			evidence, err := ParseResourceEvidenceV2(raw)
			if err != nil {
				t.Fatal(err)
			}
			evidence.ArtifactSHA256 = strings.Repeat("c", 64)
			raw, err = evidence.MarshalCanonical()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := rewriteBundleChecksums(t, staging); err != nil {
				t.Fatal(err)
			}
		},
		"forbidden resource": func(t *testing.T, staging string) {
			path := filepath.Join(staging, ManifestEvidence)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(raw, []byte("\"external_output_ports\":0"), []byte("\"external_output_ports\":1"), 1)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := rewriteBundleChecksums(t, staging); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			staging, manifest := task4ProductionBundle(t)
			mutate(t, staging)
			deps := fixture.dependencies()
			deps.bind = nil
			deps.artifact = &ArtifactAccess{ExpectedUID: uint32(os.Getuid())}
			deps.manifestUID = uint32(os.Getuid())
			runner := newFixtureRunner(deps)
			request := Request{
				ManifestPath: filepath.Join(staging, "manifest.json"),
				ArtifactPath: filepath.Join(staging, ManifestArtifact),
			}
			if err := runner.Preflight(context.Background(), request); err == nil || !errors.Is(err, ErrRunPreflight) {
				t.Fatalf("Preflight() = %v, want preflight rejection", err)
			}
			if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 || fixture.mapper.openCalls != 0 {
				t.Fatalf("unverified bundle crossed mutation boundary: owner_replaces=%d fifo=%d mmio=%d", fixture.store.replaceCalls, fixture.fifo.calls, fixture.mapper.openCalls)
			}
			_ = manifest
		})
	}
}

func TestProductionBindCachesDescriptorVerifiedEvidenceForRepeatedReads(t *testing.T) {
	staging, manifest := task4ProductionBundle(t)
	bind := productionBind(ArtifactAccess{ExpectedUID: uint32(os.Getuid())}, productionBindOptions{ExpectedUID: uint32(os.Getuid())})
	_, binding, err := bind(context.Background(), Request{
		ManifestPath: filepath.Join(staging, "manifest.json"),
		ArtifactPath: filepath.Join(staging, ManifestArtifact),
	})
	if err != nil {
		t.Fatalf("productionBind() = %v", err)
	}
	defer binding.Close()
	if binding.state.resourceEvidence == nil {
		t.Fatal("productionBind returned without caching verified evidence")
	}
	if err := os.Remove(filepath.Join(staging, ManifestEvidence)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(staging, ManifestChecksums)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := binding.ResourceEvidence()
		if err != nil {
			t.Fatalf("ResourceEvidence() iteration %d = %v", i, err)
		}
		if got.ArtifactSHA256 != manifest.ArtifactSHA256 {
			t.Fatalf("cached evidence iteration %d = %#v", i, got)
		}
	}
}

func task4ProductionBundle(t *testing.T) (string, Manifest) {
	t.Helper()
	source, manifest, _ := makeVerifiedBundleFixture(t)
	staging := task7ProductionStaging(t, &manifest)
	for _, name := range append(append([]string{}, requiredBundleMembers...), ManifestChecksums) {
		raw, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(staging, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifestRaw, err := manifest.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rewriteBundleChecksums(t, staging); err != nil {
		t.Fatal(err)
	}
	return staging, manifest
}

func TestRunnerNominalLifecyclePersistsTerminalFenceAndRequestsRecovery(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("terminal result invalid: %v", err)
	}
	if result.PrimaryCode != string(CodeOK) || result.Phase != ResultPhaseDoneObserved {
		t.Fatalf("terminal result = %#v", result)
	}
	if fixture.fifo.calls != 1 || fixture.fifo.command != "load_core /anonymous/proc-fd\n" {
		t.Fatalf("FIFO dispatch = calls:%d command:%q", fixture.fifo.calls, fixture.fifo.command)
	}
	if fixture.mapper.openCalls != 1 || fixture.mapper.registers == nil || fixture.mapper.registers.closeCalls != 1 {
		t.Fatalf("mailbox mapping lifecycle = opens:%d closes:%d", fixture.mapper.openCalls, fixture.mapper.registers.closeCalls)
	}
	if len(fixture.results.created) != 1 || fixture.reboot.calls != 1 {
		t.Fatalf("terminal persistence/reboot = results:%d reboot:%d", len(fixture.results.created), fixture.reboot.calls)
	}
	fixture.store.mu.Lock()
	final := fixture.store.record
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired || final.FirstFailure != "" {
		t.Fatalf("final owner record = %#v", final)
	}
}

func TestRunnerStartsMainHandoffTimeoutAfterCompletedFIFODispatch(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	const dispatchDelay = 300 * time.Millisecond
	observer := &task7HandoffDeadlineObserver{baseline: append([]ProcessIdentity(nil), fixture.baseline...)}
	deps := fixture.dependencies()
	deps.fifo = task7DelayedCompletedFIFO{delay: dispatchDelay}
	deps.observer = observer

	result, err := newFixtureRunner(deps).RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if result.PrimaryCode != string(CodeOK) {
		t.Fatalf("primary code = %q, want ok", result.PrimaryCode)
	}
	if observer.waitCalls != 1 {
		t.Fatalf("handoff observer calls = %d, want 1", observer.waitCalls)
	}
	if observer.deadlineRemaining < mainHandoffTimeout-dispatchDelay/2 {
		t.Fatalf("handoff deadline remaining after %v dispatch = %v, want a fresh %v post-dispatch window", dispatchDelay, observer.deadlineRemaining, mainHandoffTimeout)
	}
}

func TestBoundedFIFOPreservesCompletedDispatchAtConcurrentContextExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempt, err := boundedFIFO(ctx, task7CancelingCompletedFIFO{cancel: cancel}, "load_core /anonymous/proc-fd\n")
	if err != nil {
		t.Fatalf("boundedFIFO() error = %v, want completed dispatch preserved", err)
	}
	if attempt != Completed {
		t.Fatalf("boundedFIFO() attempt = %v, want Completed", attempt)
	}
}

func TestRunnerPersistsMailboxBoundariesInOrderWithoutRetrospectivePromotion(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	runner := newFixtureRunner(fixture.dependencies())
	if _, err := runner.RunCommand(context.Background(), fixture.request); err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	fixture.store.mu.Lock()
	defer fixture.store.mu.Unlock()
	var phases []hardwareowner.Phase
	for _, record := range fixture.store.history {
		if record.State == hardwareowner.StateFPGADefaultActive || record.State == hardwareowner.StateRecoveryRequired {
			phases = append(phases, record.Phase)
		}
	}
	want := []hardwareowner.Phase{
		hardwareowner.PhaseLeaseActive,
		hardwareowner.PhaseHelloObserved,
		hardwareowner.PhaseMessagePartial,
		hardwareowner.PhaseEndAckWritten,
		hardwareowner.PhaseDoneObserved,
		hardwareowner.PhaseDoneObserved,
	}
	if !reflect.DeepEqual(phases, want) {
		t.Fatalf("durable mailbox phases = %v, want %v", phases, want)
	}
}

func TestRunnerRejectsOutOfOrderMailboxProgress(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.mailboxProgress = func(ctx context.Context, _ Registers, _ Clock, callback mailboxProgressCallback) (Observation, error) {
		if err := callback(mailboxProgress{Phase: mailboxProgressEnd}); err != nil {
			return Observation{}, err
		}
		return Observation{}, nil
	}
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if result.PrimaryCode != string(CodeProtocolViolation) {
		t.Fatalf("out-of-order mailbox primary = %q, want protocol_violation", result.PrimaryCode)
	}
	fixture.store.mu.Lock()
	defer fixture.store.mu.Unlock()
	if fixture.store.record.Phase != hardwareowner.PhaseLeaseActive {
		t.Fatalf("out-of-order mailbox advanced owner phase to %q", fixture.store.record.Phase)
	}
}

func TestRunnerDoesNotPromoteUnacceptedTerminalEvidence(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.mailboxProgress = func(_ context.Context, _ Registers, _ Clock, callback mailboxProgressCallback) (Observation, error) {
		if err := callback(mailboxProgress{Phase: mailboxProgressHello, Observation: Observation{}}); err != nil {
			return Observation{}, err
		}
		return Observation{Payload: []byte("OSS FPGA OK\n"), TerminalWord: 0xd3130c00}, errors.New("protocol stopped before DONE")
	}
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v, want durable post-intent result", err)
	}
	if result.Phase != ResultPhaseHelloObserved || result.TerminalWord != "00000000" {
		t.Fatalf("unaccepted terminal evidence promoted in result: %#v", result)
	}
}

func TestRunnerRejectsTerminalWordDifferentFromAcceptedDone(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.mailboxProgress = func(_ context.Context, _ Registers, _ Clock, callback mailboxProgressCallback) (Observation, error) {
		if err := callback(mailboxProgress{Phase: mailboxProgressHello, Observation: Observation{}}); err != nil {
			return Observation{}, err
		}
		partial := Observation{Payload: []byte{'O'}}
		if err := callback(mailboxProgress{Phase: mailboxProgressData, Observation: partial}); err != nil {
			return partial, err
		}
		if err := callback(mailboxProgress{Phase: mailboxProgressEnd, Observation: partial}); err != nil {
			return partial, err
		}
		if err := callback(mailboxProgress{Phase: mailboxProgressDone, Observation: partial, TerminalWord: 0xd3130c00}); err != nil {
			return partial, err
		}
		partial.TerminalWord = 0xdeadbeef
		return partial, nil
	}
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v, want durable post-intent result", err)
	}
	if result.PrimaryCode != string(CodeProtocolViolation) || result.TerminalWord != "d3130c00" {
		t.Fatalf("mismatched terminal evidence = %#v, want protocol failure retaining accepted terminal word", result)
	}
}

func TestRunnerClosesMappingReturnedAlongsideOpenError(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.mapper.registers = &task7Registers{}
	fixture.mapper.openErr = errors.New("map failed")
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if result.PrimaryCode != string(CodeMMIOFailed) || fixture.mapper.registers.closeCalls != 1 {
		t.Fatalf("mapping error result = %#v, close calls=%d", result, fixture.mapper.registers.closeCalls)
	}
}

func TestRunnerClosesMappingWhenContextExpiresImmediatelyAfterOpen(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	fixture.mapper.afterOpen = cancel
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(ctx, fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if result.PrimaryCode != string(CodeMessageTimeout) || fixture.mapper.registers == nil || fixture.mapper.registers.closeCalls != 1 {
		t.Fatalf("post-open cancellation = %#v closes:%v", result, fixture.mapper.registers)
	}
}

func TestRunnerMarksPendingResultFailedOnlyWhenRebootErrors(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.reboot.err = errors.New("reboot unavailable")
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != CodeRebootRequestFailed || !failure.IntentCommitted {
		t.Fatalf("RunCommand() = %v, want post-intent reboot failure", err)
	}
	if result.PrimaryCode != string(CodeOK) || result.RecoveryRequest != "failed" || fixture.results.markRecoveryFailed != 1 {
		t.Fatalf("reboot failure result = %#v, marks=%d", result, fixture.results.markRecoveryFailed)
	}
}

func TestRunnerResultWriteFailureBecomesStateStorePrimaryWithoutClearingFence(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.results.createErr = errors.New("result write failed")
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if !errors.Is(err, ErrResultUnavailable) {
		t.Fatalf("RunCommand() = %v, want result-unavailable error", err)
	}
	if result.PrimaryCode != string(CodeStateStoreFailed) {
		t.Fatalf("result write failure primary = %q", result.PrimaryCode)
	}
	fixture.store.mu.Lock()
	final := fixture.store.record
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired || final.FirstFailure != "" {
		t.Fatalf("result write failure owner record = %#v", final)
	}
}

func TestRunnerResultWriteFailureOverridesEarlierPrimaryInMemory(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	dispatchErr := errors.New("dispatch consumed")
	fixture.fifo.err = dispatchErr
	fixture.results.createErr = errors.New("result write failed")
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if !errors.Is(err, ErrResultUnavailable) {
		t.Fatalf("RunCommand() = %v, want result-unavailable error", err)
	}
	if result.PrimaryCode != string(CodeStateStoreFailed) {
		t.Fatalf("result-write failure primary = %q, want result-unavailable state-store failure", result.PrimaryCode)
	}
	if !errors.Is(err, dispatchErr) {
		t.Fatalf("result-write failure lost earlier private dispatch error: %v", err)
	}
}

func TestRunnerRefusesSameBootReadmissionAfterPostIntentFence(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	runner := newFixtureRunner(fixture.dependencies())
	if _, err := runner.RunCommand(context.Background(), fixture.request); err != nil {
		t.Fatalf("initial RunCommand() = %v", err)
	}
	replaces := fixture.store.replaceCalls
	second, err := runner.RunCommand(context.Background(), fixture.request)
	if err == nil || !errors.Is(err, ErrRunPreflight) {
		t.Fatalf("second RunCommand() error = %v, want fenced pre-intent failure", err)
	}
	if second.PrimaryCode != string(CodeOwnershipConflict) || fixture.store.replaceCalls != replaces {
		t.Fatalf("same-boot re-admission = result:%#v replaces:%d/%d", second, fixture.store.replaceCalls, replaces)
	}
}

func TestRunnerRejectsEmptyBaselineBeforeDurableIntentOrDispatch(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.baseline = nil
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err == nil || !errors.Is(err, ErrRunPreflight) {
		t.Fatalf("RunCommand() error = %v, want pre-intent failure", err)
	}
	if result.PrimaryCode != string(CodeManifestRejected) && result.PrimaryCode != string(CodeOwnershipConflict) {
		t.Fatalf("pre-intent result code = %q", result.PrimaryCode)
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 {
		t.Fatalf("empty baseline reached mutation: replace=%d fifo=%d", fixture.store.replaceCalls, fixture.fifo.calls)
	}
}

func TestRunnerRejectsNoncanonicalBaselineBeforeDurableIntent(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]ProcessIdentity) []ProcessIdentity
	}{
		{name: "pid replacement", mutate: func(in []ProcessIdentity) []ProcessIdentity {
			in[0].PID = 0
			return in
		}},
		{name: "start time replacement", mutate: func(in []ProcessIdentity) []ProcessIdentity {
			in[0].StartTime = 0
			return in
		}},
		{name: "executable replacement", mutate: func(in []ProcessIdentity) []ProcessIdentity {
			in[0].SHA256 = strings.Repeat("D", 64)
			return in
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			fixture.baseline = test.mutate(append([]ProcessIdentity(nil), fixture.baseline...))
			runner := newFixtureRunner(fixture.dependencies())
			_, err := runner.RunCommand(context.Background(), fixture.request)
			if err == nil || !errors.Is(err, ErrRunPreflight) {
				t.Fatalf("RunCommand() error = %v, want pre-intent baseline rejection", err)
			}
			if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 {
				t.Fatalf("baseline rejection mutated state: replace=%d fifo=%d", fixture.store.replaceCalls, fixture.fifo.calls)
			}
		})
	}
}

func TestRunnerTreatsFIFOErrorAsPotentiallyConsumedAndPreservesFence(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.fifo.err = errors.New("write failed")
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() returned pre-intent error after dispatch: %v", err)
	}
	if result.PrimaryCode != string(CodeLoadDispatchFailed) {
		t.Fatalf("primary_code = %q, want %q", result.PrimaryCode, CodeLoadDispatchFailed)
	}
	if fixture.fifo.calls != 1 || fixture.store.replaceCalls < 2 {
		t.Fatalf("dispatch/fence transitions = fifo:%d replace:%d", fixture.fifo.calls, fixture.store.replaceCalls)
	}
	if fixture.reboot.calls != 1 {
		t.Fatalf("reboot calls = %d, want 1", fixture.reboot.calls)
	}
}

func TestRunnerPreservesLoadAttemptedStoreFailureAfterFIFOPrimary(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fifoErr := errors.New("FIFO primary")
	phaseStoreErr := errors.New("load_attempted store secondary")
	fixture.fifo.err = fifoErr
	fixture.store.replaceErrAt = 2
	fixture.store.replaceErrValue = phaseStoreErr
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != CodeLoadDispatchFailed || !failure.IntentCommitted {
		t.Fatalf("RunCommand() = %v, want typed FIFO-primary failure", err)
	}
	if !errors.Is(err, fifoErr) || !errors.Is(err, phaseStoreErr) {
		t.Fatalf("private failure chain = %v, want FIFO primary and later phase-store cause", err)
	}
	if result.PrimaryCode != string(CodeLoadDispatchFailed) || result.Phase != ResultPhaseIntentCommitted || result.RecoveryRequest != "pending" {
		t.Fatalf("persisted primary = %#v, want FIFO failure at last durable intent boundary", result)
	}
	fixture.store.mu.Lock()
	final := cloneTask7Record(fixture.store.record)
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired || final.Phase != hardwareowner.PhaseIntentCommitted || final.FirstFailure != string(CodeLoadDispatchFailed) {
		t.Fatalf("successful recovery fence = %#v, want intent/FIFO primary", final)
	}
	if len(fixture.results.created) != 1 || fixture.reboot.calls != 1 || fixture.locker.unlockCalls != 1 {
		t.Fatalf("successful recovery lifecycle = results:%d reboot:%d unlock:%d, want 1/1/1", len(fixture.results.created), fixture.reboot.calls, fixture.locker.unlockCalls)
	}
}

func TestRunnerPreservesFaultCheckpointFailureAfterFIFOPrimary(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fifoErr := errors.New("FIFO primary")
	checkpointErr := errors.New("fault checkpoint secondary")
	fixture.fifo.err = fifoErr
	fault := &task7LaterErrorFault{err: checkpointErr}
	fixture.fault = fault
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != CodeLoadDispatchFailed || !failure.IntentCommitted {
		t.Fatalf("RunCommand() = %v, want typed FIFO-primary failure", err)
	}
	if !errors.Is(err, fifoErr) || !errors.Is(err, checkpointErr) {
		t.Fatalf("private failure chain = %v, want FIFO primary and later checkpoint cause", err)
	}
	if fault.calls != 1 {
		t.Fatalf("fault checkpoint calls = %d, want one", fault.calls)
	}
	if result.PrimaryCode != string(CodeLoadDispatchFailed) || result.Phase != ResultPhaseLoadAttempted || result.RecoveryRequest != "pending" {
		t.Fatalf("persisted primary = %#v, want FIFO failure at load_attempted", result)
	}
	fixture.store.mu.Lock()
	final := cloneTask7Record(fixture.store.record)
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired || final.Phase != hardwareowner.PhaseLoadAttempted || final.FirstFailure != string(CodeLoadDispatchFailed) {
		t.Fatalf("successful recovery fence = %#v, want load_attempted/FIFO primary", final)
	}
	if len(fixture.results.created) != 1 || fixture.reboot.calls != 1 || fixture.locker.unlockCalls != 1 {
		t.Fatalf("successful recovery lifecycle = results:%d reboot:%d unlock:%d, want 1/1/1", len(fixture.results.created), fixture.reboot.calls, fixture.locker.unlockCalls)
	}
}

func TestRunnerLeavesIntentPhaseWhenDispatchCannotBeAttempted(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	deps := fixture.dependencies()
	deps.revalidate = func(context.Context, *ArtifactBinding) error { return errors.New("binding changed") }
	runner := newFixtureRunner(deps)
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if result.Phase != ResultPhaseIntentCommitted {
		t.Fatalf("result phase = %q, want %q when FIFO was not attempted", result.Phase, ResultPhaseIntentCommitted)
	}
	if fixture.fifo.calls != 0 {
		t.Fatalf("FIFO calls = %d, want zero", fixture.fifo.calls)
	}
	fixture.store.mu.Lock()
	final := fixture.store.record
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired || final.Phase != hardwareowner.PhaseIntentCommitted {
		t.Fatalf("final owner record = %#v, want fenced intent phase", final)
	}
}

func TestRunnerPreservesFenceWhenMainExecutableChangesDuringHandoff(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.observerErr = ErrProcessIdentityChanged
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v", err)
	}
	if result.PrimaryCode != string(CodeMainHandoffTimeout) || fixture.fifo.calls != 1 {
		t.Fatalf("replacement handoff result = %#v fifo:%d", result, fixture.fifo.calls)
	}
}

func TestRunnerInjectedStoreFailureAtEveryDurableTransition(t *testing.T) {
	wantFinal := map[int]struct {
		state       hardwareowner.State
		phase       hardwareowner.Phase
		resultPhase string
		results     int
		reboot      int
	}{
		1: {state: hardwareowner.StateNormalMain, phase: "", resultPhase: "", results: 0, reboot: 0},
		2: {state: hardwareowner.StateRecoveryRequired, phase: hardwareowner.PhaseIntentCommitted, resultPhase: ResultPhaseIntentCommitted, results: 1, reboot: 1},
		3: {state: hardwareowner.StateRecoveryRequired, phase: hardwareowner.PhaseLoadAttempted, resultPhase: ResultPhaseLoadAttempted, results: 1, reboot: 1},
		4: {state: hardwareowner.StateRecoveryRequired, phase: hardwareowner.PhaseMainAbsent, resultPhase: ResultPhaseMainAbsent, results: 1, reboot: 1},
		5: {state: hardwareowner.StateRecoveryRequired, phase: hardwareowner.PhaseLeaseActive, resultPhase: ResultPhaseLeaseActive, results: 1, reboot: 1},
		6: {state: hardwareowner.StateRecoveryRequired, phase: hardwareowner.PhaseHelloObserved, resultPhase: ResultPhaseHelloObserved, results: 1, reboot: 1},
		7: {state: hardwareowner.StateRecoveryRequired, phase: hardwareowner.PhaseMessagePartial, resultPhase: ResultPhaseMessagePartial, results: 1, reboot: 1},
		8: {state: hardwareowner.StateRecoveryRequired, phase: hardwareowner.PhaseEndAckWritten, resultPhase: ResultPhaseEndAckWritten, results: 1, reboot: 1},
		9: {state: hardwareowner.StateFPGADefaultActive, phase: hardwareowner.PhaseDoneObserved, resultPhase: ResultPhaseDoneObserved, results: 1, reboot: 1},
	}
	for replaceCall := 1; replaceCall <= 9; replaceCall++ {
		t.Run("replace-"+strconv.Itoa(replaceCall), func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			fixture.store.replaceErrAt = replaceCall
			runner := newFixtureRunner(fixture.dependencies())
			result, err := runner.RunCommand(context.Background(), fixture.request)
			want := wantFinal[replaceCall]
			if replaceCall == 1 {
				if err == nil || !errors.Is(err, ErrRunPreflight) {
					t.Fatalf("intent failure error = %v, want pre-intent failure", err)
				}
				if len(fixture.results.created) != 0 || fixture.reboot.calls != 0 {
					t.Fatalf("pre-intent failure created result/reboot: %d/%d", len(fixture.results.created), fixture.reboot.calls)
				}
			} else {
				var failure *Failure
				if replaceCall == 9 {
					if !errors.As(err, &failure) || !failure.IntentCommitted {
						t.Fatalf("post-intent replace-%d error = %v, want a recovery-fence failure", replaceCall, err)
					}
				} else if err != nil {
					t.Fatalf("post-intent replace-%d error = %v, want primary result only", replaceCall, err)
				}
				wantPrimary := CodeStateStoreFailed
				if result.PrimaryCode != string(wantPrimary) {
					t.Fatalf("replace-%d primary = %q, want %q", replaceCall, result.PrimaryCode, wantPrimary)
				}
				if result.Phase != want.resultPhase || result.RecoveryRequest != "pending" {
					t.Fatalf("replace-%d result boundary = phase:%q recovery:%q, want phase:%q/pending", replaceCall, result.Phase, result.RecoveryRequest, want.resultPhase)
				}
				if err := result.Validate(); err != nil {
					t.Fatalf("replace-%d result invalid: %v", replaceCall, err)
				}
				if len(fixture.results.created) != 1 || fixture.reboot.calls != 1 {
					t.Fatalf("replace-%d result/reboot = %d/%d, want 1/1", replaceCall, len(fixture.results.created), fixture.reboot.calls)
				}
			}
			if fixture.mapper.registers != nil && fixture.mapper.registers.closeCalls != 1 {
				t.Fatalf("replace-%d mapping closes = %d, want exactly one", replaceCall, fixture.mapper.registers.closeCalls)
			}
			fixture.store.mu.Lock()
			final := fixture.store.record
			history := append([]hardwareowner.Record(nil), fixture.store.history...)
			fixture.store.mu.Unlock()
			if final.State != want.state || final.Phase != want.phase {
				t.Fatalf("replace-%d final owner = state:%q phase:%q, want state:%q phase:%q", replaceCall, final.State, final.Phase, want.state, want.phase)
			}
			if len(fixture.results.created) != want.results || fixture.reboot.calls != want.reboot {
				t.Fatalf("replace-%d final durable outputs = result:%d reboot:%d, want %d/%d", replaceCall, len(fixture.results.created), fixture.reboot.calls, want.results, want.reboot)
			}
			if fixture.locker.unlockCalls != 1 {
				t.Fatalf("replace-%d lock releases = %d, want exactly one", replaceCall, fixture.locker.unlockCalls)
			}
			transitionStates := []hardwareowner.State{
				hardwareowner.StateRecoveringIntent,
				hardwareowner.StateRecoveringIntent,
				hardwareowner.StateNoOwner,
				hardwareowner.StateFPGADefaultActive,
				hardwareowner.StateFPGADefaultActive,
				hardwareowner.StateFPGADefaultActive,
				hardwareowner.StateFPGADefaultActive,
				hardwareowner.StateFPGADefaultActive,
			}
			transitionPhases := []hardwareowner.Phase{
				hardwareowner.PhaseIntentCommitted,
				hardwareowner.PhaseLoadAttempted,
				hardwareowner.PhaseMainAbsent,
				hardwareowner.PhaseLeaseActive,
				hardwareowner.PhaseHelloObserved,
				hardwareowner.PhaseMessagePartial,
				hardwareowner.PhaseEndAckWritten,
				hardwareowner.PhaseDoneObserved,
			}
			if replaceCall > 1 {
				for index := 0; index < replaceCall-1; index++ {
					if index >= len(history) {
						t.Fatalf("replace-%d history ended before successful boundary %d: %#v", replaceCall, index+1, history)
					}
					if history[index].State != transitionStates[index] || history[index].Phase != transitionPhases[index] {
						t.Fatalf("replace-%d history[%d] = state:%q phase:%q, want state:%q phase:%q", replaceCall, index, history[index].State, history[index].Phase, transitionStates[index], transitionPhases[index])
					}
				}
			}
			wantHistoryLen := 0
			if replaceCall > 1 {
				wantHistoryLen = replaceCall - 1
			}
			if replaceCall > 1 && replaceCall < 9 {
				wantHistoryLen++
			}
			if len(history) != wantHistoryLen {
				t.Fatalf("replace-%d durable history length = %d, want %d: %#v", replaceCall, len(history), wantHistoryLen, history)
			}
			if replaceCall > 1 && replaceCall < 9 {
				recovery := history[len(history)-1]
				wantRecoveryPhase := transitionPhases[replaceCall-2]
				if recovery.State != hardwareowner.StateRecoveryRequired || recovery.Phase != wantRecoveryPhase {
					t.Fatalf("replace-%d recovery boundary = state:%q phase:%q, want recovery_required/%q", replaceCall, recovery.State, recovery.Phase, wantRecoveryPhase)
				}
			}
		})
	}
}

func TestRunnerPreservesEarlierPrimaryWhenRecoveryFenceStoreFails(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.fifo.err = errors.New("dispatch consumed")
	fixture.store.replaceErrAt = 3 // intent, load_attempted, recovery_required
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != CodeStateStoreFailed || !failure.IntentCommitted {
		t.Fatalf("RunCommand() = %v, want first recovery state-store error", err)
	}
	if result.PrimaryCode != string(CodeLoadDispatchFailed) {
		t.Fatalf("result primary = %q, want preserved dispatch failure", result.PrimaryCode)
	}
	if fixture.results.createCalls != 1 || len(fixture.results.created) != 1 {
		t.Fatalf("result creates = %d calls/%d records, want exactly one", fixture.results.createCalls, len(fixture.results.created))
	}
	fixture.store.mu.Lock()
	final := fixture.store.record
	fixture.store.mu.Unlock()
	if final.State == hardwareowner.StateNormalMain {
		t.Fatalf("recovery-fence failure returned to normal_main: %#v", final)
	}
}

func TestRunnerUnlocksBeforeTheExclusiveResultCreate(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.locker.unlockErr = errors.New("unlock failed")
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != CodeStateStoreFailed || !failure.IntentCommitted {
		t.Fatalf("RunCommand() = %v, want unlock state-store failure", err)
	}
	if fixture.results.createCalls != 1 || len(fixture.results.created) != 1 {
		t.Fatalf("result create calls/records = %d/%d, want one", fixture.results.createCalls, len(fixture.results.created))
	}
	if result.PrimaryCode != string(CodeStateStoreFailed) || fixture.results.created[0].PrimaryCode != string(CodeStateStoreFailed) {
		t.Fatalf("unlock failure result = %#v, stored=%#v", result, fixture.results.created[0])
	}
	fixture.store.mu.Lock()
	final := fixture.store.record
	fixture.store.mu.Unlock()
	if final.State != hardwareowner.StateRecoveryRequired {
		t.Fatalf("unlock failure lost terminal fence: %#v", final)
	}
}

func TestRunnerDoesNotPersistSuccessAfterDeadlineBeforeResultCreate(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	fixture.locker.beforeUnlock = cancel
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(ctx, fixture.request)
	if err == nil {
		t.Fatal("RunCommand() succeeded after cancellation before result create")
	}
	if result.PrimaryCode != string(CodeMessageTimeout) {
		t.Fatalf("canceled terminal result primary = %q, want %q", result.PrimaryCode, CodeMessageTimeout)
	}
	fixture.results.mu.Lock()
	defer fixture.results.mu.Unlock()
	if len(fixture.results.created) != 1 || fixture.results.created[0].PrimaryCode != string(CodeMessageTimeout) {
		t.Fatalf("persisted canceled result = %#v, want one message_timeout result", fixture.results.created)
	}
}

func TestRunnerCancellationDuringSuccessfulResultCreateRetainsAuthoritativeResult(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.reboot.err = errors.New("reboot unavailable")
	ctx, cancel := context.WithCancel(context.Background())
	fixture.results.onCreate = func(Result) { cancel() }
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(ctx, fixture.request)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != CodeRebootRequestFailed {
		t.Fatalf("RunCommand() = %v, want only reboot failure after result create", err)
	}
	if result.PrimaryCode != string(CodeOK) || result.RecoveryRequest != "failed" {
		t.Fatalf("returned result after successful create cancellation = %#v", result)
	}
	fixture.results.mu.Lock()
	defer fixture.results.mu.Unlock()
	if fixture.results.createCalls != 1 || fixture.results.markRecoveryFailed != 1 || len(fixture.results.created) != 1 {
		t.Fatalf("result lifecycle = create:%d mark:%d records:%d", fixture.results.createCalls, fixture.results.markRecoveryFailed, len(fixture.results.created))
	}
	if len(fixture.results.marked) != 1 || fixture.results.marked[0].PrimaryCode != string(CodeOK) || fixture.results.marked[0].RecoveryRequest != "pending" {
		t.Fatalf("MarkRecoveryFailed expected authoritative pending result, got %#v", fixture.results.marked)
	}
	if fixture.results.created[0].PrimaryCode != string(CodeOK) || fixture.results.created[0].RecoveryRequest != "failed" {
		t.Fatalf("stored result was rewritten after successful create = %#v", fixture.results.created[0])
	}
}

func TestRunnerRetainsArtifactCloseErrorAfterAnEarlierPrimary(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.fifo.err = errors.New("dispatch consumed")
	artifactCloseErr := errors.New("artifact close failed")
	deps := fixture.dependencies()
	deps.closeBinding = func(context.Context, *ArtifactBinding) error { return artifactCloseErr }
	runner := newFixtureRunner(deps)
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if result.PrimaryCode != string(CodeLoadDispatchFailed) {
		t.Fatalf("primary = %q, want load_dispatch_failed", result.PrimaryCode)
	}
	if !errors.Is(err, artifactCloseErr) {
		t.Fatalf("RunCommand() = %v, want private artifact close diagnostic", err)
	}
	if len(fixture.results.created) != 1 || fixture.results.created[0].PrimaryCode != string(CodeLoadDispatchFailed) {
		t.Fatalf("artifact cleanup changed persisted primary: %#v", fixture.results.created)
	}
}

func TestRunnerRetainsRegisterCloseErrorAfterAnEarlierPrimary(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	mailboxErr := errors.New("mailbox failed")
	registerCloseErr := errors.New("register close failed")
	fixture.mapper.registers = &task7Registers{closeErr: registerCloseErr}
	deps := fixture.dependencies()
	deps.mailboxProgress = func(context.Context, Registers, Clock, mailboxProgressCallback) (Observation, error) {
		return Observation{}, mailboxErr
	}
	runner := newFixtureRunner(deps)
	result, err := runner.RunCommand(context.Background(), fixture.request)
	if err == nil || !errors.Is(err, registerCloseErr) {
		t.Fatalf("RunCommand() = %v, want private register-close diagnostic", err)
	}
	if result.PrimaryCode == string(CodeStateStoreFailed) || len(fixture.results.created) != 1 || fixture.results.created[0].PrimaryCode != result.PrimaryCode {
		t.Fatalf("register cleanup changed persisted primary: result=%#v stored=%#v", result, fixture.results.created)
	}
}

func TestRunnerRealMailboxPreservesOperationAndRegisterCloseProvenance(t *testing.T) {
	tests := []struct {
		name        string
		configure   func(*mailboxFakeRegisters)
		wantPrimary Code
		wantCause   error
	}{
		{
			name: "protocol primary plus close failure",
			configure: func(registers *mailboxFakeRegisters) {
				registers.gpiValues[0] ^= uint32(1) << 24
				registers.closeErr = errors.New("register close failed")
			},
			wantPrimary: CodeProtocolViolation,
		},
		{
			name: "identical EIO register and close occurrences",
			configure: func(registers *mailboxFakeRegisters) {
				registers.gpiErr = syscall.EIO
				registers.gpiErrAt = 1
				registers.closeErr = syscall.EIO
			},
			wantPrimary: CodeMMIOFailed,
			wantCause:   syscall.EIO,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			registers, clock := mailboxProductionFixture()
			test.configure(registers)
			closeErr := registers.closeErr
			deps := fixture.dependencies()
			deps.mapper = task7GenericMapper{registers: registers}
			deps.clock = clock
			deps.mailboxProgress = runMailboxWithProgress
			runner := newFixtureRunner(deps)

			result, err := runner.RunCommand(context.Background(), fixture.request)
			if result.PrimaryCode != string(test.wantPrimary) {
				t.Fatalf("primary = %q, want %q", result.PrimaryCode, test.wantPrimary)
			}
			if err == nil || !errors.Is(err, closeErr) {
				t.Fatalf("RunCommand() = %v, want private real-register close occurrence %v", err, closeErr)
			}
			if test.wantCause != nil && task7ExactErrorOccurrences(err, test.wantCause) != 2 {
				t.Fatalf("RunCommand() = %v, want exactly two independent operation and cleanup occurrences of %v", err, test.wantCause)
			}
			if registers.closeCalls != 1 {
				t.Fatalf("real mailbox Close calls = %d, want one", registers.closeCalls)
			}
			if len(fixture.results.created) != 1 || fixture.results.created[0].PrimaryCode != string(test.wantPrimary) {
				t.Fatalf("register cleanup changed durable primary: %#v", fixture.results.created)
			}
		})
	}
}

func TestRunnerRetainsIdenticalEIOArtifactCloseOccurrenceAfterDispatchFailure(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.fifo.err = syscall.EIO
	deps := fixture.dependencies()
	deps.closeBinding = func(context.Context, *ArtifactBinding) error { return syscall.EIO }
	runner := newFixtureRunner(deps)

	result, err := runner.RunCommand(context.Background(), fixture.request)
	if result.PrimaryCode != string(CodeLoadDispatchFailed) {
		t.Fatalf("primary = %q, want %q", result.PrimaryCode, CodeLoadDispatchFailed)
	}
	if err == nil || task7ExactErrorOccurrences(err, syscall.EIO) != 2 {
		t.Fatalf("RunCommand() = %v, want exactly two independent dispatch and artifact-close EIO occurrences", err)
	}
	if len(fixture.results.created) != 1 || fixture.results.created[0].PrimaryCode != string(CodeLoadDispatchFailed) {
		t.Fatalf("artifact cleanup changed durable primary: %#v", fixture.results.created)
	}
}

func TestRunnerClosesMalformedBindingReturnedByBindAndPreservesCleanupError(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	closedArtifact, err := os.CreateTemp(t.TempDir(), "already-closed-artifact-")
	if err != nil {
		t.Fatal(err)
	}
	if err := closedArtifact.Close(); err != nil {
		t.Fatal(err)
	}
	metadata := fixture.binding.Metadata()
	metadata.Path = "/anonymous/not-the-requested-artifact.rbf"
	state := &artifactBindingState{artifact: closedArtifact, metadata: metadata, manifest: fixture.manifest}
	malformed := ArtifactBinding{state: state}
	deps := fixture.dependencies()
	deps.bind = func(context.Context, Request) (Manifest, ArtifactBinding, error) {
		return fixture.manifest, malformed, nil
	}
	deps.closeBinding = nil
	runner := newFixtureRunner(deps)

	result, err := runner.RunCommand(context.Background(), fixture.request)
	if result.PrimaryCode != string(CodeManifestRejected) || !errors.Is(err, ErrRunPreflight) || !errors.Is(err, ErrInvalidReceipt) || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("RunCommand() = result:%#v err:%v, want malformed-binding rejection plus cleanup", result, err)
	}
	state.mu.Lock()
	closed := state.closed
	state.mu.Unlock()
	if !closed {
		t.Fatal("malformed local binding was not closed")
	}
	if fixture.store.replaceCalls != 0 || fixture.fifo.calls != 0 {
		t.Fatalf("malformed binding reached intent/FIFO: replaces=%d fifo=%d", fixture.store.replaceCalls, fixture.fifo.calls)
	}
}

func task7ExactErrorOccurrences(err, target error) int {
	if err == nil {
		return 0
	}
	if err == target {
		return 1
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		count := 0
		for _, child := range joined.Unwrap() {
			count += task7ExactErrorOccurrences(child, target)
		}
		return count
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return task7ExactErrorOccurrences(wrapped.Unwrap(), target)
	}
	return 0
}

func TestCleanupErrorDeltaDropsOnlyExplicitSyntheticContextDuplication(t *testing.T) {
	independent := cleanupErrorDelta(context.Canceled)
	if independent != context.Canceled {
		t.Fatalf("independent cleanup context = %v, want retained sentinel", independent)
	}
	delta := cleanupErrorDelta(errors.Join(context.Canceled, markSyntheticCleanupContext(context.Canceled)))
	if occurrences := task7ExactErrorOccurrences(delta, context.Canceled); occurrences != 1 {
		t.Fatalf("marked synthetic cleanup delta = %v with %d context occurrences, want one independent occurrence", delta, occurrences)
	}
}

func TestMailboxProgressCancellationAfterReplaceKeepsTheCommittedBoundary(t *testing.T) {
	phases := []struct {
		name  string
		phase string
	}{
		{name: "HELLO", phase: mailboxProgressHello},
		{name: "DATA", phase: mailboxProgressData},
		{name: "END", phase: mailboxProgressEnd},
		{name: "DONE", phase: mailboxProgressDone},
	}
	for boundary, test := range phases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			current := task7ActiveRecord(hardwareowner.PhaseLeaseActive)
			fixture.store.record = current
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var callbackErrors []error
			fixture.store.onReplace = func(call int, _ hardwareowner.Record) {
				if call == boundary+1 {
					cancel()
				}
			}
			deps := fixture.dependencies()
			deps.mailboxProgress = func(_ context.Context, _ Registers, _ Clock, callback mailboxProgressCallback) (Observation, error) {
				payload := []byte("OSS FPGA OK\n")
				progress := []mailboxProgress{
					{Phase: mailboxProgressHello, Observation: Observation{}},
					{Phase: mailboxProgressData, Observation: Observation{Payload: payload[:1]}},
					{Phase: mailboxProgressEnd, Observation: Observation{Payload: payload}},
					{Phase: mailboxProgressDone, Observation: Observation{Payload: payload}, TerminalWord: 0xd3130c00},
				}
				for _, item := range progress {
					err := callbackErrorsAppend(callback, item, &callbackErrors)
					if err != nil {
						return item.Observation, err
					}
				}
				last := progress[len(progress)-1].Observation
				last.TerminalWord = 0xd3130c00
				return last, nil
			}
			runner := newFixtureRunner(deps)
			prepared := &preparedRun{current: current}
			observation, err := runner.runMailboxWithProgress(ctx, deps, &task7Registers{}, mailboxRealClock{}, &current, prepared)
			if len(callbackErrors) <= boundary || callbackErrors[boundary] != nil {
				t.Fatalf("callback error after committed %s boundary = %v, all=%v", test.name, callbackErrors, callbackErrors)
			}
			fixture.store.mu.Lock()
			stored := fixture.store.record
			fixture.store.mu.Unlock()
			wantPhase, _ := mailboxHardwarePhase(test.phase)
			if stored.Phase != wantPhase {
				t.Fatalf("stored phase after %s cancellation = %q, want %q", test.name, stored.Phase, wantPhase)
			}
			if test.phase == mailboxProgressDone {
				if err != nil || observation.TerminalWord != 0xd3130c00 {
					t.Fatalf("DONE cancellation after committed boundary = observation:%#v err:%v", observation, err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("%s continuation error = %v, want cancellation after next boundary", test.name, err)
			}
		})
	}
}

func callbackErrorsAppend(callback mailboxProgressCallback, progress mailboxProgress, errorsSeen *[]error) error {
	err := callback(progress)
	*errorsSeen = append(*errorsSeen, err)
	return err
}

func TestRunnerDoesNotStoreAfterFaultCheckpointLosesTheOwnerLock(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.fault = &task7LostLockFault{}
	gate := &task7MaintenanceGate{status: task7ValidMaintenanceStatus()}
	deps := fixture.dependencies()
	deps.maintenance = gate
	runner := newFixtureRunner(deps)
	result, err := runner.RunCommand(context.Background(), fixture.request)
	var failure *Failure
	if !errors.As(err, &failure) || !failure.IntentCommitted {
		t.Fatalf("RunCommand() = %v, want post-intent lock-loss failure", err)
	}
	if result.PrimaryCode == string(CodeOK) {
		t.Fatalf("lock-loss result promoted success: %#v", result)
	}
	// The fixture fault occurs after load_attempted. No later owner replacement
	// may happen without a successfully reacquired lock.
	if fixture.store.replaceCalls != 2 {
		t.Fatalf("owner replacements after lost lock = %d, want intent+load_attempted only", fixture.store.replaceCalls)
	}
	if gate.unlockCalls != 1 {
		t.Fatalf("lost owner lock left maintenance held: unlock calls=%d, want one", gate.unlockCalls)
	}
}

func TestRunnerRejectsAChangedFaultDiagnosticAfterReacquiringTheLock(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.fault = &task7DiagnosticMismatchFault{}
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(context.Background(), fixture.request)
	var failure *Failure
	if !errors.As(err, &failure) || !failure.IntentCommitted {
		t.Fatalf("RunCommand() = %v, want post-intent diagnostic mismatch", err)
	}
	if result.PrimaryCode == string(CodeOK) {
		t.Fatalf("changed fault diagnostic promoted a successful run: %#v", result)
	}
	if fixture.store.replaceCalls != 2 {
		t.Fatalf("owner replacements after changed diagnostic = %d, want intent+load_attempted only", fixture.store.replaceCalls)
	}
}

func TestRunnerRejectsContextExpiryAtEveryOrderedBoundary(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.expireAt = "fifo"
	runner := newFixtureRunner(fixture.dependencies())
	ctx, cancel := context.WithCancel(context.Background())
	fixture.cancel = cancel
	result, err := runner.RunCommand(ctx, fixture.request)
	if err != nil {
		t.Fatalf("expired run error = %v, want durable post-intent result", err)
	}
	if result.PrimaryCode == "" {
		t.Fatal("expired run returned an empty failure result")
	}
	if strings.Contains(result.PrimaryDetail, fixture.secret) {
		t.Fatalf("result leaked private fixture value: %q", result.PrimaryDetail)
	}
}

func TestRunnerAdoptsDurableNoOwnerBeforeClassifyingImmediateDeadline(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var committedNoOwner hardwareowner.Record
	fixture.store.onReplace = func(call int, next hardwareowner.Record) {
		if call == 3 {
			committedNoOwner = cloneTask7Record(next)
			cancel()
		}
	}
	runner := newFixtureRunner(fixture.dependencies())
	result, err := runner.RunCommand(ctx, fixture.request)
	if err != nil {
		t.Fatalf("RunCommand() = %v, want recovered durable deadline result", err)
	}
	if committedNoOwner.State != hardwareowner.StateNoOwner || committedNoOwner.Phase != hardwareowner.PhaseMainAbsent {
		t.Fatalf("context-unaware Replace committed %#v, want exact no_owner/main_absent", committedNoOwner)
	}
	if result.PrimaryCode != string(CodeNoOwnerQualificationFailed) || result.Phase != ResultPhaseMainAbsent || result.RecoveryRequest != "pending" {
		t.Fatalf("deadline result = %#v, want no-owner failure at main_absent with recovery pending", result)
	}
	wantFinal := recoveryRecord(committedNoOwner, CodeNoOwnerQualificationFailed)
	fixture.store.mu.Lock()
	final := cloneTask7Record(fixture.store.record)
	history := append([]hardwareowner.Record(nil), fixture.store.history...)
	fixture.store.mu.Unlock()
	if !reflect.DeepEqual(final, wantFinal) {
		t.Fatalf("recovery owner = %#v, want exact recovery from committed no_owner %#v", final, wantFinal)
	}
	if len(history) != 4 || !reflect.DeepEqual(history[2], committedNoOwner) || !reflect.DeepEqual(history[3], wantFinal) {
		t.Fatalf("durable no-owner/recovery history = %#v, want exact adjacent committed records", history)
	}
	if final.ActiveOwner != hardwareowner.OwnerNone || final.ActiveSession != "" || final.ActiveGeneration != 0 || len(final.ActiveLeases) != 0 {
		t.Fatalf("deadline recovery resurrected an active owner tuple: %#v", final)
	}
	if fixture.reboot.calls != 1 || fixture.locker.unlockCalls != 1 {
		t.Fatalf("deadline recovery lifecycle = reboot:%d unlock:%d, want 1/1", fixture.reboot.calls, fixture.locker.unlockCalls)
	}
}

func TestRunnerClosesLockReturnedAlongsideLockError(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	locker := &task7ErrorLocker{}
	deps := fixture.dependencies()
	deps.locker = locker
	runner := newFixtureRunner(deps)
	_, err := runner.RunCommand(context.Background(), fixture.request)
	if err == nil || !errors.Is(err, ErrRunPreflight) {
		t.Fatalf("RunCommand() error = %v, want pre-intent lock failure", err)
	}
	if locker.unlockCalls != 1 {
		t.Fatalf("lock cleanup calls = %d, want exactly one", locker.unlockCalls)
	}
}

func TestFaultCheckpointPersistsDiagnosticBeforeReleasingFixtureLock(t *testing.T) {
	root := task7ShortFaultRoot(t)
	runID := strings.Repeat("a", 32)
	session := strings.Repeat("b", 32)
	controller := &filesystemFaultController{
		root:        root,
		expectedUID: uint32(os.Getuid()),
		diagnostic: func(checkpoint faultDiagnosticForCheckpoint) (faultDiagnostic, error) {
			return faultDiagnostic{RunID: checkpoint.RunID, Session: checkpoint.Session, Generation: checkpoint.Generation, Phase: string(checkpoint.Phase), PID: 1234, StartTime: 5678, ExecutableSHA256: strings.Repeat("c", 64), ExecutableDevice: 9, ExecutableInode: 10}, nil
		},
	}
	if err := controller.Arm(context.Background(), runID); err != nil {
		t.Fatalf("Arm() = %v", err)
	}
	var unlockCalls, acquireCalls int
	var durableBeforeReleaseErr error
	socketPath := controller.socketName(runID)
	unlocked := func() error {
		unlockCalls++
		inspection, err := controller.Inspect(context.Background(), runID)
		if err != nil {
			durableBeforeReleaseErr = err
			return nil
		}
		if inspection.RunID != runID || inspection.Session != session || inspection.Generation != 43 || inspection.Phase != string(hardwareowner.PhaseLoadAttempted) {
			durableBeforeReleaseErr = errors.New("diagnostic did not match checkpoint before owner release")
			return nil
		}
		durableBeforeReleaseErr = validateFaultSocket(socketPath, uint32(os.Getuid()))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	reacquired, err := controller.Checkpoint(ctx, faultDiagnosticForCheckpoint{RunID: runID, Session: session, Generation: 43, Phase: hardwareowner.PhaseLoadAttempted}, unlocked, func(context.Context) (hardwareowner.Unlock, error) {
		acquireCalls++
		return func() error { return nil }, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || reacquired == nil {
		t.Fatalf("Checkpoint() = unlock:%v err:%v, want reacquired lock and deadline", reacquired, err)
	}
	if unlockCalls != 1 || acquireCalls != 1 {
		t.Fatalf("lock handoff = unlocks:%d acquires:%d", unlockCalls, acquireCalls)
	}
	if durableBeforeReleaseErr != nil {
		t.Fatalf("diagnostic/endpoint were not durable before owner release: %v", durableBeforeReleaseErr)
	}
	inspection, err := controller.Inspect(context.Background(), runID)
	if err != nil {
		t.Fatalf("Inspect() = %v", err)
	}
	if inspection.RunID != runID || inspection.Session != session || inspection.Generation != 43 || inspection.Phase != string(hardwareowner.PhaseLoadAttempted) {
		t.Fatalf("inspection = %#v", inspection)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled checkpoint socket error = %v, want absent", err)
	}
}

func TestFaultCheckpointDoesNotReturnAConsumedUnlockAfterReleaseFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runID := strings.Repeat("a", 32)
	controller := &filesystemFaultController{
		root:        root,
		expectedUID: uint32(os.Getuid()),
		diagnostic: func(checkpoint faultDiagnosticForCheckpoint) (faultDiagnostic, error) {
			return faultDiagnostic{RunID: checkpoint.RunID, Session: checkpoint.Session, Generation: checkpoint.Generation, Phase: string(checkpoint.Phase), PID: 1234, StartTime: 5678, ExecutableSHA256: strings.Repeat("c", 64), ExecutableDevice: 9, ExecutableInode: 10}, nil
		},
	}
	if err := controller.Arm(context.Background(), runID); err != nil {
		t.Fatalf("Arm() = %v", err)
	}
	wantErr := errors.New("unlock failed")
	var unlockCalls int
	reacquired, err := controller.Checkpoint(context.Background(), faultDiagnosticForCheckpoint{
		RunID: runID, Session: strings.Repeat("b", 32), Generation: 43, Phase: hardwareowner.PhaseLoadAttempted,
	}, func() error {
		unlockCalls++
		return wantErr
	}, func(context.Context) (hardwareowner.Unlock, error) {
		t.Fatal("Checkpoint reacquired after the original unlock failed")
		return nil, nil
	})
	if reacquired != nil {
		t.Fatalf("Checkpoint returned consumed unlock %v, want nil", reacquired)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Checkpoint() error = %v, want unlock failure", err)
	}
	if unlockCalls != 1 {
		t.Fatalf("unlock calls = %d, want exactly one", unlockCalls)
	}
}

func TestFaultCheckpointClosesAReacquiredLockWhenRevalidationFails(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runID := strings.Repeat("a", 32)
	controller := &filesystemFaultController{
		root:        root,
		expectedUID: uint32(os.Getuid()),
		diagnostic: func(checkpoint faultDiagnosticForCheckpoint) (faultDiagnostic, error) {
			return faultDiagnostic{RunID: checkpoint.RunID, Session: checkpoint.Session, Generation: checkpoint.Generation, Phase: string(checkpoint.Phase), PID: 1234, StartTime: 5678, ExecutableSHA256: strings.Repeat("c", 64), ExecutableDevice: 9, ExecutableInode: 10}, nil
		},
	}
	if err := controller.Arm(context.Background(), runID); err != nil {
		t.Fatalf("Arm() = %v", err)
	}
	var reacquiredUnlockCalls int
	revalidationErr := errors.New("owner revalidation failed")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	reacquired, err := controller.Checkpoint(ctx, faultDiagnosticForCheckpoint{
		RunID: runID, Session: strings.Repeat("b", 32), Generation: 43, Phase: hardwareowner.PhaseLoadAttempted,
	}, func() error { return nil }, func(context.Context) (hardwareowner.Unlock, error) {
		return func() error {
			reacquiredUnlockCalls++
			return nil
		}, revalidationErr
	})
	if reacquired != nil {
		t.Fatalf("Checkpoint returned a lock after revalidation failure: %v", reacquired)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, revalidationErr) {
		t.Fatalf("Checkpoint() error = %v, want cancellation and revalidation failure", err)
	}
	if reacquiredUnlockCalls != 1 {
		t.Fatalf("reacquired lock cleanup calls = %d, want exactly one", reacquiredUnlockCalls)
	}
}

func TestFaultCheckpointBoundsCancellationReacquisitionAndCleansEndpoint(t *testing.T) {
	t.Parallel()
	controller, checkpoint := task7BlockedCheckpointController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	missingDeadline := errors.New("cancellation reacquire had no deadline")
	blocked := &task7BlockedReacquire{missingDeadline: missingDeadline}
	var unlockCalls int
	started := time.Now()
	reacquired, err := controller.Checkpoint(ctx, checkpoint, func() error {
		unlockCalls++
		cancel()
		return nil
	}, blocked.acquire)
	if reacquired != nil {
		t.Fatalf("Checkpoint returned a lock after bounded cancellation reacquire: %v", reacquired)
	}
	if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, missingDeadline) {
		t.Fatalf("Checkpoint() = %v, want caller cancellation plus independent reacquire deadline", err)
	}
	task7AssertBlockedReacquire(t, blocked, time.Since(started))
	if unlockCalls != 1 {
		t.Fatalf("original unlock calls = %d, want one", unlockCalls)
	}
	task7AssertCheckpointSocketRemoved(t, controller, checkpoint.RunID)
}

func TestFaultCheckpointBoundsAcceptFailureReacquisitionAndCleansEndpoint(t *testing.T) {
	t.Parallel()
	controller, checkpoint := task7BlockedCheckpointController(t)
	acceptErr := errors.New("accept failed")
	var listener *task7CheckpointListener
	controller.listen = func(path string) (net.Listener, error) {
		realListener, err := net.Listen("unixpacket", path)
		if err != nil {
			return nil, err
		}
		listener = &task7CheckpointListener{Listener: realListener, acceptErr: acceptErr}
		return listener, nil
	}
	missingDeadline := errors.New("accept-failure reacquire had no deadline")
	blocked := &task7BlockedReacquire{missingDeadline: missingDeadline}
	var unlockCalls int
	started := time.Now()
	reacquired, err := controller.Checkpoint(context.Background(), checkpoint, func() error {
		unlockCalls++
		return nil
	}, blocked.acquire)
	if reacquired != nil {
		t.Fatalf("Checkpoint returned a lock after bounded accept-failure reacquire: %v", reacquired)
	}
	if !errors.Is(err, acceptErr) || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, missingDeadline) {
		t.Fatalf("Checkpoint() = %v, want accept failure plus independent reacquire deadline", err)
	}
	task7AssertBlockedReacquire(t, blocked, time.Since(started))
	if unlockCalls != 1 || listener == nil || listener.closeCalls != 1 {
		t.Fatalf("accept-failure cleanup = unlock:%d listener:%#v, want one unlock/close", unlockCalls, listener)
	}
	task7AssertCheckpointSocketRemoved(t, controller, checkpoint.RunID)
}

func TestFaultCheckpointBoundsFailedSelfKillReacquisitionAndCleansEndpoint(t *testing.T) {
	t.Parallel()
	controller, checkpoint := task7BlockedCheckpointController(t)
	killErr := errors.New("self kill failed")
	var killCalls int
	controller.selfKill = func() error {
		killCalls++
		return killErr
	}
	clientDone := make(chan error, 1)
	var listener *task7CheckpointListener
	controller.listen = func(path string) (net.Listener, error) {
		realListener, err := net.Listen("unixpacket", path)
		if err != nil {
			return nil, err
		}
		listener = &task7CheckpointListener{Listener: realListener}
		go func() {
			connection, dialErr := net.DialTimeout("unixpacket", path, time.Second)
			if dialErr != nil {
				clientDone <- dialErr
				return
			}
			request, marshalErr := json.Marshal(faultRequest{RunID: checkpoint.RunID, Session: checkpoint.Session, Generation: checkpoint.Generation})
			if marshalErr == nil {
				_, marshalErr = connection.Write(request)
			}
			clientDone <- errors.Join(marshalErr, connection.Close())
		}()
		return listener, nil
	}
	missingDeadline := errors.New("self-kill-failure reacquire had no deadline")
	blocked := &task7BlockedReacquire{missingDeadline: missingDeadline}
	var unlockCalls int
	started := time.Now()
	reacquired, err := controller.Checkpoint(context.Background(), checkpoint, func() error {
		unlockCalls++
		return nil
	}, blocked.acquire)
	if clientErr := <-clientDone; clientErr != nil {
		t.Fatalf("fault client = %v", clientErr)
	}
	if reacquired != nil {
		t.Fatalf("Checkpoint returned a lock after bounded self-kill-failure reacquire: %v", reacquired)
	}
	if !errors.Is(err, killErr) || !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, missingDeadline) {
		t.Fatalf("Checkpoint() = %v, want self-kill failure plus independent reacquire deadline", err)
	}
	task7AssertBlockedReacquire(t, blocked, time.Since(started))
	if unlockCalls != 1 || killCalls != 1 || listener == nil || listener.closeCalls != 1 {
		t.Fatalf("self-kill-failure cleanup = unlock:%d kill:%d listener:%#v, want 1/1/one close", unlockCalls, killCalls, listener)
	}
	task7AssertCheckpointSocketRemoved(t, controller, checkpoint.RunID)
}

type task7BlockedReacquire struct {
	calls           int
	remaining       time.Duration
	missingDeadline error
}

func (b *task7BlockedReacquire) acquire(ctx context.Context) (hardwareowner.Unlock, error) {
	b.calls++
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, b.missingDeadline
	}
	b.remaining = time.Until(deadline)
	<-ctx.Done()
	return nil, ctx.Err()
}

type task7CheckpointListener struct {
	net.Listener
	acceptErr  error
	closeCalls int
}

func (l *task7CheckpointListener) Accept() (net.Conn, error) {
	if l.acceptErr != nil {
		return nil, l.acceptErr
	}
	return l.Listener.Accept()
}

func (l *task7CheckpointListener) Close() error {
	l.closeCalls++
	return l.Listener.Close()
}

func task7BlockedCheckpointController(t *testing.T) (*filesystemFaultController, faultDiagnosticForCheckpoint) {
	t.Helper()
	root := task7ShortFaultRoot(t)
	diagnostic := task7FaultDiagnostic(os.Getpid())
	controller := &filesystemFaultController{
		root:        root,
		expectedUID: uint32(os.Getuid()),
		diagnostic: func(faultDiagnosticForCheckpoint) (faultDiagnostic, error) {
			return diagnostic, nil
		},
	}
	if err := controller.Arm(context.Background(), diagnostic.RunID); err != nil {
		t.Fatalf("Arm() = %v", err)
	}
	return controller, faultDiagnosticForCheckpoint{
		RunID: diagnostic.RunID, Session: diagnostic.Session, Generation: diagnostic.Generation, Phase: hardwareowner.PhaseLoadAttempted,
	}
}

func task7AssertBlockedReacquire(t *testing.T, blocked *task7BlockedReacquire, elapsed time.Duration) {
	t.Helper()
	if blocked.calls != 1 || blocked.remaining < 500*time.Millisecond || blocked.remaining > 2*time.Second {
		t.Fatalf("blocked reacquire = calls:%d remaining:%v, want one fresh finite approximately-one-second bound", blocked.calls, blocked.remaining)
	}
	if elapsed < 500*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("blocked reacquire elapsed = %v, want finite fixed bound", elapsed)
	}
}

func task7AssertCheckpointSocketRemoved(t *testing.T, controller *filesystemFaultController, runID string) {
	t.Helper()
	if _, err := os.Lstat(controller.socketName(runID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkpoint socket after teardown = %v, want absent", err)
	}
}

func TestFaultDiagnosticRequiresExecutableIdentityAndPeerRevalidation(t *testing.T) {
	diagnostic := faultDiagnostic{
		RunID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32),
		Generation: 43, Phase: string(hardwareowner.PhaseLoadAttempted), PID: 1234,
		StartTime: 5678, ExecutableSHA256: strings.Repeat("c", 64),
		ExecutableDevice: 9, ExecutableInode: 10,
	}
	if err := validateFaultDiagnostic(diagnostic); err != nil {
		t.Fatalf("valid diagnostic rejected: %v", err)
	}
	diagnostic.ExecutableInode = 0
	if err := validateFaultDiagnostic(diagnostic); !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("incomplete executable identity error = %v, want unsupported", err)
	}
	identity := faultPeerIdentity{PID: diagnostic.PID, StartTime: diagnostic.StartTime, Device: diagnostic.ExecutableDevice, Inode: 11, SHA256: diagnostic.ExecutableSHA256}
	if err := validateFaultPeer(identity, diagnostic); !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("replaced executable error = %v, want unsupported", err)
	}
}

func TestFaultDiagnosticBindsTheRunningImageDescriptorAcrossAtomicReplacement(t *testing.T) {
	root := t.TempDir()
	executablePath := filepath.Join(root, "fixture-runner")
	oldImage := []byte("fixture executable before replacement\n")
	newImage := []byte("fixture executable after replacement\n")
	if err := os.WriteFile(executablePath, oldImage, 0o700); err != nil {
		t.Fatal(err)
	}
	runningImage, err := os.Open(executablePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runningImage.Close() })
	info, err := runningImage.Stat()
	if err != nil {
		t.Fatal(err)
	}
	device, inode, ok := fileDeviceInode(info)
	if !ok || device == 0 || inode == 0 {
		t.Fatal("controlled running image lacks a device/inode identity")
	}
	replacement := filepath.Join(root, "replacement")
	if err := os.WriteFile(replacement, newImage, 0o700); err != nil {
		t.Fatal(err)
	}
	var replaceErr error
	controller := &filesystemFaultController{
		openSelfExecutable: func() (*os.File, error) { return runningImage, nil },
		selfStartTime:      func() (uint64, error) { return 99, nil },
		afterExecutableOpen: func() {
			replaceErr = os.Rename(replacement, executablePath)
		},
	}
	diagnostic, err := controller.diagnosticFor(faultDiagnosticForCheckpoint{
		RunID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Generation: 7, Phase: hardwareowner.PhaseLoadAttempted,
	})
	if replaceErr != nil {
		t.Fatalf("atomic executable replacement = %v", replaceErr)
	}
	if err != nil {
		t.Fatalf("diagnosticFor() = %v", err)
	}
	oldDigest := sha256.Sum256(oldImage)
	if diagnostic.ExecutableSHA256 != hex.EncodeToString(oldDigest[:]) || diagnostic.ExecutableDevice != device || diagnostic.ExecutableInode != inode || diagnostic.StartTime != 99 {
		t.Fatalf("descriptor-bound diagnostic = %#v, want original running image", diagnostic)
	}
	onDisk, err := os.ReadFile(executablePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, newImage) {
		t.Fatalf("replacement did not change pathname image: %q", onDisk)
	}
}

func TestFaultDiagnosticMustMatchTheFencedOwnerTuple(t *testing.T) {
	diagnostic := faultDiagnostic{
		RunID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32),
		Generation: 43, Phase: string(hardwareowner.PhaseLoadAttempted), PID: 1234,
		StartTime: 5678, ExecutableSHA256: strings.Repeat("c", 64),
		ExecutableDevice: 9, ExecutableInode: 10,
	}
	if err := validateFaultDiagnosticFence(diagnostic, diagnostic.RunID, diagnostic.Session, diagnostic.Generation, hardwareowner.PhaseLoadAttempted); err != nil {
		t.Fatalf("valid fenced diagnostic rejected: %v", err)
	}
	mutations := []struct {
		name string
		edit func(*faultDiagnostic)
	}{
		{name: "run", edit: func(value *faultDiagnostic) { value.RunID = strings.Repeat("d", 32) }},
		{name: "session", edit: func(value *faultDiagnostic) { value.Session = strings.Repeat("d", 32) }},
		{name: "generation", edit: func(value *faultDiagnostic) { value.Generation++ }},
		{name: "phase", edit: func(value *faultDiagnostic) { value.Phase = string(hardwareowner.PhaseIntentCommitted) }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := diagnostic
			mutation.edit(&candidate)
			if err := validateFaultDiagnosticFence(candidate, diagnostic.RunID, diagnostic.Session, diagnostic.Generation, hardwareowner.PhaseLoadAttempted); !errors.Is(err, ErrFaultUnsupported) {
				t.Fatalf("%s mismatch error = %v, want unsupported", mutation.name, err)
			}
		})
	}
}

func TestRunnerFaultKillPassesTheFencedOwnerTuple(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	intent := task7NormalMainRecord()
	intent.State = hardwareowner.StateRecoveringIntent
	intent.Phase = hardwareowner.PhaseLoadAttempted
	intent.RunID = fixture.manifest.RunID
	intent.GenerationHighWater = 43
	intent.CandidateSession = strings.Repeat("3", 32)
	intent.CandidateGeneration = 43
	intent.CandidateMode = hardwareowner.ModeUpdating
	intent.CandidateOwner = hardwareowner.OwnerFPGADev
	intent.QuiescingOwner = hardwareowner.OwnerCompatMain
	intent.RequestedResources = hardwareowner.DevelopmentLeases()
	if err := intent.Validate(); err != nil {
		t.Fatalf("fenced fixture record invalid: %v", err)
	}
	fixture.store.record = intent
	faults := &task7BoundFault{}
	deps := fixture.dependencies()
	deps.fault = faults
	runner := newFixtureRunner(deps)
	if err := runner.FaultKill(context.Background(), fixture.manifest.RunID); err != nil {
		t.Fatalf("FaultKill() = %v", err)
	}
	if faults.unboundCalls != 0 || faults.runID != intent.RunID || faults.session != intent.CandidateSession || faults.generation != intent.CandidateGeneration || faults.phase != intent.Phase {
		t.Fatalf("fault fence = %#v, want exact owner tuple", faults)
	}
}

func TestRunnerFaultKillRejectsAStaleBootFence(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	intent := task7NormalMainRecord()
	intent.State = hardwareowner.StateRecoveringIntent
	intent.Phase = hardwareowner.PhaseLoadAttempted
	intent.RunID = fixture.manifest.RunID
	intent.GenerationHighWater = 43
	intent.CandidateSession = strings.Repeat("3", 32)
	intent.CandidateGeneration = 43
	intent.CandidateMode = hardwareowner.ModeUpdating
	intent.CandidateOwner = hardwareowner.OwnerFPGADev
	intent.QuiescingOwner = hardwareowner.OwnerCompatMain
	intent.RequestedResources = hardwareowner.DevelopmentLeases()
	intent.BootID = "fedcba98-7654-3210-fedc-ba9876543210"
	if err := intent.Validate(); err != nil {
		t.Fatalf("stale boot fixture record invalid: %v", err)
	}
	fixture.store.record = intent
	faults := &task7BoundFault{}
	deps := fixture.dependencies()
	deps.fault = faults
	runner := newFixtureRunner(deps)
	if err := runner.FaultKill(context.Background(), fixture.manifest.RunID); !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("FaultKill() = %v, want stale-boot rejection", err)
	}
	if faults.runID != "" {
		t.Fatalf("stale boot fence reached fault endpoint: %#v", faults)
	}
}

func TestRecoveryRebootAcceptsEveryCanonicalPostIntentState(t *testing.T) {
	legal := []struct {
		name   string
		record hardwareowner.Record
	}{
		{name: "recovering intent committed", record: task7IntentRecord(hardwareowner.PhaseIntentCommitted)},
		{name: "recovering load attempted", record: task7IntentRecord(hardwareowner.PhaseLoadAttempted)},
		{name: "no owner", record: task7NoOwnerRecord()},
		{name: "lease active", record: task7ActiveRecord(hardwareowner.PhaseLeaseActive)},
		{name: "hello observed", record: task7ActiveRecord(hardwareowner.PhaseHelloObserved)},
		{name: "message partial", record: task7ActiveRecord(hardwareowner.PhaseMessagePartial)},
		{name: "end ack", record: task7ActiveRecord(hardwareowner.PhaseEndAckWritten)},
		{name: "done observed", record: task7ActiveRecord(hardwareowner.PhaseDoneObserved)},
		{name: "recovery required", record: task7RecoveryRequiredRecord()},
	}
	for _, test := range legal {
		t.Run(test.name, func(t *testing.T) {
			if err := test.record.Validate(); err != nil {
				t.Fatalf("fixture record is invalid: %v", err)
			}
			if !validRecoveryRebootRecord(test.record) {
				t.Fatalf("validRecoveryRebootRecord(%#v) = false", test.record)
			}
			fixture := newTask7RunnerFixture(t)
			fixture.store.record = test.record
			events := &task7EventLog{}
			deps := fixture.dependencies()
			deps.maintenance = &task7MaintenanceGate{status: task7ValidMaintenanceStatus(), events: events}
			deps.locker = &task7EventOwnerLocker{events: events}
			runner := newFixtureRunner(deps)
			if err := runner.RecoveryReboot(context.Background(), test.record.RunID); err != nil {
				t.Fatalf("RecoveryReboot() = %v", err)
			}
			if fixture.reboot.calls != 1 {
				t.Fatalf("reboot calls = %d, want one", fixture.reboot.calls)
			}
			if got, want := events.snapshot(), []string{"maintenance.enter", "owner.enter", "owner.release", "maintenance.release"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("legal recovery lock order = %v, want %v", got, want)
			}
		})
	}

	rejected := []struct {
		name   string
		record hardwareowner.Record
		runID  string
	}{
		{name: "normal main", record: task7NormalMainRecord(), runID: strings.Repeat("a", 32)},
		{name: "run ID mismatch", record: task7IntentRecord(hardwareowner.PhaseIntentCommitted), runID: strings.Repeat("f", 32)},
		{name: "recovering invalid phase", record: task7IntentRecord(hardwareowner.PhaseMainAbsent), runID: strings.Repeat("a", 32)},
	}
	for _, test := range rejected {
		t.Run("reject "+test.name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			fixture.store.record = test.record
			events := &task7EventLog{}
			deps := fixture.dependencies()
			deps.maintenance = &task7MaintenanceGate{status: task7ValidMaintenanceStatus(), events: events}
			deps.locker = &task7EventOwnerLocker{events: events}
			runner := newFixtureRunner(deps)
			if err := runner.RecoveryReboot(context.Background(), test.runID); !errors.Is(err, ErrFaultUnsupported) {
				t.Fatalf("RecoveryReboot() = %v, want unsupported", err)
			}
			if fixture.reboot.calls != 0 {
				t.Fatalf("reboot calls = %d, want zero", fixture.reboot.calls)
			}
			if got, want := events.snapshot(), []string{"maintenance.enter", "owner.enter", "owner.release", "maintenance.release"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("rejected recovery lock order = %v, want %v", got, want)
			}
		})
	}
}

func TestRecoveryRebootAcquiresMaintenanceBeforeOwnerAndReleasesInReverseOrder(t *testing.T) {
	fixture := newTask7RunnerFixture(t)
	fixture.store.record = task7RecoveryRequiredRecord()
	events := &task7EventLog{}
	gate := &task7MaintenanceGate{status: task7ValidMaintenanceStatus(), events: events}
	locker := &task7EventOwnerLocker{events: events}
	deps := fixture.dependencies()
	deps.maintenance = gate
	deps.locker = locker
	runner := newFixtureRunner(deps)
	if err := runner.RecoveryReboot(context.Background(), fixture.store.record.RunID); err != nil {
		t.Fatalf("RecoveryReboot() = %v", err)
	}
	if got, want := events.snapshot(), []string{"maintenance.enter", "owner.enter", "owner.release", "maintenance.release"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recovery lock order = %v, want %v", got, want)
	}
}

func TestRecoveryRebootMaintainsLockOrderingOnErrorAndCancellationPaths(t *testing.T) {
	tests := []struct {
		name       string
		record     hardwareowner.Record
		configure  func(*runnerDependencies, *task7EventOwnerLocker, context.CancelFunc)
		wantEvents []string
	}{
		{
			name:       "invalid owner record",
			record:     task7NormalMainRecord(),
			wantEvents: []string{"maintenance.enter", "owner.enter", "owner.release", "maintenance.release"},
		},
		{
			name:   "owner lock returns unlock and error",
			record: task7RecoveryRequiredRecord(),
			configure: func(_ *runnerDependencies, locker *task7EventOwnerLocker, _ context.CancelFunc) {
				locker.lockErr = errors.New("owner lock failed after acquisition")
			},
			wantEvents: []string{"maintenance.enter", "owner.enter", "owner.release", "maintenance.release"},
		},
		{
			name:   "reboot error",
			record: task7RecoveryRequiredRecord(),
			configure: func(d *runnerDependencies, _ *task7EventOwnerLocker, _ context.CancelFunc) {
				d.reboot = task7RebooterFunc(func(context.Context) error { return errors.New("reboot failed") })
			},
			wantEvents: []string{"maintenance.enter", "owner.enter", "owner.release", "maintenance.release"},
		},
		{
			name:   "cancellation during reboot request",
			record: task7RecoveryRequiredRecord(),
			configure: func(d *runnerDependencies, _ *task7EventOwnerLocker, cancel context.CancelFunc) {
				d.reboot = task7RebooterFunc(func(context.Context) error {
					cancel()
					return context.Canceled
				})
			},
			wantEvents: []string{"maintenance.enter", "owner.enter", "owner.release", "maintenance.release"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newTask7RunnerFixture(t)
			fixture.store.record = test.record
			events := &task7EventLog{}
			gate := &task7MaintenanceGate{status: task7ValidMaintenanceStatus(), events: events}
			locker := &task7EventOwnerLocker{events: events}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deps := fixture.dependencies()
			deps.maintenance, deps.locker = gate, locker
			if test.configure != nil {
				test.configure(&deps, locker, cancel)
			}
			runner := newFixtureRunner(deps)
			runID := test.record.RunID
			if runID == "" {
				runID = strings.Repeat("a", 32)
			}
			if err := runner.RecoveryReboot(ctx, runID); err == nil {
				t.Fatal("RecoveryReboot() succeeded on an error/cancellation path")
			}
			if got := events.snapshot(); !reflect.DeepEqual(got, test.wantEvents) {
				t.Fatalf("recovery %s lock order = %v, want %v", test.name, got, test.wantEvents)
			}
			if fixture.store.replaceCalls != 0 {
				t.Fatalf("recovery %s mutated owner record: replaces=%d", test.name, fixture.store.replaceCalls)
			}
		})
	}

	// Maintenance status rejection happens before an owner lock exists, but its
	// returned unlock must still be released exactly once.
	fixture := newTask7RunnerFixture(t)
	events := &task7EventLog{}
	gate := &task7MaintenanceGate{status: MaintenanceStatus{}, events: events}
	deps := fixture.dependencies()
	deps.maintenance = gate
	runner := newFixtureRunner(deps)
	if err := runner.RecoveryReboot(context.Background(), strings.Repeat("a", 32)); err == nil {
		t.Fatal("RecoveryReboot() accepted an invalid maintenance status")
	}
	if got, want := events.snapshot(), []string{"maintenance.enter", "maintenance.release"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("maintenance-status lock order = %v, want %v", got, want)
	}
}

func TestFaultInspectionRejectsDuplicateOrNoncanonicalDiagnostic(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	controller := &filesystemFaultController{root: root, expectedUID: uint32(os.Getuid())}
	runID := strings.Repeat("a", 32)
	diagnosticPath := filepath.Join(root, "fpga-dev-"+runID+".diagnostic")
	if err := os.WriteFile(filepath.Join(root, "fpga-dev-"+runID+".armed"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	raw := `{"run_id":"` + runID + `","session":"` + strings.Repeat("b", 32) + `","generation":43,"phase":"load_attempted","pid":1234,"start_time":5678,"executable_sha256":"` + strings.Repeat("c", 64) + `","executable_device":9,"executable_inode":10,"executable_inode":10}
`
	if err := os.WriteFile(diagnosticPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Inspect(context.Background(), runID); !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("duplicate diagnostic error = %v, want unsupported", err)
	}
}

func TestFaultInspectionRequiresTheArmedMarker(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	controller := &filesystemFaultController{root: root, expectedUID: uint32(os.Getuid())}
	runID := strings.Repeat("a", 32)
	diagnostic := faultDiagnostic{
		RunID: runID, Session: strings.Repeat("b", 32), Generation: 43,
		Phase: string(hardwareowner.PhaseLoadAttempted), PID: 1234, StartTime: 5678,
		ExecutableSHA256: strings.Repeat("c", 64), ExecutableDevice: 9, ExecutableInode: 10,
	}
	raw, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fpga-dev-"+runID+".diagnostic"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Inspect(context.Background(), runID); !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("unarmed diagnostic error = %v, want unsupported", err)
	}
}

func TestFaultRequestRejectsNoncanonicalFraming(t *testing.T) {
	request := faultRequest{RunID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Generation: 43}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseFaultRequest(append(raw, '\n')); !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("newline-terminated fault request error = %v, want unsupported", err)
	}
}

func TestLinuxPeerCredentialsReadsRealUnixPacketPeer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.Listen("unixpacket", path)
	if err != nil {
		t.Skipf("unixpacket is unavailable in this test environment: %v", err)
	}
	defer listener.Close()

	client, err := net.Dial("unixpacket", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	pid, uid, err := linuxPeerCredentials(server)
	if err != nil {
		t.Fatalf("linuxPeerCredentials() = %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("peer pid = %d, want test process %d", pid, os.Getpid())
	}
	if uid != uint32(os.Getuid()) {
		t.Fatalf("peer uid = %d, want test uid %d", uid, os.Getuid())
	}
}

func TestFaultKillUsesAnIdentityBoundUnixPacketEndpoint(t *testing.T) {
	// Keep the fixture root short enough for a filesystem AF_UNIX pathname;
	// the production fallback to an abstract address is only for roots that
	// cannot represent the full collision-resistant digest on disk.
	root, err := os.MkdirTemp("/tmp", "fg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runID := strings.Repeat("a", 32)
	session := strings.Repeat("b", 32)
	diagnostic := faultDiagnostic{
		RunID: runID, Session: session, Generation: 7,
		Phase: string(hardwareowner.PhaseLoadAttempted), PID: os.Getpid(), StartTime: 11,
		ExecutableSHA256: strings.Repeat("c", 64), ExecutableDevice: 12, ExecutableInode: 13,
	}
	marker := filepath.Join(root, "fpga-dev-"+runID+".armed")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fpga-dev-"+runID+".diagnostic"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &filesystemFaultController{
		root:        root,
		expectedUID: uint32(os.Getuid()),
		identity: func(int) (faultPeerIdentity, error) {
			return faultPeerIdentity{PID: diagnostic.PID, StartTime: diagnostic.StartTime, Device: diagnostic.ExecutableDevice, Inode: diagnostic.ExecutableInode, SHA256: diagnostic.ExecutableSHA256}, nil
		},
		peerCredentials: func(net.Conn) (int, uint32, error) {
			return diagnostic.PID, uint32(os.Getuid()), nil
		},
		processGone: func(context.Context, faultDiagnostic) error { return nil },
	}
	socketPath := controller.socketName(runID)
	listener, err := net.Listen("unixpacket", socketPath)
	if err != nil {
		t.Skipf("unixpacket is unavailable in this test environment: %v", err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}
	requestCh := make(chan []byte, 1)
	serverErr := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverErr <- acceptErr
			return
		}
		defer connection.Close()
		var request [512]byte
		n, readErr := connection.Read(request[:])
		if readErr != nil {
			serverErr <- readErr
			return
		}
		requestCh <- append([]byte(nil), request[:n]...)
	}()

	controller.dial = func(ctx context.Context, network, path string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, path)
	}
	if err := controller.KillBound(context.Background(), runID, session, diagnostic.Generation, hardwareowner.PhaseLoadAttempted); err != nil {
		t.Fatalf("KillBound() = %v", err)
	}
	select {
	case request := <-requestCh:
		parsed, parseErr := parseFaultRequest(request)
		if parseErr != nil || parsed.RunID != runID || parsed.Session != session || parsed.Generation != diagnostic.Generation {
			t.Fatalf("endpoint request = %q, parse error = %v, parsed = %#v", request, parseErr, parsed)
		}
	case err := <-serverErr:
		t.Fatalf("endpoint server = %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for identity-bound request")
	}
}

func TestFaultKillControlMatrixRejectsExitPeerReplacementPIDReuseAndStaleEndpoint(t *testing.T) {
	t.Run("exit before connect", func(t *testing.T) {
		root := task7ShortFaultRoot(t)
		diagnostic := task7FaultDiagnostic(4242)
		controller := task7BoundFaultController(t, root, diagnostic)
		dialCalls := 0
		controller.identity = func(int) (faultPeerIdentity, error) { return faultPeerIdentity{}, os.ErrNotExist }
		controller.dial = func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial a disappeared process")
		}
		if err := controller.KillBound(context.Background(), diagnostic.RunID, diagnostic.Session, diagnostic.Generation, hardwareowner.PhaseLoadAttempted); !errors.Is(err, ErrFaultUnsupported) {
			t.Fatalf("KillBound() = %v, want disappeared-peer refusal", err)
		}
		if dialCalls != 0 {
			t.Fatalf("disappeared peer reached endpoint dial %d times", dialCalls)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(*faultPeerIdentity)
	}{
		{name: "peer executable replacement", mutate: func(peer *faultPeerIdentity) { peer.Inode++ }},
		{name: "PID reuse after initial validation", mutate: func(peer *faultPeerIdentity) { peer.StartTime++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := task7ShortFaultRoot(t)
			diagnostic := task7FaultDiagnostic(4242)
			controller := task7BoundFaultController(t, root, diagnostic)
			socketPath := controller.socketName(diagnostic.RunID)
			listener, err := net.Listen("unixpacket", socketPath)
			if err != nil {
				t.Skipf("unixpacket is unavailable in this test environment: %v", err)
			}
			defer listener.Close()
			if err := os.Chmod(socketPath, 0o600); err != nil {
				t.Fatal(err)
			}
			readResult := make(chan struct {
				n   int
				err error
			}, 1)
			go func() {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					readResult <- struct {
						n   int
						err error
					}{err: acceptErr}
					return
				}
				defer connection.Close()
				_ = connection.SetReadDeadline(time.Now().Add(time.Second))
				var raw [512]byte
				n, readErr := connection.Read(raw[:])
				readResult <- struct {
					n   int
					err error
				}{n: n, err: readErr}
			}()
			identityCalls := 0
			controller.identity = func(int) (faultPeerIdentity, error) {
				identityCalls++
				peer := faultPeerIdentity{PID: diagnostic.PID, StartTime: diagnostic.StartTime, Device: diagnostic.ExecutableDevice, Inode: diagnostic.ExecutableInode, SHA256: diagnostic.ExecutableSHA256}
				if identityCalls == 2 {
					test.mutate(&peer)
				}
				return peer, nil
			}
			controller.peerCredentials = func(net.Conn) (int, uint32, error) { return diagnostic.PID, uint32(os.Getuid()), nil }
			if err := controller.KillBound(context.Background(), diagnostic.RunID, diagnostic.Session, diagnostic.Generation, hardwareowner.PhaseLoadAttempted); !errors.Is(err, ErrFaultUnsupported) {
				t.Fatalf("KillBound() = %v, want peer revalidation refusal", err)
			}
			if identityCalls != 2 {
				t.Fatalf("identity checks = %d, want before-connect and post-peer checks", identityCalls)
			}
			select {
			case result := <-readResult:
				if result.n != 0 {
					t.Fatalf("replaced/reused peer received a fault packet: bytes=%d err=%v", result.n, result.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("replaced/reused peer endpoint did not close")
			}
		})
	}

	t.Run("stale endpoint leaves unrelated helper alive", func(t *testing.T) {
		root := task7ShortFaultRoot(t)
		marker := filepath.Join(root, "unrelated-helper-survived")
		command := exec.Command(os.Args[0], "-test.run=^TestTask7UnrelatedFaultProcessHelper$")
		command.Env = append(os.Environ(), "FOGCAST_TASK7_UNRELATED_HELPER=1", "FOGCAST_TASK7_UNRELATED_MARKER="+marker)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		diagnostic := task7FaultDiagnostic(command.Process.Pid)
		controller := task7BoundFaultController(t, root, diagnostic)
		controller.identity = func(int) (faultPeerIdentity, error) {
			return faultPeerIdentity{PID: diagnostic.PID, StartTime: diagnostic.StartTime, Device: diagnostic.ExecutableDevice, Inode: diagnostic.ExecutableInode, SHA256: diagnostic.ExecutableSHA256}, nil
		}
		processGoneCalls := 0
		controller.processGone = func(context.Context, faultDiagnostic) error {
			processGoneCalls++
			return nil
		}
		if err := controller.KillBound(context.Background(), diagnostic.RunID, diagnostic.Session, diagnostic.Generation, hardwareowner.PhaseLoadAttempted); !errors.Is(err, ErrFaultUnsupported) {
			t.Fatalf("KillBound() = %v, want stale-endpoint refusal", err)
		}
		if processGoneCalls != 0 {
			t.Fatalf("stale endpoint reached process-gone completion %d times", processGoneCalls)
		}
		if err := command.Wait(); err != nil {
			t.Fatalf("external stale-endpoint path killed unrelated helper: %v", err)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("unrelated helper did not survive stale endpoint: %v", err)
		}
	})
}

func TestTask7UnrelatedFaultProcessHelper(t *testing.T) {
	if os.Getenv("FOGCAST_TASK7_UNRELATED_HELPER") != "1" {
		return
	}
	marker := os.Getenv("FOGCAST_TASK7_UNRELATED_MARKER")
	if marker == "" {
		os.Exit(72)
	}
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(marker, []byte("survived\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func task7ShortFaultRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "fg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func task7FaultDiagnostic(pid int) faultDiagnostic {
	return faultDiagnostic{
		RunID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Generation: 7,
		Phase: string(hardwareowner.PhaseLoadAttempted), PID: pid, StartTime: 11,
		ExecutableSHA256: strings.Repeat("c", 64), ExecutableDevice: 12, ExecutableInode: 13,
	}
}

func task7BoundFaultController(t *testing.T, root string, diagnostic faultDiagnostic) *filesystemFaultController {
	t.Helper()
	controller := &filesystemFaultController{root: root, expectedUID: uint32(os.Getuid())}
	if err := controller.Arm(context.Background(), diagnostic.RunID); err != nil {
		t.Fatalf("Arm() = %v", err)
	}
	if err := controller.writeDiagnostic(diagnostic.RunID, diagnostic); err != nil {
		t.Fatalf("writeDiagnostic() = %v", err)
	}
	return controller
}

func TestFix5FaultKillRejectsRealStaleProtectedUnixPacketEndpoint(t *testing.T) {
	root := task7ShortFaultRoot(t)
	target := task7StartFix5ManagedProcess(t, "idle", nil)
	target.requireLine(t, "ready")
	unrelated := task7StartFix5ManagedProcess(t, "idle", nil)
	unrelated.requireLine(t, "ready")

	diagnostic := task7Fix5DiagnosticForProcess(t, target.command.Process.Pid)
	runner, controller := task7Fix5BoundRunner(t, root, diagnostic)
	socketPath := controller.socketName(diagnostic.RunID)
	task7Fix5LeaveStalePacketEndpoint(t, socketPath)

	dialCalls := 0
	var realDialErr error
	controller.dial = func(ctx context.Context, network, path string) (net.Conn, error) {
		dialCalls++
		connection, err := (&net.Dialer{}).DialContext(ctx, network, path)
		realDialErr = err
		return connection, err
	}
	processGoneCalls := 0
	controller.processGone = func(context.Context, faultDiagnostic) error {
		processGoneCalls++
		return nil
	}

	err := runner.FaultKill(context.Background(), diagnostic.RunID)
	if !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("FaultKill() = %v, want real stale-endpoint refusal", err)
	}
	if dialCalls != 1 {
		t.Fatalf("real unixpacket dial calls = %d, want one", dialCalls)
	}
	if realDialErr == nil {
		t.Fatal("closed SOCK_SEQPACKET listener unexpectedly accepted a real connection")
	}
	if processGoneCalls != 0 {
		t.Fatalf("stale endpoint reached process-gone completion %d times", processGoneCalls)
	}
	if err := validateFaultSocket(socketPath, uint32(os.Getuid())); err != nil {
		t.Fatalf("closed listener did not leave the exact protected socket node: %v", err)
	}
	target.requireCleanStop(t)
	unrelated.requireCleanStop(t)
}

func TestFix5FaultKillRejectsLiveReplacementPeerByRealPeerCredentials(t *testing.T) {
	root := task7ShortFaultRoot(t)
	diagnosticProcess := task7StartFix5ManagedProcess(t, "idle", nil)
	diagnosticProcess.requireLine(t, "ready")
	diagnostic := task7Fix5DiagnosticForProcess(t, diagnosticProcess.command.Process.Pid)
	runner, controller := task7Fix5BoundRunner(t, root, diagnostic)
	socketPath := controller.socketName(diagnostic.RunID)
	replacement := task7StartFix5ManagedProcess(t, "peer", map[string]string{
		"FOGCAST_TASK7_FIX5_SOCKET": socketPath,
	})
	replacement.requireLine(t, "ready")
	if err := validateFaultSocket(socketPath, uint32(os.Getuid())); err != nil {
		t.Fatalf("replacement endpoint metadata = %v", err)
	}

	dialCalls := 0
	controller.dial = func(ctx context.Context, network, path string) (net.Conn, error) {
		dialCalls++
		return (&net.Dialer{}).DialContext(ctx, network, path)
	}
	processGoneCalls := 0
	controller.processGone = func(context.Context, faultDiagnostic) error {
		processGoneCalls++
		return nil
	}

	err := runner.FaultKill(context.Background(), diagnostic.RunID)
	if !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("FaultKill() = %v, want real SO_PEERCRED replacement refusal", err)
	}
	if dialCalls != 1 {
		t.Fatalf("replacement endpoint dial calls = %d, want one", dialCalls)
	}
	if processGoneCalls != 0 {
		t.Fatalf("replacement endpoint reached process-gone completion %d times", processGoneCalls)
	}
	replacement.requireLine(t, "packet=0")
	diagnosticProcess.requireCleanStop(t)
	replacement.requireCleanStop(t)
}

func TestFix5FaultKillTargetExitsAfterInitialValidationBeforeRealDial(t *testing.T) {
	root := task7ShortFaultRoot(t)
	target := task7StartFix5ManagedProcess(t, "idle", nil)
	target.requireLine(t, "ready")
	unrelated := task7StartFix5ManagedProcess(t, "idle", nil)
	unrelated.requireLine(t, "ready")
	diagnostic := task7Fix5DiagnosticForProcess(t, target.command.Process.Pid)
	runner, controller := task7Fix5BoundRunner(t, root, diagnostic)
	task7Fix5LeaveStalePacketEndpoint(t, controller.socketName(diagnostic.RunID))

	identityCalls := 0
	controller.identity = func(pid int) (faultPeerIdentity, error) {
		identityCalls++
		return faultProcessIdentity(pid)
	}
	dialCalls := 0
	var realDialErr error
	controller.dial = func(ctx context.Context, network, path string) (net.Conn, error) {
		dialCalls++
		if err := target.cleanStop(); err != nil {
			return nil, fmt.Errorf("target did not exit at dial boundary: %w", err)
		}
		connection, err := (&net.Dialer{}).DialContext(ctx, network, path)
		realDialErr = err
		return connection, err
	}

	err := runner.FaultKill(context.Background(), diagnostic.RunID)
	if !errors.Is(err, ErrFaultUnsupported) {
		t.Fatalf("FaultKill() = %v, want post-validation/pre-connect exit refusal", err)
	}
	if identityCalls != 1 || dialCalls != 1 {
		t.Fatalf("validation/dial calls = %d/%d, want one real initial validation then one real dial", identityCalls, dialCalls)
	}
	if realDialErr == nil {
		t.Fatal("post-validation target exit unexpectedly left a connectable endpoint")
	}
	if target.waitErr != nil {
		t.Fatalf("target exit at dial boundary = %v, want cooperative clean exit", target.waitErr)
	}
	unrelated.requireCleanStop(t)
}

func TestFix5ConcurrentArmedRunnerFaultKillLifecycle(t *testing.T) {
	root := task7ShortFaultRoot(t)
	runtimeRoot := filepath.Join(root, "fault")
	if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	runID := strings.Repeat("1", 32)
	ownerPath := filepath.Join(root, "owner.json")
	ownerLockPath := filepath.Join(root, "owner.lock")
	installLockPath := filepath.Join(root, "install.lock")
	orderMarker := filepath.Join(root, "endpoint-before-owner-release")
	store := hardwareowner.NewStore(ownerPath, uid)
	if err := store.Replace(task7NormalMainRecord()); err != nil {
		t.Fatalf("seed real owner store: %v", err)
	}
	controller := &filesystemFaultController{root: runtimeRoot, expectedUID: uid}
	armRunner := newFixtureRunner(runnerDependencies{fault: controller})
	if err := armRunner.FaultArm(context.Background(), runID); err != nil {
		t.Fatalf("FaultArm() = %v", err)
	}

	child := task7StartFix5ManagedProcess(t, "lifecycle", map[string]string{
		"FOGCAST_TASK7_FIX5_ROOT":         root,
		"FOGCAST_TASK7_FIX5_RUNTIME_ROOT": runtimeRoot,
		"FOGCAST_TASK7_FIX5_ORDER_MARKER": orderMarker,
	})
	task7Fix5WaitForPath(t, orderMarker)
	if raw, err := os.ReadFile(orderMarker); err != nil || string(raw) != "diagnostic-and-endpoint-durable-before-owner-release\n" {
		t.Fatalf("owner-release ordering marker = %q/%v", raw, err)
	}

	inspection, err := controller.Inspect(context.Background(), runID)
	if err != nil {
		t.Fatalf("durable diagnostic Inspect() = %v", err)
	}
	if inspection.PID != child.command.Process.Pid || inspection.Session != strings.Repeat("3", 32) || inspection.Generation != 43 || inspection.Phase != string(hardwareowner.PhaseLoadAttempted) {
		t.Fatalf("durable diagnostic = %#v, want child and exact fenced tuple", inspection)
	}
	if err := validateFaultSocket(controller.socketName(runID), uid); err != nil {
		t.Fatalf("ready protected endpoint = %v", err)
	}
	before, exists, err := store.Load()
	if err != nil || !exists || before.State != hardwareowner.StateRecoveringIntent || before.Phase != hardwareowner.PhaseLoadAttempted || before.RunID != runID {
		t.Fatalf("durable pre-kill fence = %#v exists=%v err=%v", before, exists, err)
	}

	blockedCtx, cancelBlocked := context.WithTimeout(context.Background(), 300*time.Millisecond)
	blockedUnlock, blockedErr := hardwareowner.NewLocker(installLockPath, uid).Lock(blockedCtx)
	cancelBlocked()
	if blockedUnlock != nil {
		_ = blockedUnlock()
		t.Fatal("install lock was available while the armed runner was alive")
	}
	if !errors.Is(blockedErr, context.DeadlineExceeded) {
		t.Fatalf("contended install lock = %v, want deadline", blockedErr)
	}

	unrelated := task7StartFix5ManagedProcess(t, "idle", nil)
	unrelated.requireLine(t, "ready")
	killerLocker := &task7Fix5CountingLocker{locker: hardwareowner.NewLocker(ownerLockPath, uid)}
	killer := newFixtureRunner(runnerDependencies{
		store:  hardwareowner.NewStore(ownerPath, uid),
		locker: killerLocker,
		fault:  &filesystemFaultController{root: runtimeRoot, expectedUID: uid},
		bootID: func() (string, error) { return task7BootID, nil },
	})
	killCtx, cancelKill := context.WithTimeout(context.Background(), 15*time.Second)
	err = killer.FaultKill(killCtx, runID)
	cancelKill()
	if err != nil {
		t.Fatalf("separate Runner.FaultKill() = %v", err)
	}
	if killerLocker.lockCalls != 1 || killerLocker.unlockCalls != 1 {
		t.Fatalf("FaultKill real owner-lock lifecycle = %d acquire/%d release, want 1/1", killerLocker.lockCalls, killerLocker.unlockCalls)
	}
	waitErr := child.wait()
	status, statusOK := child.command.ProcessState.Sys().(syscall.WaitStatus)
	if waitErr == nil || !statusOK || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("armed runner exit = %v status=%#v, want matched self-SIGKILL", waitErr, status)
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(child.command.Process.Pid))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reaped diagnostic process still exists: %v", err)
	}
	unrelated.requireCleanStop(t)

	after, exists, err := hardwareowner.NewStore(ownerPath, uid).Load()
	if err != nil || !exists || !sameFencedOwner(before, after) {
		t.Fatalf("post-kill durable fence = %#v exists=%v err=%v, want unchanged %#v", after, exists, err, before)
	}
	installCtx, cancelInstall := context.WithTimeout(context.Background(), time.Second)
	installUnlock, err := hardwareowner.NewLocker(installLockPath, uid).Lock(installCtx)
	cancelInstall()
	if err != nil || installUnlock == nil {
		t.Fatalf("install lock after process death = unlock:%v err:%v", installUnlock, err)
	}
	if err := installUnlock(); err != nil {
		t.Fatalf("release post-death install lock: %v", err)
	}
}

func TestTask7Fix5ManagedProcessHelper(t *testing.T) {
	if os.Getenv("FOGCAST_TASK7_FIX5_HELPER") != "1" {
		return
	}
	switch os.Getenv("FOGCAST_TASK7_FIX5_MODE") {
	case "idle":
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "peer":
		task7Fix5ReplacementPeerHelper()
	case "lifecycle":
		task7Fix5ArmedLifecycleHelper(t)
	default:
		os.Exit(81)
	}
}

func task7Fix5ReplacementPeerHelper() {
	socketPath := os.Getenv("FOGCAST_TASK7_FIX5_SOCKET")
	if socketPath == "" {
		os.Exit(82)
	}
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socketPath, Net: "unixpacket"})
	if err != nil {
		os.Exit(83)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		os.Exit(84)
	}
	stop := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		close(stop)
	}()
	packet := make(chan int, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			packet <- -1
			return
		}
		defer connection.Close()
		_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
		var raw [512]byte
		n, _ := connection.Read(raw[:])
		packet <- n
	}()
	_, _ = fmt.Fprintln(os.Stdout, "ready")
	select {
	case n := <-packet:
		_, _ = fmt.Fprintf(os.Stdout, "packet=%d\n", n)
		<-stop
	case <-stop:
	}
}

func task7Fix5ArmedLifecycleHelper(t *testing.T) {
	root := os.Getenv("FOGCAST_TASK7_FIX5_ROOT")
	runtimeRoot := os.Getenv("FOGCAST_TASK7_FIX5_RUNTIME_ROOT")
	orderMarker := os.Getenv("FOGCAST_TASK7_FIX5_ORDER_MARKER")
	if root == "" || runtimeRoot == "" || orderMarker == "" {
		os.Exit(85)
	}
	// Closing the control pipe is the only parent-driven cleanup path. The
	// successful test path never uses it: Checkpoint validates the request and
	// the helper invokes SIGKILL on itself.
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(86)
	}()
	uid := uint32(os.Getuid())
	fixture := newTask7RunnerFixture(t)
	controller := &filesystemFaultController{root: runtimeRoot, expectedUID: uid}
	deps := fixture.dependencies()
	deps.maintenance = task7Fix5RealMaintenanceGate{locker: hardwareowner.NewLocker(filepath.Join(root, "install.lock"), uid)}
	deps.store = hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	deps.locker = &task7Fix5EndpointCheckingLocker{
		locker:     hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid),
		controller: controller,
		runID:      fixture.manifest.RunID,
		session:    strings.Repeat("3", 32),
		generation: 43,
		marker:     orderMarker,
	}
	deps.fault = controller
	_, err := newFixtureRunner(deps).RunCommand(context.Background(), fixture.request)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "armed lifecycle returned: %v\n", err)
	}
	os.Exit(87)
}

type task7Fix5ManagedProcess struct {
	command   *exec.Cmd
	stdin     io.WriteCloser
	lines     chan string
	stderr    *bytes.Buffer
	closeOnce sync.Once
	waitOnce  sync.Once
	waitErr   error
}

func task7StartFix5ManagedProcess(t *testing.T, mode string, environment map[string]string) *task7Fix5ManagedProcess {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestTask7Fix5ManagedProcessHelper$")
	command.Env = append(os.Environ(), "FOGCAST_TASK7_FIX5_HELPER=1", "FOGCAST_TASK7_FIX5_MODE="+mode)
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := &task7Fix5ManagedProcess{command: command, stdin: stdin, lines: make(chan string, 16), stderr: stderr}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			process.lines <- scanner.Text()
		}
		close(process.lines)
	}()
	t.Cleanup(func() {
		_ = process.cleanStop()
	})
	return process
}

func (p *task7Fix5ManagedProcess) requireLine(t *testing.T, want string) {
	t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok || line != want {
			t.Fatalf("helper line = %q/open:%v, want %q (stderr=%q)", line, ok, want, p.stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for helper line %q (stderr=%q)", want, p.stderr.String())
	}
}

func (p *task7Fix5ManagedProcess) wait() error {
	p.waitOnce.Do(func() { p.waitErr = p.command.Wait() })
	return p.waitErr
}

func (p *task7Fix5ManagedProcess) cleanStop() error {
	p.closeOnce.Do(func() { _ = p.stdin.Close() })
	return p.wait()
}

func (p *task7Fix5ManagedProcess) requireCleanStop(t *testing.T) {
	t.Helper()
	if err := p.cleanStop(); err != nil {
		t.Fatalf("cooperative helper stop = %v (stderr=%q)", err, p.stderr.String())
	}
}

func task7Fix5DiagnosticForProcess(t *testing.T, pid int) faultDiagnostic {
	t.Helper()
	identity, err := faultProcessIdentity(pid)
	if err != nil {
		t.Fatalf("real process identity for %d: %v", pid, err)
	}
	return faultDiagnostic{
		RunID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Generation: 43,
		Phase: string(hardwareowner.PhaseLoadAttempted), PID: pid, StartTime: identity.StartTime,
		ExecutableSHA256: identity.SHA256, ExecutableDevice: identity.Device, ExecutableInode: identity.Inode,
	}
}

func task7Fix5BoundRunner(t *testing.T, root string, diagnostic faultDiagnostic) (*Runner, *filesystemFaultController) {
	t.Helper()
	uid := uint32(os.Getuid())
	controller := task7BoundFaultController(t, root, diagnostic)
	store := hardwareowner.NewStore(filepath.Join(root, "owner.json"), uid)
	record := task7IntentRecord(hardwareowner.PhaseLoadAttempted)
	record.RunID = diagnostic.RunID
	record.CandidateSession = diagnostic.Session
	record.CandidateGeneration = diagnostic.Generation
	record.GenerationHighWater = diagnostic.Generation
	if err := store.Replace(record); err != nil {
		t.Fatalf("seed fault owner fence: %v", err)
	}
	return newFixtureRunner(runnerDependencies{
		store:  store,
		locker: hardwareowner.NewLocker(filepath.Join(root, "owner.lock"), uid),
		fault:  controller,
		bootID: func() (string, error) { return task7BootID, nil },
	}), controller
}

func task7Fix5LeaveStalePacketEndpoint(t *testing.T, socketPath string) {
	t.Helper()
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: socketPath, Net: "unixpacket"})
	if err != nil {
		t.Fatalf("bind exact unixpacket endpoint: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	if err := validateFaultSocket(socketPath, uint32(os.Getuid())); err != nil {
		_ = listener.Close()
		t.Fatalf("live protected unixpacket endpoint: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close unixpacket listener without unlink: %v", err)
	}
	if err := validateFaultSocket(socketPath, uint32(os.Getuid())); err != nil {
		t.Fatalf("stale protected unixpacket endpoint: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(socketPath) })
}

type task7Fix5RealMaintenanceGate struct{ locker ownerLocker }

func (g task7Fix5RealMaintenanceGate) Enter(ctx context.Context) (MaintenanceStatus, MaintenanceUnlock, error) {
	unlock, err := g.locker.Lock(ctx)
	if err != nil || unlock == nil {
		return MaintenanceStatus{}, nil, err
	}
	return task7ValidMaintenanceStatus(), &task7Fix5RealMaintenanceUnlock{unlock: unlock}, nil
}

type task7Fix5RealMaintenanceUnlock struct {
	unlock hardwareowner.Unlock
	once   sync.Once
	err    error
}

func (u *task7Fix5RealMaintenanceUnlock) Unlock() error {
	u.once.Do(func() { u.err = u.unlock() })
	return u.err
}

type task7Fix5EndpointCheckingLocker struct {
	locker     ownerLocker
	controller *filesystemFaultController
	runID      string
	session    string
	generation uint64
	marker     string
}

type task7Fix5CountingLocker struct {
	locker      ownerLocker
	lockCalls   int
	unlockCalls int
}

func (l *task7Fix5CountingLocker) Lock(ctx context.Context) (hardwareowner.Unlock, error) {
	l.lockCalls++
	unlock, err := l.locker.Lock(ctx)
	if err != nil || unlock == nil {
		return unlock, err
	}
	var once sync.Once
	var unlockErr error
	return func() error {
		once.Do(func() {
			l.unlockCalls++
			unlockErr = unlock()
		})
		return unlockErr
	}, nil
}

func (l *task7Fix5EndpointCheckingLocker) Lock(ctx context.Context) (hardwareowner.Unlock, error) {
	unlock, err := l.locker.Lock(ctx)
	if err != nil || unlock == nil {
		return unlock, err
	}
	var once sync.Once
	var unlockErr error
	return func() error {
		once.Do(func() {
			inspection, inspectErr := l.controller.Inspect(context.Background(), l.runID)
			if inspectErr == nil && (inspection.RunID != l.runID || inspection.Session != l.session || inspection.Generation != l.generation || inspection.Phase != string(hardwareowner.PhaseLoadAttempted) || inspection.PID != os.Getpid()) {
				inspectErr = ErrFaultUnsupported
			}
			socketErr := validateFaultSocket(l.controller.socketName(l.runID), uint32(os.Getuid()))
			markerErr := errors.Join(inspectErr, socketErr)
			if markerErr == nil {
				markerErr = task7Fix5WriteDurableMarker(l.marker, []byte("diagnostic-and-endpoint-durable-before-owner-release\n"))
			}
			unlockErr = errors.Join(markerErr, unlock())
		})
		return unlockErr
	}, nil
}

func task7Fix5WriteDurableMarker(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(contents)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return syncFaultDirectory(filepath.Dir(path))
}

func task7Fix5WaitForPath(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Lstat(path); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("wait for %s: %v", filepath.Base(path), err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("timed out waiting for %s", filepath.Base(path))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFaultCheckpointSelfKillHelper(t *testing.T) {
	if os.Getenv("FOGCAST_FAULT_CHECKPOINT_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	go func() {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(88)
	}()
	root := os.Getenv("FOGCAST_FAULT_CHECKPOINT_ROOT")
	if root == "" {
		t.Fatal("missing helper runtime root")
	}
	runID := strings.Repeat("a", 32)
	session := strings.Repeat("b", 32)
	controller := &filesystemFaultController{
		root:        root,
		expectedUID: uint32(os.Getuid()),
		diagnostic: func(checkpoint faultDiagnosticForCheckpoint) (faultDiagnostic, error) {
			return faultDiagnostic{
				RunID: checkpoint.RunID, Session: checkpoint.Session, Generation: checkpoint.Generation,
				Phase: string(checkpoint.Phase), PID: os.Getpid(), StartTime: 1,
				ExecutableSHA256: strings.Repeat("c", 64), ExecutableDevice: 1, ExecutableInode: 1,
			}, nil
		},
	}
	if err := controller.Arm(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	controller.listen = func(path string) (net.Listener, error) {
		listener, err := net.Listen("unixpacket", path)
		if err == nil {
			_, _ = fmt.Fprintln(os.Stdout, path)
		}
		return listener, err
	}
	_, err := controller.Checkpoint(context.Background(), faultDiagnosticForCheckpoint{
		RunID: runID, Session: session, Generation: 7, Phase: hardwareowner.PhaseLoadAttempted,
	}, func() error { return nil }, func(context.Context) (hardwareowner.Unlock, error) {
		return nil, errors.New("unexpected reacquire")
	})
	t.Fatalf("Checkpoint() returned after valid self-kill request: %v", err)
}

func TestFaultCheckpointSelfKillsHelperAndClientObservesClosure(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "fg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	command := exec.Command(os.Args[0], "-test.run=^TestFaultCheckpointSelfKillHelper$")
	command.Env = append(os.Environ(), "FOGCAST_FAULT_CHECKPOINT_HELPER=1", "FOGCAST_FAULT_CHECKPOINT_ROOT="+root)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = stdin.Close()
			_ = command.Wait()
		}
	})
	pathLine := make(chan struct {
		path string
		err  error
	}, 1)
	go func() {
		path, readErr := bufio.NewReader(stdout).ReadString('\n')
		pathLine <- struct {
			path string
			err  error
		}{path: strings.TrimSpace(path), err: readErr}
	}()
	var endpoint string
	select {
	case result := <-pathLine:
		if result.err != nil {
			t.Fatalf("helper endpoint announcement = %q/%v, stderr=%q", result.path, result.err, stderr.String())
		}
		endpoint = result.path
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for helper endpoint")
	}
	if endpoint == "" || filepath.Dir(endpoint) != root {
		t.Fatalf("helper endpoint = %q, want protected fixture root %q", endpoint, root)
	}
	connection, err := net.DialTimeout("unixpacket", endpoint, time.Second)
	if err != nil {
		t.Fatalf("dial helper endpoint: %v", err)
	}
	request, err := json.Marshal(faultRequest{RunID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Generation: 7})
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	if _, err := connection.Write(request); err != nil {
		_ = connection.Close()
		t.Fatalf("write helper request: %v", err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	var response [1]byte
	readN, readErr := connection.Read(response[:])
	_ = connection.Close()
	if readN != 0 || readErr == nil || (!errors.Is(readErr, io.EOF) && !errors.Is(readErr, syscall.ECONNRESET) && !errors.Is(readErr, syscall.ENOTCONN)) {
		t.Fatalf("helper connection close = bytes:%d err:%v, want EOF/reset", readN, readErr)
	}
	waitErr := command.Wait()
	waited = true
	status, ok := command.ProcessState.Sys().(syscall.WaitStatus)
	if waitErr == nil || !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("helper wait = %v status=%#v stderr=%q, want SIGKILL", waitErr, status, stderr.String())
	}
}

func TestFaultCheckpointIgnoresMalformedPartialAndExtraPacketsWithoutKillingTheProcess(t *testing.T) {
	valid, err := json.Marshal(faultRequest{RunID: strings.Repeat("a", 32), Session: strings.Repeat("b", 32), Generation: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		packet []byte
	}{
		{name: "malformed", packet: []byte("not-json")},
		{name: "partial", packet: []byte(`{"run_id":"aaaaaaaaaaaaaaaa`)},
		{name: "extra", packet: append(append([]byte(nil), valid...), 'x')},
	} {
		t.Run(test.name, func(t *testing.T) { task7CheckpointRejectsInvalidPacket(t, test.packet) })
	}
}

func task7CheckpointRejectsInvalidPacket(t *testing.T, packet []byte) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runID := strings.Repeat("a", 32)
	session := strings.Repeat("b", 32)
	controller := &filesystemFaultController{
		root:        root,
		expectedUID: uint32(os.Getuid()),
		diagnostic: func(checkpoint faultDiagnosticForCheckpoint) (faultDiagnostic, error) {
			return faultDiagnostic{
				RunID: checkpoint.RunID, Session: checkpoint.Session, Generation: checkpoint.Generation, Phase: string(checkpoint.Phase),
				PID: os.Getpid(), StartTime: 11, ExecutableSHA256: strings.Repeat("c", 64), ExecutableDevice: 12, ExecutableInode: 13,
			}, nil
		},
	}
	if err := controller.Arm(context.Background(), runID); err != nil {
		t.Fatal(err)
	}
	// Keep startup allowance independent from the malformed-packet action. Race
	// instrumentation can spend longer than the old 250ms bound before the
	// child listener is ready; the fixture remains bounded and cancels
	// explicitly after the malformed request is observed.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan string, 1)
	controller.listen = func(path string) (net.Listener, error) {
		listener, err := net.Listen("unixpacket", path)
		if err == nil {
			ready <- path
		}
		return listener, err
	}
	reacquired := 0
	checkpointDone := make(chan error, 1)
	go func() {
		_, err := controller.Checkpoint(ctx, faultDiagnosticForCheckpoint{RunID: runID, Session: session, Generation: 7, Phase: hardwareowner.PhaseLoadAttempted}, func() error { return nil }, func(context.Context) (hardwareowner.Unlock, error) {
			reacquired++
			return func() error { return nil }, nil
		})
		checkpointDone <- err
	}()
	var socketPath string
	select {
	case socketPath = <-ready:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fault endpoint")
	}
	var connection net.Conn
	var err error
	for attempt := 0; attempt < 20; attempt++ {
		connection, err = net.Dial("unixpacket", socketPath)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(packet); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	cancel()
	select {
	case <-checkpointDone:
		if reacquired != 1 {
			t.Fatalf("reacquired locks = %d, want one after cancellation", reacquired)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fault checkpoint did not terminate after cancellation")
	}
}

type task7RunnerFixture struct {
	request         Request
	manifest        Manifest
	binding         ArtifactBinding
	baseline        []ProcessIdentity
	secret          string
	expireAt        string
	cancel          context.CancelFunc
	observerErr     error
	store           *task7OwnerStore
	locker          *task7OwnerLocker
	fifo            *task7FIFO
	observer        *task7Observer
	mapper          *task7Mapper
	results         *task7Results
	reboot          *task7Reboot
	profile         *task7Profile
	readiness       *task7Readiness
	qualifier       *task7Qualification
	mailbox         func(context.Context, Registers, Clock) (Observation, error)
	mailboxProgress func(context.Context, Registers, Clock, mailboxProgressCallback) (Observation, error)
	fault           any
}

func newTask7RunnerFixture(t *testing.T) *task7RunnerFixture {
	t.Helper()
	manifest := qualificationArtifactBinding().state.manifest
	binding := qualificationArtifactBinding()
	qDeps, _, _ := validQualificationDeps()
	receipt, err := newFixtureQualifier(qDeps).Qualify(context.Background(), binding)
	if err != nil {
		t.Fatalf("construct qualification fixture: %v", err)
	}
	f := &task7RunnerFixture{
		request:   Request{ManifestPath: "/anonymous/manifest.json", ArtifactPath: "/anonymous/top.rbf"},
		manifest:  manifest,
		binding:   binding,
		baseline:  []ProcessIdentity{validMainBaseline()},
		secret:    "private-fixture-secret",
		store:     &task7OwnerStore{record: task7NormalMainRecord()},
		locker:    &task7OwnerLocker{},
		fifo:      &task7FIFO{},
		results:   &task7Results{},
		reboot:    &task7Reboot{},
		profile:   &task7Profile{},
		readiness: &task7Readiness{},
		qualifier: &task7Qualification{receipt: receipt},
	}
	if err := f.store.record.Validate(); err != nil {
		t.Fatalf("task7 owner fixture invalid: %v", err)
	}
	f.observer = &task7Observer{fixture: f}
	f.fifo.fixture = f
	f.mapper = &task7Mapper{}
	f.mailbox = func(ctx context.Context, _ Registers, _ Clock) (Observation, error) {
		if err := ctx.Err(); err != nil {
			return Observation{}, err
		}
		return Observation{Payload: []byte("OSS FPGA OK\n"), TerminalWord: 0xd3130c00}, nil
	}
	f.mailboxProgress = func(ctx context.Context, _ Registers, _ Clock, callback mailboxProgressCallback) (Observation, error) {
		if err := ctx.Err(); err != nil {
			return Observation{}, err
		}
		if err := callback(mailboxProgress{Phase: mailboxProgressHello, Observation: Observation{}}); err != nil {
			return Observation{}, err
		}
		payload := []byte("OSS FPGA OK\n")
		partial := Observation{Payload: append([]byte(nil), payload[:1]...)}
		if err := callback(mailboxProgress{Phase: mailboxProgressData, Observation: partial}); err != nil {
			return partial, err
		}
		partial.Payload = append([]byte(nil), payload...)
		if err := callback(mailboxProgress{Phase: mailboxProgressEnd, Observation: partial}); err != nil {
			return partial, err
		}
		if err := callback(mailboxProgress{Phase: mailboxProgressDone, Observation: partial, TerminalWord: 0xd3130c00}); err != nil {
			return partial, err
		}
		partial.TerminalWord = 0xd3130c00
		return partial, nil
	}
	return f
}

func (f *task7RunnerFixture) dependencies() runnerDependencies {
	return runnerDependencies{
		maintenance: semanticMaintenanceFixture{},
		quiescence:  semanticQuiescenceFixture{},
		designation: newFixtureDesignation(nil),
		profile:     f.profile,
		store:       f.store,
		locker:      f.locker,
		artifact:    &task7Artifact{},
		fifo:        f.fifo,
		observer:    f.observer,
		qualifier:   f.qualifier,
		mapper:      f.mapper,
		results:     f.results,
		readiness:   f.readiness,
		install:     task7TerminalInstall{},
		reboot:      f.reboot,
		clock:       task7Clock{},
		bootID:      func() (string, error) { return task7BootID, nil },
		privilege:   func() bool { return true },
		tool:        func(context.Context) error { return nil },
		bind: func(ctx context.Context, _ Request) (Manifest, ArtifactBinding, error) {
			if err := ctx.Err(); err != nil {
				return Manifest{}, ArtifactBinding{}, err
			}
			return f.manifest, f.binding, nil
		},
		revalidate: func(ctx context.Context, _ *ArtifactBinding) error { return ctx.Err() },
		dispatchPath: func(ctx context.Context, _ *ArtifactBinding) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return "/anonymous/proc-fd", nil
		},
		mailbox:         f.mailbox,
		mailboxProgress: f.mailboxProgress,
		fault:           f.fault,
		newSession:      func() (string, error) { return strings.Repeat("3", 32), nil },
		closeBinding: func(ctx context.Context, b *ArtifactBinding) error {
			closeErr := b.Close()
			return errors.Join(closeErr, markSyntheticCleanupContext(ctx.Err()))
		},
	}
}

const task7BootID = "01234567-89ab-cdef-0123-456789abcdef"

func task7NormalMainRecord() hardwareowner.Record {
	return hardwareowner.Record{
		Schema:              1,
		State:               hardwareowner.StateNormalMain,
		Phase:               "",
		BootID:              task7BootID,
		GenerationHighWater: 42,
		ActiveSession:       strings.Repeat("2", 32),
		ActiveGeneration:    42,
		ActiveMode:          hardwareowner.ModeFPGANative,
		CandidateMode:       hardwareowner.ModeNone,
		QuiescingOwner:      hardwareowner.OwnerNone,
		CandidateOwner:      hardwareowner.OwnerNone,
		ActiveOwner:         hardwareowner.OwnerCompatMain,
		ActiveLeases:        hardwareowner.NormalLeases(),
		RequestedResources:  []string{},
	}
}

func task7IntentRecord(phase hardwareowner.Phase) hardwareowner.Record {
	record := task7NormalMainRecord()
	record.State = hardwareowner.StateRecoveringIntent
	record.Phase = phase
	record.RunID = strings.Repeat("a", 32)
	record.GenerationHighWater = 43
	record.CandidateSession = strings.Repeat("3", 32)
	record.CandidateGeneration = 43
	record.CandidateMode = hardwareowner.ModeUpdating
	record.CandidateOwner = hardwareowner.OwnerFPGADev
	record.QuiescingOwner = hardwareowner.OwnerCompatMain
	record.RequestedResources = hardwareowner.DevelopmentLeases()
	return record
}

func task7NoOwnerRecord() hardwareowner.Record {
	record := task7IntentRecord(hardwareowner.PhaseIntentCommitted)
	record.State = hardwareowner.StateNoOwner
	record.Phase = hardwareowner.PhaseMainAbsent
	record.ActiveSession = ""
	record.ActiveGeneration = 0
	record.ActiveMode = hardwareowner.ModeNone
	record.ActiveOwner = hardwareowner.OwnerNone
	record.ActiveLeases = []string{}
	record.QuiescingOwner = hardwareowner.OwnerNone
	return record
}

func task7ActiveRecord(phase hardwareowner.Phase) hardwareowner.Record {
	record := task7NoOwnerRecord()
	record.State = hardwareowner.StateFPGADefaultActive
	record.Phase = phase
	record.ActiveSession = record.CandidateSession
	record.ActiveGeneration = record.CandidateGeneration
	record.ActiveMode = hardwareowner.ModeUpdating
	record.ActiveOwner = hardwareowner.OwnerFPGADev
	record.ActiveLeases = hardwareowner.DevelopmentLeases()
	record.CandidateSession = ""
	record.CandidateGeneration = 0
	record.CandidateMode = hardwareowner.ModeNone
	record.CandidateOwner = hardwareowner.OwnerNone
	record.QuiescingOwner = hardwareowner.OwnerNone
	record.RequestedResources = []string{}
	return record
}

func task7RecoveryRequiredRecord() hardwareowner.Record {
	record := task7ActiveRecord(hardwareowner.PhaseDoneObserved)
	record.State = hardwareowner.StateRecoveryRequired
	record.FirstFailure = ""
	record.QuiescingOwner = hardwareowner.OwnerFPGADev
	return record
}

type task7Clock struct{}

func (task7Clock) Now() time.Time { return time.Now() }

type task7Artifact struct{}

func (*task7Artifact) Bind(Manifest, string) (ArtifactBinding, error) { return ArtifactBinding{}, nil }

type task7ManifestArtifactBinder struct {
	manifest Manifest
	binding  ArtifactBinding
	seen     Manifest
}

func (b *task7ManifestArtifactBinder) Bind(manifest Manifest, _ string) (ArtifactBinding, error) {
	b.seen = manifest
	return b.binding, nil
}

func (b *task7ManifestArtifactBinder) bindProductionStagingDirectory(manifest Manifest, directory *productionStagingDirectory) (ArtifactBinding, error) {
	binding, err := b.Bind(manifest, directory.path)
	return binding, errors.Join(err, directory.close())
}

type task7OwnerStore struct {
	mu              sync.Mutex
	record          hardwareowner.Record
	exists          bool
	replaceCalls    int
	replaceErr      error
	replaceErrAt    int
	replaceErrValue error
	history         []hardwareowner.Record
	onReplace       func(int, hardwareowner.Record)
}

func (s *task7OwnerStore) Load() (hardwareowner.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.exists && s.record.Schema == 0 {
		return hardwareowner.Record{}, false, nil
	}
	return cloneTask7Record(s.record), true, nil
}

func (s *task7OwnerStore) Replace(next hardwareowner.Record) error {
	s.mu.Lock()
	s.replaceCalls++
	call := s.replaceCalls
	if s.replaceErr != nil {
		s.mu.Unlock()
		return s.replaceErr
	}
	if s.replaceErrAt == call {
		replaceErr := s.replaceErrValue
		if replaceErr == nil {
			replaceErr = errors.New("owner replace injected failure")
		}
		s.mu.Unlock()
		return replaceErr
	}
	s.record = cloneTask7Record(next)
	s.exists = true
	s.history = append(s.history, cloneTask7Record(next))
	onReplace := s.onReplace
	s.mu.Unlock()
	if onReplace != nil {
		onReplace(call, next)
	}
	return nil
}

func cloneTask7Record(record hardwareowner.Record) hardwareowner.Record {
	if record.ActiveLeases != nil {
		record.ActiveLeases = append([]string{}, record.ActiveLeases...)
	}
	if record.RequestedResources != nil {
		record.RequestedResources = append([]string{}, record.RequestedResources...)
	}
	return record
}

type task7OwnerLocker struct {
	mu           sync.Mutex
	lockCalls    int
	unlockCalls  int
	unlockErr    error
	beforeUnlock func()
}

type task7EventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *task7EventLog) add(event string) {
	l.mu.Lock()
	l.events = append(l.events, event)
	l.mu.Unlock()
}

func (l *task7EventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type task7EventOwnerLocker struct {
	events    *task7EventLog
	lockErr   error
	unlockErr error
}

func (l *task7EventOwnerLocker) Lock(ctx context.Context) (hardwareowner.Unlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l.events != nil {
		l.events.add("owner.enter")
	}
	var once sync.Once
	return func() error {
		once.Do(func() {
			if l.events != nil {
				l.events.add("owner.release")
			}
		})
		return l.unlockErr
	}, l.lockErr
}

type task7ErrorLocker struct {
	mu          sync.Mutex
	unlockCalls int
}

type task7BoundFault struct {
	unboundCalls int
	runID        string
	session      string
	generation   uint64
	phase        hardwareowner.Phase
}

type task7LostLockFault struct{}

type task7LaterErrorFault struct {
	calls int
	err   error
}

func (f *task7LaterErrorFault) Checkpoint(_ context.Context, _ faultDiagnosticForCheckpoint, unlock hardwareowner.Unlock, _ func(context.Context) (hardwareowner.Unlock, error)) (hardwareowner.Unlock, error) {
	f.calls++
	return unlock, f.err
}

func (*task7LostLockFault) Checkpoint(context.Context, faultDiagnosticForCheckpoint, hardwareowner.Unlock, func(context.Context) (hardwareowner.Unlock, error)) (hardwareowner.Unlock, error) {
	return nil, errors.New("reacquire failed")
}

type task7DiagnosticMismatchFault struct{}

func (*task7DiagnosticMismatchFault) Checkpoint(ctx context.Context, _ faultDiagnosticForCheckpoint, _ hardwareowner.Unlock, acquire func(context.Context) (hardwareowner.Unlock, error)) (hardwareowner.Unlock, error) {
	lock, err := acquire(ctx)
	if err != nil {
		if lock != nil {
			_ = lock()
		}
		return nil, err
	}
	return lock, nil
}

func (*task7DiagnosticMismatchFault) Inspect(context.Context, string) (Inspection, error) {
	return Inspection{Session: strings.Repeat("d", 32), Generation: 99, Phase: string(hardwareowner.PhaseLoadAttempted)}, nil
}

func (f *task7BoundFault) Arm(context.Context, string) error { return nil }

func (f *task7BoundFault) Inspect(context.Context, string) (Inspection, error) {
	return Inspection{}, nil
}

func (f *task7BoundFault) Kill(context.Context, string) error {
	f.unboundCalls++
	return errors.New("unbound fault kill should not be used")
}

func (f *task7BoundFault) KillBound(_ context.Context, runID, session string, generation uint64, phase hardwareowner.Phase) error {
	f.runID, f.session, f.generation, f.phase = runID, session, generation, phase
	return nil
}

func (l *task7ErrorLocker) Lock(context.Context) (hardwareowner.Unlock, error) {
	return func() error {
		l.mu.Lock()
		l.unlockCalls++
		l.mu.Unlock()
		return nil
	}, errors.New("lock failed after acquisition")
}

func (l *task7OwnerLocker) Lock(ctx context.Context) (hardwareowner.Unlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.lockCalls++
	l.mu.Unlock()
	var once sync.Once
	return func() error {
		once.Do(func() {
			if l.beforeUnlock != nil {
				l.beforeUnlock()
			}
			l.mu.Lock()
			l.unlockCalls++
			l.mu.Unlock()
		})
		return l.unlockErr
	}, nil
}

type task7FIFO struct {
	mu      sync.Mutex
	calls   int
	command string
	attempt Attempt
	err     error
	fixture *task7RunnerFixture
}

func (f *task7FIFO) Dispatch(ctx context.Context, command string) (Attempt, error) {
	f.mu.Lock()
	f.calls++
	f.command = command
	if f.attempt == NotInvoked {
		f.attempt = Invoked
	}
	err := f.err
	fixture := f.fixture
	f.mu.Unlock()
	if fixture != nil && fixture.expireAt == "fifo" && fixture.cancel != nil {
		fixture.cancel()
	}
	if err != nil {
		return f.attempt, err
	}
	if err := ctx.Err(); err != nil {
		return f.attempt, err
	}
	return f.attempt, nil
}

type task7Observer struct{ fixture *task7RunnerFixture }

func (o *task7Observer) Snapshot() ([]ProcessIdentity, error) {
	return append([]ProcessIdentity(nil), o.fixture.baseline...), nil
}

func (o *task7Observer) WaitStableAbsent(ctx context.Context, _ []ProcessIdentity, _ time.Duration) error {
	if o.fixture.observerErr != nil {
		return o.fixture.observerErr
	}
	return ctx.Err()
}

type task7DelayedCompletedFIFO struct{ delay time.Duration }

func (f task7DelayedCompletedFIFO) Dispatch(ctx context.Context, _ string) (Attempt, error) {
	timer := time.NewTimer(f.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return Completed, nil
	case <-ctx.Done():
		return Invoked, ctx.Err()
	}
}

type task7CancelingCompletedFIFO struct{ cancel context.CancelFunc }

func (f task7CancelingCompletedFIFO) Dispatch(context.Context, string) (Attempt, error) {
	f.cancel()
	return Completed, nil
}

type task7HandoffDeadlineObserver struct {
	baseline          []ProcessIdentity
	waitCalls         int
	deadlineRemaining time.Duration
}

func (o *task7HandoffDeadlineObserver) Snapshot() ([]ProcessIdentity, error) {
	return append([]ProcessIdentity(nil), o.baseline...), nil
}

func (o *task7HandoffDeadlineObserver) WaitStableAbsent(ctx context.Context, _ []ProcessIdentity, _ time.Duration) error {
	o.waitCalls++
	deadline, ok := ctx.Deadline()
	if !ok {
		return errors.New("Main handoff context has no deadline")
	}
	o.deadlineRemaining = time.Until(deadline)
	return nil
}

type task7Qualification struct {
	receipt Receipt
	err     error
}

func (q *task7Qualification) Qualify(ctx context.Context, _ ArtifactBinding) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	return q.receipt, q.err
}

type task7Mapper struct {
	mu        sync.Mutex
	openCalls int
	registers *task7Registers
	openErr   error
	afterOpen func()
}

func (m *task7Mapper) OpenMailbox() (Registers, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.openCalls++
	if m.openErr != nil {
		return m.registers, m.openErr
	}
	if m.registers == nil {
		m.registers = &task7Registers{}
	}
	if m.afterOpen != nil {
		m.afterOpen()
	}
	return m.registers, nil
}

type task7GenericMapper struct {
	registers Registers
	err       error
}

func (m task7GenericMapper) OpenMailbox() (Registers, error) {
	return m.registers, m.err
}

type task7Registers struct {
	mu         sync.Mutex
	closeCalls int
	closeErr   error
}

func (*task7Registers) ReadGPI() (uint32, error) { return 0, nil }
func (*task7Registers) WriteGPO(uint32) error    { return nil }
func (*task7Registers) ReadGPO() (uint32, error) { return 0, nil }
func (r *task7Registers) Close() error {
	r.mu.Lock()
	r.closeCalls++
	err := r.closeErr
	r.mu.Unlock()
	return err
}

type task7Results struct {
	mu                 sync.Mutex
	created            []Result
	marked             []Result
	createCalls        int
	markRecoveryFailed int
	exists             bool
	createErr          error
	markErr            error
	onCreate           func(Result)
}

func (s *task7Results) Create(result Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, result)
	s.exists = true
	if s.onCreate != nil {
		s.onCreate(result)
	}
	return nil
}

func (s *task7Results) MarkRecoveryFailed(result Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markRecoveryFailed++
	if s.markErr != nil {
		return s.markErr
	}
	s.marked = append(s.marked, result)
	if len(s.created) != 0 {
		updated := result
		updated.RecoveryRequest = "failed"
		s.created[len(s.created)-1] = updated
	}
	return nil
}

func (s *task7Results) Exists(string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exists, nil
}

type task7Profile struct {
	verifyErr error
}

func (p *task7Profile) Verify(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.verifyErr
}

type task7Readiness struct {
	calls  int
	failAt int
	err    error
}

func (r *task7Readiness) Verify(ctx context.Context) error {
	r.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.err != nil && (r.failAt == 0 || r.calls == r.failAt) {
		return r.err
	}
	return nil
}

type task7TerminalInstall struct{}

func (task7TerminalInstall) Verify(ctx context.Context) error { return ctx.Err() }

type task7Reboot struct {
	mu    sync.Mutex
	calls int
	err   error
}

type task7RebooterFunc func(context.Context) error

func (f task7RebooterFunc) Request(ctx context.Context) error { return f(ctx) }

func (r *task7Reboot) Request(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	r.calls++
	err := r.err
	r.mu.Unlock()
	return err
}
