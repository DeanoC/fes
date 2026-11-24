package localfeed

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func v2Pair(t *testing.T, timeout time.Duration) (*V2Transport, net.Conn) {
	t.Helper()
	a, b := net.Pipe() // in-memory connection; no local socket or device
	transport, err := NewV2RecoveryTransport(a, timeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close(); _ = transport.Close() })
	agent := strings.Repeat("a", 32)
	done := make(chan error, 1)
	go func() {
		req, err := localhid.ReadRequest(bufio.NewReader(b))
		if err == nil {
			var wire []byte
			wire, err = localhid.EncodeReply(localhid.Reply{Version: 2, Op: "recover", Request: req.Request, AgentInstance: agent, TargetID: "kit", BootID: "boot", Status: "reader-drained"})
			if err == nil {
				_, err = b.Write(wire)
			}
		}
		done <- err
	}()
	reply, err := transport.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: agent, TargetID: "kit", BootID: "boot"})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := transport.PromoteRecovered(reply); err != nil {
		t.Fatal(err)
	}
	return transport, b
}

func v2RecoveryPair(t *testing.T, timeout time.Duration) (*V2Transport, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	transport, err := NewV2RecoveryTransport(a, timeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close(); _ = transport.Close() })
	return transport, b
}

func v2Begin() localhid.Request {
	var device localhid.DeviceID
	device[0] = 1
	return localhid.Request{Op: "begin", Publisher: strings.Repeat("1", 32), QueueEpoch: 1,
		LeaseGeneration: strings.Repeat("a", 64), AgentInstance: strings.Repeat("e", 32), RuntimeInstance: strings.Repeat("d", 32), PackageID: strings.Repeat("b", 64), CoreGeneration: 1,
		Devices: []localhid.DeviceSnapshot{{ID: device, Held: []uint8{4}}}}
}

func v2Reply(t *testing.T, conn net.Conn, reply localhid.Reply) {
	t.Helper()
	wire, err := localhid.EncodeReply(reply)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Write(wire); err != nil {
		t.Fatal(err)
	}
}

func TestV2RecoveryCorrelatesFullAuthorityAndFailsClosed(t *testing.T) {
	oldAgent, newAgent := strings.Repeat("a", 32), strings.Repeat("b", 32)
	for _, tc := range []struct {
		name, status, agent, target, boot string
		success                           bool
	}{
		{"same-reader", "reader-drained", oldAgent, "kit-b", "boot-1", true},
		{"new-agent", "startup-retired", newAgent, "kit-b", "boot-1", true},
		{"wrong-agent", "reader-drained", newAgent, "kit-b", "boot-1", false},
		{"wrong-target", "reader-drained", oldAgent, "kit-a", "boot-1", false},
		{"wrong-boot", "reader-drained", oldAgent, "kit-b", "boot-2", false},
		{"unavailable", "unavailable", oldAgent, "kit-b", "boot-1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, peer := v2RecoveryPair(t, time.Second)
			done := make(chan v2Result, 1)
			go func() {
				reply, err := tr.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: oldAgent, TargetID: "kit-b", BootID: "boot-1"})
				done <- v2Result{reply, err}
			}()
			req, err := localhid.ReadRequest(bufio.NewReader(peer))
			if err != nil || req.Op != "recover" || req.AgentInstance != oldAgent || req.TargetID != "kit-b" || req.BootID != "boot-1" {
				t.Fatalf("request %+v %v", req, err)
			}
			v2Reply(t, peer, localhid.Reply{Version: 2, Op: "recover", Request: req.Request, Status: tc.status, AgentInstance: tc.agent, TargetID: tc.target, BootID: tc.boot})
			got := <-done
			if tc.success {
				if got.err != nil || got.reply.AgentInstance != tc.agent {
					t.Fatalf("recovery %+v", got)
				}
			} else {
				if got.err == nil {
					t.Fatalf("invalid recovery accepted: %+v", got)
				}
				select {
				case <-tr.done:
				case <-time.After(time.Second):
					t.Fatal("invalid recovery did not close transport")
				}
			}
		})
	}
}

