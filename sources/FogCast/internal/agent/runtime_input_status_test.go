package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type runtimeInputTestRuntime struct {
	observe func(context.Context) (*protocol.RuntimeInputIdentity, error)
}

func (*runtimeInputTestRuntime) Health(string) protocol.Health             { return protocol.Health{} }
func (*runtimeInputTestRuntime) Reconcile(context.Context) protocol.Status { return protocol.Status{} }
func (*runtimeInputTestRuntime) LoadDevelopmentRBF(context.Context, int64, io.Reader) (string, bool, *protocol.APIError) {
	return "", false, nil
}
func (*runtimeInputTestRuntime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "", nil
}
func (*runtimeInputTestRuntime) Stop(context.Context) (string, *protocol.APIError) { return "", nil }
func (r *runtimeInputTestRuntime) ObserveRuntimeInput(ctx context.Context) (*protocol.RuntimeInputIdentity, error) {
	return r.observe(ctx)
}

func TestFreshRuntimeInputStatusRejectsStaleAndBusy(t *testing.T) {
	identity := &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDescribed, RuntimeInstance: strings.Repeat("a", 32), PackageID: strings.Repeat("b", 64), Generation: 7}
	calls := 0
	r := &runtimeInputTestRuntime{observe: func(context.Context) (*protocol.RuntimeInputIdentity, error) { calls++; return identity, nil }}
	c := New(r, 0, 0)
	c.set(protocol.Status{State: protocol.StateActive, CorePackage: &protocol.CorePackageStatus{PackageID: identity.PackageID, Generation: identity.Generation}, RuntimeInput: identity})
	got := c.StatusWithRuntimeInput(context.Background())
	if got.RuntimeInput == nil || *got.RuntimeInput != *identity || calls != 1 {
		t.Fatalf("valid observation %+v calls %d", got.RuntimeInput, calls)
	}
	got.RuntimeInput.Generation = 99
	if c.Status().RuntimeInput.Generation != 7 {
		t.Fatal("status clone leaked identity pointer")
	}
	r.observe = func(context.Context) (*protocol.RuntimeInputIdentity, error) {
		mismatch := *identity
		mismatch.Generation++
		return &mismatch, nil
	}
	if got = c.StatusWithRuntimeInput(context.Background()); got.RuntimeInput != nil {
		t.Fatal("different generation was relabelled as current")
	}
	r.observe = func(context.Context) (*protocol.RuntimeInputIdentity, error) { return nil, errors.New("unavailable") }
	if got = c.StatusWithRuntimeInput(context.Background()); got.RuntimeInput != nil {
		t.Fatal("failed observation reused cached identity")
	}
	if !c.begin() {
		t.Fatal("failed to simulate transition")
	}
	got = c.StatusWithRuntimeInput(context.Background())
	c.end()
	if got.RuntimeInput != nil {
		t.Fatal("transition exposed an identity")
	}
}

func TestFreshRuntimeInputStatusSupportsExplicitDevelopmentAndDeadline(t *testing.T) {
	identity := &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 9}
	r := &runtimeInputTestRuntime{observe: func(context.Context) (*protocol.RuntimeInputIdentity, error) { return identity, nil }}
	c := New(r, 0, 0)
	c.set(protocol.Status{State: protocol.StateActive, Development: true})
	if got := c.StatusWithRuntimeInput(context.Background()); got.RuntimeInput == nil || got.RuntimeInput.Kind != protocol.RuntimeInputDevelopment {
		t.Fatal("direct-development identity unavailable")
	}
	r.observe = func(ctx context.Context) (*protocol.RuntimeInputIdentity, error) { <-ctx.Done(); return nil, ctx.Err() }
	start := time.Now()
	got := c.StatusWithRuntimeInput(context.Background())
	if got.RuntimeInput != nil || time.Since(start) > time.Second {
		t.Fatal("fresh status deadline failed")
	}
}

func TestFreshRuntimeInputStatusRejectsConcurrentStatusChangeAndCancellation(t *testing.T) {
	identity := &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 9}
	started, release := make(chan struct{}), make(chan struct{})
	r := &runtimeInputTestRuntime{observe: func(ctx context.Context) (*protocol.RuntimeInputIdentity, error) {
		close(started)
		select {
		case <-release:
			return identity, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	c := New(r, 0, 0)
	c.set(protocol.Status{State: protocol.StateActive, Development: true})
	result := make(chan protocol.Status, 1)
	go func() { result <- c.StatusWithRuntimeInput(context.Background()) }()
	<-started
	c.set(protocol.Status{State: protocol.StateIdle})
	close(release)
	if got := <-result; got.State != protocol.StateIdle || got.RuntimeInput != nil {
		t.Fatalf("published stale identity after status changed: %+v", got)
	}
	c.set(protocol.Status{State: protocol.StateActive, Development: true})
	r.observe = func(ctx context.Context) (*protocol.RuntimeInputIdentity, error) { return identity, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := c.StatusWithRuntimeInput(ctx); got.RuntimeInput != nil {
		t.Fatal("published identity from canceled observation")
	}
}

func TestFreshRuntimeInputObservationDoesNotBlockControl(t *testing.T) {
	identity := &protocol.RuntimeInputIdentity{Kind: protocol.RuntimeInputDevelopment, RuntimeInstance: strings.Repeat("a", 32), Generation: 9}
	started, release := make(chan struct{}), make(chan struct{})
	r := &runtimeInputTestRuntime{observe: func(context.Context) (*protocol.RuntimeInputIdentity, error) {
		close(started)
		<-release
		return identity, nil
	}}
	c := New(r, time.Second, time.Second)
	c.set(protocol.Status{State: protocol.StateActive, Development: true})
	result := make(chan protocol.Status, 1)
	go func() { result <- c.StatusWithRuntimeInput(context.Background()) }()
	<-started
	if _, err := c.Stop(context.Background()); err != nil {
		t.Fatalf("control rejected while read was waiting: %v", err)
	}
	// Even an unchanged-looking status cannot make a completed transition's
	// in-flight observation valid again.
	c.set(protocol.Status{State: protocol.StateActive, Development: true})
	close(release)
	if got := <-result; got.RuntimeInput != nil {
		t.Fatal("observation spanning completed control exposed identity")
	}
}

func TestStatusClonePreservesExplicitEmptyInterfaceList(t *testing.T) {
	status:=protocol.Status{CorePackage:&protocol.CorePackageStatus{ActiveInterfaces:[]protocol.RuntimeInterface{}}}
	if cloneStatus(status).CorePackage.ActiveInterfaces==nil {t.Fatal("explicit empty interface list became unknown")}
	status.CorePackage.ActiveInterfaces=nil
	if cloneStatus(status).CorePackage.ActiveInterfaces!=nil {t.Fatal("unknown interface list became explicit")}
}
