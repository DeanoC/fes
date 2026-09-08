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

	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/protocol"
)

type fakeController struct {
	health        protocol.Health
	status        protocol.Status
	launchErr     *protocol.APIError
	stopErr       *protocol.APIError
	launchRequest protocol.LaunchRequest
	healthVersion string
	statusCalls   int
	launchCalls   int
	stopCalls     int
}

type fakeDevelopmentController struct {
	status      protocol.Status
	size        int64
	body        []byte
	calls       int
	rebootCalls int
	coreCalls   int
	coreErr     *protocol.APIError
}

func (f *fakeDevelopmentController) LoadCore(_ context.Context, size int64, content io.Reader) (protocol.Status, *protocol.APIError) {
	body, err := io.ReadAll(content)
	if err != nil {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "test reader failed"}
	}
	f.coreCalls++
	f.size = size
	f.body = append([]byte(nil), body...)
	return f.status, f.coreErr
}

func (f *fakeDevelopmentController) RebootDevelopment(context.Context) (protocol.Status, *protocol.APIError) {
	f.rebootCalls++
	return protocol.Status{State: protocol.StateIdle}, nil
}

func (f *fakeDevelopmentController) LoadDevelopmentRBF(_ context.Context, size int64, content io.Reader) (protocol.Status, *protocol.APIError) {
	body, err := io.ReadAll(content)
	if err != nil {
		return protocol.Status{}, &protocol.APIError{Code: protocol.CodeInternal, Message: "test reader failed"}
	}
	f.calls++
	f.size = size
	f.body = append([]byte(nil), body...)
	return f.status, nil
}

func (f *fakeController) Health(version string) protocol.Health {
	f.healthVersion = version
	return f.health
}

func (f *fakeController) Status() protocol.Status {
	f.statusCalls++
	return f.status
}

func (f *fakeController) Launch(_ context.Context, request protocol.LaunchRequest) (protocol.Status, *protocol.APIError) {
	f.launchCalls++
	f.launchRequest = request
	return f.status, f.launchErr
}

func (f *fakeController) Stop(context.Context) (protocol.Status, *protocol.APIError) {
	f.stopCalls++
	return protocol.Status{State: protocol.StateIdle}, f.stopErr
}

func TestDevelopmentRBFUploadStreamsToController(t *testing.T) {
	t.Parallel()
	payload := []byte("development-rbf")
	observed := "DEVCORE"
	development := &fakeDevelopmentController{status: protocol.Status{State: protocol.StateActive, Development: true, ObservedCore: &observed}}
	handler := httpapi.New(&fakeController{}, "test-token", "0.1.0", discardLogger(), httpapi.WithDevelopment(development))
	request := httptest.NewRequest(http.MethodPost, "/v1/development/rbf", bytes.NewReader(payload))
	request.ContentLength = int64(len(payload))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if development.calls != 1 || development.size != int64(len(payload)) || !bytes.Equal(development.body, payload) {
		t.Fatalf("development call = count %d size %d body %q", development.calls, development.size, development.body)
	}
	var status protocol.Status
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateActive || !status.Development || status.GameID != nil || status.System != nil || status.ExpectedCore != nil || status.ObservedCore == nil || *status.ObservedCore != "DEVCORE" || status.LastError != nil || status.Recovery != "" {
		t.Fatalf("response = %#v", status)
	}
}

func TestDevelopmentRBFUploadRejectsInvalidStreamMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		contentLength    int64
		contentType      string
		transferEncoding []string
	}{
		{name: "empty", contentLength: 0, contentType: "application/octet-stream"},
		{name: "unknown length", contentLength: -1, contentType: "application/octet-stream"},
		{name: "too large", contentLength: (32 << 20) + 1, contentType: "application/octet-stream"},
		{name: "missing media type", contentLength: 3},
		{name: "wrong media type", contentLength: 3, contentType: "application/json"},
		{name: "transfer encoded", contentLength: -1, contentType: "application/octet-stream", transferEncoding: []string{"chunked"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			development := &fakeDevelopmentController{}
			handler := httpapi.New(&fakeController{}, "test-token", "0.1.0", discardLogger(), httpapi.WithDevelopment(development))
			body := &observedReader{data: []byte("rbf"), err: io.EOF}
			request := httptest.NewRequest(http.MethodPost, "/v1/development/rbf", body)
			request.ContentLength = test.contentLength
			request.TransferEncoding = test.transferEncoding
			request.Header.Set("Authorization", "Bearer test-token")
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			assertAPIError(t, response, http.StatusBadRequest, protocol.CodeBadRequest)
			if development.calls != 0 || body.reads != 0 {
				t.Fatalf("invalid upload reached controller: calls=%d reads=%d", development.calls, body.reads)
			}
		})
	}
}

