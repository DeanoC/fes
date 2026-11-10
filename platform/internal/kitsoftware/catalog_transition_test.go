package kitsoftware

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// catalogTransitionFake models data rather than filesystem mechanics. A failed
// candidate may have written its newer format, and only this operation's
// checkpoint can put the prior host's data back before that host starts.
type catalogTransitionFake struct {
	events         *[]string
	live           string
	checkpoint     string
	owner          Request
	applied        []Operation
	sources        []string
	recovered      []Operation
	recoveryIDs    []string
	failApply      error
	failBeforeSave bool
	failRecover    error
}

func (f *catalogTransitionFake) Apply(_ context.Context, op Operation, source string) error {
	*f.events = append(*f.events, "apply")
	f.applied = append(f.applied, op)
	f.sources = append(f.sources, source)
	if f.failBeforeSave {
		return f.failApply
	}
	f.checkpoint, f.owner = f.live, op.Request
	f.live = "candidate-format: " + f.live
	return f.failApply
}

func (f *catalogTransitionFake) Recover(_ context.Context, op Operation, recoveryTarget string) error {
	*f.events = append(*f.events, "recover")
	f.recovered = append(f.recovered, op)
	f.recoveryIDs = append(f.recoveryIDs, recoveryTarget)
	if f.failRecover != nil {
		return f.failRecover
	}
	if f.owner == (Request{}) {
		return errors.New("no durable checkpoint for this operation")
	}
	if f.owner != op.Request {
		return ErrConflict
	}
	if f.checkpoint != "" {
		f.live = f.checkpoint
	}
	return nil
}

type catalogTransitionControl struct {
	*fakeControl
	t             *testing.T
	events        *[]string
	catalog       *catalogTransitionFake
	candidateID   string
	candidateFail error
	candidateExit int
}

func (c *catalogTransitionControl) Stop(ctx context.Context) error {
	*c.events = append(*c.events, "stop")
	err := c.fakeControl.Stop(ctx)
	if err == nil {
		c.active = ""
	}
	return err
}

func (c *catalogTransitionControl) Start(ctx context.Context, id string, request Request) error {
	candidate := c.candidateID
	if candidate == "" {
		candidate = testCandidateID
	}
	if id == candidate {
		*c.events = append(*c.events, "start candidate")
		if c.candidateExit > 0 {
			c.candidateExit--
			c.starts++
			return ErrProcessExited
		}
		if c.candidateFail != nil {
			c.starts++
			return c.candidateFail
		}
	} else {
		*c.events = append(*c.events, "start prior")
		if c.catalog.live != "prior entries, firmware and settings" {
			c.t.Fatalf("prior host started against candidate data %q", c.catalog.live)
		}
	}
	return c.fakeControl.Start(ctx, id, request)
}

func setupCatalogTransition(t *testing.T) (*FileRepository, *Engine, *catalogTransitionControl, *catalogTransitionFake, *fakeAdmission, *[]string) {
	t.Helper()
	r, engine, control, admission := setup(t)
	control.active = testFactoryID
	events := new([]string)
	catalog := &catalogTransitionFake{events: events, live: "prior entries, firmware and settings"}
	c := &catalogTransitionControl{fakeControl: control, t: t, events: events, catalog: catalog}
	engine.Controller, engine.Catalogs = c, catalog
	return r, engine, c, catalog, admission, events
}

func catalogTransitionOperation(t *testing.T, r *FileRepository) Operation {
	t.Helper()
	state, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Pending == nil {
		t.Fatal("expected a pending operation")
	}
	return *state.Pending
}

func requireCatalogTransitionEvents(t *testing.T, got *[]string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("transition order = %v, want %v", *got, want)
	}
}

func TestCatalogTransitionRunsAfterStopBeforeCandidateAndOnlyOnce(t *testing.T) {
	r, engine, control, catalog, _, events := setupCatalogTransition(t)
	control.candidateExit = 1
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	op := catalogTransitionOperation(t, r)
	if err := engine.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireCatalogTransitionEvents(t, events, "stop", "apply", "start candidate", "stop", "start candidate")
	if len(catalog.applied) != 1 || len(catalog.recovered) != 0 {
		t.Fatalf("startup retry repeated catalog transition: apply=%v recover=%v", catalog.applied, catalog.recovered)
	}
	if got := catalog.applied[0]; got.Request != op.Request || got.Previous != op.Previous {
		t.Fatalf("checkpoint used another operation: got=%+v want=%+v", got, op)
	}
	if catalog.sources[0] != testFactoryID {
		t.Fatalf("checkpoint source captured after Stop: got=%q want=%s", catalog.sources[0], testFactoryID)
	}
	// A lost completion reply retries the receipt, not the checkpoint.
	if err := engine.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(catalog.applied) != 1 || control.starts != 2 {
		t.Fatal("completed activation replayed catalog or process changes")
	}
}

