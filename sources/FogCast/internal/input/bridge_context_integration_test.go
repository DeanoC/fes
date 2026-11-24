package input

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type memoryListener struct {
	incoming chan net.Conn
	done     chan struct{}
	once     sync.Once
}

func (l *memoryListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.incoming:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *memoryListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *memoryListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }

func TestRetainedTapSinkPropagatesBridgeCancellation(t *testing.T) {
	listener := &memoryListener{incoming: make(chan net.Conn, 1), done: make(chan struct{})}
	hid := &keyboardHIDSink{}
	entered := make(chan struct{})
	canceled := make(chan struct{})
	var calls atomic.Int32
	hid.setPoster(func(ctx context.Context, _, _ string, _ uint64, _ hidkeys.Rows) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			close(canceled)
			return ctx.Err()
		}
		return nil
	})
	instance, id := strings.Repeat("a", 32), strings.Repeat("b", 64)
	binding := KeyboardHIDBinding{RuntimeInstance: instance, PackageID: id, Generation: 7}
	ports := &controllerPortsSink{hid: hid}
	c := newTargetControllerWithSink("", ports)
	c.ports = ports
	c.listen = func(string, string) (net.Listener, error) { return listener, nil }
	c.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, RuntimeInstance: instance, PackageID: id, Generation: 7, KeyboardHID: &binding}, nil
	})
	token := []byte("0123456789abcdef")
	if err := c.Attach(context.Background(), Spec{Session: 9, Token: token, Core: "fes.c64"}); err != nil {
		t.Fatal(err)
	}
	client, peer := net.Pipe()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	listener.incoming <- peer
	if _, err := fmt.Fprintf(client, `{"version":1,"session":9,"core":"fes.c64","proof":"%s"}`+"\n", hex.EncodeToString(token)); err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(client).ReadString('\n'); err != nil || line != "{\"ok\":true}\n" {
		t.Fatalf("welcome %q %v", line, err)
	}
	code, _ := hidkeys.Code(4)
	frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 9}, Seq: 1, Device: uint8(remoteinput.DeviceKeyboard), Kind: uint8(remoteinput.KindKey), Action: uint8(remoteinput.ActionPress), Code: uint16(code)}
	if err := bridge.WriteFrame(client, frame); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("HID write did not start")
	}
	done := make(chan error, 1)
	go func() { done <- c.Detach(context.Background(), 9) }()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("underlying write did not receive cancellation")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("detach did not drain")
	}
}
