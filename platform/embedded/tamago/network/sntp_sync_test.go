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

func TestSNTPSyncerPlausibilityDefaultMaxStepSerializationAndBoundedCallbacks(t *testing.T) {
	// 1. Initial sample plausibility validation (with default 5m MaxStep still admitting valid 2026 epoch).
	var mu sync.Mutex
	badSamples := []time.Time{
		{},
		time.Date(1970, time.January, 1, 0, 0, 1, 0, time.UTC),
		time.Date(2200, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	idx := 0
	monoNow := time.Unix(100, 0)
	clk := rtc.NewUnsyncedClock(nil)
	syncer, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink: clk,
		Now: func() time.Time {
			mu.Lock()
			defer mu.Unlock()
			return monoNow
		},
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			mu.Lock()
			defer mu.Unlock()
			tSample := badSamples[idx]
			return SNTPResult{Time: tSample, Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range badSamples {
		mu.Lock()
		idx = i
		mu.Unlock()
		if _, err := syncer.SyncOnce(context.Background()); !errors.Is(err, ErrSNTPImplausibleTime) {
			t.Fatalf("implausible sample %d err=%v, want ErrSNTPImplausibleTime", i, err)
		}
		if clk.Synced() || syncer.Status().Synced {
			t.Fatalf("implausible sample %d marked clock synced", i)
		}
	}

	// Valid 2026 initial sample succeeds even when default 5m MaxStep is active on an unsynced clock.
	validEpoch := time.Date(2026, time.November, 25, 11, 0, 0, 0, time.UTC)
	mu.Lock()
	badSamples[0] = validEpoch
	idx = 0
	mu.Unlock()
	if _, err := syncer.SyncOnce(context.Background()); err != nil {
		t.Fatalf("valid initial sample failed: %v", err)
	}
	if !clk.Synced() {
		t.Fatal("valid initial sample did not sync clock")
	}

	// Default MaxStep (5m) rejects a subsequent 10m jump; DisableMaxStep permits it.
	mu.Lock()
	monoNow = monoNow.Add(10 * time.Second)
	badSamples[0] = validEpoch.Add(10 * time.Minute)
	mu.Unlock()
	if _, err := syncer.SyncOnce(context.Background()); !errors.Is(err, ErrSNTPStepTooLarge) {
		t.Fatalf("10m jump with default 5m MaxStep err=%v, want ErrSNTPStepTooLarge", err)
	}

	noLimitSyncer, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		DisableMaxStep: true,
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: validEpoch.Add(time.Hour), Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noLimitSyncer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := noLimitSyncer.SyncOnce(context.Background()); err != nil {
		t.Fatalf("DisableMaxStep should allow large step: %v", err)
	}

	// Invalid plausibility window must reject in constructor.
	if _, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		MinValidTime: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		MaxValidTime: time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC),
		Query:        func(context.Context, SNTPConfig) (SNTPResult, error) { return SNTPResult{}, nil },
	}); !errors.Is(err, ErrSNTPInvalidConfig) {
		t.Fatalf("inverted MinValidTime/MaxValidTime err=%v, want ErrSNTPInvalidConfig", err)
	}

	// 2. Concurrent SyncOnce serialization prevents delayed older response from overwriting newer sample.
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var callSeq atomic.Int32
	serialClk := rtc.NewUnsyncedClock(nil)
	sSerial, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink:           serialClk,
		DisableMaxStep: true,
		Query: func(ctx context.Context, _ SNTPConfig) (SNTPResult, error) {
			n := callSeq.Add(1)
			if n == 1 {
				close(firstStarted)
				<-releaseFirst
				return SNTPResult{Time: validEpoch, Stratum: 1}, nil
			}
			return SNTPResult{Time: validEpoch.Add(30 * time.Second), Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := sSerial.SyncOnce(context.Background())
		firstDone <- err
	}()
	<-firstStarted

	// Canceled caller waiting on syncSem exits immediately without waiting for call 1.
	cancelCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if _, err := sSerial.SyncOnce(cancelCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled queued SyncOnce err=%v, want context.Canceled", err)
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := sSerial.SyncOnce(context.Background())
		secondDone <- err
	}()
	// Confirm second caller stays serialized behind call 1.
	time.Sleep(20 * time.Millisecond)
	if st := sSerial.Status(); st.SyncCount != 0 {
		t.Fatalf("second SyncOnce bypassed serialization lock: %+v", st)
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if st := sSerial.Status(); !st.LastSync.Equal(validEpoch.Add(30 * time.Second)) {
		t.Fatalf("LastSync=%v want later sample %v", st.LastSync, validEpoch.Add(30*time.Second))
	}

	// 3. Blocked callback stays bounded (1 worker, drops excess instead of leaking goroutines).
	cbEntered := make(chan struct{})
	cbRelease := make(chan struct{})
	sBounded, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		DisableMaxStep: true,
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: validEpoch, Stratum: 1}, nil
		},
		OnSync: func(SNTPResult) {
			select {
			case <-cbEntered:
			default:
				close(cbEntered)
			}
			<-cbRelease
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sBounded.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-cbEntered
	for i := 0; i < 20; i++ {
		if _, err := sBounded.SyncOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if st := sBounded.Status(); st.DroppedCallbacks == 0 {
		t.Fatalf("expected DroppedCallbacks > 0 when callback is blocked, got %+v", st)
	}
	sBounded.Stop()
	close(cbRelease)
	select {
	case <-sBounded.cbDone:
	case <-time.After(2 * time.Second):
		t.Fatal("callback worker did not exit after Stop + unblocking")
	}

	// 4. Custom WallClockSink.Sync can call Status() without deadlocking on s.mu.
	var sinkSyncer *SNTPSyncer
	var sinkSawStatus SNTPSyncStatus
	customSink := funcSink(func(w time.Time) {
		sinkSawStatus = sinkSyncer.Status()
	})
	sinkSyncer, err = NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink: customSink,
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: validEpoch, Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sinkSyncer.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !sinkSawStatus.Synced || sinkSawStatus.SyncCount != 1 {
		t.Fatalf("custom sink did not observe updated Status(): %+v", sinkSawStatus)
	}
}

type funcSink func(time.Time)

func (f funcSink) Sync(t time.Time) { f(t) }
