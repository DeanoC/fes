package agent

import (
	"context"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type blockedIdleRuntime struct{ entered, release chan struct{} }

func (*blockedIdleRuntime) HealthContext(context.Context, string) protocol.Health {
	return protocol.Health{Ready: true}
}
func (*blockedIdleRuntime) Reconcile(context.Context) protocol.Status {
	return protocol.Status{State: protocol.StateFailed}
}
func (*blockedIdleRuntime) LoadDevelopmentRBF(context.Context, int64, io.Reader) (string, bool, *protocol.APIError) {
	return "", false, nil
}
func (*blockedIdleRuntime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "", nil
}
func (*blockedIdleRuntime) Stop(context.Context) (string, *protocol.APIError) { return "", nil }
func (r *blockedIdleRuntime) ConfirmIdle(context.Context) bool {
	close(r.entered)
	<-r.release
	return true
}

func TestHealthCancellationWhileWaitingForStateLock(t *testing.T) {
	rt := &blockedIdleRuntime{entered: make(chan struct{}), release: make(chan struct{})}
	c := New(rt, time.Second, time.Second)
	c.status = protocol.Status{State: protocol.StateFailed}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan protocol.Health, 1)
	go func() { result <- c.HealthContext(ctx, "test") }()
	<-rt.entered
	c.mu.Lock()
	close(rt.release)
	deadline := time.Now().Add(time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		for _, stack := range strings.Split(string(buf[:n]), "\n\n") {
			if strings.Contains(stack, "(*Coordinator).HealthContext") && strings.Contains(stack, "sync.(*RWMutex).Lock") {
				blocked = true
				break
			}
		}
		if blocked {
			break
		}
		runtime.Gosched()
	}
	if !blocked {
		c.mu.Unlock()
		t.Fatal("health did not block waiting for coordinator lock")
	}
	cancel()
	c.mu.Unlock()
	select {
	case health := <-result:
		if health.Ready || c.Status().State != protocol.StateFailed {
			t.Fatalf("canceled result recovered idle: %+v %+v", health, c.Status())
		}
	case <-time.After(time.Second):
		t.Fatal("health did not exit")
	}
}
