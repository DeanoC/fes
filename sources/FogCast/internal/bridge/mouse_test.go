package bridge

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"github.com/DeanoC/FogCast/protocol"
	"net"
	"testing"
	"time"
)

func TestMouseDuplicatesNeverReachSink(t *testing.T) {
	sink := &testSink{}
	token := []byte("0123456789abcdef")
	s, err := New(Config{Addr: "127.0.0.1:0", Session: 9, Core: "fes.atari-st", Token: token}, sink)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.ListenAndServe(ctx)
	<-s.Ready()
	defer s.Close()
	c, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(time.Second))
	fmt.Fprintf(c, "{\"version\":1,\"session\":9,\"core\":\"fes.atari-st\",\"proof\":\"%s\"}\n", hex.EncodeToString(token))
	if _, err := bufio.NewReader(c).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	mouse := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 9}, Seq: 10, Device: 2, Kind: 4, Action: 3, Code: 1, Value: 5}
	key := mouse
	key.Seq = 11
	key.Device = 0
	key.Kind = 0
	key.Action = 1
	key.Code = 1
	key.Value = 0
	for _, frame := range []protocol.InputFrame{mouse, mouse, key, func() protocol.InputFrame { f := mouse; f.Seq = 11; return f }(), func() protocol.InputFrame { f := mouse; f.Seq = 12; return f }()} {
		if err := WriteFrame(c, frame); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		n := len(sink.events)
		sink.mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != 3 || sink.events[0].Seq != 10 || sink.events[1].Seq != 11 || sink.events[2].Seq != 12 {
		t.Fatalf("duplicate motion %+v", sink.events)
	}
}
