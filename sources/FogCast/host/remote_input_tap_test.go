package host

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

var tapA = remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: 0x1004}

func tapAllowed() error { return nil }

func tapRig(t *testing.T) (*RemoteInput, <-chan protocol.InputFrame) {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() { left.Close(); right.Close() })
	r := &RemoteInput{conn: left, reader: bufio.NewReader(left), state: RemoteInputAttached, session: 7, now: time.Now, dial: time.Second}
	frames := make(chan protocol.InputFrame, 16)
	go func() {
		defer close(frames)
		for {
			f, err := protocol.DecodeInputFrame(right, 1024)
			if err != nil {
				return
			}
			if f.Header.Type == protocol.InputTypePing && f.Header.Flags == protocol.InputFlagTapProbe {
				reply := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeReceipt, Flags: protocol.InputFlagTapSupported, Session: f.Header.Session}, Seq: f.Seq, ClientMonoNS: f.ClientMonoNS, ServerMonoNS: 1}
				wire, _ := protocol.EncodeInputFrame(reply)
				if _, err := right.Write(wire); err != nil {
					return
				}
				continue
			}
			frames <- f
			if f.Header.Type == protocol.InputTypeTap {
				time.Sleep(time.Duration(protocol.InputTapHoldMS) * time.Millisecond)
				reply := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeReceipt, Session: f.Header.Session}, Seq: f.Seq, ClientMonoNS: f.ClientMonoNS, ServerMonoNS: 1}
				wire, _ := protocol.EncodeInputFrame(reply)
				if _, err := right.Write(wire); err != nil {
					return
				}
			}
		}
	}()
	return r, frames
}
func nextTapFrame(t *testing.T, frames <-chan protocol.InputFrame) protocol.InputFrame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("stream closed")
		}
		return f
	case <-time.After(time.Second):
		t.Fatal("frame timeout")
		return protocol.InputFrame{}
	}
}
func TestTapKeyOrderedWithConcurrentInput(t *testing.T) {
	r, frames := tapRig(t)
	result := make(chan error, 1)
	go func() { _, err := r.TapKey(context.Background(), "7", 7, tapA, tapAllowed); result <- err }()
	first := nextTapFrame(t, frames)
	second := tapA
	second.Code++
	sent := make(chan error, 1)
	go func() { sent <- r.SendEvent(context.Background(), second, time.Now()) }()
	other := nextTapFrame(t, frames)
	if first.Header.Type != protocol.InputTypeTap || first.Code != uint16(tapA.Code) || first.ServerMonoNS != 7 || other.Code != uint16(second.Code) {
		t.Fatalf("unordered frames: %+v %+v", first, other)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
}
func TestTapKeyCancellationReleasesAndNoLatePress(t *testing.T) {
	r, frames := tapRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := r.TapKey(ctx, "7", 7, tapA, tapAllowed); done <- err }()
	if f := nextTapFrame(t, frames); f.Header.Type != protocol.InputTypeTap {
		t.Fatal(f)
	}
	cancel()
	if err := <-done; !errors.Is(err, ErrRemoteInputUnconfirmed) {
		t.Fatalf("tap: %v", err)
	}
	if r.inputState.Pressed(tapA.Code) {
		t.Fatal("key held after cancel")
	}

	// A canceled event queued behind a heartbeat/other mutex holder must not
	// run after an emergency release or into the replacement session.
	r.mu.Lock()
	ctx2, cancel2 := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { queued <- r.SendEvent(ctx2, tapA, time.Now()) }()
	cancel2()
	r.mu.Unlock()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued event: %v", err)
	}
	select {
	case f, ok := <-frames:
		if ok {
			t.Fatalf("late event: %+v", f)
		}
	case <-time.After(30 * time.Millisecond):
	}
}
func TestTapKeyAdmissionAfterLock(t *testing.T) {
	r, frames := tapRig(t)
	denied := errors.New("ownership lost")
	if _, err := r.TapKey(context.Background(), "7", 7, tapA, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("ownership: %v", err)
	}
	if _, err := r.TapKey(context.Background(), "8", 7, tapA, tapAllowed); !errors.Is(err, ErrRemoteInputInvalid) {
		t.Fatalf("session: %v", err)
	}
	_ = r.inputState.Apply(tapA)
	if _, err := r.TapKey(context.Background(), "7", 7, tapA, tapAllowed); !errors.Is(err, ErrRemoteInputBusy) {
		t.Fatalf("held: %v", err)
	}
	if !r.inputState.Pressed(tapA.Code) {
		t.Fatal("existing press disturbed")
	}
	select {
	case f := <-frames:
		t.Fatalf("rejected frame: %+v", f)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestTapKeyRejectsUnsupportedHIDUsages(t *testing.T) {
	r, frames := tapRig(t)
	for _, code := range []remoteinput.Code{0x1003, 0x1080, 0x10df, 0x10e8, 0x1104} {
		event := tapA
		event.Code = code
		if _, err := r.TapKey(context.Background(), "7", 7, event, tapAllowed); !errors.Is(err, ErrRemoteInputInvalid) {
			t.Fatalf("code %#x: %v", code, err)
		}
	}
	select {
	case f := <-frames:
		t.Fatalf("invalid usage emitted: %+v", f)
	case <-time.After(30 * time.Millisecond):
	}
}

type failAfterWriteConn struct {
	net.Conn
	failAt int
	writes int
}

func (c *failAfterWriteConn) Write(data []byte) (int, error) {
	c.writes++
	if c.writes == c.failAt {
		n, err := c.Conn.Write(data)
		if err != nil {
			return n, err
		}
		return n, errors.New("response lost after frame delivery")
	}
	return c.Conn.Write(data)
}
func TestTapUncertainWriteClearsStateAndClosesStream(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			r, frames := tapRig(t)
			r.conn = &failAfterWriteConn{Conn: r.conn, failAt: failAt}
			done := make(chan error, 1)
			go func() { _, err := r.TapKey(context.Background(), "7", 7, tapA, tapAllowed); done <- err }()
			if failAt == 2 {
				if f := nextTapFrame(t, frames); f.Header.Type != protocol.InputTypeTap {
					t.Fatalf("tap frame %+v", f)
				}
			}
			if err := <-done; err == nil {
				t.Fatal("uncertain write accepted")
			}
			wantState := RemoteInputFailed
			if failAt == 1 { // capability probe contains no input
				wantState = RemoteInputReconnecting
			}
			if r.inputState.Pressed(tapA.Code) || r.conn != nil || r.state != wantState {
				t.Fatalf("unsafe cleanup: state=%v conn=%v", r.state, r.conn)
			}
		})
	}
}
func TestTapRechecksOwnershipAndSessionAfterContention(t *testing.T) {
	for _, lostSession := range []bool{false, true} {
		r, frames := tapRig(t)
		r.mu.Lock()
		started := make(chan struct{})
		done := make(chan error, 1)
		owned := true
		go func() {
			close(started)
			_, err := r.TapKey(context.Background(), "7", 7, tapA, func() error {
				if !owned {
					return errors.New("lease lost")
				}
				return nil
			})
			done <- err
		}()
		<-started
		if lostSession {
			r.session = 8
		} else {
			owned = false
		}
		r.mu.Unlock()
		if err := <-done; err == nil {
			t.Fatal("stale attachment or lease accepted")
		}
		select {
		case f := <-frames:
			t.Fatalf("stale tap emitted %+v", f)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestCanceledTapWaitingForLockDoesNotPress(t *testing.T) {
	r, frames := tapRig(t)
	r.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := r.TapKey(ctx, "7", 7, tapA, tapAllowed); done <- err }()
	cancel()
	r.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled tap: %v", err)
	}
	select {
	case f := <-frames:
		t.Fatalf("canceled tap sent %+v", f)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestTapProbeUnsupportedNeverDispatches(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			left, right := net.Pipe()
			t.Cleanup(func() { left.Close(); right.Close() })
			r := &RemoteInput{conn: left, reader: bufio.NewReader(left), state: RemoteInputAttached, session: 7, now: time.Now, dial: time.Second}
			seen := make(chan protocol.InputFrame, 2)
			go func() {
				defer close(seen)
				f, err := protocol.DecodeInputFrame(right, 1024)
				if err != nil {
					return
				}
				seen <- f
				if legacy {
					right.Close()
					return
				}
				reply := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeReceipt, Session: f.Header.Session}, Seq: f.Seq, ClientMonoNS: f.ClientMonoNS, ServerMonoNS: 1}
				wire, _ := protocol.EncodeInputFrame(reply)
				if _, err := right.Write(wire); err != nil {
					return
				}
				right.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				if f, err := protocol.DecodeInputFrame(right, 1024); err == nil {
					seen <- f
				}
			}()
			if _, err := r.TapKey(context.Background(), "7", 7, tapA, tapAllowed); !errors.Is(err, ErrRemoteInputNoStream) {
				t.Fatalf("tap: %v", err)
			}
			f := <-seen
			if f.Header.Type != protocol.InputTypePing || f.Header.Flags != protocol.InputFlagTapProbe {
				t.Fatalf("first frame %+v", f)
			}
			if extra, ok := <-seen; ok {
				t.Fatalf("input dispatched %+v", extra)
			}
			if r.inputState.Pressed(tapA.Code) {
				t.Fatal("unsupported tap changed held keys")
			}
			if legacy && (r.state != RemoteInputReconnecting || r.conn != nil) {
				t.Fatalf("legacy state=%v conn=%v", r.state, r.conn)
			}
			if !legacy && (r.state != RemoteInputAttached || r.conn == nil) {
				t.Fatalf("unsupported state=%v conn=%v", r.state, r.conn)
			}
		})
	}
}

