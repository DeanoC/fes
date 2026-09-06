package input

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/protocol"
)

type persistentTestSink struct {
	mu              sync.Mutex
	pressed         bool
	events          int
	released        int
	releaseAttempts int
	closed          int
	releaseErr      error
}

func (s *persistentTestSink) Apply(protocol.InputFrame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pressed = true
	s.events++
	return nil
}

func (s *persistentTestSink) ReleaseAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseAttempts++
	if s.releaseErr != nil {
		return s.releaseErr
	}
	s.pressed = false
	s.released++
	return nil
}

func TestPersistentTargetControllerBlocksSuccessorUntilFailedReleaseRecovers(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	token := []byte("0123456789abcdef")
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: token, Core: "MegaDrive"}); err != nil {
		t.Fatal(err)
	}
	releaseErr := errors.New("release failed")
	sink.mu.Lock()
	sink.releaseErr = releaseErr
	sink.mu.Unlock()
	if err := controller.Detach(context.Background(), 1); !errors.Is(err, releaseErr) {
		t.Fatalf("Detach error = %v, want release failure", err)
	}
	if err := controller.Attach(context.Background(), Spec{Session: 2, Token: token, Core: "MegaDrive"}); !errors.Is(err, releaseErr) {
		t.Fatalf("successor Attach error = %v, want pending release failure", err)
	}
	if _, err := controller.OpenStream(context.Background(), 2); err == nil {
		t.Fatal("successor lease became active before neutralization")
	}
	sink.mu.Lock()
	sink.releaseErr = nil
	sink.mu.Unlock()
	if err := controller.Attach(context.Background(), Spec{Session: 2, Token: token, Core: "MegaDrive"}); err != nil {
		t.Fatalf("successor Attach after neutralization = %v", err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentTargetControllerCloseReportsReleaseFailure(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"}); err != nil {
		t.Fatal(err)
	}
	releaseErr := errors.New("release failed")
	sink.mu.Lock()
	sink.releaseErr = releaseErr
	sink.mu.Unlock()
	if err := controller.Close(); !errors.Is(err, releaseErr) {
		t.Fatalf("Close error = %v, want release failure", err)
	}
}

func TestPersistentTargetControllerCloseRetriesAndReportsPendingNeutralization(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"}); err != nil {
		t.Fatal(err)
	}
	releaseErr := errors.New("release failed")
	sink.mu.Lock()
	sink.releaseErr = releaseErr
	sink.mu.Unlock()
	if err := controller.Detach(context.Background(), 1); !errors.Is(err, releaseErr) {
		t.Fatalf("Detach error = %v, want release failure", err)
	}
	sink.mu.Lock()
	attemptsAfterDetach := sink.releaseAttempts
	sink.mu.Unlock()
	if err := controller.Close(); !errors.Is(err, releaseErr) {
		t.Fatalf("Close error = %v, want pending release failure", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.releaseAttempts != attemptsAfterDetach+1 {
		t.Fatalf("shutdown release attempts = %d, want %d", sink.releaseAttempts, attemptsAfterDetach+1)
	}
}

func TestTargetControllerConcurrentClosePublishesCancellationBeforeLifecycleWait(t *testing.T) {
	controller := newTargetControllerWithSink("127.0.0.1:0", &persistentTestSink{})
	controller.lifecycle.Lock()
	closeResult := make(chan error, 1)
	go func() { closeResult <- controller.Close() }()

	deadline := time.Now().Add(time.Second)
	published := false
	for time.Now().Before(deadline) {
		controller.mu.Lock()
		published = controller.closed
		controller.mu.Unlock()
		if published {
			break
		}
		time.Sleep(time.Millisecond)
	}
	controller.lifecycle.Unlock()
	if !published {
		t.Fatal("Close waited for the lifecycle lock before publishing cancellation")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after the lifecycle barrier was released")
	}
}

func TestTargetControllerReportsBindFailure(t *testing.T) {
	bindErr := errors.New("bind failed")
	pendingAtListen := make(chan bool, 1)
	controller := newTargetControllerWithSink("127.0.0.1:0", &persistentTestSink{})
	controller.listen = func(string, string) (net.Listener, error) {
		controller.mu.Lock()
		pending := controller.pending
		controller.mu.Unlock()
		pendingAtListen <- pending != nil && pending.session == 1
		return nil, bindErr
	}
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"}); !errors.Is(err, bindErr) {
		t.Fatalf("Attach error = %v, want bind failure", err)
	}
	if !<-pendingAtListen {
		t.Fatal("Attach reached listen without publishing its pending lease")
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetControllerCloseCancelsAttachAfterPendingStartup(t *testing.T) {
	bindErr := errors.New("late bind failure")
	listenEntered := make(chan struct{})
	allowListenReturn := make(chan struct{})
	listenReturned := make(chan struct{})
	var allowOnce sync.Once
	allowReturn := func() { allowOnce.Do(func() { close(allowListenReturn) }) }
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	controller.listen = func(string, string) (net.Listener, error) {
		close(listenEntered)
		<-allowListenReturn
		close(listenReturned)
		return nil, bindErr
	}
	defer func() {
		allowReturn()
		_ = controller.Close()
	}()

	attachResult := make(chan error, 1)
	go func() {
		attachResult <- controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"})
	}()
	select {
	case <-listenEntered:
	case err := <-attachResult:
		t.Fatalf("Attach returned before reaching pending startup: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Attach did not reach pending startup")
	}
	controller.mu.Lock()
	pending := controller.pending
	controller.mu.Unlock()
	if pending == nil || pending.session != 1 {
		t.Fatal("Attach reached listen without publishing its pending lease")
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- controller.Close() }()
	select {
	case err := <-attachResult:
		if !errors.Is(err, net.ErrClosed) || errors.Is(err, bindErr) {
			t.Fatalf("Attach error = %v, want Close outcome before late bind result", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not release Attach from pending startup")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close blocked behind pending Attach")
	}
	allowReturn()
	select {
	case <-listenReturned:
	case <-time.After(time.Second):
		t.Fatal("listener call did not return after test barrier opened")
	}
	sink.mu.Lock()
	closed := sink.closed
	sink.mu.Unlock()
	if closed != 1 {
		t.Fatalf("persistent sink close calls = %d, want 1", closed)
	}
}

func TestTargetControllerDetachCancelsAttachAfterPendingStartup(t *testing.T) {
	listenEntered := make(chan struct{})
	allowListenReturn := make(chan struct{})
	var allowOnce sync.Once
	allowReturn := func() { allowOnce.Do(func() { close(allowListenReturn) }) }
	controller := newTargetControllerWithSink("127.0.0.1:0", &persistentTestSink{})
	controller.listen = func(string, string) (net.Listener, error) {
		close(listenEntered)
		<-allowListenReturn
		return nil, errors.New("late bind failure")
	}
	defer func() {
		allowReturn()
		_ = controller.Close()
	}()

	attachResult := make(chan error, 1)
	go func() {
		attachResult <- controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "MegaDrive"})
	}()
	select {
	case <-listenEntered:
	case err := <-attachResult:
		t.Fatalf("Attach returned before reaching pending startup: %v", err)
	case <-time.After(time.Second):
		t.Fatal("Attach did not reach pending startup")
	}
	controller.mu.Lock()
	pending := controller.pending
	controller.mu.Unlock()
	if pending == nil || pending.session != 1 {
		t.Fatal("Attach reached listen without publishing its pending lease")
	}

	detachResult := make(chan error, 1)
	go func() { detachResult <- controller.Detach(context.Background(), 1) }()
	select {
	case err := <-attachResult:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Attach error = %v, want pending lease closed by Detach", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Detach did not release Attach from pending startup")
	}
	select {
	case err := <-detachResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Detach blocked behind pending Attach")
	}
	allowReturn()
}

func (s *persistentTestSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return nil
}

func TestTargetControllerRejectsInvalidLease(t *testing.T) {
	controller := NewTargetController()
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("short"), Core: "SNES"}); err == nil {
		t.Fatal("short token accepted")
	}
	if err := controller.Attach(context.Background(), Spec{Session: 1, Token: []byte("0123456789abcdef"), Core: "/private"}); err == nil {
		t.Fatal("unsafe core accepted")
	}
}

