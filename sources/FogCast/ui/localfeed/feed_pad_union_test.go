package localfeed

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

type cancelAfterMutation struct {
	context.Context
	calls atomic.Int32
}

func (c *cancelAfterMutation) Err() error {
	if c.calls.Add(1) >= 2 {
		return context.Canceled
	}
	return nil
}

func recoveredPadFeed(t *testing.T) (*Feed, V2Handle, net.Conn, *bufio.Reader) {
	return recoveredPadFeedWithWrap(t, nil)
}

func recoveredPadFeedWithWrap(t *testing.T, wrap func(net.Conn) net.Conn) (*Feed, V2Handle, net.Conn, *bufio.Reader) {
	t.Helper()
	client, peer := net.Pipe() // In-memory connection only.
	if wrap != nil {
		client = wrap(client)
	}
	_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) { return client, nil })
	t.Cleanup(func() { feed.Close(); _ = peer.Close() })
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	type result struct {
		reply  localhid.Reply
		handle V2Handle
		err    error
	}
	done := make(chan result, 1)
	agent := strings.Repeat("a", 32)
	go func() {
		reply, handle, err := feed.RecoverV2(context.Background(), agent, "kit", "boot", time.Second)
		done <- result{reply, handle, err}
	}()
	reader := bufio.NewReader(peer)
	req, err := localhid.ReadRequest(reader)
	if err != nil {
		t.Fatal(err)
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: agent, TargetID: "kit", BootID: "boot", Status: "reader-drained"})
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if err := feed.PromoteRecoveredPad(got.handle, got.reply, 7, &PadWriteFence{}); err != nil {
		t.Fatal(err)
	}
	return feed, got.handle, peer, reader
}

type pausedPadWrite struct {
	net.Conn
	armed            atomic.Bool
	once             sync.Once
	entered, release chan struct{}
}

func (c *pausedPadWrite) Write(data []byte) (int, error) {
	if c.armed.Load() {
		c.once.Do(func() { close(c.entered); <-c.release })
	}
	return c.Conn.Write(data)
}

func sendPadFrame(t *testing.T, reader *bufio.Reader, send func() error, want remoteinput.Event) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- send() }()
	frame, err := protocol.DecodeInputFrame(reader, 48)
	if err != nil || eventOf(frame) != want {
		t.Fatalf("frame=%+v want=%+v err=%v", eventOf(frame), want, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func noPadFrame(t *testing.T, send func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- send() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("unexpected uncoalesced write")
	}
}

func TestMappedPadUnionPreservesOtherKeyboardAndPhysicalPad(t *testing.T) {
	feed, handle, _, reader := recoveredPadFeed(t)
	ctx := context.Background()
	button := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	release := button
	release.Action = remoteinput.ActionRelease
	a, b := localhid.DeviceID{1}, localhid.DeviceID{2}
	sendPadFrame(t, reader, func() error {
		return feed.SendNativeMappedPad(ctx, handle, 7, &PadWriteFence{}, &PadWriteFence{}, a, 30, button)
	}, button)
	noPadFrame(t, func() error { return feed.Send(button, time.Time{}) })
	noPadFrame(t, func() error {
		return feed.SendNativeMappedPad(ctx, handle, 7, &PadWriteFence{}, &PadWriteFence{}, b, 31, button)
	})
	noPadFrame(t, func() error { return feed.RetireNativeMappedPad(ctx, handle, 7, a) })
	noPadFrame(t, func() error { return feed.Send(release, time.Time{}) })
	sendPadFrame(t, reader, func() error {
		return feed.SendNativeMappedPad(ctx, handle, 7, &PadWriteFence{}, &PadWriteFence{}, b, 31, release)
	}, release)
	if err := feed.SendNativeMappedPad(ctx, handle, 8, &PadWriteFence{}, &PadWriteFence{}, b, 31, button); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale epoch: %v", err)
	}
}

