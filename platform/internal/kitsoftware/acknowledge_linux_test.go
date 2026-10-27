package kitsoftware

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeReservation is the failed operation's maintenance reservation. Held only
// reads it; Release is the one call that ends it.
type fakeReservation struct {
	heldErr         error
	failRelease     bool
	helds, releases int
}

func (f *fakeReservation) Held(context.Context, Request) error {
	f.helds++
	return f.heldErr
}
func (f *fakeReservation) Release(context.Context, Request) error {
	f.releases++
	if f.failRelease {
		return errors.New("agent down")
	}
	return nil
}

func acknowledgmentFixture(t *testing.T) (*FileRepository, AcknowledgeRequest) {
	r, _, _, _ := setup(t)
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(func(s *State) error {
		s.Pending.Phase = "blocked"
		s.Pending.Reason = "candidate failed: deadline; recovery failed: deadline"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return r, AcknowledgeRequest{Operation: req().ID, Generation: 0, Selected: testFactoryID, Base: strings.Repeat("b", 64), Boot: "11111111-2222-3333-4444-555555555555", Action: strings.Repeat("d", 64)}
}
func TestAcknowledgmentPreservesFailureAndReplaysLostRelease(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	a := &fakeReservation{failRelease: true}
	checks := 0
	stable := func(context.Context) error { checks++; return nil }
	if _, err := acknowledgePrior(context.Background(), r, a, request, stable); err == nil {
		t.Fatal("lost release accepted")
	}
	state, _ := r.Read()
	audit, exists, err := r.readAcknowledgment(request.Action)
	if err != nil || !exists || audit.Failed.Reason != "candidate failed: deadline; recovery failed: deadline" {
		t.Fatal(audit, exists, err)
	}
	receipt := state.Receipts[request.Operation]
	if state.Pending != nil || state.Unreleased == nil || state.Generation != 0 || state.Selected != testFactoryID || receipt.Result != "recovered" || receipt.Reason != acknowledgmentReason(audit) {
		t.Fatal(state)
	}
	if err := validateState(state); err != nil {
		t.Fatal("old supervisor cannot read completion", err)
	}
	a.failRelease = false
	result, err := acknowledgePrior(context.Background(), r, a, request, stable)
	if err != nil || !result.Completed {
		t.Fatal(result, err)
	}
	if checks != 1 || a.helds != 2 {
		t.Fatal("replay repeated repair", checks, a)
	}
	if _, err := acknowledgePrior(context.Background(), r, a, request, stable); err != nil {
		t.Fatal(err)
	}
	if a.releases != 2 {
		t.Fatal("completed replay released again", a)
	}
	info, _ := os.Stat(filepath.Join(r.root, "acknowledgment-"+request.Action+".json"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}
func TestAcknowledgmentFailureBeforeCompletionKeepsBlocked(t *testing.T) {
	for _, fault := range []string{"health", "audit", "journal"} {
		t.Run(fault, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			a := &fakeReservation{}
			stable := func(context.Context) error {
				switch fault {
				case "health":
					return errors.New("launcher not ready")
				case "audit":
					return os.Mkdir(filepath.Join(r.root, "acknowledgment-"+request.Action+".json"), 0700)
				case "journal":
					return os.Mkdir(filepath.Join(r.root, "state.tmp"), 0700)
				}
				return nil
			}
			if _, err := acknowledgePrior(context.Background(), r, a, request, stable); err == nil {
				t.Fatal("injected failure accepted")
			}
			state, _ := r.Read()
			if state.Pending == nil || state.Pending.Phase != "blocked" || len(state.Receipts) != 0 || a.releases != 0 {
				t.Fatal(state, a)
			}
			if fault == "journal" {
				if err := os.Remove(filepath.Join(r.root, "state.tmp")); err != nil {
					t.Fatal(err)
				}
				result, err := acknowledgePrior(context.Background(), r, a, request, func(context.Context) error { return nil })
				if err != nil || !result.Completed {
					t.Fatal("durable audit could not resume", result, err)
				}
			}
		})
	}
}
func TestAcknowledgmentReadsReservationAroundReadiness(t *testing.T) {
	for _, when := range []string{"before", "after"} {
		t.Run(when, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			a := &fakeReservation{}
			if when == "before" {
				a.heldErr = errors.New("failed operation no longer owns this boot's maintenance reservation")
			}
			probes := 0
			_, err := acknowledgePrior(context.Background(), r, a, request, func(context.Context) error {
				probes++
				a.heldErr = errors.New("failed maintenance reservation is finishing, not blocked or ready")
				return nil
			})
			if err == nil {
				t.Fatal("changed reservation acknowledged")
			}
			if (when == "before") != (probes == 0) {
				t.Fatal("readiness probe ran out of order", when, probes)
			}
			state, _ := r.Read()
			if _, exists, _ := r.readAcknowledgment(request.Action); exists || state.Pending == nil || state.Pending.Phase != "blocked" || len(state.Receipts) != 0 || a.releases != 0 {
				t.Fatal("unheld reservation reached the journal", state, a)
			}
		})
	}
}
func TestAcknowledgmentRejectsStaleIdentityAndConflictingAction(t *testing.T) {
	for _, field := range []string{"operation", "generation", "selected", "action"} {
		t.Run(field, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			state, _ := r.Read()
			audit := acknowledgmentAudit{Request: request, Failed: *state.Pending, Previous: state.Previous}
			if err := r.writeAcknowledgment(audit); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "operation":
				request.Operation = strings.Repeat("e", 64)
			case "generation":
				request.Generation++
			case "selected":
				request.Selected = testCandidateID
			case "action":
				request.Action = strings.Repeat("e", 64)
			}
			a := &fakeReservation{}
			if _, err := acknowledgePrior(context.Background(), r, a, request, func(context.Context) error { return nil }); err == nil {
				t.Fatal("conflicting repair accepted")
			}
			if a.helds != 0 || a.releases != 0 {
				t.Fatal("conflicting request touched maintenance")
			}
		})
	}
}
func TestAcknowledgmentCannotUndoNewerSelection(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	a := &fakeReservation{}
	if _, err := acknowledgePrior(context.Background(), r, a, request, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(func(s *State) error {
		s.Generation++
		s.Previous = s.Selected
		s.Selected = testCandidateID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := acknowledgePrior(context.Background(), r, a, request, func(context.Context) error { t.Fatal("stale action reached health"); return nil }); err == nil {
		t.Fatal("stale action accepted")
	}
	state, _ := r.Read()
	if state.Selected != testCandidateID || state.Generation != 1 {
		t.Fatal(state)
	}
}
func TestAcknowledgeProductionRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged test process")
	}
	if _, err := AcknowledgeRunningPrior(context.Background(), AcknowledgeRequest{}); err == nil {
		t.Fatal("nonroot repair accepted")
	}
}

func TestAcknowledgmentSerializesWorkerAndRejectsChangedPending(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	a := &fakeReservation{}
	_, err := acknowledgePrior(context.Background(), r, a, request, func(context.Context) error {
		if unlock, err := r.WorkerLock(); err == nil {
			unlock()
			t.Fatal("supervisor worker was not excluded")
		} else if !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
		// Even a privileged writer that ignores WorkerLock cannot make the final
		// CAS acknowledge or release a different operation.
		return r.Update(func(s *State) error { s.Pending.ID = strings.Repeat("e", 64); return nil })
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	state, _ := r.Read()
	if state.Pending == nil || state.Pending.ID != strings.Repeat("e", 64) || len(state.Receipts) != 0 || a.releases != 0 {
		t.Fatal(state, a)
	}
}

func TestAcknowledgmentStableWindowStartsAfterFirstSuccessfulProbe(t *testing.T) {
	started := time.Now()
	calls := 0
	err := stableAcknowledgmentWindow(context.Background(), func(context.Context) error {
		calls++
		if calls == 1 {
			time.Sleep(30 * time.Millisecond)
		}
		return nil
	}, time.Second, 20*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if calls < 2 || time.Since(started) < 50*time.Millisecond {
		t.Fatal("slow first probe counted as stability", calls, time.Since(started))
	}
}
func TestAcknowledgmentNeverAcceptsProbeAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := stableAcknowledgmentWindow(ctx, func(context.Context) error { cancel(); return nil }, time.Second, 0, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled successful probe accepted", err)
	}
}
