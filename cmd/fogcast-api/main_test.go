package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/host"
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

type compositionCapture struct{ closed int }

func (*compositionCapture) Start() error { return nil }
func (*compositionCapture) Next(context.Context) (remotemedia.EncodedSample, error) {
	return remotemedia.EncodedSample{}, context.Canceled
}
func (*compositionCapture) Stats() remotemedia.CaptureStats { return remotemedia.CaptureStats{} }
func (c *compositionCapture) Close() error                  { c.closed++; return nil }

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
	if err != nil || handler == nil || cleanup == nil || !captureMade {
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
	if capture.closed != 1 {
		t.Fatalf("capture close count = %d, want 1", capture.closed)
	}
}
