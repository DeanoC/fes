package host

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestControllerReplayPreservesBothPlayersAndRelease(t *testing.T) {
	var state remoteinput.State
	for port := uint8(0); port < 2; port++ {
		if err := state.Apply(remoteinput.Event{Player: port, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.Apply(remoteinput.Event{Player: 0, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionRelease, Code: remoteinput.ButtonA}); err != nil {
		t.Fatal(err)
	}
	_ = state.Apply(remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.KeypadStar})
	_ = state.Apply(remoteinput.Event{Player: 0, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftX, Value: -32768})
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	r := &RemoteInput{conn: left, reader: bufio.NewReader(left), inputState: state, session: 9, now: time.Now, dial: time.Second, controllerPorts: true, keypadPorts: true}
	done := make(chan error, 1)
	go func() { done <- r.replayStateLocked(context.Background()) }()
	var frames []protocol.InputFrame
	for range 3 {
		f, err := protocol.DecodeInputFrame(right, 1024)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, f)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if frames[0].Player != 0 || frames[0].Code != 200 || frames[0].Value != -32768 || frames[1].Player != 1 || frames[1].Code != 104 || frames[2].Player != 1 || frames[2].Code != 130 {
		t.Fatalf("replay %+v", frames)
	}
	r.inputState.ReleaseAll()
	if err := r.replayStateLocked(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.sequence != 3 {
		t.Fatal("released state replayed")
	}
}

func TestControllerPlayerAdmissionBeforeStateMutation(t *testing.T) {
	r := &RemoteInput{state: RemoteInputAttached}
	e := remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if err := r.sendEventLocked(context.Background(), e, time.Time{}); err != ErrRemoteInputInvalid {
		t.Fatalf("legacy player2: %v", err)
	}
	if len(r.inputState.SnapshotForPlayer(1).Pressed) != 0 {
		t.Fatal("rejected event retained")
	}
	r.controllerPorts = true
	e.Player = 0
	e.Code = remoteinput.Keypad0
	if err := r.sendEventLocked(context.Background(), e, time.Time{}); err != ErrRemoteInputInvalid {
		t.Fatalf("missing keypad capability: %v", err)
	}
}