func TestV2RecoveryTransportFencesRawBeforeDuringAndAfterAck(t *testing.T) {
	tr, peer := v2RecoveryPair(t, time.Second)
	pad, err := protocol.EncodeInputFrame(Frame(1, remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err = tr.WriteRaw(context.Background(), pad); err == nil {
		t.Fatal("raw sent before recovery")
	}
	old := strings.Repeat("a", 32)
	done := make(chan v2Result, 1)
	go func() {
		reply, err := tr.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"})
		done <- v2Result{reply, err}
	}()
	req, err := localhid.ReadRequest(bufio.NewReader(peer))
	if err != nil {
		t.Fatal(err)
	}
	if err = tr.WriteRaw(context.Background(), pad); err == nil {
		t.Fatal("raw sent while recovery pending")
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "recover", Request: req.Request, Status: "reader-drained", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"})
	if got := <-done; got.err != nil {
		t.Fatal(got.err)
	}
	if err = tr.WriteRaw(context.Background(), pad); err == nil {
		t.Fatal("raw sent after recovery without target cleanup proof")
	}
	if _, err = tr.Request(context.Background(), v2Begin()); err == nil {
		t.Fatal("recovery-only transport accepted BEGIN")
	}
	ordinary, _ := v2Pair(t, time.Second)
	if _, err = ordinary.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"}); err == nil {
		t.Fatal("ordinary transport accepted RECOVER")
	}
}

func TestV2RecoveryPromotionKeepsSameSocketAndRequiresBegin(t *testing.T) {
	tr, peer := v2RecoveryPair(t, time.Second)
	old := strings.Repeat("a", 32)
	if tr.PromoteRecovered(localhid.Reply{}) == nil {
		t.Fatal("unacknowledged recovery promoted")
	}
	done := make(chan v2Result, 1)
	go func() {
		reply, err := tr.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"})
		done <- v2Result{reply, err}
	}()
	r := bufio.NewReader(peer)
	req, err := localhid.ReadRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "recover", Request: req.Request, Status: "reader-drained", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"})
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	other, _ := v2RecoveryPair(t, time.Second)
	if other.PromoteRecovered(result.reply) == nil {
		t.Fatal("reply promoted another connection")
	}
	if err := tr.PromoteRecovered(result.reply); err != nil {
		t.Fatal(err)
	}
	if tr.PromoteRecovered(result.reply) == nil {
		t.Fatal("promotion repeated")
	}
	var device localhid.DeviceID
	device[0] = 1
	if _, err := tr.Request(context.Background(), localhid.Request{Op: "edge", Token: strings.Repeat("c", 64), EventSeq: 1, Device: device, Usage: 4, Down: true}); err == nil {
		t.Fatal("edge accepted before BEGIN")
	}
	keyboard, err := protocol.EncodeInputFrame(Frame(1, remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: remoteinput.KeyA}, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if tr.WriteRaw(context.Background(), keyboard) == nil {
		t.Fatal("promotion allowed raw keyboard")
	}
	pad, err := protocol.EncodeInputFrame(Frame(2, remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	wrote := make(chan error, 1)
	go func() { wrote <- tr.WriteRaw(context.Background(), pad) }()
	frame, err := protocol.DecodeInputFrame(r, 48)
	if err != nil || frame.Device != uint8(remoteinput.DeviceGamepad) {
		t.Fatalf("pad %+v %v", frame, err)
	}
	if err := <-wrote; err != nil {
		t.Fatal(err)
	}
	go func() { reply, err := tr.Request(context.Background(), v2Begin()); done <- v2Result{reply, err} }()
	req, err = localhid.ReadRequest(r)
	if err != nil || len(req.Devices) == 0 {
		t.Fatal("missing fresh BEGIN snapshot")
	}
	token := strings.Repeat("c", 64)
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "begin", Request: req.Request, Status: "ready", Token: token})
	if got := <-done; got.err != nil {
		t.Fatal(got.err)
	}
	go func() {
		reply, err := tr.Request(context.Background(), localhid.Request{Op: "edge", Token: token, EventSeq: 1, Device: device, Usage: 4, Down: false})
		done <- v2Result{reply, err}
	}()
	req, err = localhid.ReadRequest(r)
	if err != nil || req.Op != "edge" {
		t.Fatal("acknowledged source did not send edge")
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "edge", Request: req.Request, Status: "accepted", Token: token})
	if got := <-done; got.err != nil {
		t.Fatal(got.err)
	}
}

func TestV2RecoveryTimeoutCannotPromoteOrReplay(t *testing.T) {
	tr, peer := v2RecoveryPair(t, 40*time.Millisecond)
	old := strings.Repeat("a", 32)
	done := make(chan v2Result, 1)
	go func() {
		reply, err := tr.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"})
		done <- v2Result{reply, err}
	}()
	req, err := localhid.ReadRequest(bufio.NewReader(peer))
	if err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.err == nil {
		t.Fatal("unanswered recovery succeeded")
	}
	select {
	case <-tr.done:
	case <-time.After(time.Second):
		t.Fatal("timeout did not close reader")
	}
	late := localhid.Reply{Version: 2, Op: "recover", Request: req.Request, Status: "reader-drained", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"}
	if err := tr.PromoteRecovered(late); err == nil {
		t.Fatal("timeout promoted a late response")
	}
	if _, err := tr.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"}); err == nil {
		t.Fatal("timed out request replayed")
	}
}

func TestV2RecoveryPromotionFailsFastWhenTransportBusy(t *testing.T) {
	tr, _ := v2RecoveryPair(t, time.Second)
	tr.mu.Lock()
	err := tr.PromoteRecovered(localhid.Reply{})
	tr.mu.Unlock()
	if err == nil {
		t.Fatal("busy transport promotion succeeded")
	}
}

func TestV2RecoveryImmediatePromotionDoesNotRaceReader(t *testing.T) {
	for i := 0; i < 100; i++ {
		client, peer := net.Pipe()
		tr, err := NewV2RecoveryTransport(client, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		old := strings.Repeat("a", 32)
		done := make(chan v2Result, 1)
		go func() {
			reply, err := tr.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"})
			done <- v2Result{reply, err}
		}()
		req, err := localhid.ReadRequest(bufio.NewReader(peer))
		if err != nil {
			t.Fatal(err)
		}
		wire, err := localhid.EncodeReply(localhid.Reply{Version: 2, Op: "recover", Request: req.Request, Status: "reader-drained", AgentInstance: old, TargetID: "kit-b", BootID: "boot-1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = peer.Write(wire); err != nil {
			t.Fatal(err)
		}
		result := <-done
		if result.err != nil {
			t.Fatal(result.err)
		}
		if err = tr.PromoteRecovered(result.reply); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		_ = peer.Close()
		_ = tr.Close()
	}
}

func TestV2ControlDoesNotBlockPadAndDemultiplexesPong(t *testing.T) {
	tr, peer := v2Pair(t, 2*time.Second)
	result := make(chan v2Result, 1)
	go func() { reply, err := tr.Request(context.Background(), v2Begin()); result <- v2Result{reply, err} }()
	r := bufio.NewReader(peer)
	req, err := localhid.ReadRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	pad, err := protocol.EncodeInputFrame(Frame(1, remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	write := make(chan error, 1)
	go func() { write <- tr.WriteRaw(context.Background(), pad) }()
	f, err := protocol.DecodeInputFrame(r, 48)
	if err != nil || f.Device != uint8(remoteinput.DeviceGamepad) {
		t.Fatalf("pad %+v %v", f, err)
	}
	if err = <-write; err != nil {
		t.Fatal(err)
	}
	ping, err := protocol.EncodeInputFrame(protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 7}, Seq: 1, ClientMonoNS: 42})
	if err != nil {
		t.Fatal(err)
	}
	go func() { write <- tr.WriteRaw(context.Background(), ping) }()
	f, err = protocol.DecodeInputFrame(r, 48)
	if err != nil || f.Header.Type != protocol.InputTypePing {
		t.Fatalf("ping %+v %v", f, err)
	}
	if err = <-write; err != nil {
		t.Fatal(err)
	}
	f.Header.Type = protocol.InputTypePong
	f.ServerMonoNS = 99
	pong, err := protocol.EncodeInputFrame(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = peer.Write(pong); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("c", 64)
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "begin", Request: req.Request, Token: token, Status: "ready"})
	got := <-result
	if got.err != nil || got.reply.Token != token {
		t.Fatalf("begin %+v", got)
	}
}

func TestV2EdgeQuarantineAllowsOnlySameTokenReset(t *testing.T) {
	tr, peer := v2Pair(t, 2*time.Second)
	r := bufio.NewReader(peer)
	res := make(chan v2Result, 1)
	start := func(q localhid.Request) {
		go func() { reply, err := tr.Request(context.Background(), q); res <- v2Result{reply, err} }()
	}
	token := strings.Repeat("c", 64)
	start(v2Begin())
	q, err := localhid.ReadRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "begin", Request: q.Request, Token: token, Status: "ready"})
	if got := <-res; got.err != nil {
		t.Fatal(got.err)
	}
	var device localhid.DeviceID
	device[0] = 1
	start(localhid.Request{Op: "edge", Token: token, EventSeq: 1, Device: device, Usage: 4, Down: true})
	q, err = localhid.ReadRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "edge", Request: q.Request, Token: token, Status: "quarantined"})
	if got := <-res; !errors.Is(got.err, localhid.ErrQuarantined) {
		t.Fatalf("edge %+v", got)
	}
	if _, err := tr.Request(context.Background(), localhid.Request{Op: "barrier", Token: token}); !errors.Is(err, localhid.ErrQuarantined) {
		t.Fatalf("barrier %v", err)
	}
	start(localhid.Request{Op: "reset", Token: token, LastEventSeq: 1})
	q, err = localhid.ReadRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	v2Reply(t, peer, localhid.Reply{Version: 2, Op: "reset", Request: q.Request, Token: token, Status: "neutral-confirmed"})
	if got := <-res; got.err != nil {
		t.Fatal(got.err)
	}
}

