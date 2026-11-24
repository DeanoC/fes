package input

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// The observations and snapshot are simulated; net.Pipe connects the real
// Feed transport and target reader without an OS socket, device, or kit.
func TestLocalFeedChangedAgentWaitsForRetirementAndReleases(t *testing.T) {
	f := newV2BackendFixture(t)
	if err := f.c.SetLocalTargetIdentity("test-kit", "test-boot"); err != nil {
		t.Fatal(err)
	}
	if err := f.c.EnableLocalInputRecovery(); err != nil {
		t.Fatal(err)
	}
	var obsMu sync.Mutex
	active := CoreObservation{Active: true, RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration,
		KeyboardHID: &KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}}
	obs := active
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) {
		obsMu.Lock()
		defer obsMu.Unlock()
		return obs, nil
	})
	if ready, _ := f.c.ObserveLocalStartupRecovery(context.Background()); ready {
		t.Fatal("old active core counted as retired")
	}
	f.c.localV2Guard.Store(true) // normally armed by the listener before Accept
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dials := make(chan net.Conn, 2)
	feed := localfeed.New("in-memory", func(_, _ string, _ time.Duration) (net.Conn, error) {
		select {
		case conn := <-dials:
			return conn, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	defer feed.Close()
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	startReader := func() <-chan struct{} {
		t.Helper()
		reader, writer := net.Pipe()
		dials <- writer
		done := make(chan struct{})
		go func() { defer close(done); f.c.readLocal(ctx, reader, true) }()
		return done
	}
	await := func(done <-chan struct{}) {
		t.Helper()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("target reader did not drain")
		}
	}
	oldAgent := strings.Repeat("f", 32)
	if oldAgent == f.req.AgentInstance {
		t.Fatal("test requires distinct agent instances")
	}
	first := startReader()
	if _, _, err := feed.RecoverV2(ctx, oldAgent, "test-kit", "test-boot", time.Second); err == nil {
		t.Fatal("changed agent recovered while old core was active")
	}
	await(first)
	f.mu.Lock()
	writes := len(f.rows)
	f.mu.Unlock()
	if writes != 0 {
		t.Fatal("unavailable recovery dispatched keyboard input")
	}

	obsMu.Lock()
	obs = CoreObservation{IdleConfirmed: true, RuntimeInstance: f.req.RuntimeInstance}
	obsMu.Unlock()
	second := startReader()
	reply, handle, err := feed.RecoverV2(ctx, oldAgent, "test-kit", "test-boot", time.Second)
	if err != nil || reply.Status != "startup-retired" || reply.AgentInstance != f.req.AgentInstance {
		t.Fatalf("retired recovery = %+v, %v", reply, err)
	}
	if err := feed.PromoteRecovered(handle, reply); err != nil {
		t.Fatal(err)
	}
	// A real launch after confirmed idle has a new core generation. The
	// fixture supplies its new admission and HID binding explicitly.
	f.req.CoreGeneration++
	successor := KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	f.c.ports.setObservation(CoreObservation{Active: true, RuntimeInstance: successor.RuntimeInstance, PackageID: successor.PackageID, Generation: successor.Generation, KeyboardHID: &successor})
	f.c.mu.Lock()
	f.c.localKeyboardGrant = &localKeyboardGrant{executionGeneration: f.req.LeaseGeneration, binding: successor}
	f.c.mu.Unlock()
	obsMu.Lock()
	obs = CoreObservation{Active: true, RuntimeInstance: successor.RuntimeInstance, PackageID: successor.PackageID, Generation: successor.Generation, KeyboardHID: &successor}
	obsMu.Unlock()
	begin := v2BeginFor(f)
	begin.CoreGeneration = successor.Generation
	ready, err := feed.RequestV2(ctx, handle, begin)
	if err != nil || ready.Status != "ready" {
		t.Fatalf("begin = %+v, %v", ready, err)
	}
	edge := localhid.Request{Op: "edge", Token: ready.Token, Device: localhid.DeviceID{1}, Usage: 0xe1, EventSeq: 1, Down: true}
	if reply, err := feed.RequestV2(ctx, handle, edge); err != nil || reply.Status != "accepted" {
		t.Fatalf("press = %+v, %v", reply, err)
	}
	f.mu.Lock()
	pressed := len(f.rows) > 0 && f.rows[len(f.rows)-1][8]&2 != 0
	f.mu.Unlock()
	if !pressed {
		t.Fatal("Shift press did not reach target")
	}
	edge.EventSeq, edge.Down = 2, false
	if reply, err := feed.RequestV2(ctx, handle, edge); err != nil || reply.Status != "accepted" {
		t.Fatalf("release = %+v, %v", reply, err)
	}
	f.mu.Lock()
	released := len(f.rows) > 0 && f.rows[len(f.rows)-1][8]&2 == 0
	f.mu.Unlock()
	if !released {
		t.Fatal("Shift release did not reach target before reset")
	}
	if reply, err := feed.RequestV2(ctx, handle, localhid.Request{Op: "reset", Token: ready.Token, LastEventSeq: 2}); err != nil || reply.Status != "neutral-confirmed" {
		t.Fatalf("reset = %+v, %v", reply, err)
	}
	f.mu.Lock()
	neutral := len(f.rows) > 0 && f.rows[len(f.rows)-1][8]&2 == 0
	f.mu.Unlock()
	if !neutral {
		t.Fatal("Shift remained held after release and reset")
	}
	feed.CloseV2(handle)
	await(second)
}
