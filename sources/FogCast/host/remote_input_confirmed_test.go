package host

import (
	"bufio"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func confirmedRig(t *testing.T, timeout time.Duration) (*RemoteInput, net.Conn) {
	t.Helper()
	client, peer := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close() })
	return &RemoteInput{conn: client, reader: bufio.NewReader(client), state: RemoteInputAttached,
		ready: true, session: 7, now: time.Now, dial: timeout, mouse: true}, peer
}

func readConfirmedFrame(t *testing.T, peer net.Conn) protocol.InputFrame {
	t.Helper()
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	frame, err := protocol.DecodeInputFrame(peer, 4096)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	_ = peer.SetReadDeadline(time.Time{})
	return frame
}

func replyConfirmed(t *testing.T, peer net.Conn, ping protocol.InputFrame) {
	t.Helper()
	ping.Header.Type = protocol.InputTypePong
	ping.ServerMonoNS = 1
	wire, err := protocol.EncodeInputFrame(ping)
	if err != nil {
		t.Fatal(err)
	}
	_ = peer.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := peer.Write(wire); err != nil {
		t.Fatalf("write pong: %v", err)
	}
	_ = peer.SetWriteDeadline(time.Time{})
}

func TestConfirmedInputWaitsForCorrelatedPongAndIgnoresStale(t *testing.T) {
	r, peer := confirmedRig(t, time.Second)
	done := make(chan error, 1)
	go func() { done <- r.SendEventConfirmed(context.Background(), tapA, time.Now()) }()
	event := readConfirmedFrame(t, peer)
	ping := readConfirmedFrame(t, peer)
	if event.Header.Type != protocol.InputTypeInput || ping.Header.Type != protocol.InputTypePing || ping.Seq != event.Seq {
		t.Fatalf("event=%+v ping=%+v", event, ping)
	}
	stale := ping
	stale.ClientMonoNS--
	replyConfirmed(t, peer, stale)
	select {
	case err := <-done:
		t.Fatalf("stale pong completed input: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	replyConfirmed(t, peer, ping)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r.sequence != event.Seq {
		t.Fatalf("barrier changed input sequence: %d vs %d", r.sequence, event.Seq)
	}
}

func TestConfirmedInputFailsClosedWhenReceiptIsLost(t *testing.T) {
	for _, mode := range []string{"close", "timeout", "future-pong", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			r, peer := confirmedRig(t, 100*time.Millisecond)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- r.SendEventConfirmed(ctx, tapA, time.Now()) }()
			_ = readConfirmedFrame(t, peer)
			ping := readConfirmedFrame(t, peer)
			switch mode {
			case "close":
				_ = peer.Close()
			case "future-pong":
				ping.ClientMonoNS++
				replyConfirmed(t, peer, ping)
			case "cancel":
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, ErrRemoteInputUnconfirmed) {
					t.Fatalf("error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("unconfirmed input did not return")
			}
			if r.state != RemoteInputFailed || r.ready || r.conn != nil || r.inputState.Pressed(tapA.Code) {
				t.Fatalf("uncertain input left active state: state=%v ready=%v conn=%v", r.state, r.ready, r.conn)
			}
		})
	}
}

func TestConfirmedMouseMotionIsNotReplayedAfterLostPong(t *testing.T) {
	r, peer := confirmedRig(t, 80*time.Millisecond)
	bridge := &confirmedCountingBridge{}
	r.bridge = bridge
	done := make(chan error, 1)
	event := remoteinput.MouseEvent(4, -3, 1)
	go func() { done <- r.SendEventConfirmed(context.Background(), event, time.Now()) }()
	frame := readConfirmedFrame(t, peer)
	ping := readConfirmedFrame(t, peer)
	if frame.Device != uint8(remoteinput.DeviceMouse) || ping.Header.Type != protocol.InputTypePing {
		t.Fatalf("event=%+v ping=%+v", frame, ping)
	}
	if err := <-done; !errors.Is(err, ErrRemoteInputUnconfirmed) {
		t.Fatalf("error = %v", err)
	}
	if r.conn != nil || r.state != RemoteInputFailed {
		t.Fatalf("stream retained: %v %v", r.conn, r.state)
	}
	if bridge.dials != 0 || bridge.stops != 1 {
		t.Fatalf("ambiguous motion triggered reconnect: dials=%d stops=%d", bridge.dials, bridge.stops)
	}
	_ = peer.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if frame, err := protocol.DecodeInputFrame(peer, 4096); err == nil {
		t.Fatalf("motion replayed: %+v", frame)
	}
}

