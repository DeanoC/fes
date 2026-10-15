package kitsoftware

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// trialLease models the admission boundary used during trial reconciliation.
// A claim succeeds only when the boot reservation has ended and no successor
// queue is installed.
type trialLease struct {
	mu     sync.Mutex
	held   bool
	queued bool
}

func (l *trialLease) Claim() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held || l.queued {
		return false
	}
	return true
}
func (l *trialLease) Install() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.held {
		return errors.New("queue installed after the boot reservation ended")
	}
	l.queued = true
	return nil
}
func (l *trialLease) Release() error {
	l.mu.Lock()
	l.held = false
	l.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	return nil
}

func TestHandoffTrialBlocksSimultaneousClaim(t *testing.T) {
	factory := strings.Repeat("a", 64)
	repo, err := OpenRepository(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	pending := Request{ID: strings.Repeat("b", 64), Candidate: strings.Repeat("c", 64), Requester: "host", Generation: 0}
	if err := Queue(repo, pending); err != nil {
		t.Fatal(err)
	}
	lease := &trialLease{held: true}
	var claimed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if lease.Claim() {
				claimed.Add(1)
			}
		}()
	}
	var installed Request
	restore := Request{ID: strings.Repeat("d", 64), Requester: "kit-software-supervisor"}
	close(start)
	err = HandoffTrial(context.Background(), repo, restore, nil, func(_ context.Context, request Request) error {
		installed = request
		return lease.Install()
	}, nil, nil, func(context.Context) error {
		return lease.Release()
	})
	wg.Wait()
	if err != nil || claimed.Load() != 0 || !lease.queued || lease.held {
		t.Fatalf("err=%v claimed=%d queued=%v held=%v", err, claimed.Load(), lease.queued, lease.held)
	}
	if installed.ID != pending.ID || installed.Candidate != pending.Candidate {
		t.Fatal("handoff installed a different request", installed)
	}
}

