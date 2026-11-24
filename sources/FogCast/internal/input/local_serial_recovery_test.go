package input

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestLocalSuccessorPongWaitsForOldV2Neutral(t *testing.T) {
	f := newV2BackendFixture(t)
	testEnableRecovery(t, f.c)
	listener := &memoryListener{incoming: make(chan net.Conn, 2), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = f.c.serveLocalConnections(ctx, listener) }()
	oldClient, oldPeer := net.Pipe()
	defer oldClient.Close()
	listener.incoming <- oldPeer
	testRecoverConnection(t, f.c, oldClient)
	request := v2BeginFor(f)
	request.Request = 2
	begin := v2Exchange(t, oldClient, request)
	edge := localhid.Request{Version: 2, Op: "edge", Request: 3, Token: begin.Token, EventSeq: 1, Device: localhid.DeviceID{1}, Usage: 4, Down: true}
	if reply := v2Exchange(t, oldClient, edge); reply.Status != "accepted" {
		t.Fatal(reply)
	}
	var allowed atomic.Bool
	attempted := make(chan struct{})
	var once sync.Once
	f.c.ports.hid.setPoster(func(context.Context, string, string, uint64, hidkeys.Rows) error {
		once.Do(func() { close(attempted) })
		if !allowed.Load() {
			return errors.New("neutral unresolved")
		}
		return nil
	})
	padPosts := make(chan uint8, 2)
	f.c.ports.poster = func(_ context.Context, _ string, _ uint64, _ uint8, buttons uint8, _ uint16) error {
		padPosts <- buttons
		return nil
	}
	if err := f.c.ports.bind(&ControllerBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	if err := oldClient.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("old neutral not attempted")
	}
	nextClient, nextPeer := net.Pipe()
	defer nextClient.Close()
	_ = nextClient.SetDeadline(time.Now().Add(3 * time.Second))
	listener.incoming <- nextPeer
	pong := make(chan error, 1)
	go func() {
		ping := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 1}, Seq: 1, ClientMonoNS: 42}
		if err := bridge.WriteFrame(nextClient, ping); err != nil {
			pong <- err
			return
		}
		frame, err := protocol.DecodeInputFrame(nextClient, bridge.MaxFrameBytes)
		if err != nil {
			pong <- err
			return
		}
		if frame.Header.Type != protocol.InputTypePong || frame.Seq != 1 || frame.ClientMonoNS != 42 {
			pong <- errors.New("uncorrelated pong")
			return
		}
		pong <- nil
	}()
	select {
	case err := <-pong:
		t.Fatalf("successor pong before neutral: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	allowed.Store(true)
	select {
	case err := <-pong:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("successor did not receive pong after neutral")
	}
	testRecoverConnection(t, f.c, nextClient)
	pad := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1}, Seq: 2, Device: uint8(remoteinput.DeviceGamepad), Kind: uint8(remoteinput.KindButton), Action: uint8(remoteinput.ActionPress), Code: uint16(remoteinput.ButtonA)}
	if err := bridge.WriteFrame(nextClient, pad); err != nil {
		t.Fatal(err)
	}
	select {
	case buttons := <-padPosts:
		if buttons == 0 {
			t.Fatal("pad press was not applied")
		}
	case <-time.After(time.Second):
		t.Fatal("pad not delivered after drain")
	}
	if !f.c.localV2Guard.Load() {
		t.Fatal("keyboard raw fence cleared")
	}
	_ = nextClient.Close()
	cancel()
	_ = listener.Close()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("listener failed to drain")
	}
}

