package host

import (
	"context"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"net"
	"testing"
	"time"
)

func TestMouseReconnectReplaysOnlyButtons(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	r := &RemoteInput{conn: a, now: time.Now, dial: time.Second, session: 7, mouse: true}
	if err := r.inputState.Apply(remoteinput.MouseEvent(-123, 456, 3)); err != nil {
		t.Fatal(err)
	}
	got := make(chan protocol.InputFrame, 1)
	go func() { frame, _ := protocol.DecodeInputFrame(b, 1024); got <- frame }()
	if err := r.replayStateLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	frame := <-got
	dx, dy, buttons, ok := remoteinput.MouseVector(remoteinput.Event{Player: frame.Player, Device: remoteinput.Device(frame.Device), Kind: remoteinput.Kind(frame.Kind), Action: remoteinput.Action(frame.Action), Code: remoteinput.Code(frame.Code), Value: frame.Value})
	if !ok || dx != 0 || dy != 0 || buttons != 3 || r.metrics.framesSent != 1 {
		t.Fatalf("replayed motion %+v", frame)
	}
	r.inputState.ReleaseAll()
	if r.inputState.Snapshot().MouseButtons != 0 {
		t.Fatal("held buttons")
	}
}
