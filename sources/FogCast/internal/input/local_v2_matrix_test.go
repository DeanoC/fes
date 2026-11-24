package input

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type v2MatrixFixture struct {
	c       *TargetController
	manager *kitlease.Manager
	token   string
	req     localhid.Request
	posts   []uint64
	deny    bool
}

func newV2MatrixFixture(t *testing.T, packageID string) *v2MatrixFixture {
	t.Helper()
	m := kitlease.New(time.Minute, func(context.Context) error { return nil })
	t.Cleanup(m.Close)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := m.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	grant, err := m.ClaimLocal()
	if err != nil {
		t.Fatal(err)
	}
	f := &v2MatrixFixture{manager: m, token: grant.Token, req: localhid.Request{Version: 2, Op: "begin", Request: 1, Publisher: strings.Repeat("1", 32), QueueEpoch: 1, LeaseGeneration: grant.Status.Generation, RuntimeInstance: strings.Repeat("a", 32), PackageID: packageID, CoreGeneration: 7, Devices: []localhid.DeviceSnapshot{{ID: localhid.DeviceID{1}, Held: []uint8{}}}}}
	keys := NewKeyboardSink()
	keys.SetPoster(func(ctx context.Context, post localhid.MatrixPost, matrix uint64) error {
		if !post.Witness.Valid() {
			return errors.New("invalid witness")
		}
		if err := post.Validate(ctx); err != nil {
			return err
		}
		f.posts = append(f.posts, matrix)
		return nil
	})
	ports := &controllerPortsSink{keys: keys}
	f.c = newTargetControllerWithSink("", ports)
	f.c.ports = ports
	f.c.keyboard = keys
	f.c.SetLocalLeaseManager(m)
	f.req.AgentInstance, _ = f.c.LocalAgentInstance()
	f.c.SetKeyboardMatrixAdmission(func(ctx context.Context, instance, id string, generation uint64, validate func(context.Context) error) error {
		if f.deny {
			return errors.New("adapter unconfirmed")
		}
		if instance != f.req.RuntimeInstance || id != f.req.PackageID || generation != f.req.CoreGeneration {
			return errNoCore
		}
		return validate(ctx)
	})
	f.c.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}, nil
	})
	return f
}
func TestLocalV2MatrixAdmitsExactLocalHostAndSerializesEdges(t *testing.T) {
	for _, id := range []string{strings.Repeat("b", 64), ""} {
		t.Run(map[bool]string{true: "direct-development", false: "described"}[id == ""], func(t *testing.T) {
			f := newV2MatrixFixture(t, id)
			session := localhid.NewSourceSession(delegatedV2Backend{f.c})
			f.deny = true
			if _, err := session.Handle(context.Background(), f.req); err == nil {
				t.Fatal("fenced adapter admitted")
			}
			f.deny = false
			f.req.Request = 2
			f.req.QueueEpoch++
			begin, err := session.Handle(context.Background(), f.req)
			if err != nil || begin.Status != "ready" {
				t.Fatalf("begin %+v %v", begin, err)
			}
			edge := localhid.Request{Version: 2, Op: "edge", Request: 3, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
			if reply, err := session.Handle(context.Background(), edge); err != nil || reply.Status != "accepted" {
				t.Fatalf("press %+v %v", reply, err)
			}
			edge.Request = 4
			edge.EventSeq = 2
			edge.Down = false
			if reply, err := session.Handle(context.Background(), edge); err != nil || reply.Status != "accepted" {
				t.Fatalf("release %+v %v", reply, err)
			}
			if len(f.posts) != 2 || f.posts[0] == zx81keys.Neutral || f.posts[1] != zx81keys.Neutral {
				t.Fatalf("ordered matrix posts %v", f.posts)
			}
			reset := localhid.Request{Version: 2, Op: "reset", Request: 5, Token: begin.Token, LastEventSeq: 2}
			if reply, err := session.Handle(context.Background(), reset); err != nil || reply.Status != "neutral-confirmed" {
				t.Fatalf("reset %+v %v", reply, err)
			}
		})
	}
}
func TestLocalV2MatrixRejectsOldInstanceAndCanceledLifecycle(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	backend := delegatedV2Backend{f.c}
	old := f.req
	old.RuntimeInstance = strings.Repeat("c", 32)
	if _, err := backend.Open(context.Background(), old); err == nil {
		t.Fatal("old runtime admitted")
	}
	old = f.req
	old.AgentInstance = strings.Repeat("f", 32)
	if _, err := backend.Open(context.Background(), old); err == nil {
		t.Fatal("old agent admitted")
	}
	f.c.lifecycle.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := backend.Open(ctx, f.req)
	f.c.lifecycle.Unlock()
	if err == nil {
		t.Fatal("canceled lifecycle admitted")
	}
	if len(f.posts) != 0 {
		t.Fatal("rejected admission posted")
	}
}

func TestLocalV2MatrixFreshSnapshotRetainsHeldSuppression(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	f.req.Devices[0].Held = []uint8{4}
	f.c.ports.keys.blocked[sourceLocal] = map[remoteinput.Code]bool{zx81keys.Letter('A'): true}
	session := localhid.NewSourceSession(delegatedV2Backend{f.c})
	begin, err := session.Handle(context.Background(), f.req)
	if err != nil || begin.Status != "ready" {
		t.Fatalf("begin %+v %v", begin, err)
	}
	edge := localhid.Request{Version: 2, Op: "edge", Request: 2, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if reply, err := session.Handle(context.Background(), edge); err != nil || reply.Status != "accepted" {
		t.Fatalf("repeat %+v %v", reply, err)
	}
	if len(f.posts) != 0 {
		t.Fatal("snapshot-held repeat posted")
	}
	edge.Request = 3
	edge.EventSeq = 2
	edge.Down = false
	if _, err := session.Handle(context.Background(), edge); err != nil {
		t.Fatal(err)
	}
	edge.Request = 4
	edge.EventSeq = 3
	edge.Down = true
	if _, err := session.Handle(context.Background(), edge); err != nil {
		t.Fatal(err)
	}
	if len(f.posts) != 1 || f.posts[0] == zx81keys.Neutral {
		t.Fatalf("fresh press was not posted: %v", f.posts)
	}
}

func TestLocalV2MatrixLostBeginAckAndReconnectHeldKey(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	testEnableRecovery(t, f.c)
	listener := &memoryListener{incoming: make(chan net.Conn, 2), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = f.c.serveLocalConnections(ctx, listener) }()
	first, peer := net.Pipe()
	listener.incoming <- peer
	testRecoverConnection(t, f.c, first)
	f.req.Devices[0].Held = []uint8{4}
	f.req.Request = 2
	line, err := localhid.EncodeRequest(f.req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write(line); err != nil {
		t.Fatal(err)
	}
	_ = first.Close() // BEGIN may have committed, but its ACK cannot be received.
	second, peer2 := net.Pipe()
	defer second.Close()
	listener.incoming <- peer2
	testRecoverConnection(t, f.c, second)
	next := f.req
	next.Publisher = strings.Repeat("2", 32)
	next.QueueEpoch = 2
	begin := v2Exchange(t, second, next)
	if begin.Status != "ready" {
		t.Fatalf("successor %+v", begin)
	}
	before := len(f.posts)
	rawCode, _ := hidkeys.Code(4)
	raw := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1}, Seq: 1, Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Action: uint8(remoteinput.ActionPress), Code: uint16(rawCode)}
	if err := bridge.WriteFrame(second, raw); err != nil {
		t.Fatal(err)
	}
	edge := localhid.Request{Version: 2, Op: "edge", Request: 3, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if reply := v2Exchange(t, second, edge); reply.Status != "accepted" {
		t.Fatal(reply)
	}
	if len(f.posts) != before {
		t.Fatalf("held repeat or stale raw key posted: %v", f.posts)
	}
	edge.Request = 4
	edge.EventSeq = 2
	edge.Down = false
	if reply := v2Exchange(t, second, edge); reply.Status != "accepted" {
		t.Fatal(reply)
	}
	edge.Request = 5
	edge.EventSeq = 3
	edge.Down = true
	if reply := v2Exchange(t, second, edge); reply.Status != "accepted" {
		t.Fatal(reply)
	}
	if len(f.posts) != before+1 {
		t.Fatal("fresh press after release did not post")
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

func TestLocalV2MatrixQueueReservationSurvivesFailureAndLeaseChange(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	backend := delegatedV2Backend{f.c}
	f.deny = true
	if _, err := backend.Open(context.Background(), f.req); err == nil {
		t.Fatal("expected denied admission")
	}
	f.deny = false
	if _, err := backend.Open(context.Background(), f.req); err == nil {
		t.Fatal("failed BEGIN epoch was reused")
	}
	f.req.QueueEpoch++
	if _, err := backend.Open(context.Background(), f.req); err != nil {
		t.Fatal(err)
	}
	old := f.req
	if _, err := f.manager.Release(f.token); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := f.manager.Settled(ctx); err != nil {
		t.Fatal(err)
	}
	grant, err := f.manager.ClaimLocal()
	if err != nil {
		t.Fatal(err)
	}
	f.req.LeaseGeneration = grant.Status.Generation
	f.req.QueueEpoch = 1
	if _, err := backend.Open(context.Background(), f.req); err != nil {
		t.Fatalf("new lease rejected: %v", err)
	}
	if _, err := backend.Open(context.Background(), old); err == nil {
		t.Fatal("old lease admitted after successor")
	}
}
func TestLocalV2MatrixConcurrentQueueAdmissionOnlyOnce(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	backend := delegatedV2Backend{f.c}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := backend.Open(context.Background(), f.req); results <- err }()
	}
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("expected one admission: %v %v", first, second)
	}
}

func TestMatrixLocalQueuedTransitionFencesBeforeWrite(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	obs := CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	if err := f.c.ports.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	sink := f.c.ports.keys
	sink.mu.Lock()
	code, _ := hidkeys.Code(4)
	frame := protocol.InputFrame{Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Action: uint8(remoteinput.ActionPress), Code: uint16(code)}
	result := make(chan error, 1)
	go func() { result <- f.c.deliverLocal(context.Background(), frame) }()
	deadline := time.After(time.Second)
	for {
		if !f.c.lifecycle.TryLock() {
			break
		}
		f.c.lifecycle.Unlock()
		select {
		case <-deadline:
			sink.mu.Unlock()
			t.Fatal("local frame did not acquire lifecycle")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	finish := f.c.beginLifecycleTransition()
	sink.mu.Unlock()
	err := <-result
	finish()
	if err == nil {
		t.Fatal("local matrix input crossed queued transition")
	}
	if len(f.posts) != 0 {
		t.Fatal("queued transition allowed a post")
	}
}

func TestLocalV2MatrixRejectsSamePublisherEpochAcrossEOF(t *testing.T) {
	f := newV2MatrixFixture(t, strings.Repeat("b", 64))
	testEnableRecovery(t, f.c)
	listener := &memoryListener{incoming: make(chan net.Conn, 2), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = f.c.serveLocalConnections(ctx, listener) }()
	first, peer := net.Pipe()
	listener.incoming <- peer
	testRecoverConnection(t, f.c, first)
	f.req.Request = 2
	line, err := localhid.EncodeRequest(f.req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Write(line); err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	second, peer2 := net.Pipe()
	defer second.Close()
	listener.incoming <- peer2
	testRecoverConnection(t, f.c, second)
	if reply := v2Exchange(t, second, f.req); reply.Status != "quarantined" {
		t.Fatalf("replayed epoch admitted %+v", reply)
	}
	fresh := f.req
	fresh.Request = 3
	fresh.QueueEpoch++
	if reply := v2Exchange(t, second, fresh); reply.Status != "ready" {
		t.Fatalf("fresh epoch denied %+v", reply)
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
