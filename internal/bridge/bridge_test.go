package bridge

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
)

type testSink struct {
	mu       sync.Mutex
	events   []protocol.InputFrame
	released int
}

func (s *testSink) Apply(f protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, f)
	return nil
}
func (s *testSink) ReleaseAll() error { s.mu.Lock(); defer s.mu.Unlock(); s.released++; return nil }
func (s *testSink) Close() error      { return nil }

func TestServerHandshakeFrameAndDisconnectRelease(t *testing.T) {
	tok := []byte("0123456789abcdef")
	sink := &testSink{}
	s, err := New(Config{Addr: "127.0.0.1:0", Token: tok, Session: 9, Core: "snes"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.ListenAndServe(ctx) }()
	<-s.Ready()
	c, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = fmt.Fprintf(c, "{\"version\":1,\"session\":9,\"core\":\"snes\",\"proof\":\"%s\"}\n", hex.EncodeToString(tok))
	f := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 9}, Seq: 1, Device: 0, Kind: 0, Action: 1, Code: 2}
	if err := WriteFrame(c, f); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		n := len(sink.events)
		sink.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	sink.mu.Lock()
	n := len(sink.events)
	sink.mu.Unlock()
	if n != 1 {
		t.Fatalf("events=%d", n)
	}
	_ = c.Close()
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		released := sink.released
		sink.mu.Unlock()
		if released > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	sink.mu.Lock()
	released := sink.released
	sink.mu.Unlock()
	if released == 0 {
		t.Fatal("disconnect did not release")
	}
	_ = s.Close()
}

func TestServerRejectsStaleFrameAndCountsSequenceGap(t *testing.T) {
	tok := []byte("0123456789abcdef")
	sink := &testSink{}
	s, err := New(Config{Addr: "127.0.0.1:0", Token: tok, Session: 9, Core: "snes"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = s.ListenAndServe(ctx) }()
	<-s.Ready()
	c, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = fmt.Fprintf(c, "{\"version\":1,\"session\":9,\"core\":\"snes\",\"proof\":\"%s\"}\n", hex.EncodeToString(tok))
	base := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 9}, Device: 0, Kind: 0, Action: 1, Code: 2}
	for _, seq := range []uint32{1, 3, 2} {
		base.Seq = seq
		if err := WriteFrame(c, base); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && s.Metrics().Snapshot()["applied"] < 2 {
		time.Sleep(time.Millisecond)
	}
	metrics := s.Metrics().Snapshot()
	if metrics["applied"] != 2 || metrics["sequence_gaps"] != 1 || metrics["rejected"] != 1 {
		t.Fatalf("metrics=%v", metrics)
	}
	_ = s.Close()
}
