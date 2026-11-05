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
			frames <- f
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
	go func() { _, err := r.TapKey(context.Background(), "7", tapA, tapAllowed); result <- err }()
	first := nextTapFrame(t, frames)
	second := tapA
	second.Code++
	sent := make(chan error, 1)
	go func() { sent <- r.SendEvent(context.Background(), second, time.Now()) }()
	release := nextTapFrame(t, frames)
	other := nextTapFrame(t, frames)
	if first.Code != uint16(tapA.Code) || first.Action != uint8(remoteinput.ActionPress) || release.Code != first.Code || release.Action != uint8(remoteinput.ActionRelease) || other.Code != uint16(second.Code) {
		t.Fatalf("unordered frames: %+v %+v %+v", first, release, other)
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
	go func() { _, err := r.TapKey(ctx, "7", tapA, tapAllowed); done <- err }()
	if f := nextTapFrame(t, frames); f.Action != uint8(remoteinput.ActionPress) {
		t.Fatal(f)
	}
	cancel()
	if f := nextTapFrame(t, frames); f.Action != uint8(remoteinput.ActionRelease) {
		t.Fatal(f)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
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
	case f := <-frames:
		t.Fatalf("late event: %+v", f)
	case <-time.After(30 * time.Millisecond):
	}
}
func TestTapKeyAdmissionAfterLock(t *testing.T) {
	r, frames := tapRig(t)
	denied := errors.New("ownership lost")
	if _, err := r.TapKey(context.Background(), "7", tapA, func() error { return denied }); !errors.Is(err, denied) {
		t.Fatalf("ownership: %v", err)
	}
	if _, err := r.TapKey(context.Background(), "8", tapA, tapAllowed); !errors.Is(err, ErrRemoteInputInvalid) {
		t.Fatalf("session: %v", err)
	}
	_ = r.inputState.Apply(tapA)
	if _, err := r.TapKey(context.Background(), "7", tapA, tapAllowed); !errors.Is(err, ErrRemoteInputBusy) {
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
		if _, err := r.TapKey(context.Background(), "7", event, tapAllowed); !errors.Is(err, ErrRemoteInputInvalid) {
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
			go func() { _, err := r.TapKey(context.Background(), "7", tapA, tapAllowed); done <- err }()
			f := nextTapFrame(t, frames)
			if f.Action != uint8(remoteinput.ActionPress) {
				t.Fatalf("first frame %+v", f)
			}
			if failAt == 2 {
				if f = nextTapFrame(t, frames); f.Action != uint8(remoteinput.ActionRelease) {
					t.Fatalf("second frame %+v", f)
				}
			}
			if err := <-done; err == nil {
				t.Fatal("uncertain write accepted")
			}
			if r.inputState.Pressed(tapA.Code) || r.conn != nil || r.state != RemoteInputFailed {
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
			_, err := r.TapKey(context.Background(), "7", tapA, func() error {
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
	go func() { _, err := r.TapKey(ctx, "7", tapA, tapAllowed); done <- err }()
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
