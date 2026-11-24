package kitlease

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInputCleanupMarkerRequiresLiveManagerContext(t *testing.T) {
	marker := &inputCleanupMarker{}
	ConfirmInputCleanup(context.Background())
	if marker.confirmed.Load() {
		t.Fatal("wrong context marked cleanup")
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), inputCleanupMarkerKey{}, marker))
	cancel()
	ConfirmInputCleanup(ctx)
	if marker.confirmed.Load() {
		t.Fatal("expired context marked cleanup")
	}
	marker.confirmed.Store(true)
	if inputCleanupSucceeded(ctx, marker, nil) {
		t.Fatal("nil return after cancellation certified cleanup")
	}
	if inputCleanupSucceeded(context.Background(), marker, errors.New("cleanup failed")) {
		t.Fatal("error after marker certified cleanup")
	}
}

func TestInputCleanupReceiptRequiresMarkedSuccessfulExactLease(t *testing.T) {
	var calls atomic.Int32
	manager := New(time.Minute, func(ctx context.Context) error {
		switch calls.Add(1) {
		case 1: // startup reconciliation
			return nil
		case 2: // old lease: nil alone is not proof
			return nil
		default:
			ConfirmInputCleanup(ctx)
			return nil
		}
	})
	t.Cleanup(manager.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	claim := func(id string) Grant {
		t.Helper()
		g, err := manager.Claim(ClaimRequest{RequestID: strings.Repeat(id, 32), Owner: "test", Purpose: "test"})
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	old := claim("a")
	oldCtx, done, err := manager.BeginLocalInput(old.Status.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Release(old.Token); err != nil {
		t.Fatal(err)
	}
	if InputCleanupConfirmed(oldCtx) {
		t.Fatal("revoke certified before admitted operation drained")
	}
	done()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	if InputCleanupConfirmed(oldCtx) {
		t.Fatal("unmarked nil callback certified cleanup")
	}
	next := claim("b")
	nextCtx, done, err := manager.BeginLocalInput(next.Status.Generation)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if _, err := manager.Release(next.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	if !InputCleanupConfirmed(nextCtx) {
		t.Fatal("marked successful cleanup was not certified")
	}
	if InputCleanupConfirmed(oldCtx) {
		t.Fatal("successor cleanup certified old lease")
	}
}

func TestInputCleanupReceiptRejectsMarkerFollowedByError(t *testing.T) {
	var calls atomic.Int32
	manager := New(time.Minute, func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			return nil
		}
		ConfirmInputCleanup(ctx)
		return ErrRuntimeUnreachable
	})
	t.Cleanup(manager.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Claim(ClaimRequest{RequestID: strings.Repeat("c", 32), Owner: "test", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, done, err := manager.BeginLocalInput(lease.Status.Generation)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if _, err := manager.Release(lease.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	if InputCleanupConfirmed(leaseCtx) {
		t.Fatal("unreachable cleanup certified neutral")
	}
}

func TestInputCleanupReceiptRejectsNilAfterCleanupTimeout(t *testing.T) {
	var calls atomic.Int32
	manager := New(time.Minute, func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			return nil
		}
		ConfirmInputCleanup(ctx)
		<-ctx.Done()
		return nil
	})
	t.Cleanup(manager.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.cleanupTimeout = 40 * time.Millisecond
	manager.mu.Unlock()
	lease, err := manager.Claim(ClaimRequest{RequestID: strings.Repeat("d", 32), Owner: "test", Purpose: "test"})
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, done, err := manager.BeginLocalInput(lease.Status.Generation)
	if err != nil {
		t.Fatal(err)
	}
	done()
	if _, err := manager.Release(lease.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	if InputCleanupConfirmed(leaseCtx) {
		t.Fatal("timed-out cleanup certified neutral")
	}
}
