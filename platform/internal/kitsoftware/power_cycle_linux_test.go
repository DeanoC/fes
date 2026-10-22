package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func fakePowerCycleEvidence(request AcknowledgeRequest, op Operation) powerCycleEvidence {
	return powerCycleEvidence{Native: nativePowerCycleProof{nativeStatus: nativeStatus{Protocol: 2, OK: true, State: "reboot_required", Execution: "none", Version: "test", Error: &nativeStatusError{Code: "idle_failed", Phase: "recovery", Message: "physical failure"}}, RuntimePID: 42, RuntimeStartTime: "123", RuntimeExecutable: "/verified/bin/mister-runtime"}, Maintenance: MaintenanceStatus{State: "blocked", OperationID: request.Operation, Generation: strings.Repeat("f", 64), Owner: op.Requester, Purpose: "kit-software-activation", BootID: request.Boot}}
}
func TestPowerCyclePreparationPreservesFailureFenceAndCompetingIntent(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	proofs, unlocks := 0, 0
	verify := func(ctx context.Context, op Operation) (powerCycleEvidence, func(), error) {
		proofs++
		if unlock, err := r.WorkerLock(); err == nil {
			unlock()
			t.Fatal("worker not fenced")
		}
		return fakePowerCycleEvidence(request, op), func() { unlocks++ }, nil
	}
	result, err := preparePowerCycle(context.Background(), r, request, verify)
	if err != nil || result.Outcome != "prepared-awaiting-physical-cycle" {
		t.Fatal(result, err)
	}
	state, _ := r.Read()
	receipt := state.Receipts[request.Operation]
	if state.Pending != nil || state.Unreleased != nil || state.Selected != testFactoryID || state.Generation != 0 || state.Previous != "" || receipt.Result != "cancelled" {
		t.Fatal(state)
	}
	audit, exists, err := r.readPowerCycleAudit(request.Action)
	if err != nil || !exists || audit.Failed.Phase != "blocked" || !strings.Contains(audit.Failed.Reason, "recovery failed") || receipt.Reason != powerCycleReason(audit) {
		t.Fatal(audit, err)
	}
	if err := validateState(state); err != nil {
		t.Fatal("old supervisor cannot read receipt", err)
	}
	fresh := req()
	fresh.ID = strings.Repeat("e", 64)
	if err := Queue(r, fresh); err != nil {
		t.Fatal("legitimate new intent was fenced out of software queue", err)
	}
	if _, err := preparePowerCycle(context.Background(), r, request, verify); err != nil {
		t.Fatal("exact replay failed", err)
	}
	state, _ = r.Read()
	if state.Pending == nil || state.Pending.Request != fresh || proofs != 1 || unlocks != 1 {
		t.Fatal("replay touched new intent or old private grant", state, proofs, unlocks)
	}
	if err := r.Update(func(s *State) error {
		s.Generation++
		s.Previous = s.Selected
		s.Selected = testCandidateID
		s.Pending = nil
		s.Receipts[fresh.ID] = Receipt{Request: fresh, Result: "accepted"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := preparePowerCycle(context.Background(), r, request, verify); !errors.Is(err, ErrConflict) {
		t.Fatal("stale completion accepted", err)
	}
	state, _ = r.Read()
	if state.Generation != 1 || state.Selected != testCandidateID {
		t.Fatal("new selection overwritten")
	}
}
func TestPowerCyclePreparationFaultsKeepBlockedAndReplayAudit(t *testing.T) {
	for _, fault := range []string{"proof", "audit", "journal", "cancelled-context"} {
		t.Run(fault, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			unlocks := 0
			verify := func(ctx context.Context, op Operation) (powerCycleEvidence, func(), error) {
				if fault == "proof" {
					return powerCycleEvidence{}, nil, errors.New("wrong runtime peer")
				}
				if fault == "audit" {
					if err := os.Mkdir(filepath.Join(r.root, "power-cycle-"+request.Action+".json"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				if fault == "journal" {
					if err := os.Mkdir(filepath.Join(r.root, "state.tmp"), 0700); err != nil {
						t.Fatal(err)
					}
				}
				if fault == "cancelled-context" {
					cancel()
				}
				return fakePowerCycleEvidence(request, op), func() { unlocks++ }, nil
			}
			if _, err := preparePowerCycle(ctx, r, request, verify); err == nil {
				t.Fatal("fault accepted")
			}
			state, _ := r.Read()
			if state.Pending == nil || state.Pending.Phase != "blocked" || len(state.Receipts) != 0 || state.Unreleased != nil {
				t.Fatal(state)
			}
			if fault != "proof" && unlocks != 1 {
				t.Fatal("maintenance lock leaked")
			}
			if fault == "journal" {
				if _, exists, err := r.readPowerCycleAudit(request.Action); err != nil || !exists {
					t.Fatal("audit was not durable before journal", exists, err)
				}
				if err := os.Remove(filepath.Join(r.root, "state.tmp")); err != nil {
					t.Fatal(err)
				}
				verify = func(ctx context.Context, op Operation) (powerCycleEvidence, func(), error) {
					return fakePowerCycleEvidence(request, op), func() {}, nil
				}
				changed := request
				changed.Action = strings.Repeat("e", 64)
				if _, err := preparePowerCycle(ctx, r, changed, verify); !errors.Is(err, ErrConflict) {
					t.Fatal("conflicting operator action accepted", err)
				}
				if _, err := preparePowerCycle(ctx, r, request, verify); err != nil {
					t.Fatal("audit retry failed", err)
				}
			}
		})
	}
}
func TestPowerCyclePreparationRejectsChangedPendingBeforeCommit(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	_, err := preparePowerCycle(context.Background(), r, request, func(ctx context.Context, op Operation) (powerCycleEvidence, func(), error) {
		if err := r.Update(func(s *State) error { s.Pending.ID = strings.Repeat("e", 64); return nil }); err != nil {
			t.Fatal(err)
		}
		return fakePowerCycleEvidence(request, op), func() {}, nil
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	state, _ := r.Read()
	if state.Pending.ID != strings.Repeat("e", 64) || len(state.Receipts) != 0 {
		t.Fatal("new operation overwritten", state)
	}
}
func TestPowerCycleMaintenanceProofIsReadOnlyAndRetainsFence(t *testing.T) {
	for _, scenario := range []string{"matching", "wrong-token", "resumed"} {
		t.Run(scenario, func(t *testing.T) {
			_, request := acknowledgmentFixture(t)
			op := Operation{Request: req(), Previous: testFactoryID, Phase: "blocked"}
			dir := t.TempDir()
			lock := filepath.Join(dir, "lock")
			markerPath := filepath.Join(dir, "marker")
			grantPath := filepath.Join(dir, "grant")
			if err := os.WriteFile(lock, nil, 0600); err != nil {
				t.Fatal(err)
			}
			held := fakePowerCycleEvidence(request, op).Maintenance
			marker := powerCycleMarker{Schema: 1, MaintenanceStatus: held, Token: strings.Repeat("a", 64), StartToken: strings.Repeat("b", 64)}
			grant := maintenanceGrant{Status: held, Token: marker.Token}
			switch scenario {
			case "wrong-token":
				grant.Token = strings.Repeat("c", 64)
			case "resumed":
				marker.State = "ready"
			}
			mb, _ := json.Marshal(marker)
			gb, _ := json.Marshal(grant)
			if err := os.WriteFile(markerPath, mb, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(grantPath, gb, 0600); err != nil {
				t.Fatal(err)
			}
			got, unlock, err := lockMaintenanceReservation(context.Background(), lock, markerPath, grantPath, request.Operation, request.Boot, op.Requester, "blocked")
			switch scenario {
			case "matching":
				if err != nil || got.State != "blocked" {
					t.Fatal(got, err)
				}
				f, err := os.OpenFile(lock, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) {
					t.Fatal("marker lock not retained", err)
				}
				f.Close()
				unlock()
			case "wrong-token":
				if err == nil {
					unlock()
					t.Fatal("wrong credential accepted")
				}
			case "resumed":
				// A resumed reservation is a state mismatch, not a credential fault.
				if err == nil {
					unlock()
					t.Fatal("ready reservation accepted")
				}
				if err.Error() != "maintenance reservation of this operation is ready, not blocked" {
					t.Fatal(err)
				}
			}
			afterM, _ := os.ReadFile(markerPath)
			afterG, _ := os.ReadFile(grantPath)
			if string(afterM) != string(mb) || string(afterG) != string(gb) {
				t.Fatal("proof altered credential or fence")
			}
		})
	}
}

func TestPowerCycleRequiresSuccessfulStatusOfFailedPhysicalState(t *testing.T) {
	for _, scenario := range []string{"status-success-physical-failure", "status-failed", "physical-idle", "wrong-error"} {
		t.Run(scenario, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			_, err := preparePowerCycle(context.Background(), r, request, func(ctx context.Context, op Operation) (powerCycleEvidence, func(), error) {
				evidence := fakePowerCycleEvidence(request, op)
				switch scenario {
				case "status-failed":
					evidence.Native.OK = false
				case "physical-idle":
					evidence.Native.State = "idle"
					evidence.Native.Error = nil
				case "wrong-error":
					evidence.Native.Error.Code = "load_failed"
				}
				return evidence, func() {}, nil
			})
			if (err == nil) != (scenario == "status-success-physical-failure") {
				t.Fatalf("%s: %v", scenario, err)
			}
			state, _ := r.Read()
			if err != nil && (state.Pending == nil || state.Pending.Phase != "blocked" || len(state.Receipts) != 0) {
				t.Fatal("failed status proof modified journal", state)
			}
		})
	}
}