func TestMappedPadDepartureDoesNotClearKeyboardOrSuccessorPad(t *testing.T) {
	feed, handle, _, reader := recoveredPadFeed(t)
	ctx := context.Background()
	a := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	b := a
	b.Code = remoteinput.ButtonB
	releaseA := a
	releaseA.Action = remoteinput.ActionRelease
	releaseB := b
	releaseB.Action = remoteinput.ActionRelease
	device := localhid.DeviceID{1}
	sendPadFrame(t, reader, func() error {
		return feed.SendNativeMappedPad(ctx, handle, 7, &PadWriteFence{}, &PadWriteFence{}, device, 30, a)
	}, a)
	noPadFrame(t, func() error { return feed.Send(a, time.Time{}) })
	noPadFrame(t, func() error { return feed.Send(remoteinput.LocalPlayerDeparture(0), time.Time{}) })
	sendPadFrame(t, reader, func() error { return feed.Send(b, time.Time{}) }, b)
	sendPadFrame(t, reader, func() error {
		return feed.SendNativeMappedPad(ctx, handle, 7, &PadWriteFence{}, &PadWriteFence{}, device, 30, releaseA)
	}, releaseA)
	sendPadFrame(t, reader, func() error { return feed.Send(releaseB, time.Time{}) }, releaseB)
}

func TestMappedPadFailureClosesExactHandleWithoutReplay(t *testing.T) {
	feed, handle, peer, _ := recoveredPadFeed(t)
	_ = peer.Close()
	e := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if err := feed.SendNativeMappedPad(context.Background(), handle, 7, &PadWriteFence{}, &PadWriteFence{}, localhid.DeviceID{1}, 30, e); err == nil {
		t.Fatal("closed connection accepted mapped press")
	}
	if err := feed.SendNativeMappedPad(context.Background(), handle, 7, &PadWriteFence{}, &PadWriteFence{}, localhid.DeviceID{1}, 30, e); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("old handle replay: %v", err)
	}
}