func TestCatalogTransitionFailureRestoresPriorBeforeRecoveryStarts(t *testing.T) {
	for _, failure := range []string{"before checkpoint", "after checkpoint", "candidate start"} {
		t.Run(failure, func(t *testing.T) {
			r, engine, control, catalog, admission, events := setupCatalogTransition(t)
			cause := errors.New(failure)
			switch failure {
			case "before checkpoint":
				catalog.failBeforeSave, catalog.failApply = true, cause
			case "after checkpoint":
				catalog.failApply = cause
			case "candidate start":
				control.candidateFail = cause
			}
			if err := Queue(r, req()); err != nil {
				t.Fatal(err)
			}
			op := catalogTransitionOperation(t, r)
			err := engine.Run(context.Background())
			if failure == "before checkpoint" {
				if err == nil {
					t.Fatal("missing durable checkpoint allowed recovery")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			want := []string{"stop", "apply"}
			if failure == "candidate start" {
				want = append(want, "start candidate")
			}
			want = append(want, "stop", "recover")
			if failure != "before checkpoint" {
				want = append(want, "start prior")
			}
			requireCatalogTransitionEvents(t, events, want...)
			if len(catalog.recovered) != 1 || catalog.recovered[0].Request != op.Request || catalog.recovered[0].Previous != op.Previous {
				t.Fatalf("recovery did not use the failing operation: %+v", catalog.recovered)
			}
			if catalog.recoveryIDs[0] != testFactoryID {
				t.Fatalf("recovery did not use resolved prior: got=%s want=%s", catalog.recoveryIDs[0], testFactoryID)
			}
			state, err := r.Read()
			if err != nil {
				t.Fatal(err)
			}
			if failure == "before checkpoint" {
				if catalog.live != "prior entries, firmware and settings" || state.Selected != testFactoryID || state.Generation != 0 || state.Pending == nil || state.Pending.Phase != "blocked" || control.starts != 0 || admission.releases != 0 {
					t.Fatalf("missing checkpoint changed data or reopened admission: catalog=%+v state=%+v starts=%d releases=%d", catalog, state, control.starts, admission.releases)
				}
				return
			}
			if catalog.live != "prior entries, firmware and settings" || state.Selected != testFactoryID || state.Generation != 0 || state.Pending != nil || state.Receipts[op.ID].Result != "recovered" || admission.releases != 1 {
				t.Fatalf("prior catalog/software recovery incomplete: catalog=%+v state=%+v releases=%d", catalog, state, admission.releases)
			}
		})
	}
}

func TestCatalogTransitionRequiresHostReadinessBeforeAcceptingCandidate(t *testing.T) {
	for _, hostRecovers := range []bool{false, true} {
		t.Run(map[bool]string{false: "host never serves", true: "host becomes ready"}[hostRecovers], func(t *testing.T) {
			r, engine, control, catalog, admission, events := setupCatalogTransition(t)
			start := engine.Now()
			var readyAt time.Time
			var candidateProbes, priorProbes int
			engine.ActivationReady = func(_ context.Context, id string) error {
				if control.Active() != id {
					t.Fatalf("host readiness checked before its software starts: active=%s probe=%s", control.Active(), id)
				}
				if id == testFactoryID {
					priorProbes++
					return nil
				}
				candidateProbes++
				if !hostRecovers || engine.Now().Sub(start) < 500*time.Millisecond {
					return errors.New("host is not serving its catalog")
				}
				if readyAt.IsZero() {
					readyAt = engine.Now()
				}
				return nil
			}
			if err := Queue(r, req()); err != nil {
				t.Fatal(err)
			}
			if err := engine.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			state, err := r.Read()
			if err != nil {
				t.Fatal(err)
			}
			if candidateProbes < 2 || state.Pending != nil || admission.releases != 1 {
				t.Fatalf("host readiness did not gate activation: candidate probes=%d state=%+v releases=%d", candidateProbes, state, admission.releases)
			}
			if hostRecovers {
				requireCatalogTransitionEvents(t, events, "stop", "apply", "start candidate")
				if state.Selected != testCandidateID || state.Receipts[req().ID].Result != "accepted" || priorProbes != 0 || engine.Now().Sub(readyAt) < engine.Stable {
					t.Fatalf("host readiness skipped the continuous stability window: state=%+v ready=%s now=%s prior probes=%d", state, readyAt, engine.Now(), priorProbes)
				}
			} else {
				requireCatalogTransitionEvents(t, events, "stop", "apply", "start candidate", "stop", "recover", "start prior")
				if state.Selected != testFactoryID || state.Receipts[req().ID].Result != "recovered" || len(catalog.recovered) != 1 || catalog.recovered[0].Request != req() || priorProbes == 0 {
					t.Fatalf("non-serving candidate accepted or matching recovery skipped: state=%+v recover=%+v prior probes=%d", state, catalog.recovered, priorProbes)
				}
			}
		})
	}
}

func TestCatalogTransitionStopFailureDoesNotTouchCatalog(t *testing.T) {
	r, engine, control, catalog, admission, events := setupCatalogTransition(t)
	control.stopErr = true
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background()); err == nil {
		t.Fatal("unquiesced services accepted")
	}
	requireCatalogTransitionEvents(t, events, "stop", "stop")
	if len(catalog.applied) != 0 || len(catalog.recovered) != 0 || control.starts != 0 || admission.releases != 0 {
		t.Fatalf("catalog changed before quiescence: apply=%v recover=%v starts=%d releases=%d", catalog.applied, catalog.recovered, control.starts, admission.releases)
	}
}

func TestCatalogTransitionFailedRestoreKeepsPriorStoppedAndReservationHeld(t *testing.T) {
	for _, failure := range []string{"restore unavailable", "foreign operation"} {
		t.Run(failure, func(t *testing.T) {
			r, engine, control, catalog, admission, events := setupCatalogTransition(t)
			control.candidateFail = errors.New("candidate failed after migration")
			catalog.failRecover = errors.New(failure)
			if failure == "foreign operation" {
				catalog.failRecover = ErrConflict
			}
			if err := Queue(r, req()); err != nil {
				t.Fatal(err)
			}
			if err := engine.Run(context.Background()); err == nil {
				t.Fatal("failed catalog restoration was accepted")
			}
			requireCatalogTransitionEvents(t, events, "stop", "apply", "start candidate", "stop", "recover")
			state, err := r.Read()
			if err != nil {
				t.Fatal(err)
			}
			if state.Pending == nil || state.Pending.Phase != "blocked" || state.Pending.ID != req().ID || state.Selected != testFactoryID || state.Generation != 0 || control.starts != 1 || admission.releases != 0 {
				t.Fatalf("failed restore lost recovery fence: state=%+v starts=%d releases=%d", state, control.starts, admission.releases)
			}
			// A spent recovery may not retry a restore, even if its storage
			// error later disappears.
			catalog.failRecover = nil
			if err := engine.Run(context.Background()); err == nil || len(catalog.recovered) != 1 || control.starts != 1 {
				t.Fatal("blocked catalog recovery retried")
			}
		})
	}
}

func TestCatalogTransitionInterruptedApplyingBootRestoresExactOperation(t *testing.T) {
	r, engine, _, catalog, admission, events := setupCatalogTransition(t)
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	op := catalogTransitionOperation(t, r)
	if err := catalog.Apply(context.Background(), op, testFactoryID); err != nil {
		t.Fatal(err)
	}
	*events = nil
	if err := transition(r, op, "queued", "applying", ""); err != nil {
		t.Fatal(err)
	}
	unrelatedBoot := Request{ID: testSelectedID, Candidate: testFactoryID, Requester: "new boot", Generation: 0}
	if err := engine.Boot(context.Background(), unrelatedBoot); err != nil {
		t.Fatal(err)
	}
	requireCatalogTransitionEvents(t, events, "stop", "recover", "start prior")
	if len(catalog.recovered) != 1 || catalog.recovered[0].Request != op.Request || catalog.recovered[0].Previous != op.Previous {
		t.Fatalf("boot restored another operation: %+v", catalog.recovered)
	}
	if catalog.recoveryIDs[0] != testFactoryID {
		t.Fatalf("boot restored catalog for wrong release: got=%s want=%s", catalog.recoveryIDs[0], testFactoryID)
	}
	state, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Pending != nil || state.Receipts[op.ID].Result != "recovered" || admission.releases != 1 {
		t.Fatalf("interrupted activation did not finish its own recovery: %+v releases=%d", state, admission.releases)
	}
}

func TestCatalogTransitionInterruptedBootRejectsForeignCheckpoint(t *testing.T) {
	r, engine, control, catalog, admission, events := setupCatalogTransition(t)
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	op := catalogTransitionOperation(t, r)
	foreign := op.Request
	foreign.ID = testSelectedID
	catalog.owner, catalog.checkpoint = foreign, "another operation's data"
	catalog.live = "candidate-format: keep this data"
	if err := transition(r, op, "queued", "applying", ""); err != nil {
		t.Fatal(err)
	}
	if err := engine.Boot(context.Background(), Request{}); err == nil {
		t.Fatal("foreign catalog checkpoint accepted")
	}
	requireCatalogTransitionEvents(t, events, "stop", "recover")
	state, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.live != "candidate-format: keep this data" || control.starts != 0 || state.Pending == nil || state.Pending.Phase != "blocked" || admission.releases != 0 {
		t.Fatalf("foreign checkpoint overwrote data or opened admission: catalog=%q starts=%d state=%+v releases=%d", catalog.live, control.starts, state, admission.releases)
	}
}

func TestCatalogTransitionBindsHeldBackFactoryAsActualSource(t *testing.T) {
	r, engine, control, catalog, admission, events := setupCatalogTransition(t)
	// The durable selection belongs to another base, while this image is
	// running its compatible factory. Its live catalog therefore belongs to
	// the factory, not to the held-back selection's bundle identity.
	selectRelease(t, r, testSelectedID)
	engine.Factory = testFactoryID
	control.incompatible = map[string]bool{testSelectedID: true}
	control.candidateFail = errors.New("candidate cannot start")
	request := req()
	request.Generation = 1
	if err := Queue(r, request); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireCatalogTransitionEvents(t, events, "stop", "apply", "start candidate", "stop", "recover", "start prior")
	if len(catalog.applied) != 1 || len(catalog.sources) != 1 || catalog.sources[0] != testFactoryID || catalog.applied[0].Previous != testSelectedID || catalog.applied[0].Request != request {
		t.Fatalf("live factory was mislabeled as held-back selection: sources=%v apply=%+v", catalog.sources, catalog.applied)
	}
	if len(catalog.recovered) != 1 || catalog.recovered[0].Previous != testSelectedID || catalog.recovered[0].Request != request {
		t.Fatalf("recovery rewrote the durable operation identity: %+v", catalog.recovered)
	}
	if catalog.recoveryIDs[0] != testFactoryID {
		t.Fatalf("held-back recovery restored for selected ID rather than running factory: target=%s selected=%s factory=%s", catalog.recoveryIDs[0], testSelectedID, testFactoryID)
	}
	state, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Selected != testSelectedID || state.Generation != 1 || state.Receipts[request.ID].Result != "recovered" || control.Active() != testFactoryID || admission.releases != 1 {
		t.Fatalf("factory recovery changed held-back selection: state=%+v active=%s releases=%d", state, control.Active(), admission.releases)
	}
}

func TestCatalogTransitionRestoringSelectionFromFactoryIsNotOrdinaryRestart(t *testing.T) {
	r, engine, control, catalog, _, events := setupCatalogTransition(t)
	selectRelease(t, r, testSelectedID)
	// After an image trial the image's factory can still be running while
	// the supervisor queues the unchanged durable selection for restoration.
	// Candidate == Previous does not mean that candidate is currently live.
	control.candidateID = testSelectedID
	request := req()
	request.Candidate, request.Generation = testSelectedID, 1
	if err := Queue(r, request); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	requireCatalogTransitionEvents(t, events, "stop", "apply", "start candidate")
	if len(catalog.applied) != 1 || catalog.applied[0].Candidate != catalog.applied[0].Previous || len(catalog.sources) != 1 || catalog.sources[0] != testFactoryID {
		t.Fatalf("factory-to-selection transition skipped its actual source: apply=%+v sources=%v", catalog.applied, catalog.sources)
	}
}

func TestCatalogTransitionDoesNotRunForOrdinaryStartupOrSameBundle(t *testing.T) {
	for _, mode := range []string{"boot", "queued boot", "factory", "same bundle activation", "ordinary restart"} {
		t.Run(mode, func(t *testing.T) {
			r, engine, _, catalog, _, _ := setupCatalogTransition(t)
			var err error
			switch mode {
			case "boot":
				err = engine.Boot(context.Background(), req())
			case "queued boot":
				if err = Queue(r, req()); err == nil {
					err = engine.Boot(context.Background(), Request{})
				}
			case "factory":
				err = engine.StartFactory(context.Background(), testFactoryID)
			case "same bundle activation":
				same := req()
				same.Candidate = testFactoryID
				if err = Queue(r, same); err == nil {
					err = engine.Run(context.Background())
				}
			case "ordinary restart":
				engine.defaults()
				err = engine.switchPrepared(context.Background(), testFactoryID, Request{})
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.applied) != 0 || len(catalog.recovered) != 0 || catalog.live != "prior entries, firmware and settings" {
				t.Fatalf("ordinary startup replayed a catalog transition: %+v", catalog)
			}
		})
	}
}
