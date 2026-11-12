package bridge

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// receiptFixture uses the actual bridge TCP handler and frame codec.
func receiptFixture(t *testing.T, sink Sink) (net.Conn, *bufio.Reader) {
	t.Helper()
	token := []byte("0123456789abcdef")
	server, err := New(Config{Addr: "127.0.0.1:0", Token: token, Session: 9, Core: "snes", HeartbeatTimeout: 5 * time.Second}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = server.ListenAndServe(ctx) }()
	<-server.Ready()
	t.Cleanup(func() { cancel(); _ = server.Close() })
	c, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = fmt.Fprintf(c, `{"version":1,"session":9,"core":"snes","proof":"%s"}`+"\n", hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(c)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"ok\":true}\n"
	if line != want {
		t.Fatalf("welcome %q, want %q", line, want)
	}
	return c, reader
}

func receiptInput(session uint64, seq uint32) protocol.InputFrame {
	return protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: session}, Seq: seq, Device: 1, Kind: 1, Action: 1, Code: 104}
}

func receiptBarrier(t *testing.T, c net.Conn, r io.Reader, seq uint32, nonce uint64) protocol.InputFrame {
	t.Helper()
	ping := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Flags: protocol.InputFlagReceiptRequested, Session: 9}, Seq: seq, ClientMonoNS: nonce}
	if err := WriteFrame(c, ping); err != nil {
		t.Fatal(err)
	}
	response, err := protocol.DecodeInputFrame(r, MaxFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Type != protocol.InputTypeReceipt || response.Header.Session != 9 || response.Seq != seq || response.ClientMonoNS != nonce || response.ServerMonoNS == 0 {
		t.Fatalf("uncorrelated receipt: %+v", response)
	}
	return response
}

func TestReceiptsAcknowledgeSinkAndResetOnlyAtBarrier(t *testing.T) {
	sink := &recoverableApplySink{}
	c, r := receiptFixture(t, sink)
	// First input is rejected by the sink; a plain heartbeat must not clear it.
	if err := WriteFrame(c, receiptInput(9, 1)); err != nil {
		t.Fatal(err)
	}
	heartbeat := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 9}, Seq: 1, ClientMonoNS: 44}
	if err := WriteFrame(c, heartbeat); err != nil {
		t.Fatal(err)
	}
	pong, err := protocol.DecodeInputFrame(r, MaxFrameBytes)
	if err != nil || pong.Header.Type != protocol.InputTypePong || pong.ClientMonoNS != 44 {
		t.Fatalf("heartbeat %+v %v", pong, err)
	}
	if err := WriteFrame(c, receiptInput(9, 2)); err != nil {
		t.Fatal(err)
	}
	got := receiptBarrier(t, c, r, 2, 101)
	if got.Header.Flags != protocol.InputFlagReceiptRejected {
		t.Fatalf("rejection lost: %+v", got)
	}
	if err := WriteFrame(c, receiptInput(9, 3)); err != nil {
		t.Fatal(err)
	}
	got = receiptBarrier(t, c, r, 3, 102)
	if got.Header.Flags != 0 {
		t.Fatalf("rejection not reset: %+v", got)
	}
}

func TestReceiptsRejectWrongSessionAndStaleSequence(t *testing.T) {
	c, r := receiptFixture(t, &testSink{})
	for _, input := range []protocol.InputFrame{receiptInput(9, 3), receiptInput(9, 2), receiptInput(10, 99)} {
		if err := WriteFrame(c, input); err != nil {
			t.Fatal(err)
		}
	}
	if got := receiptBarrier(t, c, r, 3, 11); got.Header.Flags != protocol.InputFlagReceiptRejected {
		t.Fatalf("missing rejection: %+v", got)
	}
	// A wrong-session frame must not advance this session's sequence tracker.
	if err := WriteFrame(c, receiptInput(9, 4)); err != nil {
		t.Fatal(err)
	}
	if got := receiptBarrier(t, c, r, 4, 12); got.Header.Flags != 0 {
		t.Fatalf("wrong-session sequence poisoned tracker: %+v", got)
	}
}

func TestClientRepliesRejected(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame protocol.InputFrame
	}{
		{"forged receipt", protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeReceipt, Session: 9}, ServerMonoNS: 1}},
		{"forged pong", protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePong, Session: 9}, ServerMonoNS: 1}},
		{"wrong-session ping", protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 10}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sink := &testSink{}
			c, r := receiptFixture(t, sink)
			if err := WriteFrame(c, tc.frame); err != nil {
				t.Fatal(err)
			}
			_, err := r.ReadByte()
			if !errors.Is(err, io.EOF) {
				t.Fatalf("expected EOF, got %v", err)
			}
			sink.mu.Lock()
			defer sink.mu.Unlock()
			if len(sink.events) != 0 {
				t.Fatalf("forged control passed to sink: %+v", sink.events)
			}
		})
	}
}

