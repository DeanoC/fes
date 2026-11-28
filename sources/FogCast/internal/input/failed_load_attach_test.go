package input

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

// A failed load leaves a valid execution lease but no running core. Releasing
// that lease must not have to clean up an input stream that was never valid.
func TestFailedLoadRejectsInputAndLeaseReleases(t *testing.T) {
	var writes atomic.Int32
	var listeners atomic.Int32
	pad := &failedLoadPad{writes: &writes}
	ports := &controllerPortsSink{fallback: pad}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	controller.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{IdleConfirmed: true, RuntimeInstance: strings.Repeat("a", 32)}, nil
	})
	controller.listen = func(network, address string) (net.Listener, error) {
		listeners.Add(1)
		return net.Listen(network, address)
	}
	t.Cleanup(func() { _ = controller.Close() })
	manager := kitlease.New(time.Minute, controller.ReleaseAll)
	t.Cleanup(manager.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	grant, err := manager.Claim(kitlease.ClaimRequest{RequestID: strings.Repeat("a", 32), Owner: "test", Purpose: "failed load"})
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, done, err := manager.Begin(grant.Token)
	if err != nil {
		t.Fatal(err)
	}
	attachCtx := kitlease.WithAdmission(kitlease.WithLeaseLifetime(leaseCtx, leaseCtx))
	err = controller.Attach(attachCtx, Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.pc386"})
	done()
	if !errors.Is(err, errNoCore) {
		t.Fatalf("input attach after failed load = %v, want no core", err)
	}
	if _, err := controller.OpenStream(context.Background(), 1); err == nil {
		t.Fatal("failed load exposed an input stream")
	}
	if listeners.Load() != 0 || writes.Load() != 0 {
		t.Fatalf("failed load started bridge or wrote input: listeners=%d writes=%d", listeners.Load(), writes.Load())
	}
	if _, err := manager.Release(grant.Token); err != nil {
		t.Fatal(err)
	}
	status, err := manager.Settled(ctx)
	if err != nil || status.State != "free" {
		t.Fatalf("lease after failed load = %+v, %v", status, err)
	}
}

type failedLoadPad struct{ writes *atomic.Int32 }

func (s *failedLoadPad) Apply(protocol.InputFrame) error {
	s.writes.Add(1)
	return errors.New("unexpected input")
}
func (*failedLoadPad) ReleaseAll() error { return nil }
func (*failedLoadPad) Close() error      { return nil }

func TestAttachRechecksObservedCoreBeforeReuseOrReplacement(t *testing.T) {
	ports := &controllerPortsSink{fallback: &recordingSink{}}
	controller := newTargetControllerWithSink("127.0.0.1:0", ports)
	controller.ports = ports
	var observation CoreObservation
	controller.ObserveCore(func(context.Context) (CoreObservation, error) { return observation, nil })
	t.Cleanup(func() { _ = controller.Close() })
	spec := Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.pc386"}
	observation = CoreObservation{Active: true, CoreID: spec.Core}
	if err := controller.Attach(context.Background(), spec); err != nil {
		t.Fatalf("matching active core: %v", err)
	}
	observation = CoreObservation{IdleConfirmed: true}
	if err := controller.Attach(context.Background(), spec); !errors.Is(err, errNoCore) {
		t.Fatalf("same-session attach after stop = %v, want no core", err)
	}
	observation = CoreObservation{Active: true, CoreID: "fes.zx81"}
	if err := controller.Attach(context.Background(), spec); !errors.Is(err, errNoCore) {
		t.Fatalf("mismatched active core = %v, want no core", err)
	}
	// A rejected attachment request must not tear down the existing bridge.
	conn, err := controller.OpenStream(context.Background(), spec.Session)
	if err != nil {
		t.Fatalf("rejected attach destroyed existing bridge: %v", err)
	}
	_ = conn.Close()
}

func TestReattachDoesNotSucceedAfterObservationLosesAuthority(t *testing.T) {
	for _, cancelTarget := range []string{"request", "lease"} {
		t.Run(cancelTarget, func(t *testing.T) {
			ports := &controllerPortsSink{fallback: &recordingSink{}}
			controller := newTargetControllerWithSink("127.0.0.1:0", ports)
			controller.ports = ports
			t.Cleanup(func() { _ = controller.Close() })
			observation := CoreObservation{Active: true, CoreID: "fes.pc386"}
			controller.ObserveCore(func(context.Context) (CoreObservation, error) { return observation, nil })
			lifetime, cancelLifetime := context.WithCancel(context.Background())
			defer cancelLifetime()
			spec := Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "fes.pc386"}
			if err := controller.Attach(kitlease.WithLeaseLifetime(context.Background(), lifetime), spec); err != nil {
				t.Fatal(err)
			}
			entered, resume := make(chan struct{}), make(chan struct{})
			controller.ObserveCore(func(context.Context) (CoreObservation, error) {
				close(entered)
				<-resume
				return observation, nil
			})
			request, cancelRequest := context.WithCancel(context.Background())
			defer cancelRequest()
			done := make(chan error, 1)
			go func() { done <- controller.Attach(kitlease.WithLeaseLifetime(request, lifetime), spec) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("runtime observation did not start")
			}
			if cancelTarget == "request" {
				cancelRequest()
			} else {
				cancelLifetime()
			}
			close(resume)
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("reattach after %s ended = %v", cancelTarget, err)
				}
			case <-time.After(time.Second):
				t.Fatal("reattach did not finish")
			}
		})
	}
}
