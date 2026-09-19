package targetclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
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
	status, err := targetclient.NewClient(baseURL, "test-token", server.Client()).Status(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func TestClientHealthAuthenticatesIdentity(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("path = %q, authorization = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"api_version":"v1","agent_version":"0.1.0","ready":true,"mister_process":true,"command_pipe":true}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	health, err := targetclient.NewClient(baseURL, "test-token", server.Client()).Health(context.Background())
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
	status, err := targetclient.NewClient(baseURL, "test-token", server.Client()).Launch(context.Background(), want)
	if err != nil || status.State != protocol.StateActive {
		t.Fatalf("status = %#v, %v", status, err)
	}
}

func TestClientDevelopmentRBFStreamsAuthenticatedBody(t *testing.T) {
	t.Parallel()
	payload := []byte("development-rbf")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/development/rbf" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("authorization=%q content-type=%q", r.Header.Get("Authorization"), r.Header.Get("Content-Type"))
		}
		if r.ContentLength != int64(len(payload)) {
			t.Errorf("content length = %d", r.ContentLength)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if !bytes.Equal(body, payload) {
			t.Errorf("body = %q", body)
		}
		_, _ = io.WriteString(w, `{"state":"active","game_id":null,"system":null,"expected_core":null,"observed_core":"DEVCORE","last_error":null,"development":true}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)

	status, err := targetclient.NewClient(baseURL, "test-token", server.Client()).LoadDevelopmentRBF(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil || status.State != protocol.StateActive || !status.Development || status.ObservedCore == nil || *status.ObservedCore != "DEVCORE" {
		t.Fatalf("status = %#v, err = %v", status, err)
	}
}

func TestClientDevelopmentRBFRejectsInvalidInputBeforeRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		size    int64
		content io.Reader
	}{
		{name: "empty", size: 0, content: strings.NewReader("")},
		{name: "too large", size: (32 << 20) + 1, content: strings.NewReader("rbf")},
		{name: "missing reader", size: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &countingResponseTransport{}
			_, err := contentClientWithTransport(transport).LoadDevelopmentRBF(context.Background(), test.size, test.content)
			if err == nil {
				t.Fatal("invalid development RBF input accepted")
			}
			if transport.calls != 0 {
				t.Fatalf("transport calls = %d", transport.calls)
			}
		})
	}
}

func TestClientDevelopmentCoreStreamsLeasedPackageAndRequiresCustomStatus(t *testing.T) {
	payload := []byte("fcore")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/development/core" || r.ContentLength != int64(len(payload)) ||
			r.Header.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("request=%s %s length=%d type=%q", r.Method, r.URL.Path, r.ContentLength, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Equal(body, payload) {
			t.Errorf("body=%q", body)
		}
		_, _ = io.WriteString(w, `{"state":"active","game_id":null,"system":null,"expected_core":null,"observed_core":"fes.pong","last_error":null,"development":true,"core_package":{"package_id":"`+strings.Repeat("a", 64)+`","generation":7,"abi":{"id":"fes.simple-game","major":1,"minor":0},"build_id":"0123456789abcdef0123456789abcdef","active_interfaces":[{"id":"fes.gamepad","major":1,"minor":0}],"gamepad":true}}`)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)
	status, err := targetclient.NewClient(baseURL, "test-token", server.Client()).LoadCore(context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil || status.CorePackage == nil || status.CorePackage.Generation != 7 || !status.CorePackage.Gamepad {
		t.Fatalf("status=%#v error=%v", status, err)
	}
}

func TestClientPortsStatusSurvivesTargetHTTPValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		minor   int
		gamepad bool
		valid   bool
	}{
		{"negotiated ports", 0, true, true},
		{"ports falsely missing gamepad", 0, false, false},
		{"unknown minor is not gamepad", 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateActive, Development: true, CorePackage: &protocol.CorePackageStatus{PackageID: strings.Repeat("a", 64), Generation: 7, ABI: protocol.RuntimeContract{ID: "fes.application", Major: 1}, BuildID: strings.Repeat("b", 32), ActiveInterfaces: []protocol.RuntimeInterface{{ID: "fes.gamepad.ports", Major: 1, Minor: uint16(tc.minor)}, {ID: "fes.keypad.ports", Major: 1}, {ID: "fes.video.fixed-720p60", Major: 1}}, Gamepad: tc.gamepad}})
			}))
			defer server.Close()
			baseURL, _ := url.Parse(server.URL)
			client := targetclient.NewClient(baseURL, "test", server.Client())
			_, err := client.LoadCore(context.Background(), 5, strings.NewReader("fcore"))
			if (err == nil) != tc.valid {
				t.Fatalf("LoadCore error=%v valid=%v", err, tc.valid)
			}
			if tc.valid {
				status, statusErr := client.Status(context.Background())
				if statusErr != nil || status.CorePackage == nil || !status.CorePackage.Gamepad {
					t.Fatalf("Status=%+v error=%v", status, statusErr)
				}
			}
		})
	}
}

func TestClientDevelopmentCoreInspectionStreamsWithoutKitLease(t *testing.T) {
	payload := []byte("fcore")
	want := protocol.CoreInspection{PackageID: strings.Repeat("a", 64),
		Descriptor: validCoreInspectionDescriptor(), Compatible: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/development/core/inspect" ||
			r.ContentLength != int64(len(payload)) || r.Header.Get("Content-Type") != "application/octet-stream" ||
			r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-FogCast-Kit-Lease") != "" {
			t.Errorf("request=%s %s length=%d type=%q auth=%q lease=%q", r.Method, r.URL.Path,
				r.ContentLength, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), r.Header.Get("X-FogCast-Kit-Lease"))
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Equal(body, payload) {
			t.Errorf("body=%q", body)
		}
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)

	got, err := targetclient.NewClient(baseURL, "test-token", server.Client()).InspectCore(
		context.Background(), int64(len(payload)), bytes.NewReader(payload))
	if err != nil || got.PackageID != want.PackageID || !got.Compatible || got.CompatibilityError != nil ||
		got.Descriptor.Core.ID != want.Descriptor.Core.ID {
		t.Fatalf("inspection=%#v error=%v", got, err)
	}
}

func TestClientDevelopmentCoreInspectionAcceptsStructuredIncompatibility(t *testing.T) {
	want := protocol.CoreInspection{PackageID: strings.Repeat("a", 64),
		Descriptor: validCoreInspectionDescriptor(), Compatible: false,
		CompatibilityError: &protocol.APIError{Code: protocol.CodeUnsupportedOperation,
			Message: "required interface unavailable", Phase: "compatibility",
			Expected: "fes.gamepad@1.0", Observed: "none"}}
	transport := &staticResponseTransport{body: string(mustJSON(t, want))}
	got, err := contentClientWithTransport(transport).InspectCore(context.Background(), 3, strings.NewReader("pkg"))
	if err != nil || got.Compatible || got.CompatibilityError == nil ||
		got.CompatibilityError.Expected != "fes.gamepad@1.0" || got.CompatibilityError.Observed != "none" {
		t.Fatalf("inspection=%#v error=%v", got, err)
	}
}

func TestClientDevelopmentCoreInspectionRejectsInvalidInputAndResponse(t *testing.T) {
	for _, test := range []struct {
		name        string
		size        int64
		reader      io.Reader
		response    protocol.CoreInspection
		wantRequest bool
	}{
		{name: "empty input", size: 0, reader: strings.NewReader(""), wantRequest: false},
		{name: "missing reader", size: 3, wantRequest: false},
		{name: "invalid package id", size: 3, reader: strings.NewReader("pkg"), response: protocol.CoreInspection{PackageID: "bad", Descriptor: validCoreInspectionDescriptor(), Compatible: true}, wantRequest: true},
		{name: "invalid descriptor", size: 3, reader: strings.NewReader("pkg"), response: protocol.CoreInspection{PackageID: strings.Repeat("a", 64), Compatible: true}, wantRequest: true},
		{name: "compatible with error", size: 3, reader: strings.NewReader("pkg"), response: protocol.CoreInspection{PackageID: strings.Repeat("a", 64), Descriptor: validCoreInspectionDescriptor(), Compatible: true, CompatibilityError: &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "bad", Phase: "compatibility"}}, wantRequest: true},
		{name: "incompatible without error", size: 3, reader: strings.NewReader("pkg"), response: protocol.CoreInspection{PackageID: strings.Repeat("a", 64), Descriptor: validCoreInspectionDescriptor()}, wantRequest: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var client *targetclient.Client
			counter := &countingResponseTransport{}
			if test.wantRequest {
				client = contentClientWithTransport(&staticResponseTransport{body: string(mustJSON(t, test.response))})
			} else {
				client = contentClientWithTransport(counter)
			}
			_, err := client.InspectCore(context.Background(), test.size, test.reader)
			if err == nil {
				t.Fatal("invalid inspection accepted")
			}
			if !test.wantRequest && counter.calls != 0 {
				t.Fatalf("transport calls=%d", counter.calls)
			}
		})
	}
}

func TestClientDevelopmentCoreInspectionTransportFailureIsNotAMutation(t *testing.T) {
	_, err := contentClientWithTransport(&developmentDeadlineTransport{}).InspectCore(
		context.Background(), 3, strings.NewReader("pkg"))
	var apiErr *protocol.APIError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeTransferFailed {
		t.Fatalf("lost inspection response error=%v", err)
	}
	if ambiguous, ok := err.(interface{ AmbiguousMutation() bool }); ok && ambiguous.AmbiguousMutation() {
		t.Fatalf("read-only inspection was classified as an ambiguous mutation: %T", err)
	}
}

func validCoreInspectionDescriptor() corepackage.Descriptor {
	return corepackage.Descriptor{
		Format:     2,
		Core:       corepackage.Core{ID: "fes.pong", Name: "FES Pong", Description: "test", Version: "1.0.0"},
		Target:     corepackage.Target{Platform: "de10_nano", Device: "5CSEBA6U23I7", ProgrammingProfile: "fes-gp-v1"},
		Payload:    corepackage.Payload{File: "core.rbf", Size: 3, SHA256: strings.Repeat("b", 64)},
		ABI:        corepackage.Contract{ID: "fes.simple-game", Major: 1},
		Interfaces: []corepackage.Interface{{ID: "fes.gamepad", Major: 1, Required: true}},
		Build: corepackage.Build{ID: strings.Repeat("c", 32), Repository: "https://example.invalid/fes-pong",
			Revision: strings.Repeat("d", 40), RecipeSHA256: strings.Repeat("e", 64), Toolchain: "test toolchain"},
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestClientDevelopmentRBFRejectsMismatchedStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "idle", body: `{"state":"idle","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null,"development":true}`},
		{name: "ordinary active", body: `{"state":"active","game_id":null,"system":null,"expected_core":null,"observed_core":"DEVCORE","last_error":null}`},
		{name: "game identity", body: `{"state":"active","game_id":"snes-game","system":"snes","expected_core":null,"observed_core":"DEVCORE","last_error":null,"development":true}`},
		{name: "embedded error", body: `{"state":"active","game_id":null,"system":null,"expected_core":null,"observed_core":"DEVCORE","last_error":{"code":"INTERNAL","message":"failed"},"development":true}`},
		{name: "premature recovery", body: `{"state":"active","game_id":null,"system":null,"expected_core":null,"observed_core":null,"last_error":null,"development":true,"recovery":"reboot_required"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := contentClientWithTransport(&staticResponseTransport{body: test.body})
			if _, err := client.LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf")); err == nil {
				t.Fatal("mismatched development status accepted")
			}
		})
	}
}

func TestClientDevelopmentRBFLostResponsePreservesAmbiguousTransportCause(t *testing.T) {
	transport := &developmentDeadlineTransport{}
	_, err := contentClientWithTransport(transport).LoadDevelopmentRBF(context.Background(), 3, strings.NewReader("rbf"))
	var apiErr *protocol.APIError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &apiErr) || apiErr.Code != protocol.CodeTransferFailed {
		t.Fatalf("lost response error = %v", err)
	}
}

type developmentDeadlineTransport struct{}

func (*developmentDeadlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.DeadlineExceeded
}

func TestClientDevelopmentRebootUsesAuthenticatedEmptyPost(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/development/reboot" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("development reboot request = %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 {
			t.Fatalf("development reboot body = %q err=%v", body, err)
		}
		_ = json.NewEncoder(w).Encode(protocol.Status{State: protocol.StateIdle})
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL)

	status, err := targetclient.NewClient(baseURL, "test-token", server.Client()).RebootDevelopment(context.Background())
	if err != nil || status.State != protocol.StateIdle {
		t.Fatalf("development reboot = %#v, %v", status, err)
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
	_, err := targetclient.NewClient(baseURL, "test-token", server.Client()).Launch(context.Background(), protocol.LaunchRequest{GameID: "snes-test", System: protocol.SystemSNES, ROMPath: "/media/fat/games/SNES/test.sfc"})
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
	if _, err := targetclient.NewClient(baseURL, "test-token", server.Client()).Status(context.Background()); err == nil {
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
	if _, err := targetclient.NewClient(baseURL, "test-token", server.Client()).Status(context.Background()); err == nil {
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
	if _, err := targetclient.NewClient(baseURL, "test-token", server.Client()).Stop(context.Background()); err != nil {
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
	client := targetclient.NewClient(baseURL, "test-token", server.Client())
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
	status, err := targetclient.NewClient(baseURL, "test-token", server.Client()).CastStartWithMedia(context.Background(), "session", "bridge-token", 9, protocol.CastMediaSet{Version: protocol.CastMediaSetVersion, Video: true, Audio: true})
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
	_, err := targetclient.NewClient(baseURL, "test-token", server.Client()).CastStartWithMedia(context.Background(), "session", "bridge-token", 9, protocol.CastMediaSet{Version: protocol.CastMediaSetVersion, Video: true, Audio: true})
	if err == nil || stops != 1 {
		t.Fatalf("err=%v stops=%d", err, stops)
	}
}
