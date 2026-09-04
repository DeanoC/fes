package bridge

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type testSink struct {
	mu       sync.Mutex
	events   []protocol.InputFrame
	released int
	closed   int
}

type cleanupErrorSink struct {
	releaseErr error
	closeErr   error
}

type transientReleaseErrorSink struct {
	mu           sync.Mutex
	releaseErr   error
	releaseCalls int
}

type firstApplyErrorSink struct {
	mu           sync.Mutex
	applyCalls   int
	releaseCalls int
	firstRelease chan struct{}
	releaseOnce  sync.Once
}

type recoverableApplySink struct {
	mu           sync.Mutex
	applyCalls   int
	releaseCalls int
}

func (s *recoverableApplySink) Apply(protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyCalls++
	if s.applyCalls == 1 {
		return fmt.Errorf("unsupported control: %w", errRejectedInputFrame)
	}
	return nil
}

func (s *recoverableApplySink) ReleaseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCalls++
	return nil
}

func (*recoverableApplySink) Close() error { return nil }

func (s *firstApplyErrorSink) Apply(protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyCalls++
	if s.applyCalls == 1 {
		return errors.New("injected sink write failure")
	}
	return nil
}

func (s *firstApplyErrorSink) ReleaseAll() error {
	s.mu.Lock()
	s.releaseCalls++
	s.mu.Unlock()
	s.releaseOnce.Do(func() { close(s.firstRelease) })
	return nil
}

func (*firstApplyErrorSink) Close() error { return nil }

func (*cleanupErrorSink) Apply(protocol.InputFrame) error { return nil }
func (s *cleanupErrorSink) ReleaseAll() error             { return s.releaseErr }
func (s *cleanupErrorSink) Close() error                  { return s.closeErr }

func (*transientReleaseErrorSink) Apply(protocol.InputFrame) error { return nil }
func (s *transientReleaseErrorSink) ReleaseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCalls++
	if s.releaseCalls == 1 {
		return s.releaseErr
	}
	return nil
}
func (*transientReleaseErrorSink) Close() error { return nil }

func (s *testSink) Apply(f protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, f)
	return nil
}
func (s *testSink) ReleaseAll() error { s.mu.Lock(); defer s.mu.Unlock(); s.released++; return nil }
func (s *testSink) Close() error      { s.mu.Lock(); defer s.mu.Unlock(); s.closed++; return nil }

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
	sink.mu.Lock()
	closed := sink.closed
	sink.mu.Unlock()
	if closed != 1 {
		t.Fatalf("legacy sink close calls = %d, want 1", closed)
	}
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

func TestServerSinkFailureClosesStreamReleasesAndAllowsSuccessor(t *testing.T) {
	token := []byte("0123456789abcdef")
	sink := &firstApplyErrorSink{firstRelease: make(chan struct{})}
	server, err := New(Config{Addr: "127.0.0.1:0", Token: token, Session: 9, Core: "MegaDrive", HeartbeatTimeout: 10 * time.Second}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.ListenAndServe(ctx) }()
	<-server.Ready()

	connect := func() net.Conn {
		connection, err := net.Dial("tcp", server.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(connection, "{\"version\":1,\"session\":9,\"core\":\"MegaDrive\",\"proof\":\"%s\"}\n", hex.EncodeToString(token)); err != nil {
			t.Fatal(err)
		}
		ack := make([]byte, len("{\"ok\":true}\n"))
		if _, err := io.ReadFull(connection, ack); err != nil {
			t.Fatal(err)
		}
		return connection
	}

	first := connect()
	frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 9}, Seq: 1, Device: 1, Kind: 1, Action: 1, Code: 104}
	if err := WriteFrame(first, frame); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.firstRelease:
	case <-time.After(time.Second):
		t.Fatal("sink failure did not trigger disconnect release")
	}
	if err := first.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("failed stream read error = %v, want EOF", err)
	}
	_ = first.Close()

	second := connect()
	if err := WriteFrame(second, frame); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		calls := sink.applyCalls
		sink.mu.Unlock()
		if calls == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	sink.mu.Lock()
	applyCalls := sink.applyCalls
	sink.mu.Unlock()
	if applyCalls != 2 {
		t.Fatalf("successor apply calls = %d, want 2", applyCalls)
	}
	_ = second.Close()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServerRejectedFrameKeepsHealthyStreamOpen(t *testing.T) {
	token := []byte("0123456789abcdef")
	sink := &recoverableApplySink{}
	server, err := New(Config{Addr: "127.0.0.1:0", Token: token, Session: 9, Core: "MegaDrive", HeartbeatTimeout: 10 * time.Second}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.ListenAndServe(ctx) }()
	<-server.Ready()

	connection, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := fmt.Fprintf(connection, "{\"version\":1,\"session\":9,\"core\":\"MegaDrive\",\"proof\":\"%s\"}\n", hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	ack := make([]byte, len("{\"ok\":true}\n"))
	if _, err := io.ReadFull(connection, ack); err != nil {
		t.Fatal(err)
	}
	for sequence := uint32(1); sequence <= 2; sequence++ {
		frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 9}, Seq: sequence, Device: 1, Kind: 1, Action: 1, Code: 104}
		if err := WriteFrame(connection, frame); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		calls := sink.applyCalls
		sink.mu.Unlock()
		if calls == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	sink.mu.Lock()
	applyCalls := sink.applyCalls
	releaseCalls := sink.releaseCalls
	sink.mu.Unlock()
	if applyCalls != 2 {
		t.Fatalf("apply calls = %d, want rejected frame plus later valid frame", applyCalls)
	}
	if releaseCalls != 0 {
		t.Fatalf("release calls before disconnect = %d, want 0", releaseCalls)
	}
	metrics := server.Metrics().Snapshot()
	if metrics["rejected"] != 1 || metrics["applied"] != 1 {
		t.Fatalf("metrics = %v, want one rejected and one applied", metrics)
	}
}

