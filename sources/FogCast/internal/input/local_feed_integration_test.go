package input

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/ui/localfeed"
)

// This joins the real Feed transport and target reader with net.Pipe. The
// supplied snapshot is simulated: no keyboard device or kit is involved.
func TestLocalFeedRecoverySnapshotHeldInputAndReset(t *testing.T) {
	f := newV2BackendFixture(t)
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
	if ready, err := f.c.ObserveLocalStartupRecovery(context.Background()); err != nil || !ready {
		t.Fatalf("startup recovery = %v, %v", ready, err)
	}
	// The listener normally arms this guard before handing a reader the socket.
	f.c.localV2Guard.Store(true)

	reader, writer := net.Pipe()
	var dialed bool
	feed := localfeed.New("in-memory", func(_, _ string, _ time.Duration) (net.Conn, error) {
		if dialed {
			return nil, errors.New("unexpected second dial")
		}
		dialed = true
		return writer, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer feed.Close()
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); f.c.readLocal(ctx, reader, true) }()

	recovered, handle, err := feed.RecoverV2(ctx, f.req.AgentInstance, "test-kit", "test-boot", time.Second)
	if err != nil || recovered.Status != "reader-drained" {
		t.Fatalf("recover = %+v, %v", recovered, err)
	}
	if err := feed.PromoteRecovered(handle, recovered); err != nil {
		t.Fatal(err)
	}
	obsMu.Lock()
	obs = CoreObservation{Active: true, RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration,
		KeyboardHID: &KeyboardHIDBinding{RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}}
	obsMu.Unlock()

	begin := v2BeginFor(f)
	begin.Devices[0].Held = []uint8{0xe1} // physical Left Shift at the snapshot boundary
	ready, err := feed.RequestV2(ctx, handle, begin)
	if err != nil || ready.Status != "ready" {
		t.Fatalf("begin = %+v, %v", ready, err)
	}
	edge := localhid.Request{Op: "edge", Token: ready.Token, Device: localhid.DeviceID{1}, Usage: 0xe1, EventSeq: 1, Down: true}
	request := func(req localhid.Request, want string) {
		t.Helper()
		reply, err := feed.RequestV2(ctx, handle, req)
		if err != nil || reply.Status != want {
			t.Fatalf("%s = %+v, %v; want %s", req.Op, reply, err, want)
		}
	}
	request(edge, "accepted")
	f.mu.Lock()
	initialWrites := len(f.rows)
	f.mu.Unlock()
	if initialWrites != 0 {
		t.Fatal("snapshot-held Shift repeat reached the guest")
	}
	edge.EventSeq, edge.Down = 2, false
	request(edge, "accepted")
	edge.EventSeq, edge.Down = 3, true
	request(edge, "accepted")
	edge.EventSeq, edge.Usage = 4, 0x25 // physical 8 while Shift is held
	request(edge, "accepted")
	f.mu.Lock()
	combined := f.rows[len(f.rows)-1]
	f.mu.Unlock()
	if combined[8]&2 == 0 || combined[2]&(1<<5) == 0 {
		t.Fatalf("held Shift and 8 not combined: %+v", combined)
	}
	edge.EventSeq, edge.Down = 5, false
	request(edge, "accepted")
	request(localhid.Request{Op: "reset", Token: ready.Token, LastEventSeq: 5}, "neutral-confirmed")
	f.mu.Lock()
	neutral := f.rows[len(f.rows)-1]
	f.mu.Unlock()
	if neutral[8]&2 != 0 || neutral[2]&(1<<5) != 0 {
		t.Fatalf("reset left a local key held: %+v", neutral)
	}
	feed.CloseV2(handle)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("target reader did not drain")
	}
}