func TestLocalSuccessorPongWaitsForPadNeutral(t *testing.T) {
	f := newV2BackendFixture(t)
	testEnableRecovery(t, f.c)
	listener := &memoryListener{incoming: make(chan net.Conn, 2), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = f.c.serveLocalConnections(ctx, listener) }()
	var allow atomic.Bool
	pressed, neutralAttempt := make(chan struct{}), make(chan struct{})
	var pressOnce, neutralOnce sync.Once
	f.c.ports.poster = func(_ context.Context, _ string, _ uint64, _ uint8, buttons uint8, _ uint16) error {
		if buttons != 0 {
			pressOnce.Do(func() { close(pressed) })
			return nil
		}
		neutralOnce.Do(func() { close(neutralAttempt) })
		if !allow.Load() {
			return errors.New("pad neutral unresolved")
		}
		return nil
	}
	if err := f.c.ports.bind(&ControllerBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	old, peer := net.Pipe()
	defer old.Close()
	listener.incoming <- peer
	testRecoverConnection(t, f.c, old)
	request := v2BeginFor(f)
	request.Request = 2
	begin := v2Exchange(t, old, request)
	if begin.Status != "ready" {
		t.Fatal(begin)
	}
	pad := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: 1}, Seq: 1, Device: uint8(remoteinput.DeviceGamepad), Kind: uint8(remoteinput.KindButton), Action: uint8(remoteinput.ActionPress), Code: uint16(remoteinput.ButtonA)}
	if err := bridge.WriteFrame(old, pad); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pressed:
	case <-time.After(time.Second):
		t.Fatal("old pad not pressed")
	}
	_ = old.Close()
	select {
	case <-neutralAttempt:
	case <-time.After(time.Second):
		t.Fatal("old pad neutral not attempted")
	}
	next, peer2 := net.Pipe()
	defer next.Close()
	_ = next.SetDeadline(time.Now().Add(3 * time.Second))
	listener.incoming <- peer2
	pong := make(chan error, 1)
	go func() {
		ping := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 1}, Seq: 1, ClientMonoNS: 43}
		if err := bridge.WriteFrame(next, ping); err != nil {
			pong <- err
			return
		}
		frame, err := protocol.DecodeInputFrame(next, bridge.MaxFrameBytes)
		if err == nil && (frame.Header.Type != protocol.InputTypePong || frame.ClientMonoNS != 43) {
			err = errors.New("uncorrelated pong")
		}
		pong <- err
	}()
	select {
	case err := <-pong:
		t.Fatalf("pong before pad neutral: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	allow.Store(true)
	select {
	case err := <-pong:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no pong after pad neutral")
	}
	_ = next.Close()
	cancel()
	_ = listener.Close()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("listener failed to drain")
	}
}

func TestLocalSuccessorPongWaitsForHaltedControllerDrain(t *testing.T) {
	f := newV2BackendFixture(t)
	testEnableRecovery(t, f.c)
	listener := &memoryListener{incoming: make(chan net.Conn, 2), done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer listener.Close()
	stopped := make(chan struct{})
	go func() { defer close(stopped); _ = f.c.serveLocalConnections(ctx, listener) }()
	f.c.ports.poster = func(context.Context, string, uint64, uint8, uint8, uint16) error { return nil }
	if err := f.c.ports.bind(&ControllerBinding{PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}); err != nil {
		t.Fatal(err)
	}
	old, peer := net.Pipe()
	defer old.Close()
	listener.incoming <- peer
	testRecoverConnection(t, f.c, old)
	request := v2BeginFor(f)
	request.Request = 2
	if begin := v2Exchange(t, old, request); begin.Status != "ready" {
		t.Fatal(begin)
	}
	f.c.ports.mu.Lock()
	f.c.ports.haltPublish = true
	f.c.ports.dirty[0] = true
	f.c.ports.mu.Unlock()
	_ = old.Close()
	next, peer2 := net.Pipe()
	defer next.Close()
	_ = next.SetDeadline(time.Now().Add(3 * time.Second))
	listener.incoming <- peer2
	pong := make(chan error, 1)
	go func() {
		ping := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypePing, Session: 1}, Seq: 1, ClientMonoNS: 44}
		if err := bridge.WriteFrame(next, ping); err != nil {
			pong <- err
			return
		}
		_, err := protocol.DecodeInputFrame(next, bridge.MaxFrameBytes)
		pong <- err
	}()
	select {
	case err := <-pong:
		t.Fatalf("pong while controller drain was halted: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	f.c.ports.mu.Lock()
	f.c.ports.haltPublish = false
	f.c.ports.mu.Unlock()
	select {
	case err := <-pong:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no pong after drain resumed")
	}
	_ = next.Close()
	cancel()
	_ = listener.Close()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("listener failed to drain")
	}
}
