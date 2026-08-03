package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/httpapi"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type fakeController struct {
	health        protocol.Health
	status        protocol.Status
	launchErr     *protocol.APIError
	stopErr       *protocol.APIError
	launchRequest protocol.LaunchRequest
	healthVersion string
}

func (f *fakeController) Health(version string) protocol.Health {
	f.healthVersion = version
	return f.health
}

func (f *fakeController) Status() protocol.Status {
	return f.status
}

func (f *fakeController) Launch(_ context.Context, request protocol.LaunchRequest) (protocol.Status, *protocol.APIError) {
	f.launchRequest = request
	return f.status, f.launchErr
}

func (f *fakeController) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	return protocol.Status{State: protocol.StateIdle}, f.stopErr
}

func TestAPIContract(t *testing.T) {
	t.Parallel()
	active := protocol.Status{State: protocol.StateActive}
	tests := []struct {
		name          string
		method        string
		path          string
		body          string
		authorization string
		launchErr     *protocol.APIError
		wantStatus    int
		wantCode      protocol.ErrorCode
	}{
		{name: "health without auth", method: http.MethodGet, path: "/v1/health", wantStatus: http.StatusOK},
		{name: "status missing auth", method: http.MethodGet, path: "/v1/status", wantStatus: http.StatusUnauthorized, wantCode: protocol.CodeUnauthorized},
		{name: "status token without bearer scheme", method: http.MethodGet, path: "/v1/status", authorization: "test-token", wantStatus: http.StatusUnauthorized, wantCode: protocol.CodeUnauthorized},
		{name: "status wrong auth", method: http.MethodGet, path: "/v1/status", authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized, wantCode: protocol.CodeUnauthorized},
		{name: "status valid auth", method: http.MethodGet, path: "/v1/status", authorization: "Bearer test-token", wantStatus: http.StatusOK},
		{name: "launch unknown field", method: http.MethodPost, path: "/v1/launch", authorization: "Bearer test-token", body: `{"game_id":"megadrive-test","system":"megadrive","rom_path":"/media/fat/games/MegaDrive/test.md","rbf":"bad"}`, wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "launch active", method: http.MethodPost, path: "/v1/launch", authorization: "Bearer test-token", body: `{"game_id":"megadrive-test","system":"megadrive","rom_path":"/media/fat/games/MegaDrive/test.md"}`, wantStatus: http.StatusOK},
		{name: "launch missing ROM", method: http.MethodPost, path: "/v1/launch", authorization: "Bearer test-token", body: `{"game_id":"megadrive-test","system":"megadrive","rom_path":"/media/fat/games/MegaDrive/test.md"}`, launchErr: &protocol.APIError{Code: protocol.CodeROMNotFound, Message: "missing"}, wantStatus: http.StatusNotFound, wantCode: protocol.CodeROMNotFound},
		{name: "stop", method: http.MethodPost, path: "/v1/stop", authorization: "Bearer test-token", wantStatus: http.StatusOK},
		{name: "stop body rejected", method: http.MethodPost, path: "/v1/stop", authorization: "Bearer test-token", body: `{}`, wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &fakeController{health: protocol.Health{Ready: true}, status: active, launchErr: tt.launchErr}
			handler := httpapi.New(controller, "test-token", "0.1.0", slog.New(slog.NewJSONHandler(io.Discard, nil)))
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.authorization != "" {
				request.Header.Set("Authorization", tt.authorization)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			if contentType := response.Header().Get("Content-Type"); response.Code != http.StatusMethodNotAllowed && contentType != "application/json" {
				t.Fatalf("content type = %q", contentType)
			}
			if !strings.HasSuffix(response.Body.String(), "\n") {
				t.Fatalf("response does not end in newline: %q", response.Body.String())
			}
			if tt.wantCode != "" {
				var envelope protocol.ErrorEnvelope
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Error.Code != tt.wantCode {
					t.Fatalf("code = %s", envelope.Error.Code)
				}
			}
			if tt.name == "health without auth" && controller.healthVersion != "0.1.0" {
				t.Fatalf("health version = %q", controller.healthVersion)
			}
			if tt.name == "launch active" && controller.launchRequest.GameID != "megadrive-test" {
				t.Fatalf("launch request = %#v", controller.launchRequest)
			}
		})
	}
}

