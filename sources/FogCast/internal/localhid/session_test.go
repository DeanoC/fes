package localhid

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeSourceBackend struct {
	opened        int
	openFn        func()
	edges         []Delta
	checks        int
	neutrals      int
	edgeErr       error
	edgeFn        func()
	neutralStatus string
	entered       chan struct{}
	resume        chan struct{}
}

func (f *fakeSourceBackend) Open(context.Context, Request) (any, error) {
	f.opened++
	if f.openFn != nil {
		f.openFn()
	}
	return "exact-binding", nil
}
func (f *fakeSourceBackend) Edge(ctx context.Context, b any, d *Delta) error {
	if b != "exact-binding" {
		return ErrRequest
	}
	f.checks++
	if f.entered != nil {
		close(f.entered)
		<-f.resume
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if d != nil {
		f.edges = append(f.edges, *d)
	}
	if f.edgeFn != nil {
		f.edgeFn()
	}
	return f.edgeErr
}

func TestSourceSessionCommittedEdgeSurvivesLateCancellation(t *testing.T) {
	b := &fakeSourceBackend{neutralStatus: "neutral-confirmed"}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{8}, 32))
	begin, err := s.Handle(context.Background(), sourceBegin(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.edgeFn = cancel // cancellation occurs after the backend's successful write
	reply, err := s.Handle(ctx, sourceEdge(begin.Token, 2, 1, true))
	if err != nil || reply.Status != "accepted" {
		t.Fatalf("committed edge: %+v %v", reply, err)
	}
	if status, err := s.Abort(context.Background()); err != nil || status != "neutral-confirmed" || b.neutrals != 1 {
		t.Fatalf("disconnect cleanup: %q %v", status, err)
	}
}
func (f *fakeSourceBackend) Barrier(_ context.Context, b any) error {
	if b != "exact-binding" {
		return ErrRequest
	}
	return nil
}
func (f *fakeSourceBackend) Neutral(_ context.Context, b any) (string, error) {
	if b != "exact-binding" {
		return "", ErrRequest
	}
	f.neutrals++
	return f.neutralStatus, nil
}
func sourceBegin(snapshot []uint8) Request {
	return Request{Version: 2, Op: "begin", Request: 1, Publisher: strings.Repeat("1", 32), AgentInstance: strings.Repeat("e", 32), QueueEpoch: 1, LeaseGeneration: strings.Repeat("a", 64), RuntimeInstance: strings.Repeat("d", 32), PackageID: strings.Repeat("b", 64), CoreGeneration: 7, Devices: []DeviceSnapshot{{ID: device(1), Held: snapshot}}}
}
func sourceEdge(token string, request, seq uint64, down bool) Request {
	return Request{Version: 2, Op: "edge", Request: request, Token: token, EventSeq: seq, Device: device(1), Usage: 4, Down: down}
}
func sourceReset(token string, request, last uint64) Request {
	return Request{Version: 2, Op: "reset", Request: request, Token: token, LastEventSeq: last}
}
func TestSourceSessionSnapshotAndStaleTokenIsolation(t *testing.T) {
	b := &fakeSourceBackend{neutralStatus: "neutral-confirmed"}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{1}, 64))
	begin, err := s.Handle(context.Background(), sourceBegin([]uint8{4}))
	if err != nil || begin.Status != "ready" {
		t.Fatalf("begin %+v %v", begin, err)
	}
	token := begin.Token
	if _, err := s.Handle(context.Background(), sourceEdge(strings.Repeat("f", 64), 999, 1, false)); !errors.Is(err, ErrRequest) {
		t.Fatalf("stale token accepted %v", err)
	}
	if r, err := s.Handle(context.Background(), sourceEdge(token, 2, 1, true)); err != nil || r.Status != "accepted" {
		t.Fatalf("held repeat %+v %v", r, err)
	}
	if len(b.edges) != 0 {
		t.Fatal("snapshot-held repeat posted")
	}
	if _, err := s.Handle(context.Background(), sourceEdge(token, 3, 2, false)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Handle(context.Background(), sourceEdge(token, 4, 3, true)); err != nil {
		t.Fatal(err)
	}
	if len(b.edges) != 1 || b.edges[0] != (Delta{Usage: 4, Down: true}) {
		t.Fatalf("fresh edge %+v", b.edges)
	}
	barrier := Request{Version: 2, Op: "barrier", Request: 5, Token: token, LastEventSeq: 3}
	if r, err := s.Handle(context.Background(), barrier); err != nil || r.Status != "drained" {
		t.Fatalf("barrier %+v %v", r, err)
	}
	if r, err := s.Handle(context.Background(), sourceReset(token, 6, 3)); err != nil || r.Status != "neutral-confirmed" || b.neutrals != 1 {
		t.Fatalf("reset %+v %v", r, err)
	}
	if _, err := s.Handle(context.Background(), sourceEdge(token, 7, 4, false)); !errors.Is(err, ErrRequest) {
		t.Fatalf("retired token accepted %v", err)
	}
}
func TestSourceSessionAmbiguousEdgeAllowsOnlyNeutral(t *testing.T) {
	b := &fakeSourceBackend{edgeErr: errors.New("ambiguous"), neutralStatus: "neutral-confirmed"}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{2}, 32))
	begin, err := s.Handle(context.Background(), sourceBegin(nil))
	if err != nil {
		t.Fatal(err)
	}
	edge := sourceEdge(begin.Token, 2, 1, true)
	r, err := s.Handle(context.Background(), edge)
	if err != nil || r.Status != "quarantined" {
		t.Fatalf("edge %+v %v", r, err)
	}
	if _, err := s.Handle(context.Background(), sourceEdge(begin.Token, 3, 2, true)); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("replay accepted %v", err)
	}
	if r, err := s.Handle(context.Background(), sourceReset(begin.Token, 4, 1)); err != nil || r.Status != "neutral-confirmed" || b.neutrals != 1 || len(b.edges) != 1 {
		t.Fatalf("neutral %+v %v edges=%+v", r, err, b.edges)
	}
}
func TestSourceSessionWaitsForSinkDrainBeforeQuarantineAck(t *testing.T) {
	b := &fakeSourceBackend{entered: make(chan struct{}), resume: make(chan struct{}), neutralStatus: "neutral-confirmed"}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{3}, 32))
	begin, err := s.Handle(context.Background(), sourceBegin(nil))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Reply, 1)
	go func() { r, _ := s.Handle(ctx, sourceEdge(begin.Token, 2, 1, true)); done <- r }()
	<-b.entered
	cancel()
	select {
	case <-done:
		t.Fatal("ack before sink drained")
	case <-time.After(20 * time.Millisecond):
	}
	close(b.resume)
	select {
	case r := <-done:
		if r.Status != "quarantined" {
			t.Fatalf("ack %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("edge did not drain")
	}
	if b.neutrals != 0 {
		t.Fatal("neutral overtook sink call")
	}
	if _, err := s.Handle(context.Background(), sourceReset(begin.Token, 3, 1)); err != nil {
		t.Fatal(err)
	}
	if b.neutrals != 1 {
		t.Fatal("source was not neutralized")
	}
}
func TestSourceSessionRefusesTokenReuseAndUnconfirmedReset(t *testing.T) {
	b := &fakeSourceBackend{neutralStatus: "unconfirmed"}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{4}, 64))
	begin, err := s.Handle(context.Background(), sourceBegin(nil))
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Handle(context.Background(), sourceReset(begin.Token, 2, 0))
	if err != nil || r.Status != "unconfirmed" {
		t.Fatalf("reset %+v %v", r, err)
	}
	next := sourceBegin(nil)
	next.Request = 3
	if _, err := s.Handle(context.Background(), next); !errors.Is(err, ErrPending) {
		t.Fatalf("new source admitted before neutral %v", err)
	}
	b.neutralStatus = "old-generation-retired"
	if _, err := s.Handle(context.Background(), sourceReset(begin.Token, 4, 0)); err != nil {
		t.Fatal(err)
	}
	next.Request = 5
	if _, err := s.Handle(context.Background(), next); !errors.Is(err, ErrRequest) {
		t.Fatalf("token reuse accepted %v", err)
	}
}

