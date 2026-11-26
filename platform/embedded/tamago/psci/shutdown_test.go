// SPDX-License-Identifier: GPL-3.0-or-later

package psci

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakePowerController struct {
	resetCalls int
	offCalls   int
	resetErr   error
	offErr     error
	events     *[]string
}

func (f *fakePowerController) SystemReset() error {
	f.resetCalls++
	if f.events != nil {
		*f.events = append(*f.events, "psci:reset")
	}
	return f.resetErr
}

func (f *fakePowerController) SystemOff() error {
	f.offCalls++
	if f.events != nil {
		*f.events = append(*f.events, "psci:poweroff")
	}
	return f.offErr
}

func TestShutdownValidationFailsClosed(t *testing.T) {
	ctrl := &fakePowerController{}
	called := false
	step := ShutdownStep{Name: "s", Fn: func(context.Context) error { called = true; return nil }}

	for _, tc := range []struct {
		name   string
		ctx    context.Context
		ctrl   PowerController
		action PowerAction
		cfg    ShutdownConfig
	}{
		{name: "nil-ctx", ctx: nil, ctrl: ctrl, action: ActionReset},
		{name: "nil-ctrl", ctx: context.Background(), ctrl: nil, action: ActionReset},
		{name: "bad-action", ctx: context.Background(), ctrl: ctrl, action: PowerAction(99)},
		{name: "negative-timeout", ctx: context.Background(), ctrl: ctrl, action: ActionReset, cfg: ShutdownConfig{StepTimeout: -time.Millisecond}},
		{name: "empty-step-name", ctx: context.Background(), ctrl: ctrl, action: ActionReset, cfg: ShutdownConfig{Steps: []ShutdownStep{{Name: "", Fn: step.Fn}}}},
		{name: "nil-step-fn", ctx: context.Background(), ctrl: ctrl, action: ActionPowerOff, cfg: ShutdownConfig{Steps: []ShutdownStep{{Name: "a", Fn: nil}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			if err := Shutdown(tc.ctx, tc.ctrl, tc.action, tc.cfg); !errors.Is(err, ErrInvalidShutdown) {
				t.Fatalf("err=%v want ErrInvalidShutdown", err)
			}
			if called || ctrl.resetCalls != 0 || ctrl.offCalls != 0 {
				t.Fatalf("side effects occurred: called=%t reset=%d off=%d", called, ctrl.resetCalls, ctrl.offCalls)
			}
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, cfg := range []ShutdownConfig{{Steps: []ShutdownStep{step}}, {}} {
		err := Shutdown(canceled, ctrl, ActionReset, cfg)
		if !errors.Is(err, ErrShutdownAborted) || !errors.Is(err, context.Canceled) || called {
			t.Fatalf("canceled ctx ran step or wrong err: steps=%d err=%v called=%t", len(cfg.Steps), err, called)
		}
		if ctrl.resetCalls != 0 || ctrl.offCalls != 0 {
			t.Fatalf("canceled ctx called PSCI: steps=%d reset=%d off=%d", len(cfg.Steps), ctrl.resetCalls, ctrl.offCalls)
		}
	}
}

func TestShutdownOrderedStepsAndPowerAction(t *testing.T) {
	for _, action := range []PowerAction{ActionReset, ActionPowerOff} {
		t.Run(action.String(), func(t *testing.T) {
			var events []string
			ctrl := &fakePowerController{events: &events}
			err := Shutdown(context.Background(), ctrl, action, ShutdownConfig{
				StepTimeout: 100 * time.Millisecond,
				Steps: []ShutdownStep{
					{Name: "stop-watchdog", Fn: func(ctx context.Context) error {
						if _, ok := ctx.Deadline(); !ok {
							t.Fatal("expected bounded deadline on step ctx")
						}
						events = append(events, "stop-watchdog")
						return nil
					}},
					{Name: "quiesce-sd", Fn: func(context.Context) error {
						events = append(events, "quiesce-sd")
						return nil
					}},
				},
			})
			if err != nil {
				t.Fatalf("Shutdown(%s): %v", action, err)
			}
			wantPSCI := "psci:" + action.String()
			want := []string{"stop-watchdog", "quiesce-sd", wantPSCI}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("events=%v want %v", events, want)
			}
		})
	}
}

func TestShutdownRequireCleanTeardownRunsAllStepsBeforeAborting(t *testing.T) {
	var events []string
	var reported []string
	ctrl := &fakePowerController{events: &events}
	boom := errors.New("park timeout")

	err := Shutdown(context.Background(), ctrl, ActionReset, ShutdownConfig{
		RequireCleanTeardown: true,
		OnStepError: func(name string, e error) {
			reported = append(reported, name+":"+e.Error())
		},
		Steps: []ShutdownStep{
			{Name: "stop-watchdog", Fn: func(context.Context) error { events = append(events, "stop-watchdog"); return nil }},
			{Name: "park-fabric", Fn: func(context.Context) error { events = append(events, "park-fabric"); return boom }},
			{Name: "quiesce-sd", Fn: func(context.Context) error { events = append(events, "quiesce-sd"); return nil }},
		},
	})
	if !errors.Is(err, ErrTeardownFailed) || !errors.Is(err, boom) {
		t.Fatalf("err=%v want ErrTeardownFailed+boom", err)
	}
	if ctrl.resetCalls != 0 || ctrl.offCalls != 0 {
		t.Fatal("PSCI invoked despite RequireCleanTeardown failure")
	}
	wantEvents := []string{"stop-watchdog", "park-fabric", "quiesce-sd"}
	if !reflect.DeepEqual(events, wantEvents) || len(reported) != 1 || reported[0] != "park-fabric:park timeout" {
		t.Fatalf("events=%v reported=%v", events, reported)
	}
}

func TestShutdownTimeoutPanicRecoveryAndBestEffortPSCI(t *testing.T) {
	var events []string
	ctrl := &fakePowerController{events: &events, offErr: ErrUnsupported}
	hangCanceled := make(chan struct{})

	err := Shutdown(context.Background(), ctrl, ActionPowerOff, ShutdownConfig{
		StepTimeout: 20 * time.Millisecond,
		Steps: []ShutdownStep{
			{Name: "hang", Fn: func(ctx context.Context) error {
				<-ctx.Done()
				close(hangCanceled)
				return ctx.Err()
			}},
			{Name: "panic-step", Fn: func(context.Context) error {
				events = append(events, "panic-step")
				panic("usb close boom")
			}},
			{Name: "quiesce-sd", Fn: func(context.Context) error {
				events = append(events, "quiesce-sd")
				return nil
			}},
		},
	})
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err=%v want DeadlineExceeded+ErrUnsupported", err)
	}
	<-hangCanceled
	want := []string{"panic-step", "quiesce-sd", "psci:poweroff"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v want %v", events, want)
	}

	// Real Client with unsupported PSCI conduit returns ErrUnsupported after cleanup.
	realClient := New(&mockConduit{fn: func(uint64, uint64, uint64, uint64) (uint64, uint64, uint64, uint64) {
		return ^uint64(0), 0, 0, 0 // PSCI_RET_NOT_SUPPORTED
	}})
	events = nil
	err = Shutdown(context.Background(), realClient, ActionReset, ShutdownConfig{
		Steps: []ShutdownStep{{Name: "flush", Fn: func(context.Context) error { events = append(events, "flush"); return nil }}},
	})
	if !errors.Is(err, ErrUnsupported) || !reflect.DeepEqual(events, []string{"flush"}) {
		t.Fatalf("realClient err=%v events=%v", err, events)
	}
}

func TestShutdownEnforcesTimeoutOnNonCooperativeHook(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock)

	var events []string
	ctrl := &fakePowerController{events: &events}
	start := time.Now()
	err := Shutdown(context.Background(), ctrl, ActionReset, ShutdownConfig{
		StepTimeout: 25 * time.Millisecond,
		Steps: []ShutdownStep{
			{Name: "wedged-driver", Fn: func(context.Context) error {
				<-unblock // ignores ctx.Done() intentionally
				return nil
			}},
			{Name: "quiesce-sd", Fn: func(context.Context) error {
				events = append(events, "quiesce-sd")
				return nil
			}},
		},
	})
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v want context.DeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("non-cooperative hook blocked Shutdown for %v", elapsed)
	}
	want := []string{"quiesce-sd", "psci:reset"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v want %v", events, want)
	}

	strictCtrl := &fakePowerController{}
	strictErr := Shutdown(context.Background(), strictCtrl, ActionReset, ShutdownConfig{
		StepTimeout:          25 * time.Millisecond,
		RequireCleanTeardown: true,
		Steps: []ShutdownStep{
			{Name: "wedged-driver", Fn: func(context.Context) error {
				<-unblock
				return nil
			}},
		},
	})
	if !errors.Is(strictErr, ErrTeardownFailed) || !errors.Is(strictErr, context.DeadlineExceeded) {
		t.Fatalf("strictErr=%v want ErrTeardownFailed+DeadlineExceeded", strictErr)
	}
	if strictCtrl.resetCalls != 0 {
		t.Fatalf("resetCalls=%d want 0 on strict non-cooperative timeout", strictCtrl.resetCalls)
	}
}

