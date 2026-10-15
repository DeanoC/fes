package kitlease

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	contract "github.com/DeanoC/FogCast/kitlease"
)

func TestQueuedMaintenanceWaitsForOwnerAndDrainedRelease(t *testing.T) {
	var cleanups atomic.Int32
	m := maintenanceManager(t, t.TempDir(), func(context.Context) error { cleanups.Add(1); return nil })
	ownerRequest := req("a")
	owner, err := m.Claim(ownerRequest)
	if err != nil {
		t.Fatal(err)
	}
	operation, done, err := m.Begin(owner.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	queued := req("b")
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("d", 64)); err != nil {
		t.Fatal("queue retry", err)
	}
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("e", 64)); !errors.Is(err, ErrLease) {
		t.Fatal("queue credential was not checked", err)
	}
	if _, err := m.StartQueuedMaintenance(queued, strings.Repeat("d", 64)); !errors.Is(err, ErrBusy) {
		t.Fatal("started while owned", err)
	}
	if _, err := m.Claim(req("c")); !errors.Is(err, ErrBusy) {
		t.Fatal("competing claim", err)
	}
	if again, err := m.Claim(ownerRequest); err != nil || again.Token != owner.Token {
		t.Fatal("owner claim retry", again, err)
	}
	if _, err := m.Renew(owner.Token); err != nil {
		t.Fatal("owner renewal", err)
	}
	select {
	case <-operation.Done():
		t.Fatal("queue revoked the owner's operation")
	default:
	}
	if cleanups.Load() != 1 {
		t.Fatal("queue stopped current session", cleanups.Load())
	}
	if _, err := m.Release(owner.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartQueuedMaintenance(queued, strings.Repeat("d", 64)); !errors.Is(err, ErrBusy) {
		t.Fatal("overtook admitted operation", err)
	}
	if cleanups.Load() != 1 {
		t.Fatal("cleanup overtook operation", cleanups.Load())
	}
	done()
	wait(t, m, "free")
	if _, err := m.Claim(req("c")); !errors.Is(err, ErrBusy) {
		t.Fatal("claim overtook queued maintenance", err)
	}
	grant, err := m.StartQueuedMaintenance(queued, strings.Repeat("d", 64))
	if err != nil || grant.Status.OperationID != queued.RequestID || grant.Status.State != "preparing" {
		t.Fatal(grant, err)
	}
	waitMaintenance(t, m, "ready")
	if _, err := m.StartQueuedMaintenance(queued, strings.Repeat("e", 64)); !errors.Is(err, ErrLease) {
		t.Fatal("public operation identity disclosed the reservation credential", err)
	}
	if cleanups.Load() != 3 {
		t.Fatal("startup, release and maintenance must each confirm idle", cleanups.Load())
	}
	again, err := m.StartQueuedMaintenance(queued, strings.Repeat("d", 64))
	if err != nil || again.Token != grant.Token || again.Status.Generation != grant.Status.Generation {
		t.Fatal("lost response retry", again, err)
	}
	if err := m.CancelQueuedMaintenance(queued.RequestID); !errors.Is(err, ErrBusy) {
		t.Fatal("cancel opened active maintenance", err)
	}
	if _, err := m.EndMaintenance(grant.Token, maintenanceEnd(grant)); err != nil {
		t.Fatal(err)
	}
	wait(t, m, "free")
	if _, err := m.StartQueuedMaintenance(queued, strings.Repeat("d", 64)); !errors.Is(err, ErrLease) {
		t.Fatal("completed operation replayed", err)
	}
	if _, err := m.Claim(req("c")); err != nil {
		t.Fatal("completion retained queue priority", err)
	}
}

