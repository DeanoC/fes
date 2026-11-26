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
	validPortSNTP := SNTPConfig{
		LocalMAC:    "52:54:00:12:34:56",
		LocalPrefix: "10.0.2.15/24",
		Servers:     []string{"10.0.2.2"},
	}
	port := newFakeRenewPort()
	if _, err := NewSNTPSyncer(port, SNTPSyncConfig{SNTP: validPortSNTP}); err != nil {
		t.Fatalf("valid port SNTP config rejected: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*SNTPConfig)
	}{
		{name: "negative-timeout", mutate: func(c *SNTPConfig) { c.Timeout = -time.Millisecond }},
		{name: "negative-retry", mutate: func(c *SNTPConfig) { c.RetryInterval = -time.Millisecond }},
		{name: "bad-nexthop-mac", mutate: func(c *SNTPConfig) { c.NextHopMAC = "ff:ff:ff:ff:ff:ff" }},
		{name: "offsubnet-no-gw", mutate: func(c *SNTPConfig) { c.Servers = []string{"8.8.8.8"} }},
	} {
		bad := validPortSNTP
		tc.mutate(&bad)
		if _, err := NewSNTPSyncer(port, SNTPSyncConfig{SNTP: bad}); !errors.Is(err, ErrSNTPInvalidConfig) {
			t.Fatalf("%s err=%v, want ErrSNTPInvalidConfig", tc.name, err)
		}
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
	boundedStopDone := make(chan struct{})
	go func() {
		sBounded.Stop()
		close(boundedStopDone)
	}()
	select {
	case <-boundedStopDone:
		t.Fatal("external Stop returned while OnSync callback was still active")
	case <-time.After(20 * time.Millisecond):
	}
	close(cbRelease)
	select {
	case <-boundedStopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("external Stop did not return after unblocking OnSync callback")
	}
	select {
	case <-sBounded.cbDone:
	case <-time.After(2 * time.Second):
		t.Fatal("callback worker did not exit after Stop + unblocking")
	}

	// 4. Custom WallClockSink.Sync can call Status() and Stop() synchronously without deadlocking.
	var sinkSyncer *SNTPSyncer
	var sinkSawStatus SNTPSyncStatus
	sinkStopDone := make(chan struct{})
	customSink := funcSink(func(w time.Time) {
		defer close(sinkStopDone)
		sinkSawStatus = sinkSyncer.Status()
		sinkSyncer.Stop()
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
	select {
	case <-sinkStopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("WallClockSink.Sync calling Stop() deadlocked")
	}
	if !sinkSawStatus.Synced || sinkSawStatus.SyncCount != 1 {
		t.Fatalf("custom sink did not observe updated Status(): %+v", sinkSawStatus)
	}

	// 5. External Stop() during a slow WallClockSink.Sync waits until the active sink completes.
	sinkEntered := make(chan struct{})
	releaseSink := make(chan struct{})
	var sinkCompleted atomic.Bool
	var slowSyncer *SNTPSyncer
	slowSyncer, err = NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink: funcSink(func(w time.Time) {
			close(sinkEntered)
			<-releaseSink
			sinkCompleted.Store(true)
		}),
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: validEpoch, Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	slowDone := make(chan error, 1)
	go func() {
		_, err := slowSyncer.SyncOnce(context.Background())
		slowDone <- err
	}()
	<-sinkEntered

	extStopDone := make(chan struct{})
	go func() {
		slowSyncer.Stop()
		close(extStopDone)
	}()
	select {
	case <-extStopDone:
		t.Fatal("external Stop returned while WallClockSink.Sync was still active")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseSink)
	select {
	case <-extStopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("external Stop did not return after active sink finished")
	}
	if !sinkCompleted.Load() {
		t.Fatal("external Stop returned before Sink.Sync finished")
	}
	if err := <-slowDone; err != nil {
		t.Fatalf("slow SyncOnce err=%v", err)
	}

	// 6. Queued OnSync callbacks are drained before external Stop returns.
	var queuedCalls atomic.Int32
	sDrain, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		DisableMaxStep: true,
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: validEpoch, Stratum: 1}, nil
		},
		OnSync: func(SNTPResult) {
			time.Sleep(5 * time.Millisecond)
			queuedCalls.Add(1)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := sDrain.SyncOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	sDrain.Stop()
	if got := queuedCalls.Load(); got != 4 {
		t.Fatalf("Stop dropped queued callbacks: ran %d of 4", got)
	}

	// 7. Callback-local Stop drains later already-queued callbacks before Stop returns.
	var cb1Entered = make(chan struct{})
	var cb2Queued = make(chan struct{})
	var cbCount atomic.Int32
	var seenCountInsideStop int32
	var cbStopReturned = make(chan struct{})
	var sSelfDrain *SNTPSyncer
	sSelfDrain, err = NewSNTPSyncer(nil, SNTPSyncConfig{
		DisableMaxStep: true,
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: validEpoch, Stratum: 1}, nil
		},
		OnSync: func(SNTPResult) {
			n := cbCount.Add(1)
			if n == 1 {
				close(cb1Entered)
				<-cb2Queued
				sSelfDrain.Stop()
				seenCountInsideStop = cbCount.Load()
				close(cbStopReturned)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sSelfDrain.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-cb1Entered
	if _, err := sSelfDrain.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(cb2Queued)
	select {
	case <-cbStopReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("callback-local Stop deadlocked")
	}
	if seenCountInsideStop != 2 {
		t.Fatalf("callback-local Stop returned before draining cb2: count=%d want 2", seenCountInsideStop)
	}
}

type funcSink func(time.Time)

func (f funcSink) WallTime() (time.Time, bool) { return time.Time{}, false }
func (f funcSink) Sync(t time.Time)            { f(t) }

type customWallSink struct {
	wallFn func() (time.Time, bool)
	syncFn func(time.Time)
}

func (s customWallSink) WallTime() (time.Time, bool) { return s.wallFn() }
func (s customWallSink) Sync(t time.Time)            { s.syncFn(t) }

func TestSNTPSyncerEnforcesMaxStepOnPreSyncedSinkAndReentrantWallTime(t *testing.T) {
	var mono atomic.Int64
	mono.Store(1_000_000_000)
	preEpoch := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)

	// 1. Pre-synchronized sink (QEMU PL031 pattern): first sample > 5m off rejects without touching clock.
	preClk := rtc.NewClock(preEpoch, mono.Load)
	sample := preEpoch.Add(10 * time.Minute)
	var mu sync.Mutex
	sPre, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink: preClk,
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			mu.Lock()
			defer mu.Unlock()
			return SNTPResult{Time: sample, Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sPre.SyncOnce(context.Background()); !errors.Is(err, ErrSNTPStepTooLarge) {
		t.Fatalf("first sample 10m step on pre-synced sink err=%v, want ErrSNTPStepTooLarge", err)
	}
	if st := sPre.Status(); st.Synced || st.SyncCount != 0 || st.ErrorCount != 1 {
		t.Fatalf("rejected pre-synced first step mutated status: %+v", st)
	}
	if got, ok := preClk.WallTime(); !ok || !got.Equal(preEpoch) {
		t.Fatalf("rejected first step mutated pre-synced clock: got=%v ok=%t want=%v", got, ok, preEpoch)
	}

	// Valid small step (2m) on first sync against pre-synced sink is admitted.
	mu.Lock()
	sample = preEpoch.Add(2 * time.Minute)
	mu.Unlock()
	if _, err := sPre.SyncOnce(context.Background()); err != nil {
		t.Fatalf("2m first step on pre-synced sink failed: %v", err)
	}
	if got, ok := preClk.WallTime(); !ok || !got.Equal(preEpoch.Add(2*time.Minute)) {
		t.Fatalf("accepted first step clock=%v ok=%t", got, ok)
	}

	// DisableMaxStep allows > 5m first step even when Sink is pre-synchronized.
	preClkBypass := rtc.NewClock(preEpoch, mono.Load)
	sBypass, err := NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink:           preClkBypass,
		DisableMaxStep: true,
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: preEpoch.Add(time.Hour), Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sBypass.SyncOnce(context.Background()); err != nil {
		t.Fatalf("DisableMaxStep first step on pre-synced sink failed: %v", err)
	}
	if got, ok := preClkBypass.WallTime(); !ok || !got.Equal(preEpoch.Add(time.Hour)) {
		t.Fatalf("DisableMaxStep clock=%v ok=%t", got, ok)
	}

	// 2. Custom WallClockSink.WallTime calling Status() and Stop() does not deadlock and aborts SyncOnce.
	var reentrantSyncer *SNTPSyncer
	var sawStatus SNTPSyncStatus
	var syncCalled atomic.Bool
	reentrantSyncer, err = NewSNTPSyncer(nil, SNTPSyncConfig{
		Sink: customWallSink{
			wallFn: func() (time.Time, bool) {
				sawStatus = reentrantSyncer.Status()
				reentrantSyncer.Stop()
				return preEpoch, true
			},
			syncFn: func(time.Time) {
				syncCalled.Store(true)
			},
		},
		Query: func(context.Context, SNTPConfig) (SNTPResult, error) {
			return SNTPResult{Time: preEpoch.Add(time.Minute), Stratum: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reentrantSyncer.SyncOnce(context.Background()); !errors.Is(err, ErrSNTPSyncerStopped) {
		t.Fatalf("re-entrant Stop inside WallTime err=%v, want ErrSNTPSyncerStopped", err)
	}
	if syncCalled.Load() {
		t.Fatal("Sink.Sync was called after Stop inside WallTime")
	}
	if sawStatus.Synced || sawStatus.SyncCount != 0 {
		t.Fatalf("unexpected sawStatus inside WallTime: %+v", sawStatus)
	}
}
