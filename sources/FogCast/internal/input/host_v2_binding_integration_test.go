package input

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// memoryTargetBridge connects a real host RemoteInput to the target bridge
// using only in-memory pipes. It does not simulate the host launch coordinator.
type memoryTargetBridge struct {
	target   *TargetController
	listener *memoryListener
	session  uint64
	ready    chan struct{}
}

func (b *memoryTargetBridge) Ready() <-chan struct{} { return b.ready }
func (*memoryTargetBridge) Endpoint() string         { return "" }
func (b *memoryTargetBridge) Stop(ctx context.Context) error {
	return b.target.Detach(ctx, b.session)
}
func (b *memoryTargetBridge) Dial(ctx context.Context) (net.Conn, error) {
	client, peer := net.Pipe()
	select {
	case b.listener.incoming <- peer:
		return client, nil
	case <-ctx.Done():
		_ = client.Close()
		_ = peer.Close()
		return nil, ctx.Err()
	}
}

// This covers the real host input attachment and target v2 admission, but
// runtime status, physical snapshot and the HTTP lease guard are simulated.
func TestHostInputAttachBindsTargetBeforeV2Begin(t *testing.T) {
	f := newV2BackendFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.c.SetLocalTargetIdentity("test-kit", "test-boot"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.EnableLocalInputRecovery(); err != nil {
		t.Fatal(err)
	}
	var obsMu sync.Mutex
	obs := CoreObservation{IdleConfirmed: true, RuntimeInstance: f.req.RuntimeInstance}
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) {
		obsMu.Lock()
		defer obsMu.Unlock()
		return obs, nil
	})
	if ready, err := f.c.ObserveLocalStartupRecovery(ctx); err != nil || !ready {
		t.Fatalf("initial idle = %v, %v", ready, err)
	}
	f.c.localV2Guard.Store(true) // the actual listener arms this before Accept
	// Remove the test fixture's convenience binding and grant. The host's
	// target attachment and admitted grant must establish both from scratch.
	f.c.ports.mu.Lock()
	f.c.ports.hidBinding = nil
	f.c.ports.keyboardHID = false
	f.c.ports.mu.Unlock()
	f.c.ports.hid.bind(nil)
	f.c.mu.Lock()
	f.c.localKeyboardGrant = nil
	f.c.mu.Unlock()
	binding := KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	obsMu.Lock()
	obs = CoreObservation{Active: true, RuntimeInstance: binding.RuntimeInstance, PackageID: binding.PackageID, Generation: binding.Generation, KeyboardHID: &binding}
	obsMu.Unlock()
	listener := &memoryListener{incoming: make(chan net.Conn, 1), done: make(chan struct{})}
	f.c.listen = func(string, string) (net.Listener, error) { return listener, nil }
	var attached *memoryTargetBridge
	starter := host.BridgeStarterFunc(func(_ context.Context, spec host.BridgeSpec) (host.BridgeHandle, error) {
		leaseCtx, done, err := f.manager.BeginLocalInput(f.req.LeaseGeneration)
		if err != nil {
			return nil, err
		}
		defer done()
		if err := f.c.Attach(kitlease.WithAdmission(kitlease.WithLeaseLifetime(leaseCtx, leaseCtx)), Spec{Session: spec.Session, Token: spec.Token, Core: spec.Core}); err != nil {
			return nil, err
		}
		attached = &memoryTargetBridge{target: f.c, listener: listener, session: spec.Session, ready: make(chan struct{})}
		close(attached.ready)
		return attached, nil
	})
	remote, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter, HeartbeatInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if err := remote.AttachWithMouse(ctx, "fes.c64", false, false, false, false); err != nil {
		t.Fatalf("host attach: %v", err)
	}
	if status := remote.Status(); status.State != host.RemoteInputAttached || !status.Ready {
		t.Fatalf("host input not attached: %+v", status)
	}
	f.c.ports.hid.mu.Lock()
	bound := f.c.ports.hid.binding != nil && *f.c.ports.hid.binding == binding
	f.c.ports.hid.mu.Unlock()
	if !bound {
		t.Fatal("host target attachment did not bind HID")
	}
	leaseCtx, done, err := f.manager.BeginLocalInput(f.req.LeaseGeneration)
	if err != nil {
		t.Fatal(err)
	}
	err = f.c.GrantLocalKeyboard(kitlease.WithAdmission(leaseCtx), attached.session, binding)
	done()
	if err != nil {
		t.Fatalf("admitted physical keyboard grant: %v", err)
	}
	reader, writer := net.Pipe()
	feed := localfeed.New("in-memory", func(string, string, time.Duration) (net.Conn, error) { return writer, nil })
	defer feed.Close()
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	readerDone := make(chan struct{})
	go func() { defer close(readerDone); f.c.readLocal(ctx, reader, true) }()
	recovered, handle, err := feed.RecoverV2(ctx, f.req.AgentInstance, "test-kit", "test-boot", time.Second)
	if err != nil || recovered.Status != "reader-drained" {
		t.Fatalf("recovery = %+v, %v", recovered, err)
	}
	if err := feed.PromoteRecovered(handle, recovered); err != nil {
		t.Fatal(err)
	}
	begin, err := feed.RequestV2(ctx, handle, v2BeginFor(f))
	if err != nil || begin.Status != "ready" {
		t.Fatalf("v2 begin = %+v, %v", begin, err)
	}
	edge := localhid.Request{Op: "edge", Token: begin.Token, Device: localhid.DeviceID{1}, Usage: 0xe1, EventSeq: 1, Down: true}
	if reply, err := feed.RequestV2(ctx, handle, edge); err != nil || reply.Status != "accepted" {
		t.Fatalf("v2 Shift press = %+v, %v", reply, err)
	}
	f.mu.Lock()
	pressed := len(f.rows) > 0 && f.rows[len(f.rows)-1][8]&2 != 0
	f.mu.Unlock()
	if !pressed {
		t.Fatal("v2 Shift did not reach the attached HID binding")
	}
	if reset, err := feed.RequestV2(ctx, handle, localhid.Request{Op: "reset", Token: begin.Token, LastEventSeq: 1}); err != nil || reset.Status != "neutral-confirmed" {
		t.Fatalf("reset = %+v, %v", reset, err)
	}
	f.mu.Lock()
	neutral := len(f.rows) > 0 && f.rows[len(f.rows)-1][8]&2 == 0
	f.mu.Unlock()
	if !neutral {
		t.Fatal("v2 reset left Shift held")
	}
	feed.CloseV2(handle)
	select {
	case <-readerDone:
	case <-ctx.Done():
		t.Fatal("local reader did not drain")
	}
}
