package localfeed

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestFeedV2RejectsLegacyKeyboardHistory(t *testing.T) {
	conn := &memoryConn{}
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) { return conn, nil })
	key, _ := hidkeys.Event(4, true)
	if err := feed.Send(key, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := feed.ReserveV2(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("legacy source promoted without neutral: %v", err)
	}
	feed.Close()
}

func TestFeedReserveV2FencesAllInputBeforeRecovery(t *testing.T) {
	dials := 0
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) { dials++; return &memoryConn{}, nil })
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	key, _ := hidkeys.Event(4, true)
	for _, event := range []remoteinput.Event{pad, key} {
		if err := feed.Send(event, time.Time{}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("reserved feed accepted event: %v", err)
		}
	}
	if dials != 0 {
		t.Fatalf("reservation dialed %d times", dials)
	}
	if err := feed.ReserveV2(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("duplicate reservation accepted")
	}
}

func TestFeedReserveV2RejectsExistingConnection(t *testing.T) {
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) { return &memoryConn{}, nil })
	defer feed.Close()
	pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if err := feed.Send(pad, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if err := feed.ReserveV2(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("reservation adopted existing stream")
	}
	feed.Close()
	if err := feed.ReserveV2(); !errors.Is(err, ErrUnavailable) {
		t.Fatal("reservation forgot closed legacy stream")
	}
	if _, _, err := feed.RecoverV2(context.Background(), strings.Repeat("a", 32), "kit", "boot", time.Second); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("recovery adopted closed legacy stream: %v", err)
	}
}

func TestFeedRecoveryRequiresPristineReservation(t *testing.T) {
	dials := 0
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) {
		dials++
		return nil, errors.New("offline")
	})
	agent := strings.Repeat("a", 32)
	if _, _, err := feed.RecoverV2(context.Background(), agent, "kit", "boot", time.Second); !errors.Is(err, ErrUnavailable) || dials != 0 {
		t.Fatalf("unreserved recovery: err=%v dials=%d", err, dials)
	}
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := feed.RecoverV2(context.Background(), agent, "kit", "boot", time.Second); !errors.Is(err, ErrUnavailable) || dials != 1 {
		t.Fatalf("reserved failed dial: err=%v dials=%d", err, dials)
	}
	key, _ := hidkeys.Event(4, true)
	if err := feed.Send(key, time.Time{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("failed dial released the raw keyboard fence")
	}
	if _, _, err := feed.RecoverV2(context.Background(), agent, "kit", "boot", time.Second); !errors.Is(err, ErrUnavailable) || dials != 2 {
		t.Fatalf("reserved recovery retry: err=%v dials=%d", err, dials)
	}
}

func TestFeedV2SharesPadAndNeverFallsBackToRawKeyboard(t *testing.T) {
	feed, handle, peer, reader := recoveredPadFeed(t)
	beginDone := make(chan error, 1)
	go func() { _, err := feed.RequestV2(context.Background(), handle, v2Begin()); beginDone <- err }()
	req, err := localhid.ReadRequest(reader)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := hidkeys.Event(4, true)
	if err := feed.Send(key, time.Time{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("raw keyboard bypass: %v", err)
	}
	padDone := make(chan error, 1)
	go func() {
		padDone <- feed.Send(remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}, time.Time{})
	}()
	frame, err := protocol.DecodeInputFrame(reader, 48)
	if err != nil || frame.Device != uint8(remoteinput.DeviceGamepad) {
		t.Fatalf("pad frame %+v: %v", frame, err)
	}
	if err := <-padDone; err != nil {
		t.Fatal(err)
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "begin", Request: req.Request, Token: strings.Repeat("c", 64), Status: "ready"})
	if err := <-beginDone; err != nil {
		t.Fatal(err)
	}
	feed.Close()
	if err := feed.Send(key, time.Time{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("raw keyboard after close: %v", err)
	}
	if err := feed.ReserveV2(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("duplicate v2 reservation: %v", err)
	}
}

func TestFeedRecoveryRequiresCorrelatedReplyOnSameConnection(t *testing.T) {
	client, peer := net.Pipe() // In-memory only; no device or local socket.
	defer peer.Close()
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) { return client, nil })
	defer feed.Close()
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	agent := strings.Repeat("a", 32)
	type result struct {
		reply  localhid.Reply
		handle V2Handle
		err    error
	}
	done := make(chan result, 1)
	go func() {
		reply, handle, err := feed.RecoverV2(context.Background(), agent, "kit", "boot", time.Second)
		done <- result{reply, handle, err}
	}()
	reader := bufio.NewReader(peer)
	req, err := localhid.ReadRequest(reader)
	if err != nil || req.Op != "recover" {
		t.Fatalf("recovery request %+v: %v", req, err)
	}
	pad := remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}
	if err := feed.Send(pad, time.Time{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("pad before recovery: %v", err)
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: agent, TargetID: "kit", BootID: "boot", Status: "reader-drained"})
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	feed.mu.Lock()
	if err := feed.PromoteRecovered(got.handle, got.reply); !errors.Is(err, ErrUnavailable) {
		feed.mu.Unlock()
		t.Fatalf("busy promotion did not fail closed: %v", err)
	}
	feed.mu.Unlock()
	wrong := got.reply
	wrong.AgentInstance = strings.Repeat("b", 32)
	if err := feed.PromoteRecovered(got.handle, wrong); err == nil {
		t.Fatal("different recovery ACK promoted")
	}
	if err := feed.PromoteRecovered(got.handle, got.reply); err != nil {
		t.Fatal(err)
	}
	key, _ := hidkeys.Event(4, true)
	if err := feed.Send(key, time.Time{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("keyboard bypassed snapshot: %v", err)
	}
	padDone := make(chan error, 1)
	go func() { padDone <- feed.Send(pad, time.Time{}) }()
	frame, err := protocol.DecodeInputFrame(reader, 48)
	if err != nil || frame.Device != uint8(remoteinput.DeviceGamepad) {
		t.Fatalf("pad after recovery %+v: %v", frame, err)
	}
	if err := <-padDone; err != nil {
		t.Fatal(err)
	}
}