func TestV2UnsolicitedPongAndControlTimeoutClose(t *testing.T) {
	t.Run("unsolicited-pong", func(t *testing.T) {
		tr, peer := v2Pair(t, time.Second)
		pong, err := protocol.EncodeInputFrame(protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePong, Session: 1}, Seq: 1, ClientMonoNS: 2, ServerMonoNS: 3})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peer.Write(pong); err != nil {
			t.Fatal(err)
		}
		select {
		case <-tr.done:
		case <-time.After(time.Second):
			t.Fatal("reader did not fail closed")
		}
	})
	t.Run("control-timeout", func(t *testing.T) {
		tr, peer := v2Pair(t, 100*time.Millisecond)
		go func() { _, _ = localhid.ReadRequest(bufio.NewReader(peer)) }()
		if _, err := tr.Request(context.Background(), v2Begin()); err == nil {
			t.Fatal("unanswered request succeeded")
		}
		select {
		case <-tr.done:
		case <-time.After(time.Second):
			t.Fatal("reader did not stop")
		}
	})
}

func TestV2DoneSignalsIdleEOF(t *testing.T) {
	tr, peer := v2Pair(t, time.Second)
	_ = peer.Close()
	select {
	case <-tr.Done():
	case <-time.After(time.Second):
		t.Fatal("idle EOF was not signaled")
	}
}

