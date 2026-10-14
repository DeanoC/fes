package kitlease

import (
	"context"
	"encoding/json"
	"errors"
	contract "github.com/DeanoC/FogCast/kitlease"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testBoot = "12345678-1234-1234-1234-123456789abc"

func maintenanceManager(t *testing.T, root string, clean func(context.Context) error) *Manager {
	t.Helper()
	m := New(time.Minute, clean, WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), testBoot))
	t.Cleanup(m.Close)
	wait(t, m, "free")
	return m
}
func maintenanceRequest(s string) contract.MaintenanceStartRequest {
	return contract.MaintenanceStartRequest{RequestID: strings.Repeat(s, 32), Purpose: "replace compatible kit software"}
}
func waitMaintenance(t *testing.T, m *Manager, state string) contract.MaintenanceStatus {
	t.Helper()
	until := time.Now().Add(2 * time.Second)
	for time.Now().Before(until) {
		s := m.MaintenanceStatus()
		if s.State == state {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("want maintenance %s, got %+v lease %+v", state, m.MaintenanceStatus(), m.Status())
	return contract.MaintenanceStatus{}
}
func maintenanceEnd(g contract.MaintenanceGrant) contract.MaintenanceEndRequest {
	return contract.MaintenanceEndRequest{OperationID: g.Status.OperationID, ExpectedGeneration: g.Status.Generation}
}
func TestMaintenanceFencesBothClientsAndDrains(t *testing.T) {
	var cleanup atomic.Int32
	m := maintenanceManager(t, t.TempDir(), func(context.Context) error { cleanup.Add(1); return nil })
	execution, err := m.Claim(req("a"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, done, err := m.Begin(execution.Token)
	if err != nil {
		t.Fatal(err)
	}
	g, err := m.StartMaintenance(execution.Token, maintenanceRequest("b"))
	if err != nil || g.Status.State != "preparing" {
		t.Fatal(g, err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("execution was not fenced")
	}
	if cleanup.Load() != 1 {
		t.Fatal("cleanup overtook admitted operation")
	}
	if _, err = m.Claim(req("c")); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, _, err = m.Begin(execution.Token); !errors.Is(err, ErrLease) {
		t.Fatal(err)
	}
	if _, err = m.Release(execution.Token); !errors.Is(err, ErrLease) {
		t.Fatal(err)
	}
	if _, err = m.Renew(execution.Token); !errors.Is(err, ErrLease) {
		t.Fatal(err)
	}
	if _, err = m.Takeover(TakeoverRequest{ClaimRequest: req("c"), ExpectedGeneration: m.Status().Generation, Reason: "different host"}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if _, err = m.EndMaintenance(g.Token, maintenanceEnd(g)); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	done()
	waitMaintenance(t, m, "ready")
	again, err := m.StartMaintenance(execution.Token, maintenanceRequest("b"))
	if err != nil || again.Token != g.Token || again.Status.Generation != g.Status.Generation {
		t.Fatal(again, err)
	}
	if _, err = m.EndMaintenance("foreign", maintenanceEnd(g)); !errors.Is(err, ErrLease) {
		t.Fatal(err)
	}
	if _, err = m.EndMaintenance(g.Token, maintenanceEnd(g)); err != nil {
		t.Fatal(err)
	}
	wait(t, m, "free")
	if cleanup.Load() != 3 {
		t.Fatal(cleanup.Load())
	}
	if status, err := m.EndMaintenance(g.Token, maintenanceEnd(g)); err != nil || status.State != "none" {
		t.Fatal(status, err)
	}
	next, err := m.Claim(req("d"))
	if err != nil {
		t.Fatal(err)
	}
	newer, err := m.StartMaintenance(next.Token, maintenanceRequest("e"))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	if _, err = m.EndMaintenance(g.Token, maintenanceEnd(g)); !errors.Is(err, ErrLease) {
		t.Fatal("stale finish", err)
	}
	if m.MaintenanceStatus().Generation != newer.Status.Generation {
		t.Fatal("stale finish cleared another operation")
	}
}
func TestMaintenanceSurvivesRestartRequiresExplicitResume(t *testing.T) {
	root := t.TempDir()
	clean := func(context.Context) error { return nil }
	m := maintenanceManager(t, root, clean)
	original, _ := m.Claim(req("a"))
	request := maintenanceRequest("b")
	grant, err := m.StartMaintenance(original.Token, request)
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	m.Close()
	replacement := New(time.Minute, clean, WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), testBoot))
	defer replacement.Close()
	status := waitMaintenance(t, replacement, "blocked")
	if status.Generation != grant.Status.Generation || status.OperationID != request.RequestID || replacement.Status().State != "recovery-required" {
		t.Fatal(status, replacement.Status())
	}
	if _, err = replacement.Claim(req("c")); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	again, err := replacement.StartMaintenance(original.Token, request)
	if err != nil || again.Token != grant.Token {
		t.Fatal("lost start response", again, err)
	}
	if _, err = replacement.ResumeMaintenance(grant.Token, maintenanceEnd(grant)); err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, replacement, "ready")
	if _, err = replacement.EndMaintenance(grant.Token, maintenanceEnd(grant)); err != nil {
		t.Fatal(err)
	}
	wait(t, replacement, "free")
	replacement.Close()
	last := New(time.Minute, clean, WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), testBoot))
	defer last.Close()
	wait(t, last, "free")
	if status, err := last.EndMaintenance(grant.Token, maintenanceEnd(grant)); err != nil || status.State != "none" {
		t.Fatal("lost finish response", status, err)
	}
}
func TestMaintenanceUnreachableRuntimeBlocksAndFinishRechecks(t *testing.T) {
	var fail atomic.Bool
	m := maintenanceManager(t, t.TempDir(), func(context.Context) error {
		if fail.Load() {
			return ErrRuntimeUnreachable
		}
		return nil
	})
	original, _ := m.Claim(req("a"))
	fail.Store(true)
	grant, err := m.StartMaintenance(original.Token, maintenanceRequest("b"))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "blocked")
	if m.Status().State != "recovery-required" {
		t.Fatal(m.Status())
	}
	if _, err = m.EndMaintenance(grant.Token, maintenanceEnd(grant)); err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "blocked")
	if _, err = m.Claim(req("c")); !errors.Is(err, ErrBusy) {
		t.Fatal("failed finish admitted another owner", err)
	}
	fail.Store(false)
	if _, err = m.ResumeMaintenance(grant.Token, maintenanceEnd(grant)); err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
}
func TestMaintenanceLockFencesFinishAndStartup(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	clean := func(context.Context) error { calls.Add(1); return nil }
	m := maintenanceManager(t, root, clean)
	original, _ := m.Claim(req("a"))
	grant, err := m.StartMaintenance(original.Token, maintenanceRequest("b"))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	lock, err := os.OpenFile(filepath.Join(root, "kit-maintenance.lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	finished := make(chan error, 1)
	go func() { _, err := m.EndMaintenance(grant.Token, maintenanceEnd(grant)); finished <- err }()
	select {
	case err := <-finished:
		t.Fatal("finish ignored executor lock", err)
	case <-time.After(25 * time.Millisecond):
	}
	if calls.Load() != before {
		t.Fatal("cleanup ran during executor transaction")
	}
	unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	wait(t, m, "free")
	m.Close()
	unix.Flock(int(lock.Fd()), unix.LOCK_EX)
	start := time.Now()
	replacement := New(time.Minute, clean, WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), testBoot))
	defer replacement.Close()
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("startup blocked serving health")
	}
	time.Sleep(25 * time.Millisecond)
	if replacement.Status().State != "revoking" {
		t.Fatal("startup admitted before lock reconciliation")
	}
	unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	wait(t, replacement, "free")
}
func TestMaintenanceMalformedMarkerAndPersistenceFailureFailClosed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "kit-maintenance.json")
	if err := os.WriteFile(path, []byte(`{"schema":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	m := New(time.Minute, func(context.Context) error { return nil }, WithMaintenanceFile(path, filepath.Join(root, "kit-maintenance.lock"), testBoot))
	defer m.Close()
	s := wait(t, m, "blocked")
	if _, err := m.Takeover(TakeoverRequest{ClaimRequest: req("a"), ExpectedGeneration: s.Generation, Reason: "takeover cannot erase corrupt marker"}); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
	root2 := t.TempDir()
	healthy := maintenanceManager(t, root2, func(context.Context) error { return nil })
	original, _ := healthy.Claim(req("a"))
	if err := os.Mkdir(filepath.Join(root2, "kit-maintenance.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := healthy.StartMaintenance(original.Token, maintenanceRequest("b")); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
	wait(t, healthy, "blocked")
	if _, err := healthy.Claim(req("c")); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
}
func TestMaintenanceMarkerClosedSchemaAndPrivateMode(t *testing.T) {
	root := t.TempDir()
	m := maintenanceManager(t, root, func(context.Context) error { return nil })
	original, _ := m.Claim(req("a"))
	grant, err := m.StartMaintenance(original.Token, maintenanceRequest("b"))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	path := filepath.Join(root, "kit-maintenance.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record maintenanceRecord
	if err = json.Unmarshal(data, &record); err != nil || record.Schema != 1 || record.OperationID != grant.Status.OperationID || record.StartToken != original.Token || record.Token != grant.Token {
		t.Fatal("invalid marker", err)
	}
	public, err := json.Marshal(m.MaintenanceStatus())
	if err != nil || strings.Contains(string(public), original.Token) || strings.Contains(string(public), grant.Token) {
		t.Fatal("credential in public status")
	}
}

func TestMaintenanceStartupFencesDiskBeforeCleanup(t *testing.T) {
	root := t.TempDir()
	m := maintenanceManager(t, root, func(context.Context) error { return nil })
	original, _ := m.Claim(req("a"))
	grant, err := m.StartMaintenance(original.Token, maintenanceRequest("b"))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	m.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	replacement := New(time.Minute, func(context.Context) error { close(entered); <-release; return nil }, WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), testBoot))
	defer replacement.Close()
	<-entered
	data, err := os.ReadFile(filepath.Join(root, "kit-maintenance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record maintenanceRecord
	if err = json.Unmarshal(data, &record); err != nil || record.State != "blocked" {
		t.Fatal("startup cleanup did not first fence executor", err)
	}
	if replacement.MaintenanceStatus().State == "ready" {
		t.Fatal("startup advertised ready while cleanup runs")
	}
	if _, err = replacement.EndMaintenance(grant.Token, maintenanceEnd(grant)); !errors.Is(err, ErrBusy) {
		t.Fatal("finish raced startup cleanup", err)
	}
	close(release)
	waitMaintenance(t, replacement, "blocked")
}

func TestMaintenancePreparationAndFinishMarkersRequireExplicitRecovery(t *testing.T) {
	for _, phase := range []string{"preparing", "finishing"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			m := maintenanceManager(t, root, func(context.Context) error { return nil })
			original, _ := m.Claim(req("a"))
			grant, err := m.StartMaintenance(original.Token, maintenanceRequest("b"))
			if err != nil {
				t.Fatal(err)
			}
			waitMaintenance(t, m, "ready")
			m.Close()
			record := *m.maintenance
			record.State = phase
			if err = m.maintenanceStore.save(context.Background(), record, record.Generation); err != nil {
				t.Fatal(err)
			}
			replacement := New(time.Minute, func(context.Context) error { return nil }, WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), testBoot))
			defer replacement.Close()
			status := waitMaintenance(t, replacement, "blocked")
			if status.Generation != grant.Status.Generation {
				t.Fatal("restart changed reservation")
			}
			if _, err = replacement.EndMaintenance(grant.Token, maintenanceEnd(grant)); err != nil {
				t.Fatal(err)
			}
			wait(t, replacement, "free")
		})
	}
}
func TestMaintenanceStalePersistedIdentityCannotRunFinishCleanup(t *testing.T) {
	var calls atomic.Int32
	m := maintenanceManager(t, t.TempDir(), func(context.Context) error { calls.Add(1); return nil })
	original, _ := m.Claim(req("a"))
	grant, err := m.StartMaintenance(original.Token, maintenanceRequest("b"))
	if err != nil {
		t.Fatal(err)
	}
	waitMaintenance(t, m, "ready")
	record := *m.maintenance
	record.Generation = secret()
	record.OperationID = strings.Repeat("c", 32)
	record.Token = secret()
	if err = m.maintenanceStore.save(context.Background(), record, grant.Status.Generation); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	if _, err = m.EndMaintenance(grant.Token, maintenanceEnd(grant)); !errors.Is(err, ErrBlocked) {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Fatal("stale manager ran cleanup for newer reservation")
	}
	current, err := m.maintenanceStore.reconcileStartup(context.Background())
	if err != nil || current.Generation != record.Generation {
		t.Fatal("stale finish overwrote newer reservation", err)
	}
}

func TestMaintenanceStatusCannotReportAbsenceBeforePersistedLoad(t *testing.T) {
	root := t.TempDir()
	lock, err := os.OpenFile(filepath.Join(root, "kit-maintenance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	m := New(time.Minute, func(context.Context) error { return nil }, WithMaintenanceFile(filepath.Join(root, "kit-maintenance.json"), filepath.Join(root, "kit-maintenance.lock"), testBoot))
	defer m.Close()
	status, err := m.MaintenanceStatusChecked()
	if !errors.Is(err, ErrBlocked) || status.State != "unavailable" {
		t.Fatal("unknown persisted state reported absence", status, err)
	}
	if m.MaintenanceStatus().State == "none" {
		t.Fatal("unchecked status hid pending load")
	}
	unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	wait(t, m, "free")
	status, err = m.MaintenanceStatusChecked()
	if err != nil || status.State != "none" {
		t.Fatal("confirmed absence unavailable", status, err)
	}
}
