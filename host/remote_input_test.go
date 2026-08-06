package host_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/internal/bridge"
	"github.com/DeanoC/FogCast-POC/protocol"
	"github.com/DeanoC/FogCast-POC/remoteinput"
)

type testBridgeHandle struct {
	server *bridge.Server
}

func (h *testBridgeHandle) Ready() <-chan struct{} { return h.server.Ready() }
func (h *testBridgeHandle) Endpoint() string       { return h.server.Addr().String() }
func (h *testBridgeHandle) Stop(context.Context) error {
	return h.server.Close()
}

type testBridgeStarter struct {
	mu      sync.Mutex
	specs   []host.BridgeSpec
	sinks   []*testSink
	servers []*bridge.Server
}

func (s *testBridgeStarter) Start(ctx context.Context, spec host.BridgeSpec) (host.BridgeHandle, error) {
	sink := &testSink{}
	server, err := bridge.New(bridge.Config{
		Addr:    "127.0.0.1:0",
		Token:   spec.Token,
		Session: spec.Session,
		Core:    spec.Core,
	}, sink)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.specs = append(s.specs, spec)
	s.sinks = append(s.sinks, sink)
	s.servers = append(s.servers, server)
	s.mu.Unlock()
	go func() { _ = server.ListenAndServe(ctx) }()
	return &testBridgeHandle{server: server}, nil
}

type testSink struct {
	mu       sync.Mutex
	events   []protocol.InputFrame
	releases int
}

func (s *testSink) Apply(frame protocol.InputFrame) error {
	s.mu.Lock()
	s.events = append(s.events, frame)
	s.mu.Unlock()
	return nil
}
func (s *testSink) ReleaseAll() error {
	s.mu.Lock()
	s.releases++
	s.mu.Unlock()
	return nil
}
func (s *testSink) Close() error { return nil }

func TestRemoteInputAttachSendDetachOwnsSessionAndRelease(t *testing.T) {
	starter := &testBridgeStarter{}
	input, err := host.NewRemoteInput(host.RemoteInputConfig{
		Starter:           starter,
		ReconnectGrace:    time.Second,
		HeartbeatInterval: 50 * time.Millisecond,
		DialTimeout:       time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	if err := input.Attach(context.Background(), "SNES"); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if status := input.Status(); status.State != host.RemoteInputAttached || !status.Ready {
		t.Fatalf("status after attach = %#v", status)
	}
	if err := input.SendEvent(context.Background(), remoteinput.Event{
		Device: remoteinput.DeviceKeyboard,
		Kind:   remoteinput.KindKey,
		Action: remoteinput.ActionPress,
		Code:   remoteinput.KeyA,
	}, time.Now()); err != nil {
		t.Fatalf("send event: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		starter.mu.Lock()
		if len(starter.sinks) != 1 {
			starter.mu.Unlock()
			return false
		}
		sink := starter.sinks[0]
		starter.mu.Unlock()
		sink.mu.Lock()
		defer sink.mu.Unlock()
		return len(sink.events) == 1
	})
	if err := input.Detach(context.Background(), "session_stop"); err != nil {
		t.Fatalf("detach: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		starter.mu.Lock()
		if len(starter.sinks) != 1 {
			starter.mu.Unlock()
			return false
		}
		sink := starter.sinks[0]
		starter.mu.Unlock()
		sink.mu.Lock()
		defer sink.mu.Unlock()
		return sink.releases > 0
	})
	status := input.Status()
	if status.State != host.RemoteInputDetached || status.Metrics.Releases == 0 || status.Metrics.ShutdownReason != "session_stop" {
		t.Fatalf("status after detach = %#v", status)
	}
	starter.mu.Lock()
	spec := starter.specs[0]
	starter.mu.Unlock()
	if spec.Session == 0 || len(spec.Token) < 16 || spec.Core != "SNES" {
		t.Fatalf("bridge spec = %#v", spec)
	}
}

func TestRemoteInputRotatesIdentityAcrossAttachments(t *testing.T) {
	starter := &testBridgeStarter{}
	input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := input.Attach(context.Background(), "SNES"); err != nil {
		t.Fatal(err)
	}
	if err := input.Detach(context.Background(), "detach"); err != nil {
		t.Fatal(err)
	}
	if err := input.Attach(context.Background(), "SNES"); err != nil {
		t.Fatal(err)
	}
	starter.mu.Lock()
	defer starter.mu.Unlock()
	if len(starter.specs) != 2 || starter.specs[0].Session == starter.specs[1].Session || string(starter.specs[0].Token) == string(starter.specs[1].Token) {
		t.Fatalf("identities were reused: %#v", starter.specs)
	}
}

func TestRemoteInputHeartbeatReportsRoundTripTime(t *testing.T) {
	starter := &testBridgeStarter{}
	input, err := host.NewRemoteInput(host.RemoteInputConfig{
		Starter:           starter,
		HeartbeatInterval: 10 * time.Millisecond,
		DialTimeout:       time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := input.Attach(context.Background(), "SNES"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Second, func() bool {
		status := input.Status()
		return status.Metrics.FramesSent >= 2 && status.Metrics.RTTMS > 0
	})
}

func TestRemoteInputFailsWhenBridgeExitsBeforeReady(t *testing.T) {
	starter := host.BridgeStarterFunc(func(context.Context, host.BridgeSpec) (host.BridgeHandle, error) {
		return &exitedBridgeHandle{}, nil
	})
	input, err := host.NewRemoteInput(host.RemoteInputConfig{Starter: starter, DialTimeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := input.Attach(context.Background(), "SNES"); err == nil {
		t.Fatal("attach accepted an exited bridge")
	}
	if got := input.Status(); got.State != host.RemoteInputFailed || got.Ready {
		t.Fatalf("status = %#v", got)
	}
}

type exitedBridgeHandle struct{}

func (*exitedBridgeHandle) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}
func (*exitedBridgeHandle) Endpoint() string           { return "127.0.0.1:1" }
func (*exitedBridgeHandle) Stop(context.Context) error { return nil }
func (*exitedBridgeHandle) ReadyError() error          { return errors.New("bridge exited") }

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

var _ bridge.Sink = (*testSink)(nil)
