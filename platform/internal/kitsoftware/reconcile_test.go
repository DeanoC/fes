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
	err = HandoffTrial(context.Background(), repo, restore, func(_ context.Context, request Request) error {
		installed = request
		return lease.Install()
	}, func(context.Context) error {
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
	err = HandoffTrial(context.Background(), repo, restore, func(context.Context, Request) error {
		return errors.New("agent queue unavailable")
	}, func(context.Context) error {
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
