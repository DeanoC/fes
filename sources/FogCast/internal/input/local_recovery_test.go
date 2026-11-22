package input

import (
	"bytes"
	"context"
	"errors"
	"github.com/DeanoC/FogCast/internal/localhid"
	"strings"
	"testing"
)

func TestLocalStartupRecoveryWaitsForObservedOldCoreRetirement(t *testing.T) {
	old := CoreObservation{Active: true, RuntimeInstance: strings.Repeat("a", 32), PackageID: strings.Repeat("b", 64), Generation: 7}
	current := old
	c := &TargetController{localEntropy: bytes.NewReader(bytes.Repeat([]byte{1}, 16))}
	c.ObserveCore(func(context.Context) (CoreObservation, error) { return current, nil })
	if ready, err := c.ObserveLocalStartupRecovery(context.Background()); ready || !errors.Is(err, errLocalRecoveryPending) {
		t.Fatalf("active startup: %v %v", ready, err)
	}
	if ready, err := c.ObserveLocalStartupRecovery(context.Background()); ready || !errors.Is(err, errLocalRecoveryPending) {
		t.Fatalf("same binding: %v %v", ready, err)
	}
	current = CoreObservation{RuntimeInstance: old.RuntimeInstance}
	if ready, err := c.ObserveLocalStartupRecovery(context.Background()); ready || !errors.Is(err, errLocalRecoveryPending) {
		t.Fatalf("unknown is not idle: %v %v", ready, err)
	}
	current = CoreObservation{RuntimeInstance: old.RuntimeInstance, IdleConfirmed: true}
	if ready, err := c.ObserveLocalStartupRecovery(context.Background()); !ready || err != nil {
		t.Fatalf("confirmed idle: %v %v", ready, err)
	}
}
func TestLocalStartupRecoveryUnknownThenActiveStillWaits(t *testing.T) {
	current := CoreObservation{}
	c := &TargetController{localEntropy: bytes.NewReader(bytes.Repeat([]byte{2}, 16))}
	c.ObserveCore(func(context.Context) (CoreObservation, error) { return current, nil })
	if ready, _ := c.ObserveLocalStartupRecovery(context.Background()); ready {
		t.Fatal("unknown became ready")
	}
	current = CoreObservation{Active: true, RuntimeInstance: strings.Repeat("a", 32), Generation: 7}
	if ready, _ := c.ObserveLocalStartupRecovery(context.Background()); ready {
		t.Fatal("first active observation skipped quarantine")
	}
	current.RuntimeInstance = strings.Repeat("c", 32)
	if ready, err := c.ObserveLocalStartupRecovery(context.Background()); !ready || err != nil {
		t.Fatalf("reconstructed runtime: %v %v", ready, err)
	}
}
func TestLocalStartupRecoveryInitiallyIdleAndCanceled(t *testing.T) {
	c := &TargetController{localEntropy: bytes.NewReader(bytes.Repeat([]byte{3}, 16))}
	c.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{IdleConfirmed: true, RuntimeInstance: strings.Repeat("a", 32)}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ready, err := c.ObserveLocalStartupRecovery(ctx); ready || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v %v", ready, err)
	}
	if ready, err := c.ObserveLocalStartupRecovery(context.Background()); !ready || err != nil {
		t.Fatalf("initial idle: %v %v", ready, err)
	}
}

func TestLocalRecoveryReplyBindsTargetBootAndAgent(t *testing.T) {
	c := &TargetController{localEntropy: bytes.NewReader(bytes.Repeat([]byte{4}, 16))}
	if err := c.SetLocalTargetIdentity("kit-b", "boot-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetLocalTargetIdentity("kit-a", "boot-1"); err == nil {
		t.Fatal("changed target accepted")
	}
	instance, err := c.LocalAgentInstance()
	if err != nil {
		t.Fatal(err)
	}
	obs := CoreObservation{Active: true, RuntimeInstance: strings.Repeat("a", 32), PackageID: strings.Repeat("b", 64), Generation: 7}
	c.ObserveCore(func(context.Context) (CoreObservation, error) { return obs, nil })
	req := localhid.Request{Version: 2, Op: "recover", Request: 1, AgentInstance: instance, TargetID: "kit-b", BootID: "boot-1"}
	reply, err := c.localRecoveryReply(context.Background(), req, false)
	if err != nil || reply.Status != "unavailable" {
		t.Fatalf("active startup %+v %v", reply, err)
	}
	obs = CoreObservation{IdleConfirmed: true, RuntimeInstance: strings.Repeat("a", 32)}
	reply, err = c.localRecoveryReply(context.Background(), req, true)
	if err != nil || reply.Status != "unavailable" {
		t.Fatalf("busy source %+v %v", reply, err)
	}
	reply, err = c.localRecoveryReply(context.Background(), req, false)
	if err != nil || reply.Status != "reader-drained" || reply.AgentInstance != instance {
		t.Fatalf("same agent %+v %v", reply, err)
	}
	req.AgentInstance = strings.Repeat("f", 32)
	reply, err = c.localRecoveryReply(context.Background(), req, false)
	if err != nil || reply.Status != "startup-retired" {
		t.Fatalf("new agent %+v %v", reply, err)
	}
	req.TargetID = "other"
	if _, err := c.localRecoveryReply(context.Background(), req, false); err == nil {
		t.Fatal("wrong target accepted")
	}
}

func TestLocalRecoveryNeverGrantsAfterControllerClose(t *testing.T) {
	c := &TargetController{localEntropy: bytes.NewReader(bytes.Repeat([]byte{5}, 16))}
	if err := c.SetLocalTargetIdentity("kit-b", "boot-1"); err != nil {
		t.Fatal(err)
	}
	instance, err := c.LocalAgentInstance()
	if err != nil {
		t.Fatal(err)
	}
	entered, resume := make(chan struct{}), make(chan struct{})
	c.ObserveCore(func(context.Context) (CoreObservation, error) {
		close(entered)
		<-resume
		return CoreObservation{IdleConfirmed: true, RuntimeInstance: strings.Repeat("a", 32)}, nil
	})
	req := localhid.Request{Version: 2, Op: "recover", Request: 1, AgentInstance: instance, TargetID: "kit-b", BootID: "boot-1"}
	done := make(chan localhid.Reply, 1)
	go func() { reply, _ := c.localRecoveryReply(context.Background(), req, false); done <- reply }()
	<-entered
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	close(resume)
	if reply := <-done; reply.Status != "unavailable" {
		t.Fatalf("closed controller granted %+v", reply)
	}
	if ready, _ := c.ObserveLocalStartupRecovery(context.Background()); ready {
		t.Fatal("closed fast path granted readiness")
	}
}
