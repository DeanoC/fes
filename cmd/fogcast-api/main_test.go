package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/internal/hostapi"
	"github.com/DeanoC/FogCast-POC/internal/remotemedia"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestRunRejectsNonLoopbackListenAddress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--listen", "0.0.0.0:8787"}, &stdout, &stderr, nil)
	if code != 2 {
		t.Fatalf("exit = %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestNormalizeListenAddressAcceptsLoopbackForms(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		if got, err := normalizeListenAddress(address); err != nil || got == "" {
			t.Errorf("normalize %q = %q, %v", address, got, err)
		}
	}
}

type compositionService struct{}

func (*compositionService) Games(context.Context) ([]catalog.Game, error) { return nil, nil }
func (*compositionService) Search(context.Context, string) ([]catalog.Game, error) {
	return nil, nil
}
func (*compositionService) Game(context.Context, string) (catalog.Game, error) {
	return catalog.Game{}, nil
}
func (*compositionService) Health(context.Context) (protocol.Health, error) {
	return protocol.Health{}, nil
}
func (*compositionService) Status(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateIdle}, nil
}
func (*compositionService) Launch(context.Context, string, fogcast.ProgressFunc) (protocol.CachedLaunchResponse, error) {
	return protocol.CachedLaunchResponse{}, nil
}
func (*compositionService) Stop(context.Context) (protocol.Status, error) {
	return protocol.Status{State: protocol.StateIdle}, nil
}
func (*compositionService) Close() error { return nil }

type shutdownCompositionService struct {
	compositionService
	mu       sync.Mutex
	stopErrs []error
	stops    int
}

func (s *shutdownCompositionService) Stop(context.Context) (protocol.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stops++
	if len(s.stopErrs) == 0 {
		return protocol.Status{State: protocol.StateIdle}, nil
	}
	err := s.stopErrs[0]
	s.stopErrs = s.stopErrs[1:]
	return protocol.Status{State: protocol.StateIdle}, err
}