func TestLaunchRejectsMalformedMultipleAndOversizedJSON(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"malformed": `{"game_id":`,
		"multiple":  `{"game_id":"one","system":"snes","rom_path":"/one.sfc"} {"game_id":"two"}`,
		"oversized": `{"game_id":"snes-test","system":"snes","rom_path":"/` + strings.Repeat("x", 64<<10) + `.sfc"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			handler := httpapi.New(&fakeController{}, "test-token", "0.1.0", slog.New(slog.NewJSONHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodPost, "/v1/launch", strings.NewReader(body))
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			var envelope protocol.ErrorEnvelope
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != protocol.CodeBadRequest {
				t.Fatalf("error envelope = %#v, decode error = %v", envelope, err)
			}
		})
	}
}

func TestAPIErrorStatusMapping(t *testing.T) {
	t.Parallel()
	tests := map[protocol.ErrorCode]int{
		protocol.CodeBadRequest:        http.StatusBadRequest,
		protocol.CodeUnauthorized:      http.StatusUnauthorized,
		protocol.CodeROMNotFound:       http.StatusNotFound,
		protocol.CodeBusy:              http.StatusConflict,
		protocol.CodeUnsupportedSystem: http.StatusUnprocessableEntity,
		protocol.CodeInvalidROMPath:    http.StatusUnprocessableEntity,
		protocol.CodeMiSTerUnavailable: http.StatusServiceUnavailable,
		protocol.CodeCoreTimeout:       http.StatusServiceUnavailable,
		protocol.CodeContentNotCached:  http.StatusNotFound,
		protocol.CodeSourceUnavailable: http.StatusUnprocessableEntity,
		protocol.CodeInvalidArchive:    http.StatusUnprocessableEntity,
		protocol.CodeDigestMismatch:    http.StatusUnprocessableEntity,
		protocol.CodeTransferFailed:    http.StatusBadRequest,
		protocol.CodeCacheFull:         http.StatusInsufficientStorage,
		protocol.CodeInternal:          http.StatusInternalServerError,
	}
	for code, wantStatus := range tests {
		t.Run(string(code), func(t *testing.T) {
			controller := &fakeController{launchErr: &protocol.APIError{Code: code, Message: "failure"}}
			handler := httpapi.New(controller, "test-token", "0.1.0", slog.New(slog.NewJSONHandler(io.Discard, nil)))
			request := httptest.NewRequest(http.MethodPost, "/v1/launch", strings.NewReader(`{"game_id":"snes-test","system":"snes","rom_path":"/media/fat/games/SNES/test.sfc"}`))
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, wantStatus, response.Body.String())
			}
		})
	}
}

func TestV1GoldenResponsesRemainUnchangedWithContentOption(t *testing.T) {
	gameID := "snes-test"
	system := protocol.SystemSNES
	expected, observed := "SNES", "SNES"
	active := protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &expected, ObservedCore: &observed}
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		auth   bool
		want   string
	}{
		{name: "health", method: http.MethodGet, path: "/v1/health", want: "{\"api_version\":\"v1\",\"agent_version\":\"0.1.0\",\"ready\":true,\"mister_process\":true,\"command_pipe\":true}\n"},
		{name: "status", method: http.MethodGet, path: "/v1/status", auth: true, want: "{\"state\":\"active\",\"game_id\":\"snes-test\",\"system\":\"snes\",\"expected_core\":\"SNES\",\"observed_core\":\"SNES\",\"last_error\":null}\n"},
		{name: "launch", method: http.MethodPost, path: "/v1/launch", auth: true, body: `{"game_id":"snes-test","system":"snes","rom_path":"/media/fat/games/SNES/test.sfc"}`, want: "{\"state\":\"active\",\"game_id\":\"snes-test\",\"system\":\"snes\",\"expected_core\":\"SNES\",\"observed_core\":\"SNES\",\"last_error\":null}\n"},
		{name: "stop", method: http.MethodPost, path: "/v1/stop", auth: true, want: "{\"state\":\"idle\",\"game_id\":null,\"system\":null,\"expected_core\":null,\"observed_core\":null,\"last_error\":null}\n"},
		{name: "unauthorized", method: http.MethodGet, path: "/v1/status", want: "{\"error\":{\"code\":\"UNAUTHORIZED\",\"message\":\"missing or incorrect bearer token\"}}\n"},
	}
	for _, withContent := range []bool{false, true} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("content=%t/%s", withContent, tt.name), func(t *testing.T) {
				controller := &fakeController{
					health: protocol.Health{APIVersion: "v1", AgentVersion: "0.1.0", Ready: true, MiSTerProcess: true, CommandPipe: true},
					status: active,
				}
				var handler http.Handler
				if withContent {
					handler = httpapi.New(controller, "test-token", "0.1.0", slog.New(slog.NewJSONHandler(io.Discard, nil)), httpapi.WithContent(&fakeContentController{}))
				} else {
					handler = httpapi.New(controller, "test-token", "0.1.0", slog.New(slog.NewJSONHandler(io.Discard, nil)))
				}
				request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
				if tt.auth {
					request.Header.Set("Authorization", "Bearer test-token")
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Body.String() != tt.want {
					t.Fatalf("body = %q, want %q", response.Body.String(), tt.want)
				}
			})
		}
	}
}

func TestTokenAndBodyNeverAppearInLogs(t *testing.T) {
	var logs bytes.Buffer
	handler := httpapi.New(&fakeController{status: protocol.Status{State: protocol.StateActive}}, "test-token", "0.1.0", slog.New(slog.NewJSONHandler(&logs, nil)))
	request := httptest.NewRequest(http.MethodPost, "/v1/launch", strings.NewReader(`{"game_id":"megadrive-test","system":"megadrive","rom_path":"/media/fat/games/MegaDrive/private.md"}`))
	request.Header.Set("Authorization", "Bearer test-token")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if strings.Contains(logs.String(), "test-token") || strings.Contains(logs.String(), "/media/fat/games") {
		t.Fatalf("secret request data leaked in logs: %s", logs.String())
	}
	for _, field := range []string{`"game_id":"megadrive-test"`, `"system":"megadrive"`, `"state":"active"`} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("request log %s does not contain %s", logs.String(), field)
		}
	}
}
