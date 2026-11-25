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

	// 3. Plain Kicker falls back cleanly to KickNow when minTimeout > 0, and negative minTimeout fails closed.
	stub := &stubKicker{}
	supPlain, err := NewSupervisor(stub, SupervisorConfig{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supPlain.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ShutdownPrepareStep(supPlain, time.Second).Fn(ctx); err != nil {
		t.Fatalf("plain Kicker ShutdownPrepareStep: %v", err)
	}
	if err := ShutdownPrepareStep(supPlain, -time.Second).Fn(ctx); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("negative minTimeout = %v, want ErrInvalidConfig", err)
	}

	// 4. Invalid steps preserved unwrapped so psci.Shutdown returns ErrInvalidShutdown.
	bad := WrapShutdownSteps(sup, []psci.ShutdownStep{{Name: "", Fn: nil}})
	if err := psci.Shutdown(ctx, ctrl2, psci.ActionReset, psci.ShutdownConfig{Steps: bad}); !errors.Is(err, psci.ErrInvalidShutdown) {
		t.Fatalf("invalid wrapped step = %v, want ErrInvalidShutdown", err)
	}
}