func TestDevelopmentCoreUploadStreamsToControllerAndPreservesStructuredError(t *testing.T) {
	payload := []byte("canonical-fcore")
	development := &fakeDevelopmentController{status: protocol.Status{State: protocol.StateActive, Development: true,
		CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 7}},
		coreErr: &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "unsupported ABI", Phase: "compatibility", Expected: "fes.simple-game@1.0", Observed: "vendor.other@1.0"}}
	handler := httpapi.New(&fakeController{}, "test-token", "0.1.0", discardLogger(), httpapi.WithDevelopment(development))
	request := httptest.NewRequest(http.MethodPost, "/v1/development/core", bytes.NewReader(payload))
	request.ContentLength = int64(len(payload))
	request.Header.Set("Authorization", "Bearer test-token")
	request.Header.Set("Content-Type", "application/octet-stream")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if development.coreCalls != 1 || development.size != int64(len(payload)) || !bytes.Equal(development.body, payload) {
		t.Fatalf("core call = count %d size %d body %q", development.coreCalls, development.size, development.body)
	}
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"phase":"compatibility"`) ||
		!strings.Contains(response.Body.String(), `"expected":"fes.simple-game@1.0"`) {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
}

func TestDevelopmentCoreUploadRejectsInvalidStreamMetadataBeforeRead(t *testing.T) {
	for _, size := range []int64{-1, 0, corepackage.MaxArchiveSize + 1} {
		development := &fakeDevelopmentController{}
		handler := httpapi.New(&fakeController{}, "test-token", "0.1.0", discardLogger(), httpapi.WithDevelopment(development))
		body := &observedReader{data: []byte("fcore"), err: io.EOF}
		request := httptest.NewRequest(http.MethodPost, "/v1/development/core", body)
		request.ContentLength = size
		request.Header.Set("Authorization", "Bearer test-token")
		request.Header.Set("Content-Type", "application/octet-stream")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assertAPIError(t, response, http.StatusBadRequest, protocol.CodeBadRequest)
		if development.coreCalls != 0 || body.reads != 0 {
			t.Fatalf("size %d reached controller: calls=%d reads=%d", size, development.coreCalls, body.reads)
		}
	}
}

func TestDevelopmentRebootUsesAuthenticatedController(t *testing.T) {
	t.Parallel()
	development := &fakeDevelopmentController{}
	handler := httpapi.New(&fakeController{}, "test-token", "0.1.0", discardLogger(), httpapi.WithDevelopment(development))
	request := httptest.NewRequest(http.MethodPost, "/v1/development/reboot", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || development.rebootCalls != 1 {
		t.Fatalf("development reboot = %d %s calls=%d", response.Code, response.Body.String(), development.rebootCalls)
	}
	var status protocol.Status
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.StateIdle {
		t.Fatalf("development reboot response = %#v", status)
	}
}

func TestAuthenticationRequiresExactlyOneAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		media  string
	}{
		{name: "v1 status", method: http.MethodGet, path: "/v1/status"},
		{name: "v1 launch", method: http.MethodPost, path: "/v1/launch", body: `{"game_id":"snes-test","system":"snes","rom_path":"/media/fat/games/SNES/test.sfc"}`},
		{name: "v1 stop", method: http.MethodPost, path: "/v1/stop"},
		{name: "v2 probe", method: http.MethodGet, path: "/v2/cache/snes/" + v2Digest + "?extension=sfc"},
		{name: "v2 upload", method: http.MethodPut, path: "/v2/cache/snes/" + v2Digest + "?extension=sfc", body: "rom", media: "application/octet-stream"},
		{name: "v2 launch", method: http.MethodPost, path: "/v2/launch", body: validLaunchJSON(), media: "application/json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1 := &fakeController{}
			v2 := &fakeContentController{}
			handler := httpapi.New(v1, "test-token", "0.1.0", discardLogger(), httpapi.WithContent(v2))
			body := &observedReader{data: []byte(tt.body), err: io.EOF}
			request := httptest.NewRequest(tt.method, tt.path, body)
			request.ContentLength = int64(len(tt.body))
			if tt.media != "" {
				request.Header.Set("Content-Type", tt.media)
			}
			request.Header.Add("Authorization", "Bearer test-token")
			request.Header.Add("Authorization", "Bearer wrong")

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			assertAPIError(t, response, http.StatusUnauthorized, protocol.CodeUnauthorized)
			if body.reads != 0 {
				t.Fatalf("body reads = %d, want 0", body.reads)
			}
			if v1.statusCalls != 0 || v1.launchCalls != 0 || v1.stopCalls != 0 {
				t.Fatalf("duplicate authorization reached v1 controller: status=%d launch=%d stop=%d", v1.statusCalls, v1.launchCalls, v1.stopCalls)
			}
			if v2.probeCalls != 0 || v2.putCalls != 0 || v2.launchCalls != 0 {
				t.Fatalf("duplicate authorization reached v2 controller: probe=%d put=%d launch=%d", v2.probeCalls, v2.putCalls, v2.launchCalls)
			}
		})
	}
}

func TestAuthenticationRejectsMalformedBearerTokenBeforeComparison(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "empty"},
		{name: "padding only", token: "="},
		{name: "embedded space", token: "test token"},
		{name: "combined values", token: "test-token, Bearer second"},
		{name: "data after padding", token: "test=token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := httpapi.New(&fakeController{}, tt.token, "0.1.0", discardLogger())
			request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
			request.Header.Set("Authorization", "Bearer "+tt.token)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			assertAPIError(t, response, http.StatusUnauthorized, protocol.CodeUnauthorized)
		})
	}
}

func TestAuthenticationAcceptsValidPaddedBearerToken(t *testing.T) {
	controller := &fakeController{}
	handler := httpapi.New(controller, "test-token==", "0.1.0", discardLogger())
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	request.Header.Set("Authorization", "Bearer test-token==")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || controller.statusCalls != 1 {
		t.Fatalf("valid token68 rejected: status=%d controller calls=%d", response.Code, controller.statusCalls)
	}
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
		protocol.CodeBadRequest:           http.StatusBadRequest,
		protocol.CodeUnauthorized:         http.StatusUnauthorized,
		protocol.CodeROMNotFound:          http.StatusNotFound,
		protocol.CodeBusy:                 http.StatusConflict,
		protocol.CodeUnsupportedSystem:    http.StatusUnprocessableEntity,
		protocol.CodeUnsupportedOperation: http.StatusUnprocessableEntity,
		protocol.CodeInvalidROMPath:       http.StatusUnprocessableEntity,
		protocol.CodeMiSTerUnavailable:    http.StatusServiceUnavailable,
		protocol.CodeCoreTimeout:          http.StatusServiceUnavailable,
		protocol.CodeContentNotCached:     http.StatusNotFound,
		protocol.CodeSourceUnavailable:    http.StatusUnprocessableEntity,
		protocol.CodeInvalidArchive:       http.StatusUnprocessableEntity,
		protocol.CodeDigestMismatch:       http.StatusUnprocessableEntity,
		protocol.CodeTransferFailed:       http.StatusBadRequest,
		protocol.CodeCacheFull:            http.StatusInsufficientStorage,
		protocol.CodeInternal:             http.StatusInternalServerError,
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

func TestHealthRevealsTargetIdentityOnlyToAuthenticatedCaller(t *testing.T) {
	handler := httpapi.New(&fakeController{health: protocol.Health{Ready: true}}, "test-token", "0.1.0", discardLogger(), httpapi.WithTargetID("01234567-89ab-cdef-0123-456789abcdef"))
	for _, tc := range []struct {
		name, auth string
		wantID     bool
	}{{"anonymous", "", false}, {"authenticated", "Bearer test-token", true}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
			req.Header.Set("Authorization", tc.auth)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			var health protocol.Health
			if err := json.Unmarshal(res.Body.Bytes(), &health); err != nil {
				t.Fatal(err)
			}
			if (health.TargetID != "") == tc.wantID {
				return
			}
			t.Fatalf("target ID = %q, want exposed=%t", health.TargetID, tc.wantID)
		})
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