func TestTapDefersWhileRemoteControlsHeld(t *testing.T) {
	otherKey := tapA
	otherKey.Code++
	for _, tc := range []struct {
		name  string
		event remoteinput.Event
	}{
		{"other-key", otherKey},
		{"second-player-button", remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}},
		{"axis", remoteinput.Event{Player: 1, Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftX, Value: 100}},
		{"mouse-button", remoteinput.Event{Device: remoteinput.DeviceMouse, Kind: remoteinput.KindRelative, Action: remoteinput.ActionRelative, Code: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left, right := net.Pipe()
			t.Cleanup(func() { left.Close(); right.Close() })
			r := &RemoteInput{conn: left, reader: bufio.NewReader(left), state: RemoteInputAttached, session: 7, now: time.Now, dial: time.Second}
			if err := r.inputState.Apply(tc.event); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if _, err := r.TapKey(ctx, "7", 7, tapA, tapAllowed); !errors.Is(err, ErrRemoteInputBusy) {
				t.Fatalf("tap: %v", err)
			}
			if r.conn == nil || r.state != RemoteInputAttached {
				t.Fatal("held source was disconnected")
			}
		})
	}
}

func TestSupportedTapDoesNotRiskOtherRemoteHolds(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	r := &RemoteInput{conn: left, reader: bufio.NewReader(left), state: RemoteInputAttached, session: 7, now: time.Now, dial: time.Second, tapReady: true}
	otherKey := tapA
	otherKey.Code++
	if err := r.inputState.Apply(otherKey); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := r.TapKey(ctx, "7", 7, tapA, tapAllowed); !errors.Is(err, ErrRemoteInputBusy) {
		t.Fatalf("tap: %v", err)
	}
	if !r.inputState.Pressed(otherKey.Code) || r.conn == nil {
		t.Fatal("another remote hold was changed")
	}
}
