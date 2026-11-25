// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

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

	// Request above hardware maximum clamps to MaxTop (~21.47s at 100 MHz).
	maxDur, err := DurationForTop(clk, MaxTop)
	if err != nil {
		t.Fatal(err)
	}
	clamped, err := dw.SetTimeout(60 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if clamped != maxDur || dw.Timeout() != maxDur || hw.torr != uint32(MaxTop)<<4|uint32(MaxTop) {
		t.Fatalf("SetTimeout(60s) = %v (torr=%#x), want MaxTop %v", clamped, hw.torr, maxDur)
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

func TestExtendSerialization(t *testing.T) {
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
}