func TestReceiptFatalSinkFailureHasNoSuccess(t *testing.T) {
	sink := &firstApplyErrorSink{firstRelease: make(chan struct{})}
	c, r := receiptFixture(t, sink)
	if err := WriteFrame(c, receiptInput(9, 1)); err != nil {
		t.Fatal(err)
	}
	ping := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Flags: protocol.InputFlagReceiptRequested, Session: 9}, Seq: 1, ClientMonoNS: 22}
	_ = WriteFrame(c, ping) // a reset before this write is also a valid uncertain result.
	_, err := protocol.DecodeInputFrame(r, MaxFrameBytes)
	if err == nil || (!errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "reset")) {
		t.Fatalf("wanted closed stream without receipt, got %v", err)
	}
}

type shortReceiptListener struct{ net.Listener }

func (l shortReceiptListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return shortReceiptConn{Conn: c}, nil
}

type shortReceiptConn struct{ net.Conn }

func (c shortReceiptConn) Write(p []byte) (int, error) {
	if len(p) > 10 && p[9] == protocol.InputTypeReceipt {
		// Simulate a broken writer that reports a short write without an error.
		return c.Conn.Write(p[:8])
	}
	return c.Conn.Write(p)
}

func TestShortReceiptWriteClosesStream(t *testing.T) {
	token := []byte("0123456789abcdef")
	server, err := New(Config{Addr: "127.0.0.1:0", Token: token, Session: 9, Core: "snes", Listen: func(network, addr string) (net.Listener, error) {
		ln, err := net.Listen(network, addr)
		if err != nil {
			return nil, err
		}
		return shortReceiptListener{ln}, nil
	}}, &testSink{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = server.ListenAndServe(ctx) }()
	<-server.Ready()
	t.Cleanup(func() { cancel(); _ = server.Close() })
	c, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = fmt.Fprintf(c, `{"version":1,"session":9,"core":"snes","proof":"%s"}`+"\n", hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	if line, err := r.ReadString('\n'); err != nil || line != "{\"ok\":true}\n" {
		t.Fatalf("welcome %q: %v", line, err)
	}
	if err = WriteFrame(c, receiptInput(9, 1)); err != nil {
		t.Fatal(err)
	}
	ping := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Flags: protocol.InputFlagReceiptRequested, Session: 9}, Seq: 1, ClientMonoNS: 22}
	if err = WriteFrame(c, ping); err != nil {
		t.Fatal(err)
	}
	if _, err = protocol.DecodeInputFrame(r, MaxFrameBytes); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short receipt: got %v", err)
	}
}

type slowReceiptSink struct {
	entered chan struct{}
	resume  chan struct{}
}

func (s *slowReceiptSink) Apply(protocol.InputFrame) error {
	close(s.entered)
	<-s.resume
	return nil
}
func (*slowReceiptSink) ReleaseAll() error { return nil }
func (*slowReceiptSink) Close() error      { return nil }

func TestReceiptAfterSlowSinkAndIdleTimeout(t *testing.T) {
	token := []byte("0123456789abcdef")
	sink := &slowReceiptSink{entered: make(chan struct{}), resume: make(chan struct{})}
	defer func() {
		select {
		case <-sink.resume:
		default:
			close(sink.resume)
		}
	}()
	server, err := New(Config{Addr: "127.0.0.1:0", Token: token, Session: 9, Core: "snes", HeartbeatTimeout: 50 * time.Millisecond}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = server.ListenAndServe(ctx) }()
	<-server.Ready()
	t.Cleanup(func() { cancel(); _ = server.Close() })
	c, err := net.Dial("tcp", server.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = fmt.Fprintf(c, `{"version":1,"session":9,"core":"snes","proof":"%s"}`+"\n", hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(c)
	if line, err := r.ReadString('\n'); err != nil || line != "{\"ok\":true}\n" {
		t.Fatalf("welcome %q: %v", line, err)
	}
	if err = WriteFrame(c, receiptInput(9, 1)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("sink not called")
	}
	ping := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Flags: protocol.InputFlagReceiptRequested, Session: 9}, Seq: 1, ClientMonoNS: 123}
	if err = WriteFrame(c, ping); err != nil {
		t.Fatal(err)
	}
	// Wait beyond both the heartbeat timeout and the bridge's 100ms tick.
	<-time.After(250 * time.Millisecond)
	close(sink.resume)
	response, err := protocol.DecodeInputFrame(r, MaxFrameBytes)
	if err != nil || response.Header.Type != protocol.InputTypeReceipt || response.Header.Flags != 0 || response.Seq != 1 || response.ClientMonoNS != 123 {
		t.Fatalf("slow sink receipt %+v: %v", response, err)
	}
	// Accounting for sink work must not disable the idle heartbeat timeout.
	if _, err = r.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("idle stream should close, got %v", err)
	}
}
