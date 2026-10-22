package kitsoftware

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance/software"
)

const testFactoryID = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
const testCandidateID = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

type fakeControl struct {
	starts, stops int
	active        string
	fail          map[string]bool
	stopErr       bool
	// incompatible names releases this base cannot run.
	incompatible map[string]bool
}

func (f *fakeControl) Verify(_ context.Context, id string) error {
	if f.incompatible[id] {
		return fmt.Errorf("%w: release does not accept required interface menu.fes.video.menu-blitter 1.0", software.ErrIncompatible)
	}
	return nil
}
func (f *fakeControl) Active() string { return f.active }
func (f *fakeControl) Stop(context.Context) error {
	f.stops++
	if f.stopErr {
		return errors.New("stuck process")
	}
	return nil
}
func (f *fakeControl) Start(_ context.Context, id string, _ Request) error {
	f.starts++
	f.active = id
	return nil
}
func (f *fakeControl) Healthy(_ context.Context, id string) error {
	if f.fail[id] {
		return errors.New("not ready")
	}
	return nil
}

type fakeAdmission struct {
	busy, failRelease  bool
	acquires, releases int
}

func (f *fakeAdmission) Acquire(context.Context, Request) error {
	f.acquires++
	if f.busy {
		return ErrBusy
	}
	return nil
}
func (f *fakeAdmission) Release(context.Context, Request) error {
	f.releases++
	if f.failRelease {
		return errors.New("agent down")
	}
	return nil
}
func setup(t *testing.T) (*FileRepository, *Engine, *fakeControl, *fakeAdmission) {
	t.Helper()
	r, err := OpenRepository(filepath.Join(t.TempDir(), "state"), testFactoryID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	c := &fakeControl{fail: map[string]bool{}}
	a := &fakeAdmission{}
	now := time.Unix(100, 0)
	e := &Engine{Repository: r, Controller: c, Admission: a, Deadline: time.Second, Stable: 200 * time.Millisecond, Poll: 100 * time.Millisecond, Now: func() time.Time { return now }, Wait: func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }}
	return r, e, c, a
}
func req() Request {
	return Request{ID: strings.Repeat("a", 64), Candidate: testCandidateID, Requester: "host1", Generation: 0}
}
func TestQueuePersistenceAndCAS(t *testing.T) {
	r, _, _, _ := setup(t)
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	r2, err := OpenRepository(r.root, strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if err := Queue(r2, req()); err != nil {
		t.Fatal(err)
	}
	other := req()
	other.ID = strings.Repeat("b", 64)
	if !errors.Is(Queue(r2, other), ErrConflict) {
		t.Fatal("competing request accepted")
	}
	if !errors.Is(Cancel(r2, strings.Repeat("a", 64), 1), ErrConflict) {
		t.Fatal("stale cancellation accepted")
	}
	if err := Cancel(r2, strings.Repeat("a", 64), 0); err != nil {
		t.Fatal(err)
	}
	if err := Queue(r2, req()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if s.Pending != nil || s.Receipts[strings.Repeat("a", 64)].Result != "cancelled" {
		t.Fatal(s)
	}
}
func TestBusyDoesNotStopAndSuccessPersists(t *testing.T) {
	r, e, c, a := setup(t)
	Queue(r, req())
	a.busy = true
	if !errors.Is(e.Run(context.Background()), ErrBusy) || c.stops != 0 {
		t.Fatal("busy activation touched services")
	}
	a.busy = false
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if s.Selected != testCandidateID || s.Previous != testFactoryID || s.Generation != 1 || s.Pending != nil || a.releases != 1 {
		t.Fatal(s)
	}
	if err := Queue(r, req()); err != nil {
		t.Fatal("lost reply not idempotent", err)
	}
}
func TestFailedCandidateRecoversOnlyOnce(t *testing.T) {
	r, e, c, _ := setup(t)
	Queue(r, req())
	c.fail[testCandidateID] = true
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if s.Selected != testFactoryID || s.Receipts[strings.Repeat("a", 64)].Result != "recovered" || c.starts != 2 {
		t.Fatal(s, c)
	}
	e.Run(context.Background())
	if c.starts != 2 {
		t.Fatal("candidate retried")
	}
}
func TestInterruptedCandidateAndSpentRecovery(t *testing.T) {
	for _, phase := range []string{"applying", "recovering"} {
		t.Run(phase, func(t *testing.T) {
			r, e, c, _ := setup(t)
			Queue(r, req())
			r.Update(func(s *State) error { s.Pending.Phase = phase; return nil })
			err := e.Run(context.Background())
			s, _ := r.Read()
			if phase == "applying" {
				if err != nil || c.active != testFactoryID || c.starts != 1 {
					t.Fatal(err, c)
				}
			} else {
				if err == nil || c.starts != 0 || s.Pending.Phase != "blocked" {
					t.Fatal(err, c, s)
				}
			}
		})
	}
}
func TestStuckOldProcessNeverStartsReplacement(t *testing.T) {
	r, e, c, _ := setup(t)
	Queue(r, req())
	c.stopErr = true
	if e.Run(context.Background()) == nil {
		t.Fatal("stuck old processes accepted")
	}
	s, _ := r.Read()
	if c.starts != 0 || s.Pending.Phase != "blocked" {
		t.Fatal(c, s)
	}
}
func TestReleaseFailureResumesAfterDurableAcceptance(t *testing.T) {
	r, e, c, a := setup(t)
	Queue(r, req())
	a.failRelease = true
	if e.Run(context.Background()) == nil {
		t.Fatal("release error lost")
	}
	s, _ := r.Read()
	if s.Selected != testCandidateID || s.Unreleased == nil {
		t.Fatal(s)
	}
	a.failRelease = false
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ = r.Read()
	if s.Unreleased != nil || c.starts != 1 {
		t.Fatal(s, c)
	}
}
func TestCorruptStateFailsClosed(t *testing.T) {
	r, _, _, _ := setup(t)
	if err := r.Update(func(s *State) error { s.Selected = ""; return nil }); err == nil {
		t.Fatal("invalid state persisted")
	}
}
func TestConcurrentWorkerRejected(t *testing.T) {
	r, e, _, _ := setup(t)
	unlock, err := r.WorkerLock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if !errors.Is(e.Run(context.Background()), ErrBusy) {
		t.Fatal("concurrent worker admitted")
	}
}

type cancellingControl struct {
	*fakeControl
	cancel context.CancelFunc
}

func (c *cancellingControl) Start(ctx context.Context, id string, r Request) error {
	err := c.fakeControl.Start(ctx, id, r)
	c.cancel()
	return err
}
func TestShutdownDoesNotSpendRecoveryWithCancelledContext(t *testing.T) {
	r, e, c, _ := setup(t)
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.Controller = &cancellingControl{fakeControl: c, cancel: cancel}
	if !errors.Is(e.Run(ctx), context.Canceled) {
		t.Fatal("shutdown cancellation lost")
	}
	s, _ := r.Read()
	if s.Pending == nil || s.Pending.Phase != "applying" || c.starts != 1 {
		t.Fatal("shutdown spent recovery", s, c)
	}
}

type exitingControl struct {
	*fakeControl
	remaining int
}

func (c *exitingControl) Healthy(context.Context, string) error {
	if c.remaining > 0 {
		c.remaining--
		return fmt.Errorf("agent exited: %w", ErrProcessExited)
	}
	return nil
}

func TestStartupRestartsExitedProcessBeforeAcceptance(t *testing.T) {
	_, e, c, _ := setup(t)
	control := &exitingControl{fakeControl: c, remaining: 1}
	e.Controller = control
	if err := e.Boot(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if control.starts != 2 {
		t.Fatal(control.starts)
	}
}

// nativeInitExitControl reports the runtime dying inside Start, which is how
// waitNativeInitialization surfaces an exit before the protocol socket answers.
type nativeInitExitControl struct {
	*fakeControl
	exits   int
	now     func() time.Time
	at      []time.Time
	order   []string
	other   error
	healths int
}

func (c *nativeInitExitControl) Stop(ctx context.Context) error {
	c.order = append(c.order, "stop")
	return c.fakeControl.Stop(ctx)
}
func (c *nativeInitExitControl) Start(_ context.Context, id string, _ Request) error {
	c.order = append(c.order, "start")
	c.at = append(c.at, c.now())
	c.starts++
	c.active = id
	if c.other != nil {
		return c.other
	}
	if c.exits != 0 {
		if c.exits > 0 {
			c.exits--
		}
		return fmt.Errorf("runtime exited during native initialization: %w", ErrProcessExited)
	}
	return nil
}
func (c *nativeInitExitControl) Healthy(ctx context.Context, id string) error {
	c.healths++
	return c.fakeControl.Healthy(ctx, id)
}

func TestNativeInitializationExitRetriesWithinStartupDeadline(t *testing.T) {
	t.Run("once", func(t *testing.T) {
		_, e, c, _ := setup(t)
		control := &nativeInitExitControl{fakeControl: c, exits: 1, now: e.Now}
		e.Controller = control
		if err := e.StartFactory(context.Background(), testFactoryID); err != nil {
			t.Fatal(err)
		}
		if control.starts != 2 || control.healths == 0 {
			t.Fatalf("starts=%d healths=%d", control.starts, control.healths)
		}
		if strings.Join(control.order, ",") != "stop,start,stop,start" {
			t.Fatal(control.order)
		}
		if control.at[1].Sub(control.at[0]) >= e.Deadline {
			t.Fatal("retry reset startup deadline")
		}
	})
	t.Run("deadline", func(t *testing.T) {
		_, e, c, _ := setup(t)
		control := &nativeInitExitControl{fakeControl: c, exits: -1, now: e.Now}
		e.Controller = control
		err := e.StartFactory(context.Background(), testFactoryID)
		if err == nil || !errors.Is(err, ErrProcessExited) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "software readiness deadline exceeded") {
			t.Fatal(err)
		}
		if control.starts < 2 || control.healths != 0 || control.stops != control.starts+1 {
			t.Fatalf("starts=%d stops=%d healths=%d", control.starts, control.stops, control.healths)
		}
		for _, at := range control.at {
			if !at.Before(control.at[0].Add(e.Deadline)) {
				t.Fatal("retry reset startup deadline", control.at)
			}
		}
	})
	t.Run("other", func(t *testing.T) {
		_, e, c, _ := setup(t)
		sentinel := errors.New("menu selection missing")
		control := &nativeInitExitControl{fakeControl: c, other: sentinel, now: e.Now}
		e.Controller = control
		err := e.StartFactory(context.Background(), testFactoryID)
		if err != sentinel || control.starts != 1 || control.healths != 0 || strings.Join(control.order, ",") != "stop,start" {
			t.Fatal(err, control.starts, control.healths, control.order)
		}
	})
}

type slowVerifiedController struct {
	fakeControl
	t                 *testing.T
	now               *time.Time
	verificationCount map[string]int
	startsAt          map[string][]time.Time
	neverReady        bool
}

func (c *slowVerifiedController) Verify(ctx context.Context, id string) error {
	if _, ok := ctx.Deadline(); ok {
		c.t.Fatal("verification shares startup deadline")
	}
	c.verificationCount[id]++
	*c.now = c.now.Add(2 * time.Minute)
	return nil
}
func (c *slowVerifiedController) Start(ctx context.Context, id string, r Request) error {
	if _, ok := ctx.Deadline(); !ok {
		c.t.Fatal("startup has no deadline")
	}
	c.startsAt[id] = append(c.startsAt[id], *c.now)
	*c.now = c.now.Add(15 * time.Second)
	return c.fakeControl.Start(ctx, id, r)
}
func (c *slowVerifiedController) Healthy(ctx context.Context, id string) error {
	if id == testCandidateID || c.neverReady {
		return ErrProcessExited
	}
	return nil
}
func TestSlowPreparationDoesNotStarveSingleRecoveryReadiness(t *testing.T) {
	for _, failRecovery := range []bool{false, true} {
		t.Run(fmt.Sprint(failRecovery), func(t *testing.T) {
			r, e, _, a := setup(t)
			now := time.Unix(100, 0)
			c := &slowVerifiedController{t: t, now: &now, verificationCount: map[string]int{}, startsAt: map[string][]time.Time{}, neverReady: failRecovery}
			e.Controller = c
			e.Deadline = time.Minute
			e.Stable = 10 * time.Second
			e.Poll = time.Second
			e.Now = func() time.Time { return now }
			e.Wait = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
			if err := Queue(r, req()); err != nil {
				t.Fatal(err)
			}
			err := e.Run(context.Background())
			if (err != nil) != failRecovery {
				t.Fatalf("failure=%v: %v", failRecovery, err)
			}
			state, _ := r.Read()
			if state.Selected != testFactoryID || state.Generation != 0 {
				t.Fatal("selection changed", state)
			}
			for _, id := range []string{testCandidateID, testFactoryID} {
				if c.verificationCount[id] != 1 {
					t.Fatalf("%s verified %d times", id, c.verificationCount[id])
				}
				starts := c.startsAt[id]
				for _, started := range starts {
					if started.Sub(starts[0]) >= time.Minute {
						t.Fatal("retry reset startup deadline")
					}
				}
			}
			if failRecovery {
				if state.Pending == nil || state.Pending.Phase != "blocked" || a.releases != 0 {
					t.Fatal(state, a)
				}
			} else if state.Pending != nil || state.Receipts[req().ID].Result != "recovered" || a.releases != 1 {
				t.Fatal(state, a)
			}
			starts := c.starts
			_ = e.Run(context.Background())
			if c.starts != starts {
				t.Fatal("spent recovery retried")
			}
		})
	}
}

func TestFactoryPreparationUsesSharedStartupDeadlineWithoutAdmission(t *testing.T) {
	for _, neverReady := range []bool{false, true} {
		t.Run(fmt.Sprint(neverReady), func(t *testing.T) {
			now := time.Unix(100, 0)
			c := &slowVerifiedController{t: t, now: &now, verificationCount: map[string]int{}, startsAt: map[string][]time.Time{}, neverReady: neverReady}
			e := &Engine{Controller: c, Deadline: time.Minute, Stable: 10 * time.Second, Poll: time.Second, Now: func() time.Time { return now }, Wait: func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }}
			err := e.StartFactory(context.Background(), testFactoryID)
			if (err != nil) != neverReady {
				t.Fatalf("neverReady=%v: %v", neverReady, err)
			}
			if c.verificationCount[testFactoryID] != 1 {
				t.Fatal("factory retries reverified")
			}
			starts := c.startsAt[testFactoryID]
			for _, start := range starts {
				if start.Sub(starts[0]) >= time.Minute {
					t.Fatal("factory retry reset deadline")
				}
			}
		})
	}
}

const testSelectedID = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

// selectRelease commits id as the selection, as an earlier image accepted it.
func selectRelease(t *testing.T, r *FileRepository, id string) {
	t.Helper()
	if err := r.Update(func(s *State) error { s.Previous, s.Selected = s.Selected, id; s.Generation++; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestBootHoldsBackSelectionThisImageCannotRun(t *testing.T) {
	r, e, c, a := setup(t)
	selectRelease(t, r, testSelectedID)
	c.incompatible = map[string]bool{testSelectedID: true}
	if err := e.Boot(context.Background(), req()); err == nil || !errors.Is(err, software.ErrIncompatible) {
		t.Fatal("without a factory the incompatible selection must keep its error", err)
	}
	if c.starts != 0 {
		t.Fatal("incompatible selection started", c)
	}
	e.Factory = testFactoryID
	if err := e.Boot(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if c.active != testFactoryID || c.starts != 1 || s.Selected != testSelectedID || a.releases != 1 {
		t.Fatal("factory software did not replace the held-back selection", c, s, a)
	}
	if e.HeldBack != testSelectedID || !strings.Contains(e.HeldBackReason, "menu.fes.video.menu-blitter") {
		t.Fatal("held-back selection not reported", e.HeldBack, e.HeldBackReason)
	}
	c.incompatible = nil
	if err := e.Boot(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	if c.active != testSelectedID || e.HeldBack != "" || e.HeldBackReason != "" {
		t.Fatal("a runnable selection stayed held back", c, e.HeldBack)
	}
}

func TestQueuedCandidateThisImageCannotRunIsHeldBack(t *testing.T) {
	r, e, c, a := setup(t)
	e.Factory = testFactoryID
	if err := e.Boot(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	starts, stops, releases := c.starts, c.stops, a.releases
	c.incompatible = map[string]bool{testCandidateID: true}
	if err := Queue(r, req()); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	receipt := s.Receipts[req().ID]
	if receipt.Result != "recovered" || !strings.HasPrefix(receipt.Reason, "held back: ") || !strings.Contains(receipt.Reason, "menu.fes.video.menu-blitter 1.0") {
		t.Fatal("held-back candidate not recorded", receipt)
	}
	if s.Pending != nil || s.Unreleased != nil || s.Selected != testFactoryID || s.Generation != 0 {
		t.Fatal("held-back candidate changed the selection or kept the request", s)
	}
	if c.starts != starts || c.stops != stops || a.releases != releases+1 {
		t.Fatal("held-back candidate touched services or kept its reservation", c, a)
	}
	if e.HeldBack != "" {
		t.Fatal("a refused candidate is not a held-back selection", e.HeldBack)
	}
}

func TestRestoreAfterImageChangeKeepsFactoryForHeldBackSelection(t *testing.T) {
	r, e, c, a := setup(t)
	e.Factory = testFactoryID
	selectRelease(t, r, testSelectedID)
	// The trial boot ran the factory software. Confirmation queues the
	// committed selection again, which the new image cannot run.
	c.active = testFactoryID
	c.incompatible = map[string]bool{testSelectedID: true}
	state, _ := r.Read()
	restore := Request{ID: strings.Repeat("b", 64), Candidate: testSelectedID, Requester: "kit-software-supervisor", Generation: state.Generation}
	if err := Queue(r, restore); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if c.starts != 0 || c.stops != 0 || c.active != testFactoryID || s.Selected != testSelectedID || a.releases != 1 {
		t.Fatal("restore of a held-back selection replaced the factory software", c, s, a)
	}
	if s.Receipts[restore.ID].Result != "recovered" || e.HeldBack != testSelectedID {
		t.Fatal("held-back restore not recorded", s.Receipts[restore.ID], e.HeldBack)
	}
}

func TestHeldBackCandidateAfterImageChangeStartsRunnableSelection(t *testing.T) {
	r, e, c, a := setup(t)
	e.Factory = testFactoryID
	selectRelease(t, r, testSelectedID)
	c.active = testFactoryID
	c.incompatible = map[string]bool{testCandidateID: true}
	state, _ := r.Read()
	queued := req()
	queued.Generation = state.Generation
	if err := Queue(r, queued); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if c.active != testSelectedID || c.starts != 1 || s.Selected != testSelectedID || a.releases != 1 {
		t.Fatal("the runnable selection did not replace the factory software", c, s, a)
	}
	if receipt := s.Receipts[queued.ID]; receipt.Result != "recovered" || !strings.HasPrefix(receipt.Reason, "held back: ") {
		t.Fatal("held-back candidate not recorded", receipt)
	}
}

func TestRecoveryRunsFactoryWhenSelectionIsHeldBack(t *testing.T) {
	r, e, c, a := setup(t)
	e.Factory = testFactoryID
	selectRelease(t, r, testSelectedID)
	c.incompatible = map[string]bool{testSelectedID: true}
	if err := e.Boot(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	state, _ := r.Read()
	queued := req()
	queued.Generation = state.Generation
	if err := Queue(r, queued); err != nil {
		t.Fatal(err)
	}
	c.fail[testCandidateID] = true
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if c.active != testFactoryID || s.Selected != testSelectedID || s.Receipts[queued.ID].Result != "recovered" || a.releases != 2 {
		t.Fatal("failed candidate did not recover to the factory software", c, s, a)
	}
	if e.HeldBack != testSelectedID {
		t.Fatal("held-back selection not reported after recovery", e.HeldBack)
	}
}

func TestAcceptedCandidateEndsHoldBack(t *testing.T) {
	r, e, c, _ := setup(t)
	e.Factory = testFactoryID
	selectRelease(t, r, testSelectedID)
	c.incompatible = map[string]bool{testSelectedID: true}
	if err := e.Boot(context.Background(), req()); err != nil {
		t.Fatal(err)
	}
	state, _ := r.Read()
	queued := req()
	queued.Generation = state.Generation
	if err := Queue(r, queued); err != nil {
		t.Fatal(err)
	}
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ := r.Read()
	if c.active != testCandidateID || s.Selected != testCandidateID || e.HeldBack != "" || e.HeldBackReason != "" {
		t.Fatal("accepted candidate left the old selection held back", c, s, e.HeldBack)
	}
}

func TestHeldBackCandidateInterruptedWhileSelectionStartsStaysQueued(t *testing.T) {
	r, e, c, a := setup(t)
	e.Factory = testFactoryID
	selectRelease(t, r, testSelectedID)
	c.active = testFactoryID
	c.incompatible = map[string]bool{testCandidateID: true}
	state, _ := r.Read()
	queued := req()
	queued.Generation = state.Generation
	if err := Queue(r, queued); err != nil {
		t.Fatal(err)
	}
	// Shutdown arrives while the selection replaces the factory software.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.Controller = &cancellingControl{fakeControl: c, cancel: cancel}
	if !errors.Is(e.Run(ctx), context.Canceled) {
		t.Fatal("shutdown cancellation lost")
	}
	s, _ := r.Read()
	if s.Pending == nil || s.Pending.Phase != "queued" {
		t.Fatal("an interrupted hold-back spent recovery", s.Pending)
	}
	// The next boot starts the selection, as it would anyway, and the request
	// then completes without touching services again.
	e.Controller = c
	if err := e.Boot(context.Background(), Request{ID: strings.Repeat("9", 64), Candidate: testFactoryID, Requester: "kit-software-supervisor"}); err != nil {
		t.Fatal(err)
	}
	if c.active != testSelectedID {
		t.Fatal("boot did not start the selection", c)
	}
	starts := c.starts
	if err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, _ = r.Read()
	if s.Pending != nil || s.Receipts[queued.ID].Result != "recovered" || c.starts != starts || c.active != testSelectedID || a.releases == 0 {
		t.Fatal("held-back request did not complete after the reboot", s, c, a)
	}
}

func TestHeldBackCandidateBlocksOnlyWhenSelectionFailsToStart(t *testing.T) {
	r, e, c, _ := setup(t)
	e.Factory = testFactoryID
	selectRelease(t, r, testSelectedID)
	c.active = testFactoryID
	c.incompatible = map[string]bool{testCandidateID: true}
	c.fail[testSelectedID] = true
	state, _ := r.Read()
	queued := req()
	queued.Generation = state.Generation
	if err := Queue(r, queued); err != nil {
		t.Fatal(err)
	}
	if e.Run(context.Background()) == nil {
		t.Fatal("a selection that never became ready was accepted")
	}
	s, _ := r.Read()
	if s.Pending == nil || s.Pending.Phase != "blocked" || !strings.HasPrefix(s.Pending.Reason, "held back: ") || !strings.Contains(s.Pending.Reason, "starting the selection failed") {
		t.Fatal("failed selection start not recorded", s.Pending)
	}
}
