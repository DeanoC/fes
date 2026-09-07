package hostapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/remoteinput"
)

const launcherID = "73dc9f5f-1a12-4a95-a820-a9b4e600769a"
const launcherToken = "12345678901234567890123456789012"

type launcherService struct{ fakeService }

func (*launcherService) TargetConnection() fogcast.TargetConnection {
	return fogcast.TargetConnection{TargetID: launcherID, State: "ready"}
}

func launcherHandler(t *testing.T, input host.RemoteInputController) http.Handler {
	t.Helper()
	api := hostapi.New(&launcherService{}, hostapi.WithRemoteInput(input))
	handler, err := hostapi.NewLauncherHandler(api, hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
func launcherRequest(method, path string, body io.Reader) *http.Request {
	req, _ := http.NewRequest(method, path, body)
	req.Header.Set("Authorization", "Bearer "+launcherToken)
	req.Header.Set("X-FogCast-Target-ID", launcherID)
	return req
}
func TestLauncherRestrictionAndAuthentication(t *testing.T) {
	handler := launcherHandler(t, nil)
	for _, tc := range []struct {
		name, method, path, token, id string
		want                          int
	}{
		{"catalogue", "GET", "/api/v1/games", launcherToken, launcherID, 200},
		{"presentation", "GET", "/api/v1/presentation/games/snes-mario", launcherToken, launcherID, 404},
		{"no token", "GET", "/api/v1/games", "", launcherID, 401},
		{"wrong token", "GET", "/api/v1/games", "wrong", launcherID, 401},
		{"wrong identity", "GET", "/api/v1/games", launcherToken, "different", 403},
		{"settings", "GET", "/api/v1/library/settings", launcherToken, launcherID, 404},
		{"development", "POST", "/api/v1/session/development-rbf", launcherToken, launcherID, 404},
		{"input detach", "POST", "/api/v1/session/input/detach", launcherToken, launcherID, 404},
		{"browser", "GET", "/", launcherToken, launcherID, 404},
		{"method", "DELETE", "/api/v1/games", launcherToken, launcherID, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := launcherRequest(tc.method, "http://192.0.2.1:8789"+tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			req.Header.Set("X-FogCast-Target-ID", tc.id)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	api := hostapi.New(&launcherService{})
	w := httptest.NewRecorder()
	api.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1/api/v1/games", nil))
	if w.Code != 403 {
		t.Fatalf("browser API exposed: %d", w.Code)
	}
}

func TestLauncherAllowsArtworkGET(t *testing.T) {
	handler := launcherHandler(t, nil)
	handle := strings.Repeat("ab", 32)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1:8789/api/v1/presentation/artwork/"+handle, nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "ARTWORK_UNAVAILABLE") {
		t.Fatalf("admitted artwork: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("GET", "http://192.0.2.1:8789/api/v1/presentation/artwork/nope", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), "launcher operation is unavailable") {
		t.Fatalf("rejected artwork: %d %s", w.Code, w.Body.String())
	}
}

type launcherInput struct {
	mu      sync.Mutex
	source  *launcherSource
	session string
}

func (*launcherInput) Attach(context.Context, string) error { return nil }
func (*launcherInput) Detach(context.Context, string) error { return nil }
func (*launcherInput) Status() host.RemoteInputStatus {
	return host.RemoteInputStatus{State: host.RemoteInputAttached, Ready: true, SessionID: "123"}
}
func (i *launcherInput) ClaimSource(id string) (host.RemoteInputEventSource, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if id != "123" {
		return nil, host.ErrRemoteInputInvalid
	}
	if i.source != nil {
		return nil, host.ErrRemoteInputBusy
	}
	s := &launcherSource{input: i, closed: make(chan struct{}), events: make(chan remoteinput.Event, 4)}
	i.source = s
	return s, nil
}

type launcherSource struct {
	input  *launcherInput
	closed chan struct{}
	events chan remoteinput.Event
}

func (*launcherSource) Check() error { return nil }
func (s *launcherSource) SendEvent(_ context.Context, e remoteinput.Event, _ time.Time) error {
	s.events <- e
	return nil
}
func (s *launcherSource) Close() error {
	s.input.mu.Lock()
	defer s.input.mu.Unlock()
	if s.input.source == s {
		s.input.source = nil
		close(s.closed)
	}
	return nil
}

func TestLauncherStreamDisconnectAndTimeoutRelease(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "timeout"}[timeout], func(t *testing.T) {
			input := &launcherInput{}
			server := httptest.NewServer(launcherHandler(t, input))
			defer server.Close()
			reader, writer := io.Pipe()
			defer writer.Close()
			req := launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=123", reader)
			response, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("stream: %d", response.StatusCode)
			}
			var ready struct {
				Ready bool `json:"ready"`
			}
			if err = json.NewDecoder(response.Body).Decode(&ready); err != nil || !ready.Ready {
				t.Fatalf("ready=%v err=%v", ready, err)
			}
			input.mu.Lock()
			source := input.source
			input.mu.Unlock()
			if source == nil {
				t.Fatal("not claimed")
			}
			_, err = io.WriteString(writer, "{\"event\":{\"Device\":1,\"Kind\":1,\"Action\":1,\"Code\":104}}\n")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-source.events:
			case <-time.After(time.Second):
				t.Fatal("event not delivered")
			}
			if !timeout {
				writer.Close()
			}
			select {
			case <-source.closed:
			case <-time.After(2 * time.Second):
				t.Fatal("source not released")
			}
		})
	}
}
func TestLauncherStreamRejectsStaleSession(t *testing.T) {
	server := httptest.NewServer(launcherHandler(t, &launcherInput{}))
	defer server.Close()
	response, err := server.Client().Do(launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=old", strings.NewReader("{}\n")))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 409 {
		t.Fatalf("stale: %d", response.StatusCode)
	}
}