func TestQueuedMaintenanceCancellationAndDiagnosticSerialization(t *testing.T) {
	m := maintenanceManager(t, t.TempDir(), func(context.Context) error { return nil })
	owner, err := m.Claim(req("a"))
	if err != nil {
		t.Fatal(err)
	}
	queued := req("b")
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartMaintenance(owner.Token, maintenanceRequest("c")); !errors.Is(err, ErrBusy) {
		t.Fatal("diagnostic bypassed queue", err)
	}
	if _, err := m.Takeover(TakeoverRequest{ClaimRequest: req("c"), ExpectedGeneration: owner.Status.Generation, Reason: "operator request"}); !errors.Is(err, ErrBusy) {
		t.Fatal("takeover bypassed queue", err)
	}
	if err := m.CancelQueuedMaintenance(req("c").RequestID); !errors.Is(err, ErrLease) {
		t.Fatal("foreign cancellation", err)
	}
	if err := m.QueueIdleMaintenance(req("c"), strings.Repeat("d", 64)); !errors.Is(err, ErrBusy) {
		t.Fatal("queue overwritten", err)
	}
	changed := queued
	changed.Purpose = "different operation"
	if err := m.QueueIdleMaintenance(changed, strings.Repeat("d", 64)); !errors.Is(err, ErrInvalid) {
		t.Fatal("identity changed", err)
	}
	if _, err := m.StartQueuedMaintenance(changed, strings.Repeat("d", 64)); !errors.Is(err, ErrLease) {
		t.Fatal("different request acquired queue", err)
	}
	if err := m.CancelQueuedMaintenance(queued.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := m.CancelQueuedMaintenance(queued.RequestID); err != nil {
		t.Fatal("cancel retry", err)
	}
	diagnostic, err := m.StartMaintenance(owner.Token, maintenanceRequest("c"))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("d", 64)); !errors.Is(err, ErrBusy) {
		t.Fatal("queued across diagnostic", err)
	}
	if _, err := m.EndMaintenance(diagnostic.Token, maintenanceEnd(diagnostic)); err != nil {
		t.Fatal(err)
	}
	wait(t, m, "free")
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if err := m.CancelQueuedMaintenance(queued.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(req("d")); err != nil {
		t.Fatal("cancel retained priority", err)
	}
}

func TestQueuedMaintenanceAcquisitionCannotRaceClaims(t *testing.T) {
	m := maintenanceManager(t, t.TempDir(), func(context.Context) error { return nil })
	queued := req("b")
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var claimed atomic.Int32
	start := make(chan struct{})
	for range 20 {
		wg.Go(func() {
			<-start
			if _, err := m.Claim(req("c")); err == nil {
				claimed.Add(1)
			} else if !errors.Is(err, ErrBusy) {
				t.Error(err)
			}
		})
	}
	var grant contract.MaintenanceGrant
	var acquisitionError error
	wg.Go(func() { <-start; grant, acquisitionError = m.StartQueuedMaintenance(queued, strings.Repeat("d", 64)) })
	close(start)
	wg.Wait()
	if acquisitionError != nil || grant.Token == "" || claimed.Load() != 0 {
		t.Fatal(grant, acquisitionError, claimed.Load())
	}
	waitMaintenance(t, m, "ready")
}

func TestQueuedMaintenanceUsesPersistedReservationAndRestartRecovery(t *testing.T) {
	root := t.TempDir()
	clean := func(context.Context) error { return nil }
	m := maintenanceManager(t, root, clean)
	queued := req("b")
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	grant, err := m.StartQueuedMaintenance(queued, strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	m.Close()
	replacement := New(0, clean, WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), testBoot))
	defer replacement.Close()
	waitMaintenance(t, replacement, "blocked")
	resumed, err := replacement.StartQueuedMaintenance(queued, strings.Repeat("d", 64))
	if err != nil || resumed.Token != grant.Token || resumed.Status.Generation != grant.Status.Generation {
		t.Fatal("reservation lost", resumed, err)
	}
	if _, err := replacement.ResumeMaintenance(grant.Token, maintenanceEnd(grant)); err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, replacement, "ready")
}

