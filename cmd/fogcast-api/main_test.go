package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/catalog"
	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/host"
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