func TestConfirmedInputDoesNotReconnectAfterUncertainWrite(t *testing.T) {
	r, peer := confirmedRig(t, time.Second)
	bridge := &confirmedCountingBridge{}
	r.bridge = bridge
	r.conn = &failAfterWriteConn{Conn: r.conn, failAt: 1}
	done := make(chan error, 1)
	go func() { done <- r.SendEventConfirmed(context.Background(), tapA, time.Now()) }()
	frame := readConfirmedFrame(t, peer)
	if frame.Header.Type != protocol.InputTypeInput {
		t.Fatalf("input frame = %+v", frame)
	}
	if err := <-done; !errors.Is(err, ErrRemoteInputUnconfirmed) {
		t.Fatalf("error = %v", err)
	}
	if bridge.dials != 0 || bridge.stops != 1 || r.conn != nil || r.inputState.Pressed(tapA.Code) {
		t.Fatalf("uncertain input reconnected or retained state: dials=%d stops=%d conn=%v", bridge.dials, bridge.stops, r.conn)
	}
}

type confirmedCountingBridge struct{ dials, stops int }

func (*confirmedCountingBridge) Ready() <-chan struct{} { return nil }
func (*confirmedCountingBridge) Endpoint() string       { return "unused" }
func (h *confirmedCountingBridge) Stop(context.Context) error {
	h.stops++
	return nil
}
func (h *confirmedCountingBridge) Dial(context.Context) (net.Conn, error) {
	h.dials++
	return nil, errors.New("unexpected reconnect")
}

func TestConfirmedInputSerializesReleaseBehindPong(t *testing.T) {
	r, peer := confirmedRig(t, time.Second)
	pressDone := make(chan error, 1)
	go func() { pressDone <- r.SendEventConfirmed(context.Background(), tapA, time.Now()) }()
	_ = readConfirmedFrame(t, peer)
	pressPing := readConfirmedFrame(t, peer)
	release := tapA
	release.Action = remoteinput.ActionRelease
	releaseDone := make(chan error, 1)
	go func() { releaseDone <- r.SendEventConfirmed(context.Background(), release, time.Now()) }()
	// Existing ordered callers cannot dispatch the release until the previous
	// barrier completes. Keep this latency visible in the physical test plan.
	_ = peer.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if frame, err := protocol.DecodeInputFrame(peer, 4096); err == nil {
		t.Fatalf("release bypassed pending barrier: %+v", frame)
	}
	_ = peer.SetReadDeadline(time.Time{})
	replyConfirmed(t, peer, pressPing)
	if err := <-pressDone; err != nil {
		t.Fatal(err)
	}
	frame := readConfirmedFrame(t, peer)
	if frame.Action != uint8(remoteinput.ActionRelease) {
		t.Fatalf("release=%+v", frame)
	}
	ping := readConfirmedFrame(t, peer)
	replyConfirmed(t, peer, ping)
	if err := <-releaseDone; err != nil {
		t.Fatal(err)
	}
}

