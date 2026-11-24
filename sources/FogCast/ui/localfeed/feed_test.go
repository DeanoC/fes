package localfeed

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
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

func TestCancelledBoundFrameDoesNotDialAfterQueueWait(t *testing.T) {
	dials := 0
	feed := New("/unused", func(string, string, time.Duration) (net.Conn, error) { dials++; return nil, nil })
	feed.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- feed.SendBound(ctx, remoteinput.Event{}, time.Time{}, Binding{LeaseGeneration: "lease", PackageID: "core", CoreGeneration: 1})
	}()
	cancel()
	feed.mu.Unlock()
	select {
	case err := <-done:
		if err != context.Canceled || dials != 0 {
			t.Fatalf("cancelled frame dialed %d times: %v", dials, err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled frame did not finish")
	}
}

type memoryConn struct {
	bytes.Buffer
	closed bool
}

func (c *memoryConn) Close() error                     { c.closed = true; return nil }
func (c *memoryConn) LocalAddr() net.Addr              { return nil }
func (c *memoryConn) RemoteAddr() net.Addr             { return nil }
func (c *memoryConn) SetDeadline(time.Time) error      { return nil }
func (c *memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (c *memoryConn) SetWriteDeadline(time.Time) error { return nil }

func TestCloseBoundCannotCloseNewQueueLifetime(t *testing.T) {
	var conns []*memoryConn
	feed := New("test", func(string, string, time.Duration) (net.Conn, error) {
		c := &memoryConn{}
		conns = append(conns, c)
		return c, nil
	})
	e, _ := hidkeys.Event(4, true)
	old := Binding{LeaseGeneration: "lease", PackageID: "core", CoreGeneration: 1, Epoch: 1}
	newer := old
	newer.Epoch++
	if err := feed.SendBound(context.Background(), e, time.Time{}, old); err != nil {
		t.Fatal(err)
	}
	if err := feed.SendBound(context.Background(), e, time.Time{}, newer); err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 {
		t.Fatalf("binding change replaced shared connection: %+v", conns)
	}
	before := conns[0].Len()
	feed.CloseBound(old)
	if conns[0].closed || conns[0].Len() != before {
		t.Fatal("old retirement changed the new binding")
	}
	feed.CloseBound(newer)
	if conns[0].closed || conns[0].Len() == before {
		t.Fatal("current binding not reset on shared connection")
	}
}

func TestBoundFeedWritesImmutableBindingThenFrame(t *testing.T) {
	c := &memoryConn{}
	feed := New("test", func(string, string, time.Duration) (net.Conn, error) { return c, nil })
	key, _ := hidkeys.Event(4, true)
	binding := Binding{LeaseGeneration: strings.Repeat("a", 64), PackageID: strings.Repeat("b", 64), CoreGeneration: 3, Epoch: 17}
	if err := feed.SendBound(context.Background(), key, time.Time{}, binding); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(bytes.NewReader(c.Bytes()))
	header, err := r.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(header, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 5 || decoded["operation"] != "key" || decoded["epoch"] != float64(binding.Epoch) || decoded["lease_generation"] != binding.LeaseGeneration || decoded["package_id"] != binding.PackageID || decoded["core_generation"] != float64(binding.CoreGeneration) {
		t.Fatalf("unexpected binding header: %s", header)
	}
	frame, err := protocol.DecodeInputFrame(r, 4096)
	if err != nil || eventOf(frame) != key {
		t.Fatalf("frame after binding: %+v, %v", frame, err)
	}
}

func TestBoundKeyboardAndRawPadShareConnection(t *testing.T) {
	c := &memoryConn{}
	feed := New("test", func(string, string, time.Duration) (net.Conn, error) { return c, nil })
	key, _ := hidkeys.Event(4, true)
	pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	binding := Binding{LeaseGeneration: strings.Repeat("a", 64), PackageID: strings.Repeat("b", 64), CoreGeneration: 3, Epoch: 4}
	if err := feed.SendBound(context.Background(), key, time.Time{}, binding); err != nil {
		t.Fatal(err)
	}
	if err := feed.Send(pad, time.Time{}); err != nil {
		t.Fatal(err)
	}
	key.Action = remoteinput.ActionRelease
	if err := feed.SendBound(context.Background(), key, time.Time{}, binding); err != nil {
		t.Fatal(err)
	}
	if c.closed {
		t.Fatal("switching sources closed the shared feed")
	}
	r := bufio.NewReader(bytes.NewReader(c.Bytes()))
	for i := 0; i < 3; i++ {
		if i != 1 {
			line, err := r.ReadBytes('\n')
			if err != nil || !bytes.Contains(line, []byte(`"operation":"key"`)) {
				t.Fatalf("bound header %d: %s %v", i, line, err)
			}
		}
		frame, err := protocol.DecodeInputFrame(r, 4096)
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && eventOf(frame) != pad {
			t.Fatalf("raw pad changed: %+v", frame)
		}
	}
}

func TestRawKeyboardRetiresPriorBoundKeyboardBeforeFirstFrame(t *testing.T) {
	c := &memoryConn{}
	feed := New("test", func(string, string, time.Duration) (net.Conn, error) { return c, nil })
	key, _ := hidkeys.Event(4, true)
	old := Binding{LeaseGeneration: strings.Repeat("a", 64), PackageID: strings.Repeat("b", 64), CoreGeneration: 3, Epoch: 4}
	if err := feed.SendBound(context.Background(), key, time.Time{}, old); err != nil {
		t.Fatal(err)
	}
	if err := feed.Send(key, time.Time{}); err != nil {
		t.Fatal(err)
	}
	before := c.Len()
	feed.CloseBound(old)
	if c.Len() != before || c.closed {
		t.Fatal("late old retirement affected raw keyboard")
	}
	r := bufio.NewReader(bytes.NewReader(c.Bytes()))
	if _, err := r.ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.DecodeInputFrame(r, 4096); err != nil {
		t.Fatal(err)
	}
	reset, err := r.ReadBytes('\n')
	if err != nil || !bytes.Contains(reset, []byte(`"operation":"reset"`)) {
		t.Fatalf("missing reset before raw key: %s %v", reset, err)
	}
	if _, err := protocol.DecodeInputFrame(r, 4096); err != nil {
		t.Fatal(err)
	}
}