// stepsStarted collects every step name sent on started, then keeps listening
// for grace so a step that a broken Shutdown started late, on a goroutine it
// never awaited, is caught too.
func stepsStarted(started <-chan string, grace time.Duration) []string {
	var names []string
	quiet := time.After(grace)
	for {
		select {
		case name := <-started:
			names = append(names, name)
		case <-quiet:
			return names
		}
	}
}

// A ctx cancelled between steps stops Shutdown at the next boundary, as a ctx
// cancelled on entry does: no later step starts, even late on a stray
// goroutine, and PSCI is never called, with or without RequireCleanTeardown.
func TestShutdownCtxCancelledMidSequenceStartsNoStepAndSkipsPSCI(t *testing.T) {
	names := []string{"stop-watchdog", "park-fabric", "flush-sd", "quiesce-usb"}
	for _, tc := range []struct {
		name     string
		action   PowerAction
		strict   bool
		cancelIn int // index of the step that cancels ctx and then returns nil
	}{
		{name: "reset", action: ActionReset, cancelIn: 1},
		{name: "poweroff-require-clean", action: ActionPowerOff, strict: true, cancelIn: 1},
		{name: "reset-cancel-in-last-step", action: ActionReset, cancelIn: len(names) - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan string, len(names))
			var steps []ShutdownStep
			for i, name := range names {
				steps = append(steps, ShutdownStep{Name: name, Fn: func(context.Context) error {
					started <- name
					if i == tc.cancelIn {
						cancel()
					}
					return nil
				}})
			}
			var reported []string
			ctrl := &fakePowerController{}
			err := Shutdown(ctx, ctrl, tc.action, ShutdownConfig{
				StepTimeout:          time.Minute,
				RequireCleanTeardown: tc.strict,
				OnStepError:          func(name string, e error) { reported = append(reported, name+":"+e.Error()) },
				Steps:                steps,
			})
			if ctrl.resetCalls != 0 || ctrl.offCalls != 0 {
				t.Errorf("PSCI called after ctx was cancelled: reset=%d off=%d", ctrl.resetCalls, ctrl.offCalls)
			}
			if got, want := stepsStarted(started, 50*time.Millisecond), names[:tc.cancelIn+1]; !reflect.DeepEqual(got, want) {
				t.Errorf("steps started=%v want %v", got, want)
			}
			// The cancelling step returned nil and was awaited, so it is not
			// misreported as cancelled.
			if len(reported) != 0 {
				t.Errorf("OnStepError reported %v, want none", reported)
			}
			if !errors.Is(err, ErrShutdownAborted) || !errors.Is(err, context.Canceled) || errors.Is(err, ErrTeardownFailed) {
				t.Errorf("err=%v want ErrShutdownAborted+context.Canceled and not ErrTeardownFailed", err)
			}
		})
	}
}

