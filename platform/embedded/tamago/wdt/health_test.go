// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompositeCheckAndSupervisorIntegration(t *testing.T) {
	sentinel := errors.New("subsystem wedged")
	var calls1, calls2 atomic.Int32
	var failFirst atomic.Bool

	check := CompositeCheck(
		HealthProbe{Name: ""},
		HealthProbe{
			Name: "first",
			Check: func() error {
				calls1.Add(1)
				if failFirst.Load() {
					return sentinel
				}
				return nil
			},
		},
		HealthProbe{
			Name: "second",
			Check: func() error {
				calls2.Add(1)
				return nil
			},
		},
	)
	if err := check(); err != nil {
		t.Fatalf("healthy CompositeCheck: %v", err)
	}
	if calls1.Load() != 1 || calls2.Load() != 1 {
		t.Fatalf("calls=(%d,%d), want (1,1)", calls1.Load(), calls2.Load())
	}

	// Failure short-circuits remaining probes and wraps the root error.
	failFirst.Store(true)
	err := check()
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), `"first"`) {
		t.Fatalf("failing CompositeCheck err=%v, want wrapped %v with name", err, sentinel)
	}
	if calls2.Load() != 1 {
		t.Fatalf("second probe called (%d) after first failed", calls2.Load())
	}

	// Unnamed failing probe gets positional label.
	unnamed := CompositeCheck(HealthProbe{Check: func() error { return sentinel }})
	if uErr := unnamed(); !errors.Is(uErr, sentinel) || !strings.Contains(uErr.Error(), `"#0"`) {
		t.Fatalf("unnamed probe err=%v", uErr)
	}

	// Integration with Supervisor: failing probe halts kicks and invokes OnError.
	failFirst.Store(false)
	var errCh = make(chan error, 1)
	var devCalls atomic.Int32
	kicker := &fakeHealthKicker{kickCalls: &devCalls}
	sup, err := NewSupervisor(kicker, SupervisorConfig{
		Timeout:      60 * time.Millisecond,
		KickInterval: 10 * time.Millisecond,
		Mode:         ModeReset,
		Check:        check,
		OnError: func(e error) {
			select {
			case errCh <- e:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	if _, err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for sup.Kicks() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if sup.Kicks() < 2 {
		sup.Stop()
		t.Fatalf("expected >= 2 kicks before fault, got %d", sup.Kicks())
	}

	failFirst.Store(true)
	select {
	case gotErr := <-errCh:
		if !errors.Is(gotErr, sentinel) {
			sup.Stop()
			t.Fatalf("OnError=%v, want wrapped %v", gotErr, sentinel)
		}
	case <-time.After(time.Second):
		sup.Stop()
		t.Fatal("timed out waiting for supervisor OnError on failing health probe")
	}
	sup.Stop()
	if !errors.Is(sup.Err(), sentinel) {
		t.Fatalf("sup.Err()=%v, want wrapped %v", sup.Err(), sentinel)
	}
}

type fakeHealthKicker struct {
	kickCalls *atomic.Int32
}

func (k *fakeHealthKicker) Start(timeout time.Duration, _ ResponseMode) (time.Duration, error) {
	return timeout, nil
}

func (k *fakeHealthKicker) Kick() error {
	k.kickCalls.Add(1)
	return nil
}
