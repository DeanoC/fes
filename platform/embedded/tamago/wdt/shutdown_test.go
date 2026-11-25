// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/psci"
)

type fakePowerCtrl struct {
	mu     sync.Mutex
	resets int
	offs   int
	err    error
}

func (p *fakePowerCtrl) SystemReset() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resets++
	return p.err
}

func (p *fakePowerCtrl) SystemOff() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.offs++
	return p.err
}

func TestDWTimeoutAndSetTimeout(t *testing.T) {
	hw := newFakeDW()
	const clk = 100_000_000 // 100 MHz
	dw, err := Open(hw, clk)
	if err != nil {
		t.Fatal(err)
	}

	// Unarmed watchdog reports 0 Timeout and refuses SetTimeout without arming WDT_EN.
	if got := dw.Timeout(); got != 0 {
		t.Fatalf("unarmed Timeout = %v, want 0", got)
	}
	if _, err := dw.SetTimeout(time.Second); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("unarmed SetTimeout = %v, want ErrNotRunning", err)
	}
	if dw.Enabled() || hw.cr != 0 {
		t.Fatalf("unarmed SetTimeout armed hardware: cr=%#x", hw.cr)
	}
	if _, err := dw.SetTimeout(0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("zero SetTimeout = %v, want ErrInvalidConfig", err)
	}

	// Start with short period, then widen via SetTimeout.
	shortActual, err := dw.Start(20*time.Millisecond, ModeReset)
	if err != nil {
		t.Fatal(err)
	}
	if got := dw.Timeout(); got != shortActual {
		t.Fatalf("Timeout after Start = %v, want %v", got, shortActual)
	}
	hw.setCounter(10)
	kicksBefore := hw.kicks

	wideActual, err := dw.SetTimeout(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if wideActual < time.Second || dw.Timeout() != wideActual {
		t.Fatalf("Timeout after SetTimeout = %v (returned %v)", dw.Timeout(), wideActual)
	}
	if hw.kicks != kicksBefore+1 || hw.ccvr <= 10 {
		t.Fatalf("SetTimeout did not reload counter: kicks=%d->%d ccvr=%d", kicksBefore, hw.kicks, hw.ccvr)
	}
}

