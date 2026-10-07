package input

import (
	"context"
	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/protocol"
	"net"
	"testing"
	"time"
)

func TestLocalMouseStrictSequence(t *testing.T) {
	writes := make(chan mouseWrite, 8)
	mouse := &mouseSink{poster: func(_ context.Context, _ string, g uint64, x, y int16, b uint8) error {
		writes <- mouseWrite{x, y, b, g}
		return nil
	}}
	sink := &controllerPortsSink{mouse: mouse}
	c := newTargetControllerWithSink("127.0.0.1:0", sink)
	c.ports = sink
	c.ObserveCore(func(context.Context) (CoreObservation, error) {
		return CoreObservation{Active: true, Mouse: &MouseBinding{"pkg", 7}}, nil
	})
	a, b := net.Pipe()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { c.readLocal(ctx, a); close(done) }()
	f := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1}, Seq: 1, Device: 2, Kind: 4, Action: 3, Value: 9}
	for _, seq := range []uint32{1, 1, 0, 2} {
		f.Seq = seq
		if err := bridge.WriteFrame(b, f); err != nil {
			t.Fatal(err)
		}
	}
	// A ping ordered after the inputs proves the duplicate checks have completed.
	f = protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 1}, Seq: 3}
	if err := bridge.WriteFrame(b, f); err != nil {
		t.Fatal(err)
	}
	b.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := protocol.DecodeInputFrame(b, 1024); err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatalf("local duplicates delivered %d", len(writes))
	}
	b.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("local close")
	}
}