func TestSourceSessionAbortNeutralizesConfirmedPressAndRetainsFailure(t *testing.T) {
	b := &fakeSourceBackend{neutralStatus: "unconfirmed"}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{5}, 32))
	begin, err := s.Handle(context.Background(), sourceBegin(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Handle(context.Background(), sourceEdge(begin.Token, 2, 1, true)); err != nil {
		t.Fatal(err)
	}
	status, err := s.Abort(context.Background())
	if err != nil || status != "unconfirmed" || b.neutrals != 1 {
		t.Fatalf("abort %q %v", status, err)
	}
	if _, err := s.Handle(context.Background(), sourceEdge(begin.Token, 3, 2, false)); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("edge after EOF accepted %v", err)
	}
	b.neutralStatus = "neutral-confirmed"
	status, err = s.Abort(context.Background())
	if err != nil || status != "neutral-confirmed" || b.neutrals != 2 {
		t.Fatalf("retry neutral %q %v", status, err)
	}
	if _, err := s.Handle(context.Background(), sourceEdge(begin.Token, 4, 2, false)); !errors.Is(err, ErrRequest) {
		t.Fatalf("old source retained %v", err)
	}
}
func TestSourceSessionMalformedFrameUsesAbortNotInputReplay(t *testing.T) {
	b := &fakeSourceBackend{neutralStatus: "neutral-confirmed"}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{6}, 32))
	begin, err := s.Handle(context.Background(), sourceBegin(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Handle(context.Background(), sourceEdge(begin.Token, 2, 1, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRequest(bufio.NewReader(strings.NewReader("{bad}\n"))); !errors.Is(err, ErrRequest) {
		t.Fatalf("bad frame %v", err)
	}
	if status, err := s.Abort(context.Background()); err != nil || status != "neutral-confirmed" || b.neutrals != 1 || len(b.edges) != 1 {
		t.Fatalf("abort %q %v edges=%d", status, err, len(b.edges))
	}
}
func TestSourceSessionGapCannotAcknowledgeBarrier(t *testing.T) {
	b := &fakeSourceBackend{neutralStatus: "neutral-confirmed"}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{7}, 32))
	begin, err := s.Handle(context.Background(), sourceBegin(nil))
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.Handle(context.Background(), sourceEdge(begin.Token, 2, 2, true)); err != nil || r.Status != "quarantined" {
		t.Fatalf("gap %+v %v", r, err)
	}
	barrier := Request{Version: 2, Op: "barrier", Request: 3, Token: begin.Token, LastEventSeq: 0}
	if _, err := s.Handle(context.Background(), barrier); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("barrier after gap %v", err)
	}
	if _, err := s.Handle(context.Background(), sourceReset(begin.Token, 4, 2)); err != nil {
		t.Fatal(err)
	}
}
func TestSourceSessionCanceledOpenDoesNotPublish(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &fakeSourceBackend{openFn: cancel}
	s := NewSourceSession(b)
	s.random = bytes.NewReader(bytes.Repeat([]byte{8}, 64))
	if _, err := s.Handle(ctx, sourceBegin(nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("open cancel %v", err)
	}
	if s.token != "" || s.ledger != nil || s.binding != nil {
		t.Fatal("canceled open published source")
	}
	b.openFn = nil
	if _, err := s.Handle(context.Background(), sourceBegin(nil)); err != nil {
		t.Fatal(err)
	}
}