func TestHandoffTrialKeepsBootReservationWhenInstallFails(t *testing.T) {
	factory := strings.Repeat("a", 64)
	repo, err := OpenRepository(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease := &trialLease{held: true}
	released := false
	restore := Request{ID: strings.Repeat("d", 64), Requester: "kit-software-supervisor"}
	err = HandoffTrial(context.Background(), repo, restore, nil, func(context.Context, Request) error {
		return errors.New("agent queue unavailable")
	}, nil, nil, func(context.Context) error {
		released = true
		return lease.Release()
	})
	if err == nil || released || !lease.held {
		t.Fatal(err, released, lease.held)
	}
	state, err := repo.Read()
	if err != nil || state.Pending == nil || state.Pending.ID != restore.ID || state.Pending.Candidate != factory {
		t.Fatal("saved selection was not queued before the failed install", state, err)
	}
}

// handoffAgent is the boot reservation and its volatile successor queue.
type handoffAgent struct {
	active     bool
	queued     Request
	hasQueue   bool
	installs   int
	confirms   int
	releases   int
	releaseErr error
	confirmErr error
}

func (h *handoffAgent) install(_ context.Context, request Request) error {
	h.installs++
	if !h.active {
		return errors.New("reservation completed")
	}
	h.queued = request
	h.hasQueue = true
	return nil
}

func (h *handoffAgent) confirm(_ context.Context, request Request) error {
	h.confirms++
	if h.confirmErr != nil {
		return h.confirmErr
	}
	if !h.hasQueue {
		h.queued = request
		h.hasQueue = true
		return nil
	}
	if h.queued != request {
		return ErrBusy
	}
	return nil
}

func (h *handoffAgent) held(context.Context) (bool, error) {
	return h.active, nil
}

func (h *handoffAgent) release(context.Context) error {
	h.releases++
	if h.releaseErr != nil {
		err := h.releaseErr
		h.releaseErr = nil
		h.active = false
		return err
	}
	h.active = false
	return nil
}

func TestHandoffTrialFinishedReservationRetriesRelease(t *testing.T) {
	factory := strings.Repeat("a", 64)
	repo, err := OpenRepository(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	pending := Request{ID: strings.Repeat("b", 64), Candidate: strings.Repeat("c", 64), Requester: "host", Generation: 0}
	if err := Queue(repo, pending); err != nil {
		t.Fatal(err)
	}
	agent := &handoffAgent{active: true, releaseErr: context.DeadlineExceeded}
	hold := &SuccessorHold{}
	restore := Request{ID: strings.Repeat("d", 64), Requester: "kit-software-supervisor"}
	err = HandoffTrial(context.Background(), repo, restore, hold, agent.install, agent.confirm, agent.held, agent.release)
	if err == nil || agent.installs != 1 || agent.confirms != 0 || agent.releases != 1 || !hold.Ready || hold.Request != pending {
		t.Fatalf("first release: err=%v installs=%d confirms=%d releases=%d hold=%+v", err, agent.installs, agent.confirms, agent.releases, hold)
	}
	if agent.active || !agent.hasQueue {
		t.Fatal("lost release left the reservation active or dropped the successor", agent.active, agent.hasQueue)
	}
	err = HandoffTrial(context.Background(), repo, restore, hold, agent.install, agent.confirm, agent.held, agent.release)
	if err != nil || agent.installs != 1 || agent.confirms != 1 || agent.releases != 2 || agent.active {
		t.Fatalf("retry: err=%v installs=%d confirms=%d releases=%d active=%v", err, agent.installs, agent.confirms, agent.releases, agent.active)
	}
	if !agent.hasQueue || agent.queued != pending {
		t.Fatal("successor was not kept", agent.queued, agent.hasQueue)
	}
}

func TestHandoffTrialFinishedReservationRestoresLostQueue(t *testing.T) {
	factory := strings.Repeat("a", 64)
	repo, err := OpenRepository(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	pending := Request{ID: strings.Repeat("b", 64), Candidate: strings.Repeat("c", 64), Requester: "host", Generation: 0}
	if err := Queue(repo, pending); err != nil {
		t.Fatal(err)
	}
	agent := &handoffAgent{active: true, releaseErr: context.DeadlineExceeded}
	hold := &SuccessorHold{}
	restore := Request{ID: strings.Repeat("d", 64), Requester: "kit-software-supervisor"}
	if err := HandoffTrial(context.Background(), repo, restore, hold, agent.install, agent.confirm, agent.held, agent.release); err == nil {
		t.Fatal("lost release reported success")
	}
	agent.hasQueue = false
	if err := HandoffTrial(context.Background(), repo, restore, hold, agent.install, agent.confirm, agent.held, agent.release); err != nil {
		t.Fatal(err)
	}
	if agent.installs != 1 || !agent.hasQueue || agent.queued != pending || agent.releases != 2 {
		t.Fatalf("installs=%d queued=%v request=%+v releases=%d", agent.installs, agent.hasQueue, agent.queued, agent.releases)
	}
}

func TestHandoffTrialChangedQueueDoesNotFinish(t *testing.T) {
	factory := strings.Repeat("a", 64)
	repo, err := OpenRepository(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	pending := Request{ID: strings.Repeat("b", 64), Candidate: strings.Repeat("c", 64), Requester: "host", Generation: 0}
	if err := Queue(repo, pending); err != nil {
		t.Fatal(err)
	}
	agent := &handoffAgent{active: true, releaseErr: context.DeadlineExceeded}
	hold := &SuccessorHold{}
	restore := Request{ID: strings.Repeat("d", 64), Requester: "kit-software-supervisor"}
	if err := HandoffTrial(context.Background(), repo, restore, hold, agent.install, agent.confirm, agent.held, agent.release); err == nil {
		t.Fatal("lost release reported success")
	}
	agent.confirmErr = ErrBusy
	err = HandoffTrial(context.Background(), repo, restore, hold, agent.install, agent.confirm, agent.held, agent.release)
	if err == nil || agent.installs != 1 || agent.releases != 1 || !hold.Ready {
		t.Fatalf("changed queue finished the handoff: err=%v installs=%d releases=%d hold=%+v", err, agent.installs, agent.releases, hold)
	}
}

func TestHandoffTrialChangedRequestWhileReservationHeld(t *testing.T) {
	factory := strings.Repeat("a", 64)
	repo, err := OpenRepository(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	pending := Request{ID: strings.Repeat("b", 64), Candidate: strings.Repeat("c", 64), Requester: "host", Generation: 0}
	if err := Queue(repo, pending); err != nil {
		t.Fatal(err)
	}
	agent := &handoffAgent{active: true, releaseErr: context.DeadlineExceeded}
	hold := &SuccessorHold{}
	restore := Request{ID: strings.Repeat("d", 64), Requester: "kit-software-supervisor"}
	if err := HandoffTrial(context.Background(), repo, restore, hold, agent.install, agent.confirm, agent.held, agent.release); err == nil {
		t.Fatal("lost release reported success")
	}
	agent.active = true
	if err := Cancel(repo, pending.ID, pending.Generation); err != nil {
		t.Fatal(err)
	}
	replacement := Request{ID: strings.Repeat("e", 64), Candidate: strings.Repeat("f", 64), Requester: "host", Generation: 0}
	if err := Queue(repo, replacement); err != nil {
		t.Fatal(err)
	}
	if err := HandoffTrial(context.Background(), repo, restore, hold, agent.install, agent.confirm, agent.held, agent.release); err != nil {
		t.Fatal(err)
	}
	if agent.installs != 2 || agent.confirms != 0 || agent.queued != replacement || !hold.Ready || hold.Request != replacement {
		t.Fatalf("installs=%d confirms=%d queued=%+v hold=%+v", agent.installs, agent.confirms, agent.queued, hold)
	}
}

func TestReservationStillHeld(t *testing.T) {
	id := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	for _, tc := range []struct {
		state string
		op    string
		held  bool
	}{
		{"ready", id, true},
		{"finishing", id, true},
		{"preparing", id, true},
		{"blocked", id, true},
		{"none", id, false},
		{"completed", id, false},
		{"", id, false},
		{"finishing", other, false},
		{"none", "", false},
		{"ready", "", true},
	} {
		status := AdmissionStatus{Maintenance: MaintenanceStatus{State: tc.state, OperationID: tc.op}}
		if got := ReservationStillHeld(status, id); got != tc.held {
			t.Fatalf("state %q op %q: got %v", tc.state, tc.op, got)
		}
	}
}