func TestMappedPadDepartureFlushesOnlyAfterLastKeyboardRelease(t *testing.T) {
	feed, handle, _, reader := recoveredPadFeed(t)
	press := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	release := press
	release.Action = remoteinput.ActionRelease
	device := localhid.DeviceID{1}
	sendPadFrame(t, reader, func() error {
		return feed.SendNativeMappedPad(context.Background(), handle, 7, &PadWriteFence{}, &PadWriteFence{}, device, 30, press)
	}, press)
	noPadFrame(t, func() error { return feed.Send(remoteinput.LocalPlayerDeparture(0), time.Time{}) })
	done := make(chan error, 1)
	go func() {
		done <- feed.SendNativeMappedPad(context.Background(), handle, 7, &PadWriteFence{}, &PadWriteFence{}, device, 30, release)
	}()
	first, err := protocol.DecodeInputFrame(reader, 48)
	if err != nil || eventOf(first) != release {
		t.Fatalf("release=%+v err=%v", eventOf(first), err)
	}
	second, err := protocol.DecodeInputFrame(reader, 48)
	if err != nil || !remoteinput.IsLocalPlayerDeparture(eventOf(second)) {
		t.Fatalf("departure=%+v err=%v", eventOf(second), err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPadUnionCancellationAfterMutationClosesConnection(t *testing.T) {
	for _, mapped := range []bool{false, true} {
		t.Run(map[bool]string{false: "physical", true: "mapped"}[mapped], func(t *testing.T) {
			feed, handle, _, _ := recoveredPadFeed(t)
			ctx := &cancelAfterMutation{Context: context.Background()}
			e := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
			var err error
			if mapped {
				err = feed.SendNativeMappedPad(ctx, handle, 7, &PadWriteFence{}, &PadWriteFence{}, localhid.DeviceID{1}, 30, e)
			} else {
				err = feed.send(ctx, e, time.Time{}, nil)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			select {
			case <-handle.Done():
			default:
				t.Fatal("mutated union kept connection live")
			}
			feed.mu.Lock()
			if len(feed.padUnion.physical) != 0 || len(feed.padUnion.mapped) != 0 {
				feed.mu.Unlock()
				t.Fatal("mutated union survived")
			}
			feed.mu.Unlock()
		})
	}
}

func TestMappedPadFenceRetiresOnlyMappedSource(t *testing.T) {
	feed, handle, _, reader := recoveredPadFeed(t)
	ctx := context.Background()
	mapping, deviceFence := &PadWriteFence{}, &PadWriteFence{}
	device := localhid.DeviceID{1}
	button := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	release := button
	release.Action = remoteinput.ActionRelease
	sendPadFrame(t, reader, func() error {
		return feed.SendNativeMappedPad(ctx, handle, 7, mapping, deviceFence, device, 30, button)
	}, button)
	noPadFrame(t, func() error { return feed.Send(button, time.Time{}) })
	mapping.Revoke()
	if err := feed.SendNativeMappedPad(ctx, handle, 7, mapping, deviceFence, device, 30, release); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("revoked mapped key: %v", err)
	}
	noPadFrame(t, func() error { return feed.RetireNativeMappedPad(ctx, handle, 7, device) })
	sendPadFrame(t, reader, func() error { return feed.Send(release, time.Time{}) }, release)
}

func TestMappedPadFenceRevokedDuringWriteClosesHandle(t *testing.T) {
	var guarded *pausedPadWrite
	feed, handle, _, reader := recoveredPadFeedWithWrap(t, func(conn net.Conn) net.Conn {
		guarded = &pausedPadWrite{Conn: conn, entered: make(chan struct{}), release: make(chan struct{})}
		return guarded
	})
	guarded.armed.Store(true)
	mapping, deviceFence := &PadWriteFence{}, &PadWriteFence{}
	e := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	done := make(chan error, 1)
	go func() {
		done <- feed.SendNativeMappedPad(context.Background(), handle, 7, mapping, deviceFence, localhid.DeviceID{1}, 30, e)
	}()
	<-guarded.entered
	mapping.Revoke()
	close(guarded.release)
	if _, err := protocol.DecodeInputFrame(reader, 48); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("revoked in-flight write reported success")
	}
	select {
	case <-handle.Done():
	default:
		t.Fatal("revoked in-flight handle stayed live")
	}
}

func TestPadAxisRejectsRevokedConnectionFence(t *testing.T) {
	feed, handle, _, _ := recoveredPadFeed(t)
	feed.mu.Lock()
	fence := feed.padUnion.fence
	feed.mu.Unlock()
	fence.Revoke()
	axis := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftX, Value: 123}
	if err := feed.Send(axis, time.Time{}); err == nil {
		t.Fatal("revoked axis was accepted")
	}
	select {
	case <-handle.Done():
	default:
		t.Fatal("revoked axis kept handle live")
	}
}

func TestPadAxisRevokedDuringWriteClosesHandle(t *testing.T) {
	var guarded *pausedPadWrite
	feed, handle, _, reader := recoveredPadFeedWithWrap(t, func(conn net.Conn) net.Conn {
		guarded = &pausedPadWrite{Conn: conn, entered: make(chan struct{}), release: make(chan struct{})}
		return guarded
	})
	feed.mu.Lock()
	fence := feed.padUnion.fence
	feed.mu.Unlock()
	guarded.armed.Store(true)
	axis := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindAxis, Action: remoteinput.ActionAbsolute, Code: remoteinput.AxisLeftX, Value: 123}
	done := make(chan error, 1)
	go func() { done <- feed.Send(axis, time.Time{}) }()
	<-guarded.entered
	fence.Revoke()
	close(guarded.release)
	if _, err := protocol.DecodeInputFrame(reader, 48); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("revoked axis write reported success")
	}
	select {
	case <-handle.Done():
	default:
		t.Fatal("revoked axis kept handle live")
	}
}
