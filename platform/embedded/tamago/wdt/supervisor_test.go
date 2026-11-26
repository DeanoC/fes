// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stubKicker struct {
	mu       sync.Mutex
	startDur time.Duration
	startErr error
	kickErr  error
	starts   int
	kicks    int
	lastMode ResponseMode
	onKick   func()
}

func (k *stubKicker) Start(timeout time.Duration, mode ResponseMode) (time.Duration, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.starts++
	k.lastMode = mode
	if k.startErr != nil {
		return 0, k.startErr
	}
	if k.startDur != 0 {
		return k.startDur, nil
	}
	return timeout, nil
}

func (k *stubKicker) Kick() error {
	k.mu.Lock()
	err := k.kickErr
	cb := k.onKick
	if err == nil {
		k.kicks++
	}
	k.mu.Unlock()
	if cb != nil {
		cb()
	}
	return err
}

func TestNewSupervisorValidationAndOptIn(t *testing.T) {
	stub := &stubKicker{}

	if _, err := NewSupervisor(nil, SupervisorConfig{Timeout: time.Second}); !errors.Is(err, ErrNilAccess) {
		t.Fatalf("nil dev err=%v, want ErrNilAccess", err)
	}
	if _, err := NewSupervisor(stub, SupervisorConfig{Timeout: 0}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("zero timeout err=%v", err)
	}
	if _, err := NewSupervisor(stub, SupervisorConfig{Timeout: time.Second, KickInterval: -time.Millisecond}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative kick interval err=%v", err)
	}
	if _, err := NewSupervisor(stub, SupervisorConfig{Timeout: time.Second, KickInterval: time.Second}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("kick interval >= timeout err=%v", err)
	}
	if _, err := NewSupervisor(stub, SupervisorConfig{Timeout: time.Second, Mode: ResponseMode(9)}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid mode err=%v", err)
	}

	sup, err := NewSupervisor(stub, SupervisorConfig{Timeout: 30 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	if stub.starts != 0 || sup.Running() || sup.Interval() != 0 || sup.Kicks() != 0 || sup.Err() != nil {
		t.Fatalf("NewSupervisor unexpectedly armed or started: starts=%d running=%t", stub.starts, sup.Running())
	}
	// Stop before Start is a safe idempotent no-op, and subsequent Start fails closed without arming.
	sup.Stop()
	sup.Stop()
	if _, err := sup.Start(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatalf("Start after Stop err=%v, want ErrStopped", err)
	}
	if stub.starts != 0 || sup.Running() {
		t.Fatalf("Start after Stop armed hardware: starts=%d running=%t", stub.starts, sup.Running())
	}
}

func TestSupervisorPeriodicKicksWithRealDWAndStopDrain(t *testing.T) {
	// 100 MHz clock -> top=0 is 65536 cycles = 655.36µs, top=3 is ~5.24ms.
	hw := newFakeDW()
	dev, err := Open(hw, 100_000_000)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var checks atomic.Uint64
	sup, err := NewSupervisor(dev, SupervisorConfig{
		Timeout:      15 * time.Millisecond,
		KickInterval: 3 * time.Millisecond,
		Mode:         ModeReset,
		Check: func() error {
			checks.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}

	if _, err := sup.Start(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil ctx err=%v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sup.Start(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ctx err=%v", err)
	}

	actual, err := sup.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if actual < 15*time.Millisecond || !dev.Enabled() || !sup.Running() {
		t.Fatalf("Start state actual=%v enabled=%t running=%t", actual, dev.Enabled(), sup.Running())
	}
	if sup.Interval() != 3*time.Millisecond {
		t.Fatalf("Interval=%v, want 3ms", sup.Interval())
	}
	if _, err := sup.Start(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("double Start err=%v, want ErrAlreadyStarted", err)
	}

	deadline := time.Now().Add(time.Second)
	for sup.Kicks() < 3 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if sup.Kicks() < 3 || checks.Load() < 3 {
		t.Fatalf("expected >=3 kicks and checks, got kicks=%d checks=%d", sup.Kicks(), checks.Load())
	}

	sup.Stop()
	if sup.Running() {
		t.Fatal("Running still true after Stop")
	}
	kicksAtStop := sup.Kicks()
	time.Sleep(15 * time.Millisecond)
	if sup.Kicks() != kicksAtStop {
		t.Fatalf("kicks continued after Stop: %d -> %d", kicksAtStop, sup.Kicks())
	}
}

func TestSupervisorClampsIntervalWhenHardwareCapsPeriod(t *testing.T) {
	// Hardware caps achieved period below requested KickInterval -> must clamp to actual/3.
	stub := &stubKicker{startDur: 6 * time.Millisecond}
	sup, err := NewSupervisor(stub, SupervisorConfig{
		Timeout:      30 * time.Millisecond,
		KickInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	actual, err := sup.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sup.Stop()
	if actual != 6*time.Millisecond || sup.Interval() != 2*time.Millisecond {
		t.Fatalf("actual=%v interval=%v, want (6ms, 2ms)", actual, sup.Interval())
	}

	// Sub-3ns hardware period still yields positive 1ns interval; start errors propagate.
	tiny := &stubKicker{startDur: 2 * time.Nanosecond}
	supTiny, err := NewSupervisor(tiny, SupervisorConfig{Timeout: time.Millisecond})
	if err != nil {
		t.Fatalf("NewSupervisor tiny: %v", err)
	}
	if _, err := supTiny.Start(context.Background()); err != nil {
		t.Fatalf("Start tiny: %v", err)
	}
	if supTiny.Interval() != time.Nanosecond {
		t.Fatalf("tiny Interval=%v, want 1ns", supTiny.Interval())
	}
	supTiny.Stop()

	badStart := &stubKicker{startErr: errors.New("hw fault")}
	supBad, _ := NewSupervisor(badStart, SupervisorConfig{Timeout: time.Millisecond})
	if _, err := supBad.Start(context.Background()); err == nil {
		t.Fatal("expected Start error to propagate")
	}
	negStart := &stubKicker{startDur: -time.Millisecond}
	supNeg, _ := NewSupervisor(negStart, SupervisorConfig{Timeout: time.Millisecond})
	if _, err := supNeg.Start(context.Background()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative actual period err=%v, want ErrInvalidConfig", err)
	}
}

func TestSupervisorCheckFailureKickFailureAndContextCancel(t *testing.T) {
	// 1. Failing Check halts kicking and reports error via Err() and OnError.
	checkErr := errors.New("main loop wedged")
	errCh := make(chan error, 1)
	stub1 := &stubKicker{}
	sup1, err := NewSupervisor(stub1, SupervisorConfig{
		Timeout:      12 * time.Millisecond,
		KickInterval: 2 * time.Millisecond,
		Check:        func() error { return checkErr },
		OnError:      func(e error) { errCh <- e },
	})
	if err != nil {
		t.Fatalf("NewSupervisor check fail: %v", err)
	}
	if _, err := sup1.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case got := <-errCh:
		if !errors.Is(got, checkErr) {
			t.Fatalf("OnError=%v, want %v", got, checkErr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnError from Check failure")
	}
	sup1.Stop()
	if !errors.Is(sup1.Err(), checkErr) || sup1.Kicks() != 0 {
		t.Fatalf("Err=%v kicks=%d, want %v and 0", sup1.Err(), sup1.Kicks(), checkErr)
	}

	// 2. Failing Kick halts kicking and reports error.
	kickErr := errors.New("kick failed")
	errCh2 := make(chan error, 1)
	stub2 := &stubKicker{kickErr: kickErr}
	sup2, err := NewSupervisor(stub2, SupervisorConfig{
		Timeout:      12 * time.Millisecond,
		KickInterval: 2 * time.Millisecond,
		OnError:      func(e error) { errCh2 <- e },
	})
	if err != nil {
		t.Fatalf("NewSupervisor kick fail: %v", err)
	}
	if _, err := sup2.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case got := <-errCh2:
		if !errors.Is(got, kickErr) {
			t.Fatalf("OnError=%v, want %v", got, kickErr)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnError from Kick failure")
	}
	sup2.Stop()
	if !errors.Is(sup2.Err(), kickErr) {
		t.Fatalf("Err=%v, want %v", sup2.Err(), kickErr)
	}

	// 3. Context cancellation stops the supervisor cleanly.
	ctx, cancel := context.WithCancel(context.Background())
	stub3 := &stubKicker{}
	sup3, err := NewSupervisor(stub3, SupervisorConfig{
		Timeout:      30 * time.Millisecond,
		KickInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSupervisor cancel: %v", err)
	}
	if _, err := sup3.Start(ctx); err != nil {
		t.Fatalf("Start cancel: %v", err)
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for sup3.Running() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if sup3.Running() {
		t.Fatal("supervisor still running after context cancel")
	}
	sup3.Stop()

	// 4. Stop waits for an in-flight Check callback to finish before returning.
	inCheck := make(chan struct{})
	releaseCheck := make(chan struct{})
	var checkDone atomic.Bool
	stub4 := &stubKicker{}
	sup4, err := NewSupervisor(stub4, SupervisorConfig{
		Timeout:      12 * time.Millisecond,
		KickInterval: 2 * time.Millisecond,
		Check: func() error {
			select {
			case <-inCheck:
			default:
				close(inCheck)
			}
			<-releaseCheck
			checkDone.Store(true)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewSupervisor drain: %v", err)
	}
	if _, err := sup4.Start(context.Background()); err != nil {
		t.Fatalf("Start drain: %v", err)
	}
	select {
	case <-inCheck:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Check to start")
	}
	stopDone := make(chan struct{})
	go func() {
		sup4.Stop()
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop returned while Check was still in flight")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseCheck)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after Check completed")
	}
	if !checkDone.Load() || sup4.Running() {
		t.Fatalf("after Stop drain: checkDone=%t running=%t", checkDone.Load(), sup4.Running())
	}
}

func TestSupervisorStatsTelemetry(t *testing.T) {
	if st := (*Supervisor)(nil).Stats(); st != (SupervisorStats{}) {
		t.Fatalf("nil Supervisor Stats=%+v", st)
	}

	// 1. Check failure tracking via Stats().
	checkErr := errors.New("health check boom")
	stub := &stubKicker{startDur: 30 * time.Millisecond}
	errCh := make(chan error, 1)
	sup, err := NewSupervisor(stub, SupervisorConfig{
		Timeout:      30 * time.Millisecond,
		KickInterval: 2 * time.Millisecond,
		Check:        func() error { return checkErr },
		OnError:      func(e error) { errCh <- e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if st := sup.Stats(); st.Started || st.Running || st.Stopped || st.Period != 0 || st.Kicks != 0 {
		t.Fatalf("pre-Start Stats=%+v", st)
	}
	if _, err := sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-errCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for check error")
	}
	sup.Stop()
	st := sup.Stats()
	if !st.Started || st.Running || !st.Stopped || st.Period != 30*time.Millisecond || st.Interval != 2*time.Millisecond ||
		st.CheckFailures != 1 || st.KickFailures != 0 || !errors.Is(st.LastErr, checkErr) {
		t.Fatalf("after Check failure Stats=%+v", st)
	}

	// 2. KickNow failure tracking increments KickFailures.
	kickErr := errors.New("manual kick failed")
	stubFail := &stubKicker{startDur: 24 * time.Millisecond}
	sup2, err := NewSupervisor(stubFail, SupervisorConfig{
		Timeout:      24 * time.Millisecond,
		KickInterval: 6 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sup2.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sup2.Stop()
	stubFail.mu.Lock()
	stubFail.kickErr = kickErr
	stubFail.mu.Unlock()
	if err := sup2.KickNow(); !errors.Is(err, kickErr) {
		t.Fatalf("KickNow err=%v want %v", err, kickErr)
	}
	if st2 := sup2.Stats(); st2.KickFailures != 1 || !st2.Stopped {
		t.Fatalf("after KickNow failure Stats=%+v", st2)
	}
}