// When ctx's deadline passes while a step runs, the step sees its own context
// end and Shutdown waits for it to finish before returning; it then starts no
// later step and skips PSCI.
func TestShutdownCtxDeadlineAwaitsRunningStepThenAborts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	finished := make(chan struct{})
	started := make(chan string, 1)
	var reported []string
	ctrl := &fakePowerController{}
	start := time.Now()
	err := Shutdown(ctx, ctrl, ActionReset, ShutdownConfig{
		StepTimeout: time.Minute, // only ctx's deadline can end flush-sd early
		OnStepError: func(name string, e error) { reported = append(reported, name+":"+e.Error()) },
		Steps: []ShutdownStep{
			{Name: "flush-sd", Fn: func(ctx context.Context) error {
				<-ctx.Done()
				time.Sleep(10 * time.Millisecond) // cooperative wind-down after cancellation
				close(finished)
				return ctx.Err()
			}},
			{Name: "park-fabric", Fn: func(context.Context) error { started <- "park-fabric"; return nil }},
		},
	})
	elapsed := time.Since(start)
	select {
	case <-finished:
	default:
		t.Error("Shutdown returned while the step interrupted by ctx was still running")
	}
	if ctrl.resetCalls != 0 || ctrl.offCalls != 0 {
		t.Errorf("PSCI called after ctx deadline: reset=%d off=%d", ctrl.resetCalls, ctrl.offCalls)
	}
	if got := stepsStarted(started, 50*time.Millisecond); len(got) != 0 {
		t.Errorf("steps started after ctx deadline: %v", got)
	}
	if want := []string{"flush-sd:" + context.DeadlineExceeded.Error()}; !reflect.DeepEqual(reported, want) {
		t.Errorf("reported=%v want %v", reported, want)
	}
	if !errors.Is(err, ErrShutdownAborted) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err=%v want ErrShutdownAborted+context.DeadlineExceeded", err)
	}
	if elapsed > 30*time.Second {
		t.Errorf("ctx deadline did not cut the cooperative step short: %v", elapsed)
	}
}

