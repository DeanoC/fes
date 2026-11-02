package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(softwarecontrol.Status{State: softwarecontrol.State{Version: 1, Generation: 3, Selected: selected, Receipts: map[string]softwarecontrol.Receipt{}}, ActiveBundle: selected, BaseIdentity: "base-contract"})
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

func TestSoftwareHealthRejectsUnverifiedSupervisorResponses(t *testing.T) {
	active, base, selected := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", active)
	t.Setenv("FES_SOFTWARE_BASE_IDENTITY", base)
	valid := `{"version":1,"generation":0,"selected":"` + selected + `","receipts":{},"active_bundle":"` + active + `","base_identity":"` + base + `"}`
	for _, tc := range []struct {
		name, body string
		status     int
		truncated  bool
		delayed    bool
		valid      bool
	}{
		{name: "valid initial generation", body: valid, valid: true},
		{name: "wrong type after selected", body: `{"selected":"` + selected + `","generation":"bad"}`},
		{name: "truncated JSON", body: valid[:len(valid)-1]},
		{name: "truncated transport", body: valid, truncated: true},
		{name: "malformed JSON", body: valid + ` broken`},
		{name: "trailing JSON", body: valid + `{}`},
		{name: "duplicate selection", body: strings.Replace(valid, `"selected":`, `"selected":"`+selected+`","selected":`, 1)},
		{name: "case-folded duplicate selection", body: strings.Replace(valid, `"selected":`, `"Selected":"`+selected+`","selected":`, 1)},
		{name: "missing generation", body: strings.Replace(valid, `"generation":0,`, "", 1)},
		{name: "null generation", body: strings.Replace(valid, `"generation":0`, `"generation":null`, 1)},
		{name: "missing receipts", body: strings.Replace(valid, `"receipts":{},`, "", 1)},
		{name: "null receipts", body: strings.Replace(valid, `"receipts":{}`, `"receipts":null`, 1)},
		{name: "invalid selection", body: strings.Replace(valid, selected, "not-an-id", 1)},
		{name: "invalid version", body: strings.Replace(valid, `"version":1`, `"version":2`, 1)},
		{name: "supervisor reports storage error", body: strings.TrimSuffix(valid, "}") + `,"storage_error":"private"}`},
		{name: "supervisor unavailable", body: valid, status: http.StatusServiceUnavailable},
		{name: "supervisor timeout", body: valid, delayed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := softwareSocket(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.delayed {
					<-r.Context().Done()
					return
				}
				if tc.truncated {
					w.Header().Set("Content-Length", fmt.Sprint(len(tc.body)+10))
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			handler := httpapi.New(&fakeController{}, "secret", "test", nil, httpapi.WithKitSoftware(socket))
			response := softwareAgentRequest(handler, "GET", "/v1/health", "secret", "", nil)
			var health protocol.Health
			if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
				t.Fatal(err)
			}
			if health.Software == nil || health.Software.ActiveBundle != active || health.Software.BaseIdentity != base {
				t.Fatalf("independent active/base identities lost: %+v", health.Software)
			}
			if tc.valid {
				if health.Software.Selected != selected || health.Software.Generation != 0 || health.Software.StorageError != "" {
					t.Fatalf("valid status rejected: %+v", health.Software)
				}
			} else if health.Software.Selected != "" || health.Software.Generation != 0 || health.Software.StorageError == "" || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("unverified status published: %+v", health.Software)
			}
		})
	}
}

func TestSoftwareHealthUnavailableKeepsIndependentIdentities(t *testing.T) {
	active, base := strings.Repeat("a", 64), strings.Repeat("b", 64)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", active)
	t.Setenv("FES_SOFTWARE_BASE_IDENTITY", base)
	handler := httpapi.New(&fakeController{}, "secret", "test", nil, httpapi.WithKitSoftware(testsock.Path(t, "missing.sock")))
	response := softwareAgentRequest(handler, "GET", "/v1/health", "secret", "", nil)
	var health protocol.Health
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Software == nil || health.Software.ActiveBundle != active || health.Software.BaseIdentity != base || health.Software.Selected != "" || health.Software.StorageError == "" {
		t.Fatalf("unavailable supervisor health: %+v", health.Software)
	}
}