func TestFeedRecoveryFailureAllowsFreshRetryButNeverRawKeyboard(t *testing.T) {
	first, peer1 := net.Pipe()
	second, peer2 := net.Pipe()
	defer peer1.Close()
	defer peer2.Close()
	dials := 0
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) {
		dials++
		if dials == 1 {
			return first, nil
		}
		return second, nil
	})
	defer feed.Close()
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	agent := strings.Repeat("a", 32)
	type result struct {
		reply  localhid.Reply
		handle V2Handle
		err    error
	}
	start := func() <-chan result {
		done := make(chan result, 1)
		go func() {
			reply, handle, err := feed.RecoverV2(context.Background(), agent, "kit", "boot", time.Second)
			done <- result{reply, handle, err}
		}()
		return done
	}
	failed := start()
	req, err := localhid.ReadRequest(bufio.NewReader(peer1))
	if err != nil {
		t.Fatal(err)
	}
	v2Reply(t, peer1, localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: agent, TargetID: "kit", BootID: "boot", Status: "unavailable"})
	if got := <-failed; got.err == nil {
		t.Fatal("unavailable recovery accepted")
	}
	key, _ := hidkeys.Event(4, true)
	if err := feed.Send(key, time.Time{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("failed recovery enabled raw keyboard")
	}
	retry := start()
	req, err = localhid.ReadRequest(bufio.NewReader(peer2))
	if err != nil {
		t.Fatal(err)
	}
	v2Reply(t, peer2, localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: agent, TargetID: "kit", BootID: "boot", Status: "reader-drained"})
	if got := <-retry; got.err != nil {
		t.Fatalf("fresh retry: %v", got.err)
	}
}

func TestFeedCloseDuringRecoveryKeepsKeyboardFenced(t *testing.T) {
	first, peer := net.Pipe()
	defer peer.Close()
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) { return first, nil })
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := feed.RecoverV2(context.Background(), strings.Repeat("a", 32), "kit", "boot", time.Second)
		done <- err
	}()
	if _, err := localhid.ReadRequest(bufio.NewReader(peer)); err != nil {
		t.Fatal(err)
	}
	feed.Close()
	if err := <-done; err == nil {
		t.Fatal("closed recovery accepted")
	}
	key, _ := hidkeys.Event(4, true)
	if err := feed.Send(key, time.Time{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("close enabled raw keyboard")
	}
	feed.mu.Lock()
	defer feed.mu.Unlock()
	if feed.recovering || !feed.v2Used || feed.v2 != nil || feed.conn != nil {
		t.Fatal("closed recovery left inconsistent ownership")
	}
}

func TestFeedV2DoneReportsIdleConnectionLoss(t *testing.T) {
	_, handle, peer, _ := recoveredPadFeed(t)
	done := handle.Done()
	if done == nil {
		t.Fatal("missing transport signal")
	}
	_ = peer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle EOF was not reported")
	}
}

func TestFeedStaleRecoveryHandleCannotTouchNewConnection(t *testing.T) {
	first, peer1 := net.Pipe()
	second, peer2 := net.Pipe()
	defer peer1.Close()
	defer peer2.Close()
	dials := 0
	feed := New("unused", func(string, string, time.Duration) (net.Conn, error) {
		dials++
		if dials == 1 {
			return first, nil
		}
		return second, nil
	})
	defer feed.Close()
	if err := feed.ReserveV2(); err != nil {
		t.Fatal(err)
	}
	agent := strings.Repeat("a", 32)
	type result struct {
		reply  localhid.Reply
		handle V2Handle
		err    error
	}
	recoverOn := func(peer net.Conn) result {
		done := make(chan result, 1)
		go func() {
			reply, handle, err := feed.RecoverV2(context.Background(), agent, "kit", "boot", time.Second)
			done <- result{reply, handle, err}
		}()
		req, err := localhid.ReadRequest(bufio.NewReader(peer))
		if err != nil {
			t.Fatal(err)
		}
		v2Reply(t, peer, localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: agent, TargetID: "kit", BootID: "boot", Status: "reader-drained"})
		return <-done
	}
	old := recoverOn(peer1)
	if old.err != nil {
		t.Fatal(old.err)
	}
	feed.CloseV2(old.handle)
	select {
	case <-old.handle.Done():
	default:
		t.Fatal("old signal not closed")
	}
	current := recoverOn(peer2)
	if current.err != nil {
		t.Fatal(current.err)
	}
	if err := feed.PromoteRecovered(old.handle, old.reply); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale promotion: %v", err)
	}
	feed.CloseV2(old.handle)
	select {
	case <-current.handle.Done():
		t.Fatal("stale close killed current connection")
	default:
	}
	if _, err := feed.RequestV2(context.Background(), old.handle, v2Begin()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("stale request: %v", err)
	}
	if err := feed.PromoteRecovered(current.handle, current.reply); err != nil {
		t.Fatal(err)
	}
}
