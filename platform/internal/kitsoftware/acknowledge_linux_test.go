package kitsoftware

import (
	"context"
	"encoding/json"
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

func installLegacyAcknowledgment(t *testing.T, r *FileRepository, request AcknowledgeRequest) {
	t.Helper()
	if _, err := acknowledgePrior(context.Background(), r, &fakeReservation{}, request, func(context.Context) error { return nil }); err != nil {
		t.Fatal("create acknowledgment receipt", err)
	}
	audit, exists, err := r.readAcknowledgment(request.Action)
	if err != nil || !exists {
		t.Fatal("read acknowledgment audit", audit, exists, err)
	}
	// Prior versions omitted Running and used this receipt reason.
	audit.Running = ""
	raw, err := json.Marshal(audit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.root, "acknowledgment-"+request.Action+".json"), raw, 0600); err != nil {
		t.Fatal("write legacy acknowledgment audit", err)
	}
	if err := r.Update(func(s *State) error {
		receipt := s.Receipts[request.Operation]
		receipt.Reason = "operator acknowledgment " + request.Action + " of already-running prior release; automatic recovery remains spent; original failure: " + audit.Failed.Reason
		s.Receipts[request.Operation] = receipt
		return nil
	}); err != nil {
		t.Fatal("write legacy acknowledgment receipt", err)
	}
}

func TestLegacyAcknowledgmentReceiptReplaysExactAction(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	installLegacyAcknowledgment(t, r, request)
	result, err := acknowledgePrior(context.Background(), r, &fakeReservation{}, request, func(context.Context) error {
		t.Fatal("legacy replay repeated health proof")
		return nil
	})
	if err != nil || !result.Completed {
		t.Fatalf("exact legacy acknowledgment replay failed: %+v %v", result, err)
	}
}

func TestLegacyAcknowledgmentReceiptRejectsChangedIdentity(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T, *FileRepository, *AcknowledgeRequest)
		running string
	}{
		{name: "action", mutate: func(_ *testing.T, _ *FileRepository, request *AcknowledgeRequest) {
			request.Action = strings.Repeat("e", 64)
		}},
		{name: "operation", mutate: func(_ *testing.T, _ *FileRepository, request *AcknowledgeRequest) {
			request.Operation = strings.Repeat("e", 64)
		}},
		{name: "selected bundle", mutate: func(_ *testing.T, _ *FileRepository, request *AcknowledgeRequest) { request.Selected = testCandidateID }},
		{name: "base identity", mutate: func(_ *testing.T, _ *FileRepository, request *AcknowledgeRequest) {
			request.Base = strings.Repeat("c", 64)
		}},
		{name: "generation", mutate: func(_ *testing.T, _ *FileRepository, request *AcknowledgeRequest) { request.Generation++ }},
		{name: "boot identity", mutate: func(_ *testing.T, _ *FileRepository, request *AcknowledgeRequest) {
			request.Boot = "22222222-3333-4444-5555-666666666666"
		}},
		{name: "running bundle", running: testCandidateID},
		{name: "operation payload", mutate: func(t *testing.T, r *FileRepository, request *AcknowledgeRequest) {
			if err := r.Update(func(s *State) error {
				receipt := s.Receipts[request.Operation]
				receipt.Candidate = strings.Repeat("f", 64)
				s.Receipts[request.Operation] = receipt
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "failure payload", mutate: func(t *testing.T, r *FileRepository, _ *AcknowledgeRequest) {
			audit, exists, err := r.readAcknowledgment(strings.Repeat("d", 64))
			if err != nil || !exists {
				t.Fatal(audit, exists, err)
			}
			audit.Failed.Reason += "; changed failure"
			raw, err := json.Marshal(audit)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(r.root, "acknowledgment-"+audit.Request.Action+".json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "durable previous bundle", mutate: func(t *testing.T, r *FileRepository, _ *AcknowledgeRequest) {
			if err := r.Update(func(s *State) error { s.Previous = testCandidateID; return nil }); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			installLegacyAcknowledgment(t, r, request)
			if tt.mutate != nil {
				tt.mutate(t, r, &request)
			}
			var err error
			if tt.running != "" {
				_, err = acknowledgePriorAs(context.Background(), r, &fakeReservation{}, request, tt.running, func(context.Context) error { return nil })
			} else {
				_, err = acknowledgePrior(context.Background(), r, &fakeReservation{}, request, func(context.Context) error { return nil })
			}
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("changed legacy acknowledgment identity returned %v", err)
			}
		})
	}
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

func TestAcknowledgmentPreservesHeldBackSelectionWithCompatibleRunningFactory(t *testing.T) {
	r, _, _, _ := setup(t)
	selectRelease(t, r, testSelectedID)
	state, _ := r.Read()
	request := req()
	request.Generation = state.Generation
	if err := Queue(r, request); err != nil {
		t.Fatal(err)
	}
	if err := transition(r, *mustStatePending(t, r), "queued", "blocked", "candidate failed; recovery exhausted"); err != nil {
		t.Fatal(err)
	}
	operator := AcknowledgeRequest{Operation: request.ID, Generation: request.Generation, Selected: testSelectedID, Base: strings.Repeat("b", 64), Boot: "11111111-2222-3333-4444-555555555555", Action: strings.Repeat("d", 64)}
	reservation := &fakeReservation{}
	result, err := acknowledgePriorAs(context.Background(), r, reservation, operator, testFactoryID, func(context.Context) error { return nil })
	if err != nil || !result.Completed || result.Selected != testSelectedID || result.Running != testFactoryID || result.Generation != request.Generation {
		t.Fatalf("factory acknowledgment result %+v, %v", result, err)
	}
	state, _ = r.Read()
	if state.Selected != testSelectedID || state.Previous != testFactoryID || state.Generation != request.Generation || state.Pending != nil || state.Unreleased != nil || state.Receipts[request.ID].Result != "recovered" {
		t.Fatalf("acknowledgment did not preserve the held-back selection: %+v", state)
	}
	audit, exists, err := r.readAcknowledgment(operator.Action)
	if err != nil || !exists || audit.Running != testFactoryID || !strings.Contains(state.Receipts[request.ID].Reason, "held-back selection "+testSelectedID+" remains recorded") {
		t.Fatalf("running factory missing from acknowledgment audit: %+v %v %v", audit, exists, err)
	}
	wantReason := "operator acknowledgment " + operator.Action + " of already-running release; automatic recovery remains spent; compatible running bundle " + testFactoryID + " acknowledged while held-back selection " + testSelectedID + " remains recorded; original failure: candidate failed; recovery exhausted"
	if state.Receipts[request.ID].Reason != wantReason {
		t.Fatalf("new acknowledgment receipt reason changed: got %q, want %q", state.Receipts[request.ID].Reason, wantReason)
	}
	if result, err := acknowledgePriorAs(context.Background(), r, reservation, operator, testFactoryID, func(context.Context) error { t.Fatal("replayed proof"); return nil }); err != nil || !result.Completed || result.Selected != testSelectedID || result.Running != testFactoryID || result.Generation != request.Generation {
		t.Fatalf("factory acknowledgment replay failed: %+v %v", result, err)
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