// Cancelling ctx does not abandon a step that ignores its context: Shutdown
// waits for it up to StepTimeout, then still starts no later step and skips
// PSCI.
func TestShutdownCtxCancelBoundsNonCooperativeStepByStepTimeout(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 1)
	var reported []string
	ctrl := &fakePowerController{}
	const stepTimeout = 50 * time.Millisecond
	start := time.Now()
	err := Shutdown(ctx, ctrl, ActionPowerOff, ShutdownConfig{
		StepTimeout: stepTimeout,
		OnStepError: func(name string, e error) { reported = append(reported, name+":"+e.Error()) },
		Steps: []ShutdownStep{
			{Name: "wedged-driver", Fn: func(context.Context) error {
				cancel()
				<-unblock // ignores ctx.Done() intentionally
				return nil
			}},
			{Name: "park-fabric", Fn: func(context.Context) error { started <- "park-fabric"; return nil }},
		},
	})
	elapsed := time.Since(start)
	if ctrl.resetCalls != 0 || ctrl.offCalls != 0 {
		t.Errorf("PSCI called after ctx was cancelled: reset=%d off=%d", ctrl.resetCalls, ctrl.offCalls)
	}
	if got := stepsStarted(started, 50*time.Millisecond); len(got) != 0 {
		t.Errorf("steps started after ctx was cancelled: %v", got)
	}
	if elapsed < stepTimeout {
		t.Errorf("Shutdown abandoned the running step after %v, before StepTimeout %v", elapsed, stepTimeout)
	}
	if want := []string{"wedged-driver:" + context.DeadlineExceeded.Error()}; !reflect.DeepEqual(reported, want) {
		t.Errorf("reported=%v want %v", reported, want)
	}
	if !errors.Is(err, ErrShutdownAborted) || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err=%v want ErrShutdownAborted+context.Canceled+context.DeadlineExceeded", err)
	}
}

type fakeWarmController struct {
	fakePowerController
	warmSupported bool
	warmCalls     int
	warmType      uint32
	warmCook      uint64
	warmErr       error
}

func (f *fakeWarmController) SupportsWarmReset() bool {
	return f.warmSupported
}