func TestSupervisorKickNowExtendAndPlainKicker(t *testing.T) {
	var nilSup *Supervisor
	if nilSup.Started() || nilSup.Period() != 0 {
		t.Fatal("nil Supervisor should report Started=false Period=0")
	}
	if err := nilSup.KickNow(); !errors.Is(err, ErrNilAccess) {
		t.Fatalf("nil KickNow = %v, want ErrNilAccess", err)
	}
	if _, err := nilSup.Extend(time.Second); !errors.Is(err, ErrNilAccess) {
		t.Fatalf("nil Extend = %v, want ErrNilAccess", err)
	}

	// Unarmed supervisor rejects KickNow / Extend cleanly.
	stub := &stubKicker{}
	supStub, err := NewSupervisor(stub, SupervisorConfig{Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if supStub.Started() || supStub.Period() != 0 {
		t.Fatalf("unstarted Started=%t Period=%v", supStub.Started(), supStub.Period())
	}
	if err := supStub.KickNow(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("unstarted KickNow = %v, want ErrNotRunning", err)
	}
	if _, err := supStub.Extend(time.Second); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("unstarted Extend = %v, want ErrNotRunning", err)
	}

	// Plain Kicker without TimeoutSetter returns ErrUnsupported on Extend after Start.
	if _, err := supStub.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer supStub.Stop()
	if !supStub.Started() || supStub.Period() != 50*time.Millisecond {
		t.Fatalf("Started=%t Period=%v", supStub.Started(), supStub.Period())
	}
	if _, err := supStub.Extend(time.Second); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("plain Kicker Extend = %v, want ErrUnsupported", err)
	}
	if err := supStub.KickNow(); err != nil {
		t.Fatalf("KickNow: %v", err)
	}
	if _, err := supStub.Extend(0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Extend(0) = %v, want ErrInvalidConfig", err)
	}

	// *DW supports Extend, never shrinks below active Period, and works after Stop().
	hw := newFakeDW()
	dw, err := Open(hw, 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	sup, err := NewSupervisor(dw, SupervisorConfig{Timeout: 200 * time.Millisecond, KickInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := sup.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Requesting a shorter minTimeout preserves the longer active period.
	kept, err := sup.Extend(time.Millisecond)
	if err != nil {
		t.Fatalf("Extend(1ms): %v", err)
	}
	if kept != actual {
		t.Fatalf("Extend shrank timeout from %v to %v", actual, kept)
	}
	// Stop halts goroutine, yet sticky hardware servicing via Extend + KickNow continues working.
	sup.Stop()
	if !sup.Started() {
		t.Fatal("Started became false after Stop on sticky WDT")
	}
	extended, err := sup.Extend(2 * time.Second)
	if err != nil || extended < 2*time.Second || sup.Period() != extended {
		t.Fatalf("Extend(2s) = (%v, %v) period=%v", extended, err, sup.Period())
	}
	kBefore := sup.Kicks()
	if err := sup.KickNow(); err != nil || sup.Kicks() != kBefore+1 {
		t.Fatalf("post-Stop KickNow = %v kicks=%d->%d", err, kBefore, sup.Kicks())
	}
}

func TestShutdownPrepareAndWrapShutdownStepsWithPSCI(t *testing.T) {
	ctx := context.Background()

	// 1. Nil and unarmed supervisors stay no-ops across ShutdownPrepareStep + WrapShutdownSteps.
	ctrl := &fakePowerCtrl{}
	unarmedHW := newFakeDW()
	unarmedDW, err := Open(unarmedHW, 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	unarmedSup, err := NewSupervisor(unarmedDW, SupervisorConfig{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	steps := WrapShutdownSteps(nil, []psci.ShutdownStep{
		ShutdownPrepareStep(unarmedSup, 2*time.Second),
		{Name: "flush", Fn: func(context.Context) error { return nil }},
	})
	if err := psci.Shutdown(ctx, ctrl, psci.ActionPowerOff, psci.ShutdownConfig{Steps: steps}); err != nil {
		t.Fatalf("unarmed psci.Shutdown: %v", err)
	}
	if unarmedDW.Enabled() || ctrl.offs != 1 {
		t.Fatalf("unarmed shutdown enabled=%t offs=%d", unarmedDW.Enabled(), ctrl.offs)
	}

	// 2. Armed *DW Supervisor stops keepalive, extends timeout, kicks around teardown steps, then resets.
	hw := newFakeDW()
	dw, err := Open(hw, 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	sup, err := NewSupervisor(dw, SupervisorConfig{Timeout: 50 * time.Millisecond, KickInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sup.Start(ctx); err != nil {
		t.Fatal(err)
	}

	var observedRunning bool
	var kicksAtStep uint64
	teardownSteps := []psci.ShutdownStep{
		ShutdownPrepareStep(sup, 5*time.Second),
	}
	teardownSteps = append(teardownSteps, WrapShutdownSteps(sup, []psci.ShutdownStep{
		{
			Name: "quiesce-sd",
			Fn: func(context.Context) error {
				observedRunning = sup.Running()
				kicksAtStep = sup.Kicks()
				return nil
			},
		},
	})...)

	ctrl2 := &fakePowerCtrl{}
	if err := psci.Shutdown(ctx, ctrl2, psci.ActionReset, psci.ShutdownConfig{Steps: teardownSteps}); err != nil {
		t.Fatalf("armed psci.Shutdown: %v", err)
	}
	if observedRunning {
		t.Fatal("Supervisor still running during teardown step after ShutdownPrepareStep")
	}
	if dw.Timeout() < 5*time.Second {
		t.Fatalf("WDT timeout not extended before teardown: %v", dw.Timeout())
	}
	if sup.Kicks() <= kicksAtStep || ctrl2.resets != 1 {
		t.Fatalf("post-step kick or PSCI reset missing: kicks=%d->%d resets=%d", kicksAtStep, sup.Kicks(), ctrl2.resets)
	}

	// 3. Plain Kicker satisfies minTimeout <= Period(), rejects unattainable minTimeout > Period(), and rejects negative minTimeout.
	stub := &stubKicker{}
	supPlain, err := NewSupervisor(stub, SupervisorConfig{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supPlain.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ShutdownPrepareStep(supPlain, 50*time.Millisecond).Fn(ctx); err != nil {
		t.Fatalf("plain Kicker ShutdownPrepareStep(50ms): %v", err)
	}
	if err := ShutdownPrepareStep(supPlain, time.Second).Fn(ctx); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("plain Kicker ShutdownPrepareStep(1s) = %v, want ErrUnsupported", err)
	}
	if err := ShutdownPrepareStep(supPlain, -time.Second).Fn(ctx); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative minTimeout = %v, want ErrInvalidConfig", err)
	}

	// 4. Unattainable *DW shutdown timeout (exceeding MaxTop ~21.47s at 100 MHz) fails closed.
	hwMax := newFakeDW()
	dwMax, err := Open(hwMax, 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	supMax, err := NewSupervisor(dwMax, SupervisorConfig{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supMax.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ShutdownPrepareStep(supMax, 30*time.Second).Fn(ctx); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unattainable 30s ShutdownPrepareStep = %v, want ErrInvalidConfig", err)
	}
	if supMax.Running() {
		t.Fatal("ShutdownPrepareStep did not stop supervisor even after unattainable timeout")
	}

	// 5. Invalid steps preserved unwrapped so psci.Shutdown returns ErrInvalidShutdown.
	bad := WrapShutdownSteps(sup, []psci.ShutdownStep{{Name: "", Fn: nil}})
	if err := psci.Shutdown(ctx, ctrl2, psci.ActionReset, psci.ShutdownConfig{Steps: bad}); !errors.Is(err, psci.ErrInvalidShutdown) {
		t.Fatalf("invalid wrapped step = %v, want ErrInvalidShutdown", err)
	}
}

type orderedSetter struct {
	mu      sync.Mutex
	cur     time.Duration
	history []time.Duration
}

func (o *orderedSetter) Start(timeout time.Duration, mode ResponseMode) (time.Duration, error) {
	o.mu.Lock()
	o.cur = timeout
	o.mu.Unlock()
	return timeout, nil
}

func (o *orderedSetter) Kick() error { return nil }

func (o *orderedSetter) SetTimeout(timeout time.Duration) (time.Duration, error) {
	// Briefly yield to widen the race window between concurrent Extend callers.
	time.Sleep(time.Millisecond)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cur = timeout
	o.history = append(o.history, timeout)
	return timeout, nil
}

func TestExtendSerializationAndKickBeforeStop(t *testing.T) {
	// 1. Concurrent Extend callers stay serialized so a smaller request never overwrites a larger one.
	dev := &orderedSetter{}
	sup, err := NewSupervisor(dev, SupervisorConfig{Timeout: 50 * time.Millisecond, KickInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer sup.Stop()

	var wg sync.WaitGroup
	for _, d := range []time.Duration{5 * time.Second, 10 * time.Second, 2 * time.Second, 8 * time.Second} {
		d := d
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sup.Extend(d); err != nil {
				t.Errorf("Extend(%v): %v", d, err)
			}
		}()
	}
	wg.Wait()

	dev.mu.Lock()
	hist := append([]time.Duration(nil), dev.history...)
	cur := dev.cur
	dev.mu.Unlock()
	for i := 1; i < len(hist); i++ {
		if hist[i] < hist[i-1] {
			t.Fatalf("non-monotonic hardware timeout sequence: %v", hist)
		}
	}
	if cur != sup.Period() || cur != 10*time.Second {
		t.Fatalf("hardware cur=%v != Period=%v != 10s", cur, sup.Period())
	}

	// 2. ShutdownPrepareStep extends/kicks hardware BEFORE draining a slow in-flight Check.
	hw := newFakeDW()
	dw, err := Open(hw, 100_000_000)
	if err != nil {
		t.Fatal(err)
	}
	inCheck := make(chan struct{})
	releaseCheck := make(chan struct{})
	slowSup, err := NewSupervisor(dw, SupervisorConfig{
		Timeout:      20 * time.Millisecond,
		KickInterval: 5 * time.Millisecond,
		Check: func() error {
			select {
			case <-inCheck:
			default:
				close(inCheck)
			}
			<-releaseCheck
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := slowSup.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-inCheck

	prepDone := make(chan error, 1)
	go func() {
		prepDone <- ShutdownPrepareStep(slowSup, 2*time.Second).Fn(context.Background())
	}()

	// Even before releaseCheck unblocks Stop(), hardware must already have been extended!
	deadline := time.Now().Add(time.Second)
	for dw.Timeout() < 2*time.Second {
		if time.Now().After(deadline) {
			close(releaseCheck)
			t.Fatalf("watchdog was not extended before Stop() drained slow Check (timeout=%v)", dw.Timeout())
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseCheck)
	if err := <-prepDone; err != nil {
		t.Fatalf("ShutdownPrepareStep: %v", err)
	}
}