func TestServerCloseReportsReleaseAndSinkCloseFailures(t *testing.T) {
	releaseErr := errors.New("release failed")
	closeErr := errors.New("sink close failed")
	server, err := New(Config{Token: []byte("0123456789abcdef"), Session: 9}, &cleanupErrorSink{releaseErr: releaseErr, closeErr: closeErr})
	if err != nil {
		t.Fatal(err)
	}
	for call := 0; call < 2; call++ {
		err := server.Close()
		if !errors.Is(err, releaseErr) || !errors.Is(err, closeErr) {
			t.Fatalf("Close call %d error = %v, want release and sink close failures", call+1, err)
		}
	}
}

func TestServerCloseReportsEarlierDisconnectReleaseFailure(t *testing.T) {
	releaseErr := errors.New("disconnect release failed")
	sink := &transientReleaseErrorSink{releaseErr: releaseErr}
	token := []byte("0123456789abcdef")
	server, err := New(Config{Addr: "127.0.0.1:0", Token: token, Session: 9, Core: "MegaDrive"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.ListenAndServe(ctx) }()
	if err := <-server.Startup(); err != nil {
		t.Fatal(err)
	}
	connection, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(connection, "{\"version\":1,\"session\":9,\"core\":\"MegaDrive\",\"proof\":\"%s\"}\n", hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	ack := make([]byte, len("{\"ok\":true}\n"))
	if _, err := io.ReadFull(connection, ack); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && server.Metrics().Snapshot()["releases"] == 0 {
		time.Sleep(time.Millisecond)
	}
	if server.Metrics().Snapshot()["releases"] == 0 {
		t.Fatal("disconnect did not attempt release")
	}
	if err := server.Close(); !errors.Is(err, releaseErr) {
		t.Fatalf("Close error = %v, want earlier disconnect release failure", err)
	}
}

func TestServerStartupReportsBindFailure(t *testing.T) {
	bindErr := errors.New("bind failed")
	server, err := New(Config{
		Token:   []byte("0123456789abcdef"),
		Session: 9,
		Listen: func(string, string) (net.Listener, error) {
			return nil, bindErr
		},
	}, &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.ListenAndServe(context.Background()) }()
	select {
	case err := <-server.Startup():
		if !errors.Is(err, bindErr) {
			t.Fatalf("startup error = %v, want bind failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup did not report bind failure")
	}
	if err := <-serveResult; !errors.Is(err, bindErr) {
		t.Fatalf("ListenAndServe error = %v, want bind failure", err)
	}
}

func TestServerCloseBeforeListenerPublicationUnblocksStartup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	publish := make(chan struct{})
	server, err := New(Config{
		Token:   []byte("0123456789abcdef"),
		Session: 9,
		Listen: func(string, string) (net.Listener, error) {
			close(entered)
			<-publish
			return listener, nil
		},
	}, &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.ListenAndServe(context.Background()) }()
	<-entered
	closeResult := make(chan error, 1)
	go func() { closeResult <- server.Close() }()
	select {
	case err := <-server.Startup():
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("startup error = %v, want closed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock startup")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close blocked before listener publication")
	}
	close(publish)
	select {
	case err := <-serveResult:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("ListenAndServe error = %v, want closed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ListenAndServe did not return after rejected publication")
	}
}
