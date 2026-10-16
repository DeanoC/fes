package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/protocol"
)

func softwareSocket(t *testing.T, handler http.Handler) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "software.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return socket
}

func softwareAgentRequest(handler http.Handler, method, path, token, contentType string, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestKitSoftwareProxyAuthenticatesBeforeSupervisorAccess(t *testing.T) {
	var calls atomic.Int32
	body := []byte("immutable userspace tar bytes")
	socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("remote bearer forwarded to root-only supervisor")
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/kit/software/releases" {
			data, _ := io.ReadAll(r.Body)
			if !bytes.Equal(body, data) || r.ContentLength != int64(len(body)) || r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Error("bundle upload changed across proxy")
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"installed"}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"another operation is queued"}`))
	}))
	handler := httpapi.New(&fakeController{}, "secret", "test", nil, httpapi.WithKitSoftware(socket))
	for _, route := range []struct{ method, path string }{{"GET", "/v1/kit/software"}, {"GET", "/v1/kit/software/releases"}, {"POST", "/v1/kit/software/releases"}, {"POST", "/v1/kit/software/activate"}, {"POST", "/v1/kit/software/cancel"}} {
		response := softwareAgentRequest(handler, route.method, route.path, "", "application/json", []byte(`{}`))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s: %d", route.path, response.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unauthenticated caller reached supervisor")
	}
	response := softwareAgentRequest(handler, "POST", "/v1/kit/software/releases", "secret", "application/octet-stream", body)
	if response.Code != http.StatusCreated || response.Body.String() != `{"id":"installed"}` {
		t.Fatalf("upload proxy: %d %s", response.Code, response.Body.String())
	}
	response = softwareAgentRequest(handler, "POST", "/v1/kit/software/activate", "secret", "application/json", []byte(`{}`))
	var failure protocol.ErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusConflict || failure.Error.Code != "KIT_SOFTWARE_CONFLICT" {
		t.Fatalf("conflict translation: %d %+v", response.Code, failure)
	}
	before := calls.Load()
	request := httptest.NewRequest(http.MethodPost, "/v1/kit/software/releases", strings.NewReader("x"))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.ContentLength = software.MaxBundleSize + 1
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || calls.Load() != before {
		t.Fatal("oversized upload reached supervisor")
	}
	response = softwareAgentRequest(handler, "GET", "/v1/kit/software?socket=other", "secret", "", nil)
	if response.Code != http.StatusBadRequest || calls.Load() != before {
		t.Fatal("query selected supervisor request shape")
	}
}

func TestSoftwareHealthSeparatesIdentityAndRestrictsStatus(t *testing.T) {
	var calls atomic.Int32
	selected := strings.Repeat("a", 64)
	socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(softwarecontrol.Status{State: softwarecontrol.State{Version: 1, Generation: 3, Selected: selected}, ActiveBundle: selected, BaseIdentity: "base-contract"})
	}))
	handler := httpapi.New(&fakeController{health: protocol.Health{APIVersion: "v1", Artifacts: &protocol.Artifacts{ImageSHA256: strings.Repeat("b", 64)}, Software: &softwarecontrol.Status{StorageError: "private controller status"}}}, "secret", "test", nil, httpapi.WithKitSoftware(socket))
	response := softwareAgentRequest(handler, "GET", "/v1/health", "", "", nil)
	var health protocol.Health
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Software != nil || calls.Load() != 0 {
		t.Fatal("unauthenticated health exposed software status")
	}
	response = softwareAgentRequest(handler, "GET", "/v1/health", "secret", "", nil)
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Software == nil || health.Software.ActiveBundle != selected || health.Software.BaseIdentity != "base-contract" || health.Artifacts.ImageSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("software/base identities: %+v", health)
	}
}