func TestComposeAPIWiresRemoteInputController(t *testing.T) {
	service := &compositionService{}
	called := false
	handler, closeRemote, err := composeAPI(service, fogcast.Config{BaseURL: "http://127.0.0.1:8182", Token: "test-token", RemoteInput: fogcast.RemoteInputConfig{Enabled: true}}, func(fogcast.Config) (host.BridgeStarter, error) {
		called = true
		return host.BridgeStarterFunc(func(context.Context, host.BridgeSpec) (host.BridgeHandle, error) {
			return nil, errors.New("unused")
		}), nil
	})
	if err != nil || handler == nil || closeRemote == nil || !called {
		t.Fatalf("composition = handler:%v close:%v called:%v err:%v", handler != nil, closeRemote != nil, called, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session/input", nil)
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"detached"`) {
		t.Fatalf("input route = %d %s", response.Code, response.Body.String())
	}
	if err := closeRemote(); err != nil {
		t.Fatal(err)
	}
}

type compositionCapture struct {
	mu        sync.Mutex
	closed    int
	closeErrs []error
}

func (*compositionCapture) Start() error { return nil }
func (*compositionCapture) Next(context.Context) (remotemedia.EncodedSample, error) {
	return remotemedia.EncodedSample{}, context.Canceled
}
func (*compositionCapture) Stats() remotemedia.CaptureStats { return remotemedia.CaptureStats{} }
func (c *compositionCapture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	if len(c.closeErrs) != 0 {
		err := c.closeErrs[0]
		c.closeErrs = c.closeErrs[1:]
		return err
	}
	return nil
}
func (c *compositionCapture) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type compositionPacketConn struct{ closed chan struct{} }

func (c *compositionPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	<-c.closed
	return 0, nil, errors.New("closed")
}
func (c *compositionPacketConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}
func (*compositionPacketConn) LocalAddr() net.Addr { return &net.UDPAddr{} }

type compositionTransport struct{}

func (*compositionTransport) AcceptControl(remotemedia.ControlMessage) error  { return nil }
func (*compositionTransport) Ingest([]byte) ([]remotemedia.AccessUnit, error) { return nil, nil }
func (*compositionTransport) Close() error                                    { return nil }

type compositionRunner struct {
	done   chan struct{}
	source remotemedia.CaptureSource
}

func (r *compositionRunner) Run(ctx context.Context) error { <-ctx.Done(); close(r.done); return nil }
func (r *compositionRunner) Close() error                  { return r.source.Close() }

type hostOnlyCompositionService struct{ compositionService }

func (*hostOnlyCompositionService) SessionExecution(context.Context, string) (string, error) {
	return "host_only", nil
}

type compositionTargetCast struct {
	mu               sync.Mutex
	started          int
	stopped          int
	state            string
	session          string
	generation       uint64
	stopErr          error
	startErr         error
	reportSession    string
	reportGeneration uint64
	blockStop        bool
	statusErr        error
}

func (c *compositionTargetCast) CastStart(_ context.Context, session, _ string, generation uint64) (host.CastStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.started++
	c.state = "active"
	c.session = session
	c.generation = generation
	if c.startErr != nil {
		return host.CastStatus{}, c.startErr
	}
	reportSession, reportGeneration := session, generation
	if c.reportSession != "" {
		reportSession = c.reportSession
	}
	if c.reportGeneration != 0 {
		reportGeneration = c.reportGeneration
	}
	return host.CastStatus{State: "active", Session: reportSession, Generation: reportGeneration}, nil
}
func (c *compositionTargetCast) CastStop(ctx context.Context, session string, generation uint64) (host.CastStatus, error) {
	c.mu.Lock()
	if c.state == "active" && (c.session != session || c.generation != generation) {
		c.mu.Unlock()
		return host.CastStatus{}, errors.New("stale cast identity")
	}
	c.stopped++
	block := c.blockStop
	err := c.stopErr
	if err == nil && !block {
		c.state = "idle"
	}
	c.mu.Unlock()
	if block {
		<-ctx.Done()
		return host.CastStatus{}, ctx.Err()
	}
	if err != nil {
		return host.CastStatus{}, err
	}
	return host.CastStatus{State: "idle"}, nil
}
func (c *compositionTargetCast) CastStatus(context.Context) (host.CastStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.statusErr != nil {
		return host.CastStatus{}, c.statusErr
	}
	return host.CastStatus{State: c.state, Session: c.session, Generation: c.generation}, nil
}
func (c *compositionTargetCast) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started, c.stopped
}
func (c *compositionTargetCast) setState(state string) {
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
}
func (c *compositionTargetCast) setStopError(err error) {
	c.mu.Lock()
	c.stopErr = err
	c.mu.Unlock()
}
func (c *compositionTargetCast) setStatusError(err error) {
	c.mu.Lock()
	c.statusErr = err
	c.mu.Unlock()
}

func (c *compositionTargetCast) setIdentity(session string, generation uint64) {
	c.mu.Lock()
	c.state = "active"
	c.session = session
	c.generation = generation
	c.mu.Unlock()
}

func TestComposeAPIStartsAndStopsTargetCastWithHostMedia(t *testing.T) {
	target := &compositionTargetCast{}
	capture := &compositionCapture{}
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "injected", Decoder: "none"}}
	handler, cleanup, err := composeAPI(&hostOnlyCompositionService{}, config, nil,
		withTargetCast(target),
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) { return capture, nil }),
		withManagedReceiverOptions(
			remotemedia.WithManagedReceiverBind(func(string, *net.UDPAddr) (remotemedia.ManagedPacketConn, error) {
				return &compositionPacketConn{closed: make(chan struct{})}, nil
			}),
			remotemedia.WithManagedReceiverFactory(func(remotemedia.ReceiverConfig) (remotemedia.ManagedReceiverTransport, error) {
				return &compositionTransport{}, nil
			}),
		),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(remotemedia.SenderConfig, remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: capture}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	started, stopped := target.counts()
	if started != 1 || stopped != 1 {
		t.Fatalf("target lifecycle = %d/%d response=%d %s", started, stopped, response.Code, response.Body.String())
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestComposeAPIInjectsEnabledMedia(t *testing.T) {
	service := &hostOnlyCompositionService{}
	captureMade, senderMade := false, false
	capture := &compositionCapture{}
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "injected", Decoder: "none"}}
	handler, cleanup, err := composeAPI(service, config, nil,
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			captureMade = true
			return capture, nil
		}),
		withManagedReceiverOptions(
			remotemedia.WithManagedReceiverBind(func(string, *net.UDPAddr) (remotemedia.ManagedPacketConn, error) {
				return &compositionPacketConn{closed: make(chan struct{})}, nil
			}),
			remotemedia.WithManagedReceiverFactory(func(remotemedia.ReceiverConfig) (remotemedia.ManagedReceiverTransport, error) {
				return &compositionTransport{}, nil
			}),
		),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(remotemedia.SenderConfig, remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			senderMade = true
			return &compositionRunner{done: make(chan struct{}), source: capture}, nil
		})),
	)
	if err != nil || handler == nil || cleanup == nil || captureMade {
		t.Fatalf("composition = handler:%v cleanup:%v capture:%v err:%v", handler != nil, cleanup != nil, captureMade, err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code >= 500 || !senderMade {
		t.Fatalf("media launch = %d %s sender:%v", response.Code, response.Body.String(), senderMade)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if got := capture.closeCount(); got != 1 {
		t.Fatalf("capture close count = %d, want 1", got)
	}
}

func TestConfiguredCaptureSourceFactoryIsUsedByMediaComposition(t *testing.T) {
	service := &hostOnlyCompositionService{}
	called := false
	capture := &compositionCapture{}
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "screen"}}
	handler, cleanup, err := composeAPI(service, config, nil,
		withCaptureSourceFactory(func(got fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			called = got.CaptureDevice == "screen"
			return capture, nil
		}),
		withTargetCast(&compositionTargetCast{}),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(_ remotemedia.SenderConfig, source remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: source}, nil
		})),
	)
	if err != nil || cleanup == nil || called {
		t.Fatalf("composition err=%v cleanup=%v called=%v", err, cleanup != nil, called)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
	request.Host = "127.0.0.1"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code >= 500 || !called {
		t.Fatalf("media launch = %d %s called=%v", response.Code, response.Body.String(), called)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestComposeAPICreatesFreshCaptureForEachHostMediaSession(t *testing.T) {
	service := &hostOnlyCompositionService{}
	var captures []*compositionCapture
	config := fogcast.Config{Token: "token", Media: fogcast.MediaConfig{Enabled: true, Session: "session", SSRC: 7, RTPListen: "127.0.0.1:5000", RTPDestination: "127.0.0.1:5001", CaptureDevice: "screen", Decoder: "none"}}
	handler, cleanup, err := composeAPI(service, config, nil,
		withCaptureSourceFactory(func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			capture := &compositionCapture{}
			captures = append(captures, capture)
			return capture, nil
		}),
		withTargetCast(&compositionTargetCast{}),
		withManagedSenderOptions(remotemedia.WithManagedSenderFactory(func(_ remotemedia.SenderConfig, source remotemedia.CaptureSource) (remotemedia.ManagedSenderRunner, error) {
			return &compositionRunner{done: make(chan struct{}), source: source}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/session/launch", strings.NewReader(`{"game_id":"game-1"}`))
		request.Host = "127.0.0.1"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code >= 500 {
			t.Fatalf("launch %d = %d %s", i+1, response.Code, response.Body.String())
		}
	}
	if len(captures) != 2 {
		t.Fatalf("capture instances = %d, want 2", len(captures))
	}
	for i, capture := range captures {
		if got := capture.closeCount(); got != 1 {
			t.Fatalf("capture %d close count = %d, want 1", i+1, got)
		}
	}
}

type compositionDirectMediaSession struct {
	handle *compositionDirectMediaHandle
	err    error
}

func (s compositionDirectMediaSession) Start(context.Context, string) (hostapi.MediaHandle, error) {
	return s.handle, s.err
}

type compositionDirectMediaHandle struct {
	mu        sync.Mutex
	done      chan struct{}
	stopped   int
	stopErr   error
	blockStop bool
}

func (h *compositionDirectMediaHandle) Stop(ctx context.Context) error {
	h.mu.Lock()
	h.stopped++
	block := h.blockStop
	err := h.stopErr
	h.mu.Unlock()
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}
func (h *compositionDirectMediaHandle) Done() <-chan struct{} { return h.done }
func (h *compositionDirectMediaHandle) stopCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopped
}

func TestCompositionMediaHandleReportsTargetCastTermination(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	target.setState("idle")
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
	case <-time.After(time.Second):
		t.Fatal("target termination was not reported")
	}
}

func TestCompositionMediaHandleRetriesFailedTargetStopAndRetainsOwnership(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	stopErr := errors.New("target stop failed")
	target.setStopError(stopErr)
	if err := handle.Stop(context.Background()); !errors.Is(err, stopErr) {
		t.Fatalf("first stop = %v, want %v", err, stopErr)
	}
	if session.handle == nil {
		t.Fatal("failed stop cleared composition ownership")
	}
	if err := handle.Stop(context.Background()); !errors.Is(err, stopErr) {
		t.Fatalf("repeated stop = %v, want %v", err, stopErr)
	}
	if got := local.stopCount(); got != 1 {
		t.Fatalf("local stop count = %d, want 1", got)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry after target recovery = %v", err)
	}
	if session.handle != nil {
		t.Fatal("successful retry retained composition ownership")
	}
}

func TestCompositionMediaHandleBoundsTargetStop(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{blockStop: true}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := handle.Stop(context.Background()); err == nil {
		t.Fatal("blocked target stop unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("target stop took %v, want bounded cleanup", elapsed)
	}
}

func TestCompositionMediaHandleGivesTargetFreshDeadlineAfterLocalTimeout(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{}), blockStop: true}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop = %v, want local deadline failure", err)
	}
	_, stopped := target.counts()
	if stopped != 1 {
		t.Fatalf("target stop count = %d, want 1 after local timeout", stopped)
	}
}

type compositionFailingMediaSession struct{ err error }

func (s compositionFailingMediaSession) Start(context.Context, string) (hostapi.MediaHandle, error) {
	return nil, s.err
}

func TestCompositionStartReturnsRetryableOwnershipWhenTargetRollbackFails(t *testing.T) {
	target := &compositionTargetCast{stopErr: errors.New("rollback failed")}
	session := newCompositionMediaSession(compositionFailingMediaSession{err: errors.New("local start failed")}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil {
		t.Fatalf("Start = handle:%v err:%v, want retryable handle and start error", handle != nil, err)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	if session.handle != nil {
		t.Fatal("successful retry retained cleanup ownership")
	}
}

func TestCompositionMediaHandleToleratesTransientTargetStatusFailure(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	target.setStatusError(errors.New("temporary status failure"))
	time.Sleep(2 * targetStatusInterval)
	target.setStatusError(nil)
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
		t.Fatal("transient status failure closed Done")
	case <-time.After(2 * targetStatusInterval):
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCompositionMediaHandleReportsPersistentTargetStatusFailure(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	target.setStatusError(errors.New("persistent status failure"))
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
	case <-time.After(time.Second):
		t.Fatal("persistent target status failure did not close Done")
	}
}

func TestRunCleanupPropagatesStableCloseFailure(t *testing.T) {
	var stderr bytes.Buffer
	code := finishRun(0, &stderr,
		runCloser{label: "API cleanup", close: func() error { return errors.New("token=secret") }},
		runCloser{label: "service cleanup", close: func() error { return nil }},
	)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := stderr.String(); !strings.Contains(got, "fogcast-api: API cleanup failed") || strings.Contains(got, "secret") {
		t.Fatalf("stderr = %q", got)
	}
}

func TestCompositionCloseRetriesTargetCleanupAndReportsFailure(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{stopErr: errors.New("target cleanup failed")}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "session", "token", 9)
	if _, err := session.Start(context.Background(), "game"); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err == nil {
		t.Fatal("Close reported success after target cleanup failures")
	}
	_, stopped := target.counts()
	if stopped != 2 {
		t.Fatalf("target stop count = %d, want initial attempt plus bounded retry", stopped)
	}
}

func TestCompositionPartialStartRetainsLocalAndTargetCleanupOwnership(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{stopErr: errors.New("target cleanup failed")}
	session := newCompositionMediaSession(compositionDirectMediaSession{
		handle: local,
		err:    errors.New("partial local start failed"),
	}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil {
		t.Fatalf("Start = handle %v err %v, want retryable partial ownership", handle, err)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	if local.stopCount() != 1 {
		t.Fatalf("local partial stop count = %d, want 1", local.stopCount())
	}
	_, stopped := target.counts()
	if stopped != 2 {
		t.Fatalf("target stop count = %d, want failed rollback plus retry", stopped)
	}
}

func TestManagedSenderComponentRetainsFailedCallerOwnedCaptureCleanup(t *testing.T) {
	capture := &compositionCapture{closeErrs: []error{errors.New("first close failed")}}
	component := &managedSenderComponent{
		media: fogcast.MediaConfig{
			Session: "session", Generation: 9, SSRC: 7,
			RTPDestination: "127.0.0.1:5001", ControlAddress: "127.0.0.1:5002",
			Bitrate: 1_000_000, GOP: 30, MTU: 1200,
		},
		token: "token",
		newCapture: func(fogcast.MediaConfig) (remotemedia.CaptureSource, error) {
			return capture, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handle, err := component.Start(ctx, "game")
	if err == nil || handle == nil {
		t.Fatalf("Start = handle:%v err:%v, want retryable capture ownership", handle != nil, err)
	}
	if got := capture.closeCount(); got != 1 {
		t.Fatalf("initial close count = %d, want 1", got)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry cleanup = %v", err)
	}
	if got := capture.closeCount(); got != 2 {
		t.Fatalf("retry close count = %d, want 2", got)
	}
}

func TestCompositionPartialLocalHandleSurvivesSuccessfulTargetRollback(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{
		handle: local, err: errors.New("partial local start failed"),
	}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil || session.handle == nil {
		t.Fatalf("Start = handle:%v owned:%v err:%v", handle != nil, session.handle != nil, err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := local.stopCount(); got != 1 {
		t.Fatalf("local cleanup count = %d, want 1", got)
	}
	_, stopped := target.counts()
	if stopped != 1 {
		t.Fatalf("target stop count = %d, want rollback only", stopped)
	}
}

func TestCompositionFailedTargetStartRetainsFailedRollbackOwnership(t *testing.T) {
	target := &compositionTargetCast{
		startErr: errors.New("partial target start failed"),
		stopErr:  errors.New("target rollback failed"),
	}
	session := newCompositionMediaSession(compositionFailingMediaSession{}, target, "session", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil || session.handle == nil {
		t.Fatalf("Start = handle:%v owned:%v err:%v", handle != nil, session.handle != nil, err)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry target rollback = %v", err)
	}
	_, stopped := target.counts()
	if stopped != 2 {
		t.Fatalf("target stop count = %d, want failed rollback plus retry", stopped)
	}
}

func TestCompositionMismatchedStartIdentityRollsBackRequestedIdentity(t *testing.T) {
	target := &compositionTargetCast{
		reportSession: "foreign", reportGeneration: 10,
		stopErr: errors.New("requested identity rollback failed"),
	}
	session := newCompositionMediaSession(compositionFailingMediaSession{}, target, "requested", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err == nil || handle == nil || session.handle == nil {
		t.Fatalf("Start = handle:%v owned:%v err:%v", handle != nil, session.handle != nil, err)
	}
	target.setStopError(nil)
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry requested-identity rollback = %v", err)
	}
	_, stopped := target.counts()
	if stopped != 2 {
		t.Fatalf("target stop count = %d, want immediate rollback plus retry", stopped)
	}
}

func TestCompositionOldGenerationCannotStopReplacementTarget(t *testing.T) {
	local := &compositionDirectMediaHandle{done: make(chan struct{})}
	target := &compositionTargetCast{}
	session := newCompositionMediaSession(compositionDirectMediaSession{handle: local}, target, "old", "token", 9)
	handle, err := session.Start(context.Background(), "game")
	if err != nil {
		t.Fatal(err)
	}
	target.setIdentity("replacement", 10)
	select {
	case <-handle.(interface{ Done() <-chan struct{} }).Done():
	case <-time.After(time.Second):
		t.Fatal("replacement identity was not terminal for old generation")
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("old generation local cleanup = %v", err)
	}
	target.mu.Lock()
	state, sessionID, generation := target.state, target.session, target.generation
	target.mu.Unlock()
	if state != "active" || sessionID != "replacement" || generation != 10 {
		t.Fatalf("replacement changed: state=%q session=%q generation=%d", state, sessionID, generation)
	}
}

func TestStopServiceForShutdownRetriesAndPropagatesFirstFailure(t *testing.T) {
	first := errors.New("host stop failed")
	service := &shutdownCompositionService{stopErrs: []error{first, nil}}
	if err := stopServiceForShutdown(service); !errors.Is(err, first) {
		t.Fatalf("shutdown error = %v, want first failure", err)
	}
	service.mu.Lock()
	stops := service.stops
	service.mu.Unlock()
	if stops != 2 {
		t.Fatalf("shutdown stop attempts = %d, want 2", stops)
	}
}

func TestCloseAPICompositionAttemptsCompositionAfterRemoteInputFailure(t *testing.T) {
	remoteErr := errors.New("remote input close failed")
	compositionCalled := false
	err := closeAPIComposition(func() error { return remoteErr }, []func() error{
		func() error {
			compositionCalled = true
			return errors.New("composition close failed")
		},
	})
	if !errors.Is(err, remoteErr) {
		t.Fatalf("close error = %v, want first remote-input failure", err)
	}
	if !compositionCalled {
		t.Fatal("remote-input failure skipped composition cleanup")
	}
}
