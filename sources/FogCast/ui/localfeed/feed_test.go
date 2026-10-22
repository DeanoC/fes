package localfeed

import (
	"bufio"
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/testsock"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func eventOf(frame protocol.InputFrame) remoteinput.Event {
	return remoteinput.Event{Player: frame.Player, Device: remoteinput.Device(frame.Device), Kind: remoteinput.Kind(frame.Kind),
		Action: remoteinput.Action(frame.Action), Code: remoteinput.Code(frame.Code), Value: frame.Value}
}

// Player 2 and the departure marker survive the wire format as mister-agent
// reads them.
func TestFrameKeepsPlayerAndDepartureMarker(t *testing.T) {
	now := time.Unix(100, 0)
	for _, e := range []remoteinput.Event{
		{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA},
		remoteinput.LocalPlayerDeparture(1),
	} {
		wire, err := protocol.EncodeInputFrame(Frame(7, e, now))
		if err != nil {
			t.Fatalf("%+v: %v", e, err)
		}
		frame, err := protocol.DecodeInputFrame(bytes.NewReader(wire), uint32(len(wire)))
		if err != nil {
			t.Fatalf("%+v: %v", e, err)
		}
		if got := eventOf(frame); got != e || frame.Seq != 7 || frame.Header.Session != Session {
			t.Fatalf("round trip %+v -> %+v", e, got)
		}
	}
	if !remoteinput.IsLocalPlayerDeparture(eventOf(Frame(1, remoteinput.LocalPlayerDeparture(0), now))) {
		t.Fatal("departure marker lost its shape")
	}
}

func TestSendWritesPlayerOnTheLocalSocket(t *testing.T) {
	path := testsock.Path(t, "feed.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	feed := New(path, nil)
	defer feed.Close()
	e := remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonStart}
	if err := feed.Send(e, time.Time{}); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame, err := protocol.DecodeInputFrame(bufio.NewReader(conn), 1<<16)
	if err != nil {
		t.Fatal(err)
	}
	if got := eventOf(frame); got != e || frame.Seq != 1 {
		t.Fatalf("sent %+v, socket saw %+v (seq %d)", e, got, frame.Seq)
	}
}