func TestV2RawKeyboardCannotBypassLedger(t *testing.T) {
	tr, _ := v2Pair(t, time.Second)
	wire, err := protocol.EncodeInputFrame(Frame(1, remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: remoteinput.KeyA}, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err = tr.WriteRaw(context.Background(), wire); err == nil {
		t.Fatal("raw keyboard accepted")
	}
	// The target treats KindKey as keyboard even if the device claims pad.
	disguised := Frame(2, remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindKey, Action: remoteinput.ActionPress, Code: remoteinput.KeyA}, time.Now())
	wire, err = protocol.EncodeInputFrame(disguised)
	if err != nil {
		t.Fatal(err)
	}
	if err = tr.WriteRaw(context.Background(), wire); err == nil {
		t.Fatal("gamepad-labelled keyboard key accepted")
	}
}

func TestV2MalformedAndMismatchedReplyQuarantine(t *testing.T) {
	for _, kind := range []string{"bad-marker", "wrong-request", "unexpected-eof"} {
		t.Run(kind, func(t *testing.T) {
			tr, peer := v2Pair(t, time.Second)
			result := make(chan error, 1)
			go func() { _, err := tr.Request(context.Background(), v2Begin()); result <- err }()
			q, err := localhid.ReadRequest(bufio.NewReader(peer))
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "bad-marker":
				_, err = peer.Write([]byte("BAD!"))
			case "wrong-request":
				wire, e := localhid.EncodeReply(localhid.Reply{Version: 2, Op: "begin", Request: q.Request + 1, Token: strings.Repeat("c", 64), Status: "ready"})
				if e != nil {
					t.Fatal(e)
				}
				_, err = peer.Write(wire)
			case "unexpected-eof":
				err = peer.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = <-result; err == nil {
				t.Fatal("invalid reply accepted")
			}
			select {
			case <-tr.done:
			case <-time.After(time.Second):
				t.Fatal("reader did not stop")
			}
			if _, err = tr.Request(context.Background(), v2Begin()); err == nil {
				t.Fatal("closed connection reused")
			}
		})
	}
}

