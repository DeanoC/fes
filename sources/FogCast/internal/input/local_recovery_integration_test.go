package input

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type recoveryAcceptListener struct {
	*memoryListener
	accepted chan bool
	guard    *TargetController
}

func (l *recoveryAcceptListener) Accept() (net.Conn, error) {
	select {
	case l.accepted <- l.guard.localV2Guard.Load():
	default:
	}
	return l.memoryListener.Accept()
}

func TestLocalRecoveryModeFrozenBeforeAccept(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "enabled"}[enabled], func(t *testing.T) {
			c := NewTargetController()
			if enabled {
				if err := c.EnableLocalInputRecovery(); err != nil {
					t.Fatal(err)
				}
			}
			l := &recoveryAcceptListener{memoryListener: &memoryListener{incoming: make(chan net.Conn), done: make(chan struct{})}, accepted: make(chan bool, 1), guard: c}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := make(chan struct{})
			go func() { defer close(stopped); _ = c.serveLocalConnections(ctx, l) }()
			select {
			case armed := <-l.accepted:
				if armed != enabled {
					t.Fatalf("guard at first Accept = %v, want %v", armed, enabled)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("listener did not reach Accept")
			}
			if err := c.EnableLocalInputRecovery(); err == nil {
				t.Fatal("mode changed after listener started")
			}
			cancel()
			_ = l.Close()
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("listener failed to stop")
			}
		})
	}

	starting := &TargetController{localStarting: true}
	if err := starting.EnableLocalInputRecovery(); err == nil {
		t.Fatal("mode changed while listener was starting")
	}
	closed := &TargetController{localClosed: true}
	if err := closed.EnableLocalInputRecovery(); err == nil {
		t.Fatal("closed controller accepted recovery")
	}
}

func TestDisabledRecoveryGateSameSocketPromotion(t *testing.T) {
	f := newV2MatrixFixture(t, "")
	if err := f.c.SetLocalTargetIdentity("kit-b", "boot-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.EnableLocalInputRecovery(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	obs := CoreObservation{IdleConfirmed: true, RuntimeInstance: f.req.RuntimeInstance}
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) { mu.Lock(); defer mu.Unlock(); return obs, nil })
	listener := &memoryListener{incoming: make(chan net.Conn, 2), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = f.c.serveLocalConnections(ctx, listener) }()
	client, peer := net.Pipe()
	defer client.Close()
	listener.incoming <- peer
	// BEGIN cannot bypass a fresh recovery proof.
	f.req.Request = 1
	if reply := v2Exchange(t, client, f.req); reply.Status != "quarantined" {
		t.Fatalf("premature begin %+v", reply)
	}
	recover := localhid.Request{Version: 2, Op: "recover", Request: 2, AgentInstance: f.req.AgentInstance, TargetID: "kit-b", BootID: "boot-1"}
	if reply := v2Exchange(t, client, recover); reply.Status != "reader-drained" || reply.AgentInstance != f.req.AgentInstance {
		t.Fatalf("recovery %+v", reply)
	}
	mu.Lock()
	obs = CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	mu.Unlock()
	// Raw keyboard stays fenced even after positive recovery.
	code, _ := hidkeys.Code(4)
	raw := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1}, Seq: 1, Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Action: uint8(remoteinput.ActionPress), Code: uint16(code)}
	if err := bridge.WriteFrame(client, raw); err != nil {
		t.Fatal(err)
	}
	f.req.Request = 3
	begin := v2Exchange(t, client, f.req)
	if begin.Status != "ready" {
		t.Fatalf("begin %+v", begin)
	}
	if len(f.posts) != 0 {
		t.Fatal("raw keyboard bypassed recovery fence")
	}
	edge := localhid.Request{Version: 2, Op: "edge", Request: 4, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if reply := v2Exchange(t, client, edge); reply.Status != "accepted" {
		t.Fatalf("edge %+v", reply)
	}
	if len(f.posts) != 1 {
		t.Fatal("v2 edge not delivered")
	}
	// A new connection may not inherit this connection's recovery result.
	_ = client.Close()
	other, peer2 := net.Pipe()
	defer other.Close()
	listener.incoming <- peer2
	next := f.req
	next.Request = 1
	next.QueueEpoch++
	if reply := v2Exchange(t, other, next); reply.Status != "quarantined" {
		t.Fatalf("reconnect inherited recovery %+v", reply)
	}
	_ = other.Close()
	cancel()
	_ = listener.Close()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("listener failed to drain")
	}
}

func TestDisabledRecoveryGateChangedAgentWaitsForOldCore(t *testing.T) {
	f := newV2MatrixFixture(t, " ")
	// Use a valid described package; the previous agent ID is intentionally different.
	f.req.PackageID = strings.Repeat("b", 64)
	if err := f.c.SetLocalTargetIdentity("kit-b", "boot-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.EnableLocalInputRecovery(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	obs := CoreObservation{Active: true, Keyboard: true, RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) { mu.Lock(); defer mu.Unlock(); return obs, nil })
	listener := &memoryListener{incoming: make(chan net.Conn, 2), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = f.c.serveLocalConnections(ctx, listener) }()
	oldAgent := strings.Repeat("f", 32)
	first, peer := net.Pipe()
	listener.incoming <- peer
	req := localhid.Request{Version: 2, Op: "recover", Request: 1, AgentInstance: oldAgent, TargetID: "kit-b", BootID: "boot-1"}
	if reply := v2Exchange(t, first, req); reply.Status != "unavailable" {
		t.Fatalf("old core still active %+v", reply)
	}
	_ = first.Close()
	mu.Lock()
	obs = CoreObservation{IdleConfirmed: true, RuntimeInstance: f.req.RuntimeInstance}
	mu.Unlock()
	second, peer2 := net.Pipe()
	defer second.Close()
	listener.incoming <- peer2
	if reply := v2Exchange(t, second, req); reply.Status != "startup-retired" || reply.AgentInstance != f.req.AgentInstance {
		t.Fatalf("retired startup %+v", reply)
	}
	_ = second.Close()
	cancel()
	_ = listener.Close()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("listener failed to drain")
	}
}