func TestTargetControllerDetachIsIdempotent(t *testing.T) {
	controller := NewTargetController()
	if err := controller.Detach(context.Background(), 99); err != nil {
		t.Fatal(err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetControllerOpenStreamRequiresActiveLease(t *testing.T) {
	controller := NewTargetController()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := controller.OpenStream(ctx, 1); err == nil {
		t.Fatal("stream opened without lease")
	}
}

func TestPersistentTargetControllerDoesNotExposeStreamWithoutLease(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	t.Cleanup(func() { _ = controller.Close() })
	if _, err := controller.OpenStream(context.Background(), 1); err == nil {
		t.Fatal("persistent input stream opened without a lease")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.events != 0 || sink.released != 0 {
		t.Fatalf("unleased sink activity = events:%d releases:%d", sink.events, sink.released)
	}
}

func TestPersistentTargetControllerDetachReleasesWithoutDestroyingDevice(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	token := []byte("0123456789abcdef")

	attachAndPress := func(session uint64, core string, code uint16) net.Conn {
		t.Helper()
		if err := controller.Attach(context.Background(), Spec{Session: session, Token: token, Core: core}); err != nil {
			t.Fatal(err)
		}
		connection, err := controller.OpenStream(context.Background(), session)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(connection, "{\"version\":1,\"session\":%d,\"core\":\"%s\",\"proof\":\"%s\"}\n", session, core, hex.EncodeToString(token)); err != nil {
			t.Fatal(err)
		}
		frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: session}, Seq: 1, Device: 1, Kind: 1, Action: 1, Code: code}
		if err := bridge.WriteFrame(connection, frame); err != nil {
			t.Fatal(err)
		}
		waitForSink(t, sink, func(s *persistentTestSink) bool { return s.pressed })
		return connection
	}

	first := attachAndPress(1, "MegaDrive", protocol.InputCodeButtonC)
	if err := controller.Detach(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	waitForSink(t, sink, func(s *persistentTestSink) bool { return !s.pressed && s.released > 0 })
	_ = first.Close()
	sink.mu.Lock()
	closedAfterDetach := sink.closed
	sink.mu.Unlock()
	if closedAfterDetach != 0 {
		t.Fatalf("device close calls after detach = %d, want 0", closedAfterDetach)
	}

	second := attachAndPress(2, "SNES", protocol.InputCodeButtonX)
	if err := controller.Detach(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	_ = second.Close()
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.events != 2 || sink.closed != 1 {
		t.Fatalf("persistent lifecycle = events:%d closes:%d, want 2 events and 1 close", sink.events, sink.closed)
	}
}

func TestPersistentTargetControllerReleaseAllPreservesDeviceForNextOwner(t *testing.T) {
	sink := &persistentTestSink{}
	controller := newTargetControllerWithSink("127.0.0.1:0", sink)
	token := []byte("0123456789abcdef")

	attachAndPress := func(session uint64, core string, code uint16) net.Conn {
		t.Helper()
		if err := controller.Attach(context.Background(), Spec{Session: session, Token: token, Core: core}); err != nil {
			t.Fatal(err)
		}
		connection, err := controller.OpenStream(context.Background(), session)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(connection, "{\"version\":1,\"session\":%d,\"core\":\"%s\",\"proof\":\"%s\"}\n", session, core, hex.EncodeToString(token)); err != nil {
			t.Fatal(err)
		}
		frame := protocol.InputFrame{Header: protocol.InputHeader{Type: protocol.InputTypeInput, Session: session}, Seq: 1, Device: 1, Kind: 1, Action: 1, Code: code}
		if err := bridge.WriteFrame(connection, frame); err != nil {
			t.Fatal(err)
		}
		waitForSink(t, sink, func(s *persistentTestSink) bool { return s.pressed })
		return connection
	}

	first := attachAndPress(1, "MegaDrive", protocol.InputCodeButtonC)
	if err := controller.ReleaseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForSink(t, sink, func(s *persistentTestSink) bool { return !s.pressed && s.released > 0 })
	_ = first.Close()
	sink.mu.Lock()
	closedAfterDetach := sink.closed
	sink.mu.Unlock()
	if closedAfterDetach != 0 {
		t.Fatalf("device close calls after detach = %d, want 0", closedAfterDetach)
	}

	second := attachAndPress(2, "SNES", protocol.InputCodeButtonX)
	if err := controller.ReleaseAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = second.Close()
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.events != 2 || sink.closed != 1 {
		t.Fatalf("persistent lifecycle = events:%d closes:%d, want 2 events and 1 close", sink.events, sink.closed)
	}
}

func waitForSink(t *testing.T, sink *persistentTestSink, ready func(*persistentTestSink) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		ok := ready(sink)
		sink.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	t.Fatalf("timed out waiting for sink: events=%d pressed=%t releases=%d closes=%d", sink.events, sink.pressed, sink.released, sink.closed)
}
