// SPDX-License-Identifier: GPL-3.0-or-later

package network

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/rtc"
)

var _ WallClockSink = (*rtc.Clock)(nil)

func TestSNTPSyncerSyncOnceBackoffAndRTC(t *testing.T) {
	var monoNano atomic.Int64
	monoNano.Store(1_000_000_000)
	clk := rtc.NewUnsyncedClock(monoNano.Load)
	if _, ok := clk.WallTime(); ok {
		t.Fatal("clock must start unsynced")
	}

	var calls atomic.Int32
	var syncEvents atomic.Int32
	var errEvents atomic.Int32
	t0 := time.Date(2026, time.November, 25, 9, 0, 0, 0, time.UTC)

	syncer, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink:             clk,
		PollInterval:     25 * time.Millisecond,
		RetryInterval:    5 * time.Millisecond,
		MaxRetryInterval: 20 * time.Millisecond,
		Query: func(ctx context.Context, _ SNTPConfig) (SNTPResult, error) {
			n := calls.Add(1)
			switch n {
			case 1:
				return SNTPResult{}, ErrPaused
			case 2:
				return SNTPResult{}, ErrSNTPInvalidReply
			default:
				return SNTPResult{
					Time:    t0.Add(time.Duration(n-3) * 10 * time.Millisecond),
					Stratum: 1,
					Server:  "10.0.2.2",
				}, nil
			}
		},
		OnSync:  func(SNTPResult) { syncEvents.Add(1) },
		OnError: func(error) { errEvents.Add(1) },
	})
	if err != nil {
		t.Fatalf("NewSNTPSyncer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := syncer.Start(ctx); !errors.Is(err, ErrSNTPSyncerStarted) {
		t.Fatalf("second Start err=%v, want ErrSNTPSyncerStarted", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for (syncEvents.Load() < 2 || errEvents.Load() < 1) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	syncer.Stop()
	syncer.Stop() // idempotent

	if err := syncer.Start(ctx); !errors.Is(err, ErrSNTPSyncerStopped) {
		t.Fatalf("Start after Stop err=%v, want ErrSNTPSyncerStopped", err)
	}

	st := syncer.Status()
	if !st.Synced || st.SyncCount < 2 || st.ErrorCount != 1 || errEvents.Load() != 1 {
		t.Fatalf("unexpected sync status: %+v errEvents=%d", st, errEvents.Load())
	}
	if _, ok := clk.WallTime(); !ok {
		t.Fatal("rtc.Clock did not become synced after SNTPSyncer")
	}
}

func TestSNTPSyncerMaxStepGuardAndValidation(t *testing.T) {
	if _, err := NewSNTPSyncer(nil, SNTPSyncConfig{}); !errors.Is(err, ErrSNTPInvalidConfig) {
		t.Fatalf("nil port & query err=%v, want ErrSNTPInvalidConfig", err)
	}
	if _, err := NewSNTPSyncer(nil, SNTPSyncConfig{PollInterval: -time.Second, Query: func(context.Context, SNTPConfig) (SNTPResult, error) { return SNTPResult{}, nil }}); !errors.Is(err, ErrSNTPInvalidConfig) {
		t.Fatalf("negative PollInterval err=%v, want ErrSNTPInvalidConfig", err)
	}

	var mu sync.Mutex
	monoNow := time.Unix(100, 0)
	baseUTC := time.Date(2026, time.November, 25, 10, 0, 0, 0, time.UTC)
	sample := baseUTC
	clk := rtc.NewUnsyncedClock(nil)

	syncer, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink:    clk,
		MaxStep: 2 * time.Second,
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return monoNow
		},
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			mu.Lock()
			defer mu.Unlock()
			return SNTPResult{Time: sample, Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewSNTPSyncer: %v", err)
	}

	// First sync sets initial epoch even though previous clock was unsynced.
	if _, err := syncer.SyncOnce(context.Background()); err != nil {
		t.Fatalf("initial SyncOnce: %v", err)
	}

	// Step beyond MaxStep (5s drift vs 10s elapsed expected UTC) must reject without moving Sink.
	mu.Lock()
	monoNow = monoNow.Add(10 * time.Second)
	sample = baseUTC.Add(15 * time.Second)
	mu.Unlock()
	if _, err := syncer.SyncOnce(context.Background()); !errors.Is(err, ErrSNTPStepTooLarge) {
		t.Fatalf("oversized step err=%v, want ErrSNTPStepTooLarge", err)
	}
	if st := syncer.Status(); st.SyncCount != 1 || st.ErrorCount != 1 || !st.LastSync.Equal(baseUTC) {
		t.Fatalf("rejected step mutated status: %+v", st)
	}

	// Small correction (500ms from expected baseUTC+10s) succeeds cleanly.
	mu.Lock()
	sample = baseUTC.Add(10*time.Second + 500*time.Millisecond)
	mu.Unlock()
	if _, err := syncer.SyncOnce(context.Background()); err != nil {
		t.Fatalf("within-MaxStep SyncOnce: %v", err)
	}
	if st := syncer.Status(); st.SyncCount != 2 || !st.LastSync.Equal(sample) || st.LastErr != nil {
		t.Fatalf("unexpected post-recovery status: %+v", st)
	}
}

func TestSNTPSyncerCallbackStopDoesNotDeadlockAndSyncOnceBarrier(t *testing.T) {
	// 1. OnSync calling Stop() synchronously must not deadlock.
	stoppedCh := make(chan struct{})
	var syncer *SNTPSyncer
	var err error
	syncer, err = NewSNTPSyncer(nil, SNTPSyncConfig{
		PollInterval:  10 * time.Millisecond,
		RetryInterval: 5 * time.Millisecond,
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: time.Unix(1795000000, 0).UTC(), Stratum: 1}, nil
		},
		OnSync: func(SNTPResult) {
			syncer.Stop()
			close(stoppedCh)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := syncer.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stoppedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("OnSync calling Stop deadlocked")
	}

	// 2. Direct public SyncOnce participates in Stop() barrier and never updates Sink after Stop returns.
	inQuery := make(chan struct{})
	releaseQuery := make(chan struct{})
	var sinkCalls atomic.Int32
	clk := rtc.NewUnsyncedClock(nil)
	s2, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink: clk,
		Query: func(ctx context.Context, _ SNTPConfig) (SNTPResult, error) {
			close(inQuery)
			<-releaseQuery
			sinkCalls.Add(1)
			return SNTPResult{Time: time.Unix(1795000100, 0).UTC(), Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := s2.SyncOnce(context.Background())
		errCh <- err
	}()
	<-inQuery

	stopDone := make(chan struct{})
	go func() {
		s2.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		t.Fatal("Stop returned while public SyncOnce was still in flight")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseQuery)
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after in-flight SyncOnce completed")
	}
	if gotErr := <-errCh; !errors.Is(gotErr, ErrSNTPSyncerStopped) {
		t.Fatalf("in-flight SyncOnce err=%v, want ErrSNTPSyncerStopped", gotErr)
	}
	if _, ok := clk.WallTime(); ok {
		t.Fatal("Sink was updated after Stop was initiated")
	}
	if _, err := s2.SyncOnce(context.Background()); !errors.Is(err, ErrSNTPSyncerStopped) {
		t.Fatalf("SyncOnce after Stop err=%v, want ErrSNTPSyncerStopped", err)
	}
}
