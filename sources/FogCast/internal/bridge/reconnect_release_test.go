package bridge

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

type heldReleaseSink struct {
	testSink
	entered, allow chan struct{}
	once           sync.Once
}

func (s *heldReleaseSink) ReleaseAll() error {
	s.once.Do(func() { close(s.entered); <-s.allow })
	return s.testSink.ReleaseAll()
}

func TestReconnectCannotReplayUntilPreviousReleaseCompletes(t *testing.T) {
	sink := &heldReleaseSink{entered: make(chan struct{}), allow: make(chan struct{})}
	token := []byte("0123456789abcdef")
	server, err := New(Config{Addr: "127.0.0.1:0", Session: 9, Core: "fes.coleco", Token: token}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.ListenAndServe(ctx) }()
	<-server.Ready()
	var unblock sync.Once
	release := func() { unblock.Do(func() { close(sink.allow) }) }
	defer func() { release(); _ = server.Close() }()
	connect := func() (net.Conn, error) {
		c, err := net.Dial("tcp", server.Addr().String())
		if err != nil {
			return nil, err
		}
		_ = c.SetDeadline(time.Now().Add(time.Second))
		_, err = fmt.Fprintf(c, "{\"version\":1,\"session\":9,\"core\":\"fes.coleco\",\"proof\":\"%s\"}\n", hex.EncodeToString(token))
		if err == nil {
			_, err = bufio.NewReader(c).ReadString('\n')
		}
		return c, err
	}
	first, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	select {
	case <-sink.entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup not entered")
	}
	second, err := connect()
	if second != nil {
		_ = second.Close()
	}
	if err == nil {
		t.Fatal("reconnected while old release pending")
	}
	release()
	var third net.Conn
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		third, err = connect()
		if err == nil {
			break
		}
		if third != nil {
			_ = third.Close()
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatalf("reconnect after cleanup: %v", err)
	}
	defer third.Close()
	if err := WriteFrame(third, protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 9}, Seq: 1, Player: 1, Device: 1, Kind: 1, Action: 1, Code: 104}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		count := len(sink.events)
		sink.mu.Unlock()
		if count == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("new input was not applied")
}
