package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testImageFactoryID is the running image's own factory software. The fixture
// selection testFactoryID is the release an earlier image retained.
const testImageFactoryID = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

func nothingRunning(context.Context) error { return nil }

// Kit B after its unprepared power cycle: Boot refuses the spent recovery and
// starts nothing. Selecting the image's factory software is the way out, and
// the next Boot runs it with a fresh boot admission.
func TestFactorySelectionLeavesExhaustedRecovery(t *testing.T) {
	r, e, c, a := setup(t)
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	c.fail[testCandidateID], c.fail[testFactoryID] = true, true
	if err := e.Run(context.Background()); err == nil {
		t.Fatal("failed recovery accepted")
	}
	boot := Request{ID: strings.Repeat("b", 64), Candidate: testImageFactoryID, Requester: "kit-software-supervisor"}
	starts := c.starts
	if err := e.Boot(context.Background(), boot); err == nil || err.Error() != "software recovery exhausted; operator intervention required" {
		t.Fatal(err)
	}
	if c.starts != starts {
		t.Fatal("exhausted boot started software")
	}
	blocked, _ := r.Read()
	request := AcknowledgeRequest{Operation: req().ID, Generation: 0, Selected: testFactoryID, Base: strings.Repeat("b", 64), Boot: "11111111-2222-3333-4444-555555555555", Action: strings.Repeat("d", 64)}
	result, err := selectFactory(context.Background(), r, request, testImageFactoryID, nothingRunning)
	if err != nil || result.Outcome != "factory-selected" || result.Selected != testImageFactoryID || result.Generation != 1 {
		t.Fatal(result, err)
	}
	audit, exists, err := r.readFactorySelectionAudit(request.Action)
	if err != nil || !exists || audit.Failed != *blocked.Pending || audit.Previous != blocked.Previous || audit.Factory != testImageFactoryID {
		t.Fatal(audit, exists, err)
	}
	state, _ := r.Read()
	receipt := state.Receipts[request.Operation]
	if state.Pending != nil || state.Unreleased != nil || state.Selected != testImageFactoryID || state.Previous != testFactoryID || state.Generation != 1 || receipt.Result != "cancelled" || receipt.Reason != factorySelectionReason(audit) || !strings.Contains(receipt.Reason, "recovery failed") {
		t.Fatal(state)
	}
	if err := validateState(state); err != nil {
		t.Fatal("old supervisor cannot read factory selection", err)
	}
	if info, err := os.Stat(filepath.Join(r.root, "factory-selection-"+request.Action+".json")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	acquires, releases := a.acquires, a.releases
	if err := e.Boot(context.Background(), boot); err != nil {
		t.Fatal("selected factory did not start", err)
	}
	if c.active != testImageFactoryID || a.acquires != acquires+1 || a.releases != releases+1 {
		t.Fatal("boot did not run the factory and release its admission", c.active, a)
	}
}

func TestFactorySelectionReplayReportsOnlyCurrentOutcome(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	proofs := 0
	idle := func(context.Context) error { proofs++; return nil }
	first, err := selectFactory(context.Background(), r, request, testImageFactoryID, idle)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(r.root, "state.json"))
	again, err := selectFactory(context.Background(), r, request, testImageFactoryID, idle)
	if err != nil || again != first || proofs != 1 {
		t.Fatal("exact replay changed outcome or repeated proof", again, err, proofs)
	}
	if after, _ := os.ReadFile(filepath.Join(r.root, "state.json")); string(after) != string(before) {
		t.Fatal("replay rewrote the journal")
	}
	changed := request
	changed.Action = strings.Repeat("e", 64)
	if _, err := selectFactory(context.Background(), r, changed, testImageFactoryID, idle); !errors.Is(err, ErrConflict) {
		t.Fatal("second operator action for one operation accepted", err)
	}
	if err := r.Update(func(s *State) error {
		s.Generation++
		s.Previous = s.Selected
		s.Selected = testCandidateID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := selectFactory(context.Background(), r, request, testImageFactoryID, idle); !errors.Is(err, ErrConflict) {
		t.Fatal("replay reported a replaced selection", err)
	}
	state, _ := r.Read()
	if state.Selected != testCandidateID || state.Generation != 2 {
		t.Fatal("newer selection overwritten", state)
	}
}

func TestFactorySelectionRefusesActionOfAnotherRepair(t *testing.T) {
	for _, repair := range []string{"acknowledgment", "power-cycle"} {
		t.Run(repair, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			state, _ := r.Read()
			var err error
			if repair == "acknowledgment" {
				err = r.writeAcknowledgment(acknowledgmentAudit{Request: request, Failed: *state.Pending, Previous: state.Previous})
			} else {
				err = r.writePowerCycleAudit(powerCycleAudit{Request: request, Failed: *state.Pending, Previous: state.Previous, Evidence: fakePowerCycleEvidence(request, *state.Pending)})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := selectFactory(context.Background(), r, request, testImageFactoryID, func(context.Context) error {
				t.Fatal("reused action reached the idle proof")
				return nil
			}); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
			after, _ := r.Read()
			if after.Pending == nil || after.Pending.Phase != "blocked" || len(after.Receipts) != 0 {
				t.Fatal(after)
			}
		})
	}
}

// One action identifier names one operator act. A later repair of another
// operation cannot take an identifier an earlier repair recorded, so a replay
// keeps reporting the outcome that identifier made.
func TestRepairActionIdentifiersStayUniqueAcrossRepairs(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	first, err := selectFactory(context.Background(), r, request, testImageFactoryID, nothingRunning)
	if err != nil {
		t.Fatal(err)
	}
	next := Request{ID: strings.Repeat("9", 64), Candidate: testCandidateID, Requester: "host1", Generation: first.Generation}
	if err := Queue(r, next); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(func(s *State) error { s.Pending.Phase = "blocked"; return nil }); err != nil {
		t.Fatal(err)
	}
	reused := AcknowledgeRequest{Operation: next.ID, Generation: first.Generation, Selected: testImageFactoryID, Base: request.Base, Boot: request.Boot, Action: request.Action}
	unreached := func(context.Context) error { t.Fatal("reused action reached readiness"); return nil }
	a := &fakeReservation{}
	if _, err := acknowledgePrior(context.Background(), r, a, reused, unreached); !errors.Is(err, ErrConflict) || a.helds != 0 {
		t.Fatal("acknowledgment took a factory-selection action", err, a)
	}
	if _, err := preparePowerCycle(context.Background(), r, reused, func(context.Context, Operation) (powerCycleEvidence, func(), error) {
		t.Fatal("reused action reached the power-cycle proof")
		return powerCycleEvidence{}, nil, nil
	}); !errors.Is(err, ErrConflict) {
		t.Fatal("power-cycle preparation took a factory-selection action", err)
	}
	state, _ := r.Read()
	cycled := reused
	cycled.Action = strings.Repeat("8", 64)
	if err := r.writePowerCycleAudit(powerCycleAudit{Request: cycled, Failed: *state.Pending, Previous: state.Previous, Evidence: fakePowerCycleEvidence(cycled, *state.Pending)}); err != nil {
		t.Fatal(err)
	}
	if _, err := acknowledgePrior(context.Background(), r, a, cycled, unreached); !errors.Is(err, ErrConflict) || a.helds != 0 {
		t.Fatal("acknowledgment took a power-cycle action", err, a)
	}
	if again, err := selectFactory(context.Background(), r, request, testImageFactoryID, nothingRunning); err != nil || again != first {
		t.Fatal("factory-selection replay lost its outcome", again, err)
	}
}

func TestFactorySelectionRequiresExactBlockedOperation(t *testing.T) {
	for _, field := range []string{"operation", "generation", "selected", "queued", "applying", "recovering"} {
		t.Run(field, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			switch field {
			case "operation":
				request.Operation = strings.Repeat("e", 64)
			case "generation":
				request.Generation++
			case "selected":
				request.Selected = testCandidateID
			default:
				if err := r.Update(func(s *State) error { s.Pending.Phase = field; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(filepath.Join(r.root, "state.json"))
			if _, err := selectFactory(context.Background(), r, request, testImageFactoryID, func(context.Context) error {
				t.Fatal("unmatched operation reached the idle proof")
				return nil
			}); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
			if after, _ := os.ReadFile(filepath.Join(r.root, "state.json")); string(after) != string(before) {
				t.Fatal("refused selection changed the journal")
			}
			if _, exists, err := r.readFactorySelectionAudit(request.Action); err != nil || exists {
				t.Fatal("refused selection wrote an audit", exists, err)
			}
		})
	}
}

func TestFactorySelectionFaultsKeepBlockedAndReplayAudit(t *testing.T) {
	for _, fault := range []string{"idle", "audit", "journal", "journal-then-reboot", "cancelled-context"} {
		t.Run(fault, func(t *testing.T) {
			r, request := acknowledgmentFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			idle := func(context.Context) error {
				switch fault {
				case "idle":
					return errors.New("this boot still holds the blocked maintenance reservation")
				case "audit":
					return os.Mkdir(filepath.Join(r.root, "factory-selection-"+request.Action+".json"), 0700)
				case "journal", "journal-then-reboot":
					return os.Mkdir(filepath.Join(r.root, "state.tmp"), 0700)
				case "cancelled-context":
					cancel()
				}
				return nil
			}
			if _, err := selectFactory(ctx, r, request, testImageFactoryID, idle); err == nil {
				t.Fatal("fault accepted")
			}
			state, _ := r.Read()
			if state.Pending == nil || state.Pending.Phase != "blocked" || len(state.Receipts) != 0 || state.Selected != testFactoryID || state.Generation != 0 {
				t.Fatal(state)
			}
			if fault != "journal" && fault != "journal-then-reboot" {
				return
			}
			if _, exists, err := r.readFactorySelectionAudit(request.Action); err != nil || !exists {
				t.Fatal("audit was not durable before journal", exists, err)
			}
			if err := os.Remove(filepath.Join(r.root, "state.tmp")); err != nil {
				t.Fatal(err)
			}
			// The same boot repeats the exact action. After a reboot that boot
			// identity can never be repeated, so a fresh action must still work.
			retry, other := request, request
			if fault == "journal-then-reboot" {
				retry.Boot, retry.Action = "22222222-3333-4444-5555-666666666666", strings.Repeat("e", 64)
			} else {
				other.Action = strings.Repeat("e", 64)
			}
			if result, err := selectFactory(ctx, r, retry, testImageFactoryID, nothingRunning); err != nil || result.Generation != 1 {
				t.Fatal("retry after the failed journal write was refused", result, err)
			}
			if _, err := selectFactory(ctx, r, other, testImageFactoryID, nothingRunning); !errors.Is(err, ErrConflict) {
				t.Fatal("a second action reported the completed selection", err)
			}
			state, _ = r.Read()
			if state.Pending != nil || state.Selected != testImageFactoryID || state.Generation != 1 || !strings.Contains(state.Receipts[request.Operation].Reason, retry.Action) {
				t.Fatal(state)
			}
		})
	}
}

// When the blocked operation's previous release already is this image's
// factory software, selection is unchanged and only the operation ends.
func TestFactorySelectionOfSelectedFactoryKeepsGeneration(t *testing.T) {
	r, request := acknowledgmentFixture(t)
	result, err := selectFactory(context.Background(), r, request, testFactoryID, nothingRunning)
	if err != nil || result.Selected != testFactoryID || result.Generation != 0 {
		t.Fatal(result, err)
	}
	state, _ := r.Read()
	if state.Pending != nil || state.Selected != testFactoryID || state.Previous != "" || state.Generation != 0 || state.Receipts[request.Operation].Result != "cancelled" {
		t.Fatal(state)
	}
	if again, err := selectFactory(context.Background(), r, request, testFactoryID, nothingRunning); err != nil || again != result {
		t.Fatal(again, err)
	}
}

func TestExhaustedIdleRefusesLiveWorkOfThisBoot(t *testing.T) {
	const boot = "11111111-2222-3333-4444-555555555555"
	for _, scenario := range []string{"absent", "completed", "other-boot", "blocked", "ready", "running"} {
		t.Run(scenario, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "kit-maintenance.json")
			held := MaintenanceStatus{State: scenario, OperationID: req().ID, Generation: strings.Repeat("f", 64), Owner: "host1", Purpose: "kit-software-activation", BootID: boot}
			switch scenario {
			case "other-boot":
				held.State, held.BootID = "blocked", "22222222-3333-4444-5555-666666666666"
			case "running":
				held.State = "completed"
			}
			var written []byte
			if scenario != "absent" {
				written, _ = json.Marshal(powerCycleMarker{Schema: 1, MaintenanceStatus: held, Token: strings.Repeat("a", 64), StartToken: strings.Repeat("b", 64)})
				if err := os.WriteFile(marker, written, 0600); err != nil {
					t.Fatal(err)
				}
			}
			noneRunning := func() error { return nil }
			if scenario == "running" {
				noneRunning = func() error { return errors.New("managed software process 42 is still running") }
			}
			err := exhaustedIdle(marker, boot, noneRunning)
			if refused := scenario == "blocked" || scenario == "ready" || scenario == "running"; refused != (err != nil) {
				t.Fatalf("%s: %v", scenario, err)
			}
			if after, _ := os.ReadFile(marker); string(after) != string(written) {
				t.Fatal("idle proof changed the reservation marker")
			}
		})
	}
}

func TestSelectFactoryProductionRequiresRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged test process")
	}
	if _, err := SelectFactory(context.Background(), AcknowledgeRequest{}); err == nil {
		t.Fatal("nonroot factory selection accepted")
	}
}
