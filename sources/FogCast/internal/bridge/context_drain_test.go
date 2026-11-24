package bridge

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type drainingContextSink struct {
	entered      chan struct{}
	releasing    chan struct{}
	allowRelease chan struct{}
	once         sync.Once
}

func (s *drainingContextSink) Apply(protocol.InputFrame) error { panic("context-aware sink bypassed") }
func (s *drainingContextSink) ApplyContext(ctx context.Context, _ protocol.InputFrame) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}
func (s *drainingContextSink) ReleaseAll() error {
	s.once.Do(func() { close(s.releasing) })
	<-s.allowRelease
	return nil
}
func (s *drainingContextSink) Close() error { return nil }

func TestContextHandlerDrainsBeforeSuccessorAdmission(t *testing.T) {
	token := []byte("0123456789abcdef")
	sink := &drainingContextSink{entered: make(chan struct{}), releasing: make(chan struct{}), allowRelease: make(chan struct{})}
	server, err := New(Config{Token: token, Session: 9, Core: "snes"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	client, handler := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	server.wg.Add(1)
	go func() { defer server.wg.Done(); server.handle(ctx, handler) }()
	if _, err := fmt.Fprintf(client, `{"version":1,"session":9,"core":"snes","proof":"%s"}`+"\n", hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(client)
	if line, err := reader.ReadString('\n'); err != nil || line != "{\"ok\":true}\n" {
		t.Fatalf("welcome %q %v", line, err)
	}
	if err := WriteFrame(client, receiptInput(9, 1)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("handler not entered")
	}
	cancel()
	select {
	case <-sink.releasing:
	case <-time.After(time.Second):
		t.Fatal("context did not cancel handler")
	}
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("server closed before release drained: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	second, secondHandler := net.Pipe()
	defer second.Close()
	_ = second.SetDeadline(time.Now().Add(time.Second))
	go server.handle(context.Background(), secondHandler)
	if _, err := fmt.Fprintf(second, `{"version":1,"session":9,"core":"snes","proof":"%s"}`+"\n", hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(second).ReadString('\n'); err != io.EOF {
		t.Fatalf("successor admitted during release: %v", err)
	}
	close(sink.allowRelease)
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not finish after release drained")
	}
}
