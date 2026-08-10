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

func TestClientCastLifecycleUsesAuthenticatedEndpoints(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/cast/start":
			if r.Method != http.MethodPost {
				t.Errorf("start method = %s", r.Method)
			}
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode start: %v", err)
			}
			if _, ok := request["media"]; ok {
				t.Error("legacy CastStart unexpectedly sent media")
			}
			_, _ = io.WriteString(w, `{"state":"active"}`)
		case "/v1/cast/stop":
			if r.Method != http.MethodPost {
				t.Errorf("stop method = %s", r.Method)
			}
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	client := host.NewClient(baseURL, "test-token", server.Client())
	if status, err := client.CastStart(context.Background(), "session", "bridge-token", 9); err != nil || status.State != "active" {
		t.Fatalf("cast start = %#v, %v", status, err)
	}
	if status, err := client.CastStop(context.Background(), "session", 9); err != nil || status.State != "idle" {
		t.Fatalf("cast stop = %#v, %v", status, err)
	}
}

func TestClientCastStartWithMediaSendsCanonicalDescriptor(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Media protocol.CastMediaSet `json:"media"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Media != (protocol.CastMediaSet{Version: protocol.CastMediaSetVersion, Video: true, Audio: true}) {
			t.Fatalf("media = %#v", request.Media)
		}
		_, _ = io.WriteString(w, `{"state":"active","media":{"version":1,"video":true,"audio":true,"ready":true,"capabilities":{"version":1,"video":true,"audio":true}}}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	status, err := host.NewClient(baseURL, "test-token", server.Client()).CastStartWithMedia(context.Background(), "session", "bridge-token", 9, protocol.CastMediaSet{Version: protocol.CastMediaSetVersion, Video: true, Audio: true})
	if err != nil || status.Media == nil || !status.Media.Audio || !status.Media.Ready {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
}

func TestClientCastStartWithMediaStopsUnacknowledgedTarget(t *testing.T) {
	t.Parallel()
	stops := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/cast/start":
			_, _ = io.WriteString(w, `{"state":"active"}`)
		case "/v1/cast/stop":
			stops++
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	_, err := host.NewClient(baseURL, "test-token", server.Client()).CastStartWithMedia(context.Background(), "session", "bridge-token", 9, protocol.CastMediaSet{Version: protocol.CastMediaSetVersion, Video: true, Audio: true})
	if err == nil || stops != 1 {
		t.Fatalf("err=%v stops=%d", err, stops)
	}
}