func TestLauncherRejectsConfiguredTargetMismatchBeforeDispatch(t *testing.T) {
	service := &settingsFake{settings: fogcast.LibraryConfig{SelectedTarget: "other", Targets: []fogcast.TargetConfig{{Name: "other", Enabled: true, TargetID: "a3cdbf5f-1a12-4a95-a820-a9b4e600769a"}}}}
	api := hostapi.New(service)
	handler, err := hostapi.NewLauncherHandler(api, hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, launcherRequest("POST", "http://192.0.2.1/api/v1/session/launch", strings.NewReader(`{"game_id":"pong"}`)))
	if w.Code != 403 || service.launchCalls != 0 {
		t.Fatalf("status=%d launchCalls=%d", w.Code, service.launchCalls)
	}
}

func TestLauncherStreamRejectsMalformedAndCompetingSource(t *testing.T) {
	for _, body := range []string{"{invalid}\n", `{"event":{"Device":0,"Kind":0,"Action":1,"Code":2}}` + "\n", strings.Repeat("x", 4097) + "\n"} {
		t.Run(body[:8], func(t *testing.T) {
			input := &launcherInput{}
			server := httptest.NewServer(launcherHandler(t, input))
			defer server.Close()
			reader, writer := io.Pipe()
			defer writer.Close()
			response, err := server.Client().Do(launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=123", reader))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			input.mu.Lock()
			source := input.source
			input.mu.Unlock()
			if source == nil {
				t.Fatal("source absent")
			}
			second, err := server.Client().Do(launcherRequest("POST", server.URL+"/api/v1/launcher/input?session_id=123", strings.NewReader("{}\n")))
			if err != nil {
				t.Fatal(err)
			}
			second.Body.Close()
			if second.StatusCode != 409 {
				t.Fatalf("competing source: %d", second.StatusCode)
			}
			_, _ = io.WriteString(writer, body)
			select {
			case <-source.closed:
			case <-time.After(2 * time.Second):
				t.Fatal("invalid input retained source")
			}
			select {
			case event := <-source.events:
				t.Fatalf("invalid input forwarded: %#v", event)
			default:
			}
		})
	}
}