func (f *fakeWarmController) SystemReset2(resetType uint32, cookie uint64) error {
	f.warmCalls++
	f.warmType = resetType
	f.warmCook = cookie
	if f.events != nil {
		*f.events = append(*f.events, "psci:warm-reset")
	}
	return f.warmErr
}

func TestShutdownActionWarmReset(t *testing.T) {
	if ActionWarmReset.String() != "warm-reset" {
		t.Fatalf("ActionWarmReset.String()=%q, want warm-reset", ActionWarmReset.String())
	}

	// Controller lacking WarmResetController, or firmware without SYSTEM_RESET2,
	// must fail closed BEFORE running any teardown step.
	stepRan := false
	step := ShutdownStep{Name: "flush", Fn: func(context.Context) error { stepRan = true; return nil }}
	plain := &fakePowerController{}
	if err := Shutdown(context.Background(), plain, ActionWarmReset, ShutdownConfig{Steps: []ShutdownStep{step}}); !errors.Is(err, ErrInvalidShutdown) || !errors.Is(err, ErrUnsupported) || stepRan {
		t.Fatalf("plain controller warm-reset err=%v stepRan=%t, want ErrInvalidShutdown+ErrUnsupported without running steps", err, stepRan)
	}

	clientNoReset2 := New(&mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
		switch uint32(x0) {
		case FuncVersion:
			return 0x00010001, 0, 0, 0 // PSCI 1.1
		case FuncFeatures:
			if uint32(x1) == FuncSystemReset2 {
				return ^uint64(0), 0, 0, 0 // NOT_SUPPORTED
			}
		case FuncSystemReset2:
			t.Fatal("SystemReset2 called when PSCI_FEATURES denied support")
		}
		return ^uint64(0), 0, 0, 0
	}})
	if err := Shutdown(context.Background(), clientNoReset2, ActionWarmReset, ShutdownConfig{Steps: []ShutdownStep{step}}); !errors.Is(err, ErrInvalidShutdown) || !errors.Is(err, ErrUnsupported) || stepRan {
		t.Fatalf("clientNoReset2 warm-reset err=%v stepRan=%t, want ErrInvalidShutdown+ErrUnsupported without running steps", err, stepRan)
	}

	var events []string
	warm := &fakeWarmController{
		fakePowerController: fakePowerController{events: &events},
		warmSupported:       true,
	}
	if err := Shutdown(context.Background(), warm, ActionWarmReset, ShutdownConfig{
		Steps: []ShutdownStep{{Name: "quiesce", Fn: func(context.Context) error { events = append(events, "step:quiesce"); return nil }}},
	}); err != nil {
		t.Fatalf("warm Shutdown err=%v", err)
	}
	if warm.warmCalls != 1 || warm.warmType != 0 || warm.warmCook != 0 ||
		!reflect.DeepEqual(events, []string{"step:quiesce", "psci:warm-reset"}) {
		t.Fatalf("warm Shutdown state: calls=%d type=%#x cook=%#x events=%v",
			warm.warmCalls, warm.warmType, warm.warmCook, events)
	}

	// Supported PSCI 1.1 *Client runs steps then SystemReset2(0, 0) and propagates runtime errors.
	clientReset2Calls := 0
	clientDenied := New(&mockConduit{fn: func(x0, x1, _, _ uint64) (uint64, uint64, uint64, uint64) {
		switch uint32(x0) {
		case FuncVersion:
			return 0x00010001, 0, 0, 0
		case FuncFeatures:
			if uint32(x1) == FuncSystemReset2 {
				return 0, 0, 0, 0
			}
		case FuncSystemReset2:
			clientReset2Calls++
			return uint64(^uint32(2)), 0, 0, 0 // -3 = DENIED
		}
		return ^uint64(0), 0, 0, 0
	}})
	err := Shutdown(context.Background(), clientDenied, ActionWarmReset, ShutdownConfig{Steps: []ShutdownStep{step}})
	if !errors.Is(err, ErrDenied) || !stepRan || clientReset2Calls != 1 {
		t.Fatalf("clientDenied warm Shutdown err=%v stepRan=%t calls=%d, want ErrDenied stepRan=true calls=1",
			err, stepRan, clientReset2Calls)
	}
}