func TestV2PingTimeoutAndWrongPongClose(t *testing.T) {
	for _, wrongPong := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "wrong-pong"}[wrongPong], func(t *testing.T) {
			tr, peer := v2Pair(t, 150*time.Millisecond)
			ping, err := protocol.EncodeInputFrame(protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 1}, Seq: 1, ClientMonoNS: 8})
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { result <- tr.WriteRaw(context.Background(), ping) }()
			f, err := protocol.DecodeInputFrame(peer, 48)
			if err != nil {
				t.Fatal(err)
			}
			if err = <-result; err != nil {
				t.Fatal(err)
			}
			if wrongPong {
				f.Header.Type, f.ServerMonoNS, f.ClientMonoNS = protocol.InputTypePong, 1, 9
				wire, e := protocol.EncodeInputFrame(f)
				if e != nil {
					t.Fatal(e)
				}
				if _, err = peer.Write(wire); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-tr.done:
			case <-time.After(time.Second):
				t.Fatal("ping failure did not close")
			}
		})
	}
}

type shortWriteConn struct{ net.Conn }

func (c shortWriteConn) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestV2PartialWritePermanentlyCloses(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	tr, err := NewV2RecoveryTransport(shortWriteConn{a}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if _, err = tr.Request(context.Background(), localhid.Request{Op: "recover", AgentInstance: strings.Repeat("a", 32), TargetID: "kit", BootID: "boot"}); err == nil {
		t.Fatal("short write accepted")
	}
	select {
	case <-tr.done:
	case <-time.After(time.Second):
		t.Fatal("reader did not close")
	}
	if _, err = tr.Request(context.Background(), v2Begin()); err == nil {
		t.Fatal("connection reused")
	}
}
