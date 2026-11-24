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
	if err := Shutdown(canceled, ctrl, ActionReset, ShutdownConfig{Steps: []ShutdownStep{step}}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled ctx ran step or wrong err: err=%v called=%t", err, called)
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