func TestQueuedMaintenancePersistenceFailureKeepsExecutionClosed(t *testing.T) {
	root := t.TempDir()
	m := maintenanceManager(t, root, func(context.Context) error { return nil })
	queued := req("b")
	if err := m.QueueIdleMaintenance(queued, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "kit-maintenance.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StartQueuedMaintenance(queued, strings.Repeat("d", 64)); !errors.Is(err, ErrBlocked) {
		t.Fatal("failed marker admitted", err)
	}
	if err := m.CancelQueuedMaintenance(queued.RequestID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(req("c")); !errors.Is(err, ErrBlocked) {
		t.Fatal("ambiguous persistence reopened execution", err)
	}
}

func TestTrialReconciliationClaimLosesToHeldSuccessor(t *testing.T) {
	m := maintenanceManager(t, t.TempDir(), func(context.Context) error { return nil })
	boot := req("b")
	if err := m.QueueIdleMaintenance(boot, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	grant, err := m.StartQueuedMaintenance(boot, strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	successor := req("c")
	credential := strings.Repeat("f", 64)
	if err := m.QueueHeldSuccessor(successor, credential, strings.Repeat("e", 64)); !errors.Is(err, ErrLease) {
		t.Fatal("wrong boot credential installed a queue", err)
	}
	if err := m.QueueIdleMaintenance(req("d"), strings.Repeat("d", 64)); !errors.Is(err, ErrBusy) {
		t.Fatal("unheld queue crossed the boot reservation", err)
	}
	if err := m.QueueHeldSuccessor(successor, credential, grant.Token); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	var claimed atomic.Int32
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := m.Claim(req("e")); err == nil {
				claimed.Add(1)
			} else if !errors.Is(err, ErrBusy) && !errors.Is(err, ErrBlocked) {
				t.Error(err)
			}
		}()
	}
	close(start)
	if _, err := m.EndMaintenance(grant.Token, maintenanceEnd(grant)); err != nil {
		t.Fatal(err)
	}
	wait(t, m, "free")
	wg.Wait()
	if claimed.Load() != 0 {
		t.Fatalf("claim won during trial reconciliation: %d", claimed.Load())
	}
	if _, err := m.Claim(req("e")); !errors.Is(err, ErrBusy) {
		t.Fatal("claim won after the boot reservation ended", err)
	}
	next, err := m.StartQueuedMaintenance(successor, credential)
	if err != nil || next.Status.OperationID != successor.RequestID {
		t.Fatal(next, err)
	}
}

func TestExecutionFenceBlocksNewClaimsUntilQueueReturns(t *testing.T) {
	m := maintenanceManager(t, t.TempDir(), func(context.Context) error { return nil })
	owner, err := m.Claim(req("a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetExecutionFence(true); err != nil {
		t.Fatal(err)
	}
	if again, err := m.Claim(req("a")); err != nil || again.Token != owner.Token {
		t.Fatal("owner retry", again, err)
	}
	if _, err := m.Renew(owner.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(req("b")); !errors.Is(err, ErrBlocked) {
		t.Fatal("new claim", err)
	}
	if _, err := m.Takeover(TakeoverRequest{ClaimRequest: req("c"), ExpectedGeneration: owner.Status.Generation, Reason: "operator request"}); !errors.Is(err, ErrBlocked) {
		t.Fatal("takeover", err)
	}
	if err := m.QueueIdleMaintenance(req("d"), strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Release(owner.Token); err != nil {
		t.Fatal(err)
	}
	wait(t, m, "free")
	if _, err := m.Claim(req("b")); !errors.Is(err, ErrBlocked) {
		t.Fatal("fence opened a free kit", err)
	}
	if err := m.SetExecutionFence(false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Claim(req("b")); !errors.Is(err, ErrBusy) {
		t.Fatal("restored queue did not keep priority", err)
	}
}
