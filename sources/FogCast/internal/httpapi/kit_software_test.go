package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DeanoC/FogCast/appliance/software"
	"github.com/DeanoC/FogCast/appliance/softwarecontrol"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/testsock"
	"github.com/DeanoC/FogCast/protocol"
)

func softwareSocket(t *testing.T, handler http.Handler) string {
	t.Helper()
	socket := testsock.Path(t, "software.sock")
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
	receiptID := strings.Repeat("b", 64)
	socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(softwarecontrol.Status{
			State: softwarecontrol.State{Version: 1, Generation: 3, Selected: selected, Previous: strings.Repeat("c", 64),
				Pending:  &softwarecontrol.Operation{Request: softwarecontrol.Request{ID: strings.Repeat("d", 64), Candidate: strings.Repeat("e", 64), Requester: "private-owner", Generation: 3}, Previous: selected, Phase: "queued", Reason: "private-pending"},
				Receipts: map[string]softwarecontrol.Receipt{receiptID: {Request: softwarecontrol.Request{ID: receiptID, Candidate: selected, Requester: "private-requester", Generation: 2}, Result: "accepted"}}},
			ActiveBundle: selected, BaseIdentity: "base-contract", ServiceFault: "private-service",
		})
	}))
	handler := httpapi.New(&fakeController{health: protocol.Health{APIVersion: "v1", Artifacts: &protocol.Artifacts{ImageSHA256: strings.Repeat("b", 64)}, Software: &protocol.Identity{ActiveBundle: "controller"}}}, "secret", "test", nil, httpapi.WithKitSoftware(socket))
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
	want := protocol.Identity{ActiveBundle: selected, BaseIdentity: "base-contract", Selected: selected, Generation: 3}
	if health.Software == nil || *health.Software != want || health.Artifacts.ImageSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("software/base identities: %+v", health)
	}
	// Health is polled; the operation ledger stays on GET /v1/kit/software.
	if strings.Contains(response.Body.String(), "private") {
		t.Fatalf("health carried supervisor operation status: %s", response.Body.String())
	}
}

func TestSoftwareHealthFallsBackToLaunchIdentity(t *testing.T) {
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", strings.Repeat("a", 64))
	t.Setenv("FES_SOFTWARE_BASE_IDENTITY", "base-contract")
	launch := protocol.Identity{ActiveBundle: strings.Repeat("a", 64), BaseIdentity: "base-contract"}
	for name, supervisor := range map[string]http.HandlerFunc{
		"rejected": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"storage"}`, http.StatusServiceUnavailable)
		},
		"garbled": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"selected":`)) },
		"partial type error": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"selected":"` + strings.Repeat("b", 64) + `","generation":"bad"}`))
		},
		"null": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`null`)) },
	} {
		t.Run(name, func(t *testing.T) {
			handler := httpapi.New(&fakeController{health: protocol.Health{APIVersion: "v1"}}, "secret", "test", nil, httpapi.WithKitSoftware(softwareSocket(t, supervisor)))
			var health protocol.Health
			if err := json.Unmarshal(softwareAgentRequest(handler, "GET", "/v1/health", "secret", "", nil).Body.Bytes(), &health); err != nil {
				t.Fatal(err)
			}
			if health.Software == nil || *health.Software != launch {
				t.Fatalf("fallback identity: %+v", health.Software)
			}
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		handler := httpapi.New(&fakeController{health: protocol.Health{APIVersion: "v1"}}, "secret", "test", nil, httpapi.WithKitSoftware(testsock.Path(t, "absent.sock")))
		var health protocol.Health
		if err := json.Unmarshal(softwareAgentRequest(handler, "GET", "/v1/health", "secret", "", nil).Body.Bytes(), &health); err != nil {
			t.Fatal(err)
		}
		if health.Software == nil || *health.Software != launch {
			t.Fatalf("fallback identity: %+v", health.Software)
		}
	})
}

func TestSoftwareHealthDoesNotPublishUnverifiedSelection(t *testing.T) {
	active := strings.Repeat("a", 64)
	selected := strings.Repeat("b", 64)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", active)
	t.Setenv("FES_SOFTWARE_BASE_IDENTITY", "launch-base")
	valid := `{"version":1,"generation":0,"selected":"` + selected + `","receipts":{},"active_bundle":"` + selected + `","base_identity":"other-base"}`
	for _, tc := range []struct{ name, body string }{
		{"storage failure", strings.Replace(valid, `"version":1`, `"storage_error":"private-error","version":1`, 1)},
		{"missing generation", strings.Replace(valid, `"generation":0,`, "", 1)},
		{"duplicate selected", strings.Replace(valid, `"selected":`, `"selected":"`+selected+`","selected":`, 1)},
		{"trailing document", valid + `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, tc.body)
			}))
			handler := httpapi.New(&fakeController{health: protocol.Health{APIVersion: "v1"}}, "secret", "test", nil, httpapi.WithKitSoftware(socket))
			response := softwareAgentRequest(handler, "GET", "/v1/health", "secret", "", nil)
			var health protocol.Health
			if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
				t.Fatal(err)
			}
			want := protocol.Identity{ActiveBundle: active, BaseIdentity: "launch-base"}
			if health.Software == nil || *health.Software != want || calls.Load() != 1 {
				t.Fatalf("identity=%+v calls=%d", health.Software, calls.Load())
			}
			if strings.Contains(response.Body.String(), "private-error") || strings.Contains(response.Body.String(), "receipts") {
				t.Fatalf("health leaked supervisor details: %s", response.Body.String())
			}
		})
	}
}