func TestConfirmedInputSerializesDetachAndInvalidation(t *testing.T) {
	for _, action := range []string{"detach", "invalidate"} {
		t.Run(action, func(t *testing.T) {
			r, peer := confirmedRig(t, time.Second)
			sent := make(chan error, 1)
			go func() { sent <- r.SendEventConfirmed(context.Background(), tapA, time.Now()) }()
			_ = readConfirmedFrame(t, peer)
			ping := readConfirmedFrame(t, peer)
			finished := make(chan struct{})
			go func() {
				if action == "detach" {
					_ = r.Detach(context.Background(), "session_stop")
				} else {
					r.Invalidate()
				}
				close(finished)
			}()
			select {
			case <-finished:
				t.Fatal("lifecycle operation bypassed the in-flight barrier")
			case <-time.After(20 * time.Millisecond):
			}
			replyConfirmed(t, peer, ping)
			if err := <-sent; err != nil {
				t.Fatal(err)
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("lifecycle operation did not finish")
			}
			if status := r.Status(); status.State != RemoteInputDetached || status.Ready {
				t.Fatalf("status after %s: %+v", action, status)
			}
		})
	}
}

type confirmedRejectSink struct{}

func (*confirmedRejectSink) Apply(protocol.InputFrame) error {
	return bridge.RejectInput("test rejection")
}
func (*confirmedRejectSink) ReleaseAll() error { return nil }
func (*confirmedRejectSink) Close() error      { return nil }

type confirmedBridgeHandle struct{ server *bridge.Server }

func (h *confirmedBridgeHandle) Ready() <-chan struct{}     { return h.server.Ready() }
func (h *confirmedBridgeHandle) Endpoint() string           { return h.server.Addr().String() }
func (h *confirmedBridgeHandle) Stop(context.Context) error { return h.server.Close() }

func TestConfirmedPongDoesNotProveSinkAcceptedInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var server *bridge.Server
	starter := BridgeStarterFunc(func(ctx context.Context, spec BridgeSpec) (BridgeHandle, error) {
		var err error
		server, err = bridge.New(bridge.Config{Addr: "127.0.0.1:0", Token: spec.Token, Session: spec.Session, Core: spec.Core}, &confirmedRejectSink{})
		if err != nil {
			return nil, err
		}
		go func() { _ = server.ListenAndServe(ctx) }()
		return &confirmedBridgeHandle{server: server}, nil
	})
	r, err := NewRemoteInput(RemoteInputConfig{Starter: starter, HeartbeatInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Attach(ctx, "Pong"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := r.SendEventConfirmed(ctx, tapA, time.Now()); err != nil {
			t.Fatalf("stream barrier: %v", err)
		}
	}
	metrics := server.Metrics().Snapshot()
	if metrics["rejected"] != 2 || metrics["applied"] != 0 || metrics["sequence_gaps"] != 0 {
		t.Fatalf("bridge metrics = %#v", metrics)
	}
}

type confirmedAcceptSink struct{}

func (*confirmedAcceptSink) Apply(protocol.InputFrame) error { return nil }
func (*confirmedAcceptSink) ReleaseAll() error               { return nil }
func (*confirmedAcceptSink) Close() error                    { return nil }

func TestConfirmedInputReconnectsBeforeDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var server *bridge.Server
	starter := BridgeStarterFunc(func(ctx context.Context, spec BridgeSpec) (BridgeHandle, error) {
		var err error
		server, err = bridge.New(bridge.Config{Addr: "127.0.0.1:0", Token: spec.Token, Session: spec.Session, Core: spec.Core}, &confirmedAcceptSink{})
		if err != nil {
			return nil, err
		}
		go func() { _ = server.ListenAndServe(ctx) }()
		return &confirmedBridgeHandle{server: server}, nil
	})
	r, err := NewRemoteInput(RemoteInputConfig{Starter: starter, HeartbeatInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Attach(ctx, "Pong"); err != nil {
		t.Fatal(err)
	}
	source, err := r.ClaimSource(r.Status().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if got := r.Status(); got.State != RemoteInputReconnecting {
		t.Fatalf("closed source status = %+v", got)
	}
	if err := r.SendEventConfirmed(ctx, tapA, time.Now()); err != nil {
		t.Fatalf("event after source close: %v", err)
	}
	if got := server.Metrics().Snapshot(); got["applied"] != 1 {
		t.Fatalf("bridge metrics = %#v", got)
	}
}
