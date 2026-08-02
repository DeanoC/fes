package host_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/protocol"
)

const idleStatusJSON = `{"state":"idle","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null}`

func TestClientAddsBearerAndDecodesStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/status" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, idleStatusJSON)
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	status, err := host.NewClient(baseURL, "test-token", server.Client()).Status(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func TestClientHealthOmitsAuthorization(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" || r.Header.Get("Authorization") != "" {
			t.Errorf("path = %q, authorization = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"api_version":"v1","agent_version":"0.1.0","ready":true,"mister_process":true,"command_pipe":true}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	health, err := host.NewClient(baseURL, "test-token", server.Client()).Health(context.Background())
	if err != nil || !health.Ready {
		t.Fatalf("health = %#v, %v", health, err)
	}
}

func TestClientLaunchSendsJSON(t *testing.T) {
	t.Parallel()
	want := protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/launch" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var got protocol.LaunchRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode launch: %v", err)
		}
		if got != want {
			t.Errorf("launch = %#v, want %#v", got, want)
		}
		_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-test","system":"snes","expected_core":"SNES","observed_core":"SNES","last_error":null}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	status, err := host.NewClient(baseURL, "test-token", server.Client()).Launch(context.Background(), want)
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func TestClientDecodesAPIError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"ROM_NOT_FOUND","message":"missing"}}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	_, err := host.NewClient(baseURL, "test-token", server.Client()).Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc"})
	var apiErr *protocol.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeROMNotFound {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", (1<<20)+1))
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	if _, err := host.NewClient(baseURL, "test-token", server.Client()).Status(context.Background()); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestClientRejectsMultipleJSONResponses(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, idleStatusJSON+idleStatusJSON)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	if _, err := host.NewClient(baseURL, "test-token", server.Client()).Status(context.Background()); err == nil {
		t.Fatal("multiple JSON responses accepted")
	}
}

func TestClientStopSendsEmptyPOST(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/stop" || len(body) != 0 {
			t.Errorf("request = %s %s body=%q", r.Method, r.URL.Path, body)
		}
		_, _ = io.WriteString(w, idleStatusJSON)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	if _, err := host.NewClient(baseURL, "test-token", server.Client()).Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
