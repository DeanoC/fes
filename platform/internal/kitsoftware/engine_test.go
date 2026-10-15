package kitsoftware

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testFactoryID = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
const testCandidateID = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

type fakeControl struct {
	starts, stops int
	active        string
	fail          map[string]bool
	stopErr       bool
}

func (f *fakeControl) Verify(context.Context, string) error { return nil }
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
