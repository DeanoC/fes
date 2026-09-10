package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/protocol"
)

const v2Digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeContentController struct {
	probeResponse  protocol.CacheProbeResponse
	probeErr       *protocol.APIError
	putResponse    protocol.CacheUploadResponse
	putErr         *protocol.APIError
	launchResponse protocol.CachedLaunchResponse
	launchErr      *protocol.APIError
	lookupResponse protocol.CachedIdentityResponse
	lookupErr      *protocol.APIError

	probeCalls  int
	putCalls    int
	launchCalls int
	lookupCalls int
	probeSystem protocol.System
	probeKey    protocol.ContentKey
	putSystem   protocol.System
	putContent  protocol.ContentIdentity
	launch      protocol.CachedLaunchRequest
	lastLookup  string
	put         func(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError)
}

func (f *fakeContentController) ProbeContent(_ context.Context, system protocol.System, key protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError) {
	f.probeCalls++
	f.probeSystem = system
	f.probeKey = key
	return f.probeResponse, f.probeErr
}

func (f *fakeContentController) PutContent(ctx context.Context, system protocol.System, content protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
	f.putCalls++
	f.putSystem = system
	f.putContent = content
	if f.put != nil {
		return f.put(ctx, system, content, body)
	}
	return f.putResponse, f.putErr
}

func (f *fakeContentController) LaunchContent(_ context.Context, request protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, *protocol.APIError) {
	f.launchCalls++
	f.launch = request
	return f.launchResponse, f.launchErr
}

func (f *fakeContentController) LookupCachedIdentity(_ context.Context, gameID string) (protocol.CachedIdentityResponse, *protocol.APIError) {
	f.lookupCalls++
	f.lastLookup = gameID
	return f.lookupResponse, f.lookupErr
}

func TestV2RoutesAreOptionalAndMethodsAreExact(t *testing.T) {
	content := &fakeContentController{}
	withoutContent := httpapi.New(&fakeController{}, "test-token", "0.1.0", discardLogger())
	for _, request := range []*http.Request{
		newV2Request(http.MethodGet, "/v2/cache/snes/"+v2Digest+"?extension=sfc", nil, 0, ""),
		newV2Request(http.MethodPut, "/v2/cache/snes/"+v2Digest+"?extension=sfc", strings.NewReader("rom"), 3, "application/octet-stream"),
		newV2Request(http.MethodPost, "/v2/launch", strings.NewReader(validLaunchJSON()), int64(len(validLaunchJSON())), "application/json"),
	} {
		response := httptest.NewRecorder()
		withoutContent.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s status = %d, want 404", request.Method, request.URL.Path, response.Code)
		}
	}
	if content.probeCalls != 0 || content.putCalls != 0 || content.launchCalls != 0 {
		t.Fatalf("content calls without option = probe %d, put %d, launch %d", content.probeCalls, content.putCalls, content.launchCalls)
	}

	withContent := newContentHandler(content, discardLogger())
	tests := []struct {
		method    string
		path      string
		wantAllow string
	}{
		{method: http.MethodHead, path: "/v2/cache/snes/" + v2Digest + "?extension=sfc", wantAllow: "GET, PUT"},
		{method: http.MethodPost, path: "/v2/cache/snes/" + v2Digest + "?extension=sfc", wantAllow: "GET, PUT"},
		{method: http.MethodDelete, path: "/v2/cache/snes/" + v2Digest + "?extension=sfc", wantAllow: "GET, PUT"},
		{method: http.MethodOptions, path: "/v2/cache/snes/" + v2Digest + "?extension=sfc", wantAllow: "GET, PUT"},
		{method: http.MethodGet, path: "/v2/launch", wantAllow: "POST"},
		{method: http.MethodPut, path: "/v2/launch", wantAllow: "POST"},
		{method: http.MethodOptions, path: "/v2/launch", wantAllow: "POST"},
	}
	for _, tt := range tests {
		response := httptest.NewRecorder()
		withContent.ServeHTTP(response, newV2Request(tt.method, tt.path, nil, 0, ""))
		if response.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s status = %d, want 405", tt.method, tt.path, response.Code)
		}
		if allow := response.Header().Get("Allow"); allow != tt.wantAllow {
			t.Errorf("%s %s Allow = %q, want %q", tt.method, tt.path, allow, tt.wantAllow)
		}
	}
	for _, path := range []string{
		"/v2/cache/snes/" + v2Digest + "/extra?extension=sfc",
		"/v2/cache/snes?extension=sfc",
		"/v2/launch/",
		"/v2/status",
		"/v2/hostless",
		"/v2/hostless/identity",
		"/v2/hostless/identity/snes-test/extra",
	} {
		response := httptest.NewRecorder()
		withContent.ServeHTTP(response, newV2Request(http.MethodGet, path, nil, 0, ""))
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, response.Code)
		}
	}
	if content.probeCalls != 0 || content.putCalls != 0 || content.launchCalls != 0 || content.lookupCalls != 0 {
		t.Fatalf("content calls for wrong routes = probe %d, put %d, launch %d lookup %d", content.probeCalls, content.putCalls, content.launchCalls, content.lookupCalls)
	}
}

func TestHostlessIdentityLookupIsLeaseFree(t *testing.T) {
	system := protocol.SystemMegaDrive
	content := protocol.ContentIdentity{SHA256: v2Digest, Size: 4, Extension: "md"}
	controller := &fakeContentController{lookupResponse: protocol.CachedIdentityResponse{Present: true, GameID: "megadrive-sonic", System: &system, Content: &content}}
	response := serveContent(newContentHandler(controller, discardLogger()), newV2Request(http.MethodGet, "/v2/hostless/identity/megadrive-sonic", nil, 0, ""))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", response.Code, response.Body.String())
	}
	if controller.lookupCalls != 1 || controller.lastLookup != "megadrive-sonic" {
		t.Fatalf("lookup calls=%d id=%q", controller.lookupCalls, controller.lastLookup)
	}
	absent := &fakeContentController{}
	missing := serveContent(newContentHandler(absent, discardLogger()), newV2Request(http.MethodGet, "/v2/hostless/identity/megadrive-sonic", nil, 0, ""))
	if missing.Code != http.StatusOK || missing.Body.String() != "{\"present\":false}\n" {
		t.Fatalf("absent = status %d body %q", missing.Code, missing.Body.String())
	}
}

func TestV2NoncanonicalPathsAreExact404BeforeAuthentication(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		rawPath string
	}{
		{name: "repeated launch slash", method: http.MethodPost, path: "/v2//launch"},
		{name: "launch dot segment", method: http.MethodPost, path: "/v2/./launch"},
		{name: "launch parent segment", method: http.MethodPost, path: "/v2/cache/../launch"},
		{name: "launch trailing dot", method: http.MethodPost, path: "/v2/launch/."},
		{name: "repeated cache slash", method: http.MethodGet, path: "/v2/cache//snes/" + v2Digest + "?extension=sfc"},
		{name: "cache trailing dot", method: http.MethodGet, path: "/v2/cache/snes/" + v2Digest + "/.?extension=sfc"},
		{name: "non-v2 traversal into launch", method: http.MethodPost, path: "/not-v2/../v2/launch"},
		{name: "encoded v2 prefix with dot", method: http.MethodPost, path: "/%76%32/./launch"},
		{name: "encoded v2 prefix", method: http.MethodPost, path: "/%76%32/launch"},
		{name: "encoded launch literal", method: http.MethodPost, path: "/v2/la%75nch"},
		{name: "encoded cache literal", method: http.MethodGet, path: "/v2/%63ache/snes/" + v2Digest + "?extension=sfc"},
		{name: "encoded dot system", method: http.MethodGet, path: "/v2/cache/%2e/" + v2Digest + "?extension=sfc"},
		{name: "encoded parent system", method: http.MethodGet, path: "/v2/cache/%2e%2e/" + v2Digest + "?extension=sfc"},
		{name: "encoded slash system", method: http.MethodGet, path: "/v2/cache/snes%2fextra/" + v2Digest + "?extension=sfc"},
		{name: "encoded backslash system", method: http.MethodGet, path: "/v2/cache/snes%5cextra/" + v2Digest + "?extension=sfc"},
		{name: "encoded slash digest", method: http.MethodGet, path: "/v2/cache/snes/abc%2fdef?extension=sfc"},
		{name: "literal backslash system", method: http.MethodGet, path: "/v2/cache/snes\\extra/" + v2Digest + "?extension=sfc"},
		{name: "literal backslash digest", method: http.MethodGet, path: "/v2/cache/snes/abc\\def?extension=sfc"},
		{name: "malformed escape", method: http.MethodPost, path: "/v2/launch", rawPath: "/v2/%ZZ/launch"},
		{name: "inconsistent escaped path", method: http.MethodPost, path: "/v2/launch", rawPath: "/v2/%63ache"},
		{name: "ignored raw path hint", method: http.MethodGet, path: "/v2/cache/snes%20extra/" + v2Digest + "?extension=sfc", rawPath: "/v2/cache/snes extra/" + v2Digest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &observedReader{data: []byte(validLaunchJSON()), err: io.EOF}
			content := &fakeContentController{}
			request := httptest.NewRequest(tt.method, tt.path, body)
			if tt.rawPath != "" {
				request.URL.RawPath = tt.rawPath
			}
			response := serveContent(newContentHandler(content, discardLogger()), request)

			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; Location=%q", response.Code, response.Header().Get("Location"))
			}
			if location := response.Header().Get("Location"); location != "" {
				t.Fatalf("noncanonical path redirected to %q", location)
			}
			if body.reads != 0 {
				t.Fatalf("body reads = %d, want 0", body.reads)
			}
			if content.probeCalls != 0 || content.putCalls != 0 || content.launchCalls != 0 {
				t.Fatalf("noncanonical path reached controller: probe=%d put=%d launch=%d", content.probeCalls, content.putCalls, content.launchCalls)
			}
		})
	}
}

func TestV2RouteShapeAllowsEncodedVariableAndQueryValues(t *testing.T) {
	content := &fakeContentController{}
	path := "/v2/cache/s%6ees/%30" + v2Digest[1:] + "?extension=%73fc"
	response := serveContent(newContentHandler(content, discardLogger()), newV2Request(http.MethodGet, path, nil, 0, ""))

	if response.Code != http.StatusOK || response.Body.String() != "{\"present\":false}\n" {
		t.Fatalf("response = status %d, body %q", response.Code, response.Body.String())
	}
	if content.probeCalls != 1 || content.probeSystem != protocol.SystemSNES || content.probeKey != (protocol.ContentKey{SHA256: v2Digest, Extension: "sfc"}) {
		t.Fatalf("probe = calls %d, system %q, key %#v", content.probeCalls, content.probeSystem, content.probeKey)
	}
}

func TestV2AuthenticationRejectsEveryEndpointBeforeBodyRead(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		length int64
		media  string
	}{
		{name: "probe", method: http.MethodGet, path: "/v2/cache/snes/" + v2Digest + "?extension=sfc"},
		{name: "upload", method: http.MethodPut, path: "/v2/cache/snes/" + v2Digest + "?extension=sfc", length: 3, media: "application/octet-stream"},
		{name: "launch", method: http.MethodPost, path: "/v2/launch", length: 3, media: "application/json"},
	}
	authorizations := []string{"", "test-token", "bearer test-token", "Bearer wrong", "Bearer test-token extra"}
	for _, tt := range tests {
		for _, authorization := range authorizations {
			name := tt.name + "/" + strings.ReplaceAll(authorization, " ", "_")
			t.Run(name, func(t *testing.T) {
				body := &observedReader{err: errors.New("body must not be read")}
				content := &fakeContentController{}
				handler := newContentHandler(content, discardLogger())
				request := httptest.NewRequest(tt.method, tt.path, body)
				request.ContentLength = tt.length
				if tt.media != "" {
					request.Header.Set("Content-Type", tt.media)
				}
				if authorization != "" {
					request.Header.Set("Authorization", authorization)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				assertAPIError(t, response, http.StatusUnauthorized, protocol.CodeUnauthorized)
				if body.reads != 0 {
					t.Fatalf("body reads = %d, want 0", body.reads)
				}
				if content.probeCalls != 0 || content.putCalls != 0 || content.launchCalls != 0 {
					t.Fatal("unauthenticated request reached content controller")
				}
			})
		}
	}
}

func TestV2ProbeValidatesPathAndQueryBeforeController(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantCode   protocol.ErrorCode
	}{
		{name: "mixed case system", path: "/v2/cache/SNES/" + v2Digest + "?extension=sfc", wantStatus: http.StatusUnprocessableEntity, wantCode: protocol.CodeUnsupportedSystem},
		{name: "unknown system", path: "/v2/cache/mystery/" + v2Digest + "?extension=sfc", wantStatus: http.StatusUnprocessableEntity, wantCode: protocol.CodeUnsupportedSystem},
		{name: "short digest", path: "/v2/cache/snes/abcd?extension=sfc", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "mixed case digest", path: "/v2/cache/snes/" + strings.ToUpper(v2Digest) + "?extension=sfc", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "missing extension", path: "/v2/cache/snes/" + v2Digest, wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "empty extension", path: "/v2/cache/snes/" + v2Digest + "?extension=", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "duplicate extension", path: "/v2/cache/snes/" + v2Digest + "?extension=sfc&extension=bin", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "unknown query", path: "/v2/cache/snes/" + v2Digest + "?extension=sfc&source=/private", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "mixed case extension", path: "/v2/cache/snes/" + v2Digest + "?extension=SFC", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "dotted extension", path: "/v2/cache/snes/" + v2Digest + "?extension=.sfc", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "slashed extension", path: "/v2/cache/snes/" + v2Digest + "?extension=../sfc", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := &fakeContentController{}
			response := serveContent(newContentHandler(content, discardLogger()), newV2Request(http.MethodGet, tt.path, nil, 0, ""))
			assertAPIError(t, response, tt.wantStatus, tt.wantCode)
			if content.probeCalls != 0 {
				t.Fatalf("probe calls = %d, want 0", content.probeCalls)
			}
		})
	}
}

func TestV2ProbeReturnsExactAbsentAndPresentShapes(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: v2Digest, Size: 3, Extension: "sfc"}
	system := protocol.SystemSNES
	tests := []struct {
		name     string
		response protocol.CacheProbeResponse
		wantBody string
	}{
		{name: "absent", response: protocol.CacheProbeResponse{Present: false}, wantBody: "{\"present\":false}\n"},
		{name: "present", response: protocol.CacheProbeResponse{Present: true, System: &system, Content: &identity}, wantBody: "{\"present\":true,\"system\":\"snes\",\"content\":{\"sha256\":\"" + v2Digest + "\",\"size\":3,\"extension\":\"sfc\"}}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := &fakeContentController{probeResponse: tt.response}
			response := serveContent(newContentHandler(content, discardLogger()), newV2Request(http.MethodGet, "/v2/cache/snes/"+v2Digest+"?extension=sfc", nil, 0, ""))
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != tt.wantBody {
				t.Fatalf("response = status %d, type %q, body %q", response.Code, response.Header().Get("Content-Type"), response.Body.String())
			}
			if content.probeCalls != 1 || content.probeSystem != protocol.SystemSNES || content.probeKey != identity.Key() {
				t.Fatalf("probe call = %d, %q, %#v", content.probeCalls, content.probeSystem, content.probeKey)
			}
		})
	}
}

func TestV2ProbeRejectsControllerIdentityMismatch(t *testing.T) {
	valid := protocol.ContentIdentity{SHA256: v2Digest, Size: 3, Extension: "sfc"}
	snes := protocol.SystemSNES
	megaDrive := protocol.SystemMegaDrive
	other := valid
	other.SHA256 = strings.Repeat("a", 64)
	tests := []struct {
		name     string
		response protocol.CacheProbeResponse
	}{
		{name: "absent with system", response: protocol.CacheProbeResponse{Present: false, System: &snes}},
		{name: "absent with content", response: protocol.CacheProbeResponse{Present: false, Content: &valid}},
		{name: "present missing system", response: protocol.CacheProbeResponse{Present: true, Content: &valid}},
		{name: "present missing content", response: protocol.CacheProbeResponse{Present: true, System: &snes}},
		{name: "wrong system", response: protocol.CacheProbeResponse{Present: true, System: &megaDrive, Content: &valid}},
		{name: "wrong content", response: protocol.CacheProbeResponse{Present: true, System: &snes, Content: &other}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := &fakeContentController{probeResponse: tt.response}
			response := serveContent(newContentHandler(content, discardLogger()), newV2Request(http.MethodGet, "/v2/cache/snes/"+v2Digest+"?extension=sfc", nil, 0, ""))
			assertAPIError(t, response, http.StatusInternalServerError, protocol.CodeInternal)
			if content.probeCalls != 1 {
				t.Fatalf("probe calls = %d, want 1", content.probeCalls)
			}
		})
	}
}

func TestV2LaunchRequiresExactMediaTypeStrictBoundedJSONAndValidIdentity(t *testing.T) {
	valid := validLaunchJSON()
	tests := []struct {
		name  string
		body  string
		media string
	}{
		{name: "missing media type", body: valid},
		{name: "wrong media type", body: valid, media: "text/plain"},
		{name: "media type parameter", body: valid, media: "application/json; charset=utf-8"},
		{name: "malformed", body: `{"game_id":`, media: "application/json"},
		{name: "multiple objects", body: valid + `{}`, media: "application/json"},
		{name: "unknown top field", body: strings.TrimSuffix(valid, "}") + `,"source":"/Volumes/private"}`, media: "application/json"},
		{name: "unknown nested field", body: strings.Replace(valid, `"extension":"sfc"`, `"extension":"sfc","path":"/media/fat/fogcast/cache/private"`, 1), media: "application/json"},
		{name: "oversized", body: strings.TrimSuffix(valid, "}") + `,"padding":"` + strings.Repeat("x", (64<<10)+1) + `"}`, media: "application/json"},
		{name: "invalid game", body: strings.Replace(valid, "snes-synthetic", "../private", 1), media: "application/json"},
		{name: "mixed system", body: strings.Replace(valid, `"system":"snes"`, `"system":"SNES"`, 1), media: "application/json"},
		{name: "mixed digest", body: strings.Replace(valid, v2Digest, strings.ToUpper(v2Digest), 1), media: "application/json"},
		{name: "mixed extension", body: strings.Replace(valid, `"extension":"sfc"`, `"extension":"SFC"`, 1), media: "application/json"},
		{name: "zero size", body: strings.Replace(valid, `"size":3`, `"size":0`, 1), media: "application/json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := &fakeContentController{}
			request := newV2Request(http.MethodPost, "/v2/launch", strings.NewReader(tt.body), int64(len(tt.body)), tt.media)
			response := serveContent(newContentHandler(content, discardLogger()), request)
			if response.Code != http.StatusBadRequest && response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			if content.launchCalls != 0 {
				t.Fatalf("launch calls = %d, want 0", content.launchCalls)
			}
		})
	}
}

func TestV2LaunchReturnsExactShapeAndRejectsControllerMismatch(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: v2Digest, Size: 3, Extension: "sfc"}
	request := protocol.CachedLaunchRequest{GameID: "snes-synthetic", System: protocol.SystemSNES, Content: identity}
	system := protocol.SystemSNES
	gameID, expected, observed := request.GameID, "SNES", "SNES"
	success := protocol.CachedLaunchResponse{
		Status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &expected, ObservedCore: &observed},
		Content: identity,
	}
	content := &fakeContentController{launchResponse: success}
	response := serveContent(newContentHandler(content, discardLogger()), newV2Request(http.MethodPost, "/v2/launch", strings.NewReader(validLaunchJSON()), int64(len(validLaunchJSON())), "application/json"))
	wantBody := "{\"status\":{\"state\":\"active\",\"game_id\":\"snes-synthetic\",\"system\":\"snes\",\"expected_core\":\"SNES\",\"observed_core\":\"SNES\",\"last_error\":null},\"content\":{\"sha256\":\"" + v2Digest + "\",\"size\":3,\"extension\":\"sfc\"}}\n"
	if response.Code != http.StatusOK || response.Body.String() != wantBody {
		t.Fatalf("response = status %d, body %q", response.Code, response.Body.String())
	}
	if content.launchCalls != 1 || content.launch != request {
		t.Fatalf("launch call = %d, %#v", content.launchCalls, content.launch)
	}

	tests := []struct {
		name   string
		mutate func(*protocol.CachedLaunchResponse)
	}{
		{name: "content", mutate: func(response *protocol.CachedLaunchResponse) { response.Content.Size++ }},
		{name: "state", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.State = protocol.StateIdle }},
		{name: "missing game", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.GameID = nil }},
		{name: "game", mutate: func(response *protocol.CachedLaunchResponse) { other := "snes-other"; response.Status.GameID = &other }},
		{name: "missing system", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.System = nil }},
		{name: "system", mutate: func(response *protocol.CachedLaunchResponse) {
			other := protocol.SystemMegaDrive
			response.Status.System = &other
		}},
		{name: "missing expected core", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.ExpectedCore = nil }},
		{name: "missing observed core", mutate: func(response *protocol.CachedLaunchResponse) { response.Status.ObservedCore = nil }},
		{name: "wrong expected core", mutate: func(response *protocol.CachedLaunchResponse) {
			other := "MegaDrive"
			response.Status.ExpectedCore = &other
		}},
		{name: "wrong observed core", mutate: func(response *protocol.CachedLaunchResponse) {
			other := "MegaDrive"
			response.Status.ObservedCore = &other
		}},
		{name: "last error", mutate: func(response *protocol.CachedLaunchResponse) {
			response.Status.LastError = &protocol.APIError{Code: protocol.CodeInternal, Message: "/Volumes/private/controller-error-" + strings.Repeat("x", 64<<10)}
		}},
		{name: "oversized expected core", mutate: func(response *protocol.CachedLaunchResponse) {
			other := "SNES/private/" + strings.Repeat("x", 64<<10)
			response.Status.ExpectedCore = &other
		}},
		{name: "oversized observed core", mutate: func(response *protocol.CachedLaunchResponse) {
			other := "SNES/private/" + strings.Repeat("x", 64<<10)
			response.Status.ObservedCore = &other
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mismatched := success
			tt.mutate(&mismatched)
			controller := &fakeContentController{launchResponse: mismatched}
			got := serveContent(newContentHandler(controller, discardLogger()), newV2Request(http.MethodPost, "/v2/launch", strings.NewReader(validLaunchJSON()), int64(len(validLaunchJSON())), "application/json"))
			assertAPIError(t, got, http.StatusInternalServerError, protocol.CodeInternal)
		})
	}
}

func TestV2LaunchSanitizesControllerStatusAndErrorsBeforeLogging(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: v2Digest, Size: 3, Extension: "sfc"}
	privateGame := "controller-private-game-" + strings.Repeat("g", 64<<10)
	privateSystem := protocol.System("controller-private-system-" + strings.Repeat("s", 64<<10))
	privateState := protocol.State("controller-private-state-" + strings.Repeat("t", 64<<10))
	privateCore := "controller-private-core-" + strings.Repeat("c", 64<<10)
	privateMessage := "/Volumes/private/controller-message-" + strings.Repeat("m", 64<<10)
	privateCode := protocol.ErrorCode("CONTROLLER_PRIVATE_CODE_" + strings.Repeat("e", 64<<10))
	hostile := protocol.CachedLaunchResponse{
		Status: protocol.Status{
			State:        privateState,
			GameID:       &privateGame,
			System:       &privateSystem,
			ExpectedCore: &privateCore,
			ObservedCore: &privateCore,
			LastError:    &protocol.APIError{Code: privateCode, Message: privateMessage},
		},
		Content: identity,
	}
	tests := []struct {
		name       string
		controller *protocol.APIError
		wantStatus int
		wantCode   protocol.ErrorCode
		wantBody   string
	}{
		{
			name:       "known controller error",
			controller: &protocol.APIError{Code: protocol.CodeTransferFailed, Message: privateMessage},
			wantStatus: http.StatusBadRequest,
			wantCode:   protocol.CodeTransferFailed,
			wantBody:   "{\"error\":{\"code\":\"TRANSFER_FAILED\",\"message\":\"content transfer failed\"}}\n",
		},
		{
			name:       "unknown controller error",
			controller: &protocol.APIError{Code: privateCode, Message: privateMessage},
			wantStatus: http.StatusInternalServerError,
			wantCode:   protocol.CodeInternal,
			wantBody:   "{\"error\":{\"code\":\"INTERNAL\",\"message\":\"content operation failed internally\"}}\n",
		},
		{
			name:       "invalid success status",
			wantStatus: http.StatusInternalServerError,
			wantCode:   protocol.CodeInternal,
			wantBody:   "{\"error\":{\"code\":\"INTERNAL\",\"message\":\"content operation failed internally\"}}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &fakeContentController{launchResponse: hostile, launchErr: tt.controller}
			var logs bytes.Buffer
			response := serveContent(newContentHandler(controller, slog.New(slog.NewJSONHandler(&logs, nil))), newV2Request(http.MethodPost, "/v2/launch", strings.NewReader(validLaunchJSON()), int64(len(validLaunchJSON())), "application/json"))

			assertAPIError(t, response, tt.wantStatus, tt.wantCode)
			if response.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", response.Body.String(), tt.wantBody)
			}
			logText := logs.String()
			for _, want := range []string{`"game_id":"snes-synthetic"`, `"system":"snes"`, `"digest":"` + v2Digest + `"`, `"size":3`, `"error_code":"` + string(tt.wantCode) + `"`} {
				if !strings.Contains(logText, want) {
					t.Errorf("logs missing request-derived field %s", want)
				}
			}
			privateFields := []struct {
				name  string
				value string
			}{
				{name: "game", value: privateGame},
				{name: "system", value: string(privateSystem)},
				{name: "state", value: string(privateState)},
				{name: "core", value: privateCore},
				{name: "message", value: privateMessage},
				{name: "code", value: string(privateCode)},
			}
			for _, private := range privateFields {
				if strings.Contains(response.Body.String(), private.value) || strings.Contains(logText, private.value) {
					t.Errorf("oversized controller-private %s reached response or logs", private.name)
				}
			}
			if strings.Contains(logText, `"state":`) {
				t.Errorf("invalid controller state reached logs")
			}
			if len(response.Body.String()) > 256 || logs.Len() > 2048 {
				t.Errorf("sanitized output is unbounded: response=%d log=%d", response.Body.Len(), logs.Len())
			}
		})
	}
}

func TestV2UploadRejectsInvalidLengthMediaAndIdentityBeforeBodyRead(t *testing.T) {
	tests := []struct {
		name             string
		path             string
		length           int64
		transferEncoding []string
		media            string
		contentLength    string
		wantStatus       int
		wantCode         protocol.ErrorCode
	}{
		{name: "missing length", path: validUploadPath(), length: 0, media: "application/octet-stream", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "chunked", path: validUploadPath(), length: -1, transferEncoding: []string{"chunked"}, media: "application/octet-stream", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "explicit zero", path: validUploadPath(), length: 0, contentLength: "0", media: "application/octet-stream", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "negative", path: validUploadPath(), length: -2, media: "application/octet-stream", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "oversized", path: validUploadPath(), length: protocol.MaxContentBytes + 1, media: "application/octet-stream", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "missing media", path: validUploadPath(), length: 3, wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "wrong media", path: validUploadPath(), length: 3, media: "application/json", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "media parameter", path: validUploadPath(), length: 3, media: "application/octet-stream; charset=binary", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "invalid system", path: "/v2/cache/SNES/" + v2Digest + "?extension=sfc", length: 3, media: "application/octet-stream", wantStatus: http.StatusUnprocessableEntity, wantCode: protocol.CodeUnsupportedSystem},
		{name: "invalid digest", path: "/v2/cache/snes/ABC?extension=sfc", length: 3, media: "application/octet-stream", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
		{name: "invalid extension", path: "/v2/cache/snes/" + v2Digest + "?extension=.sfc", length: 3, media: "application/octet-stream", wantStatus: http.StatusBadRequest, wantCode: protocol.CodeBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &observedReader{err: errors.New("body must not be read")}
			controller := &fakeContentController{}
			request := httptest.NewRequest(http.MethodPut, tt.path, body)
			request.Header.Set("Authorization", "Bearer test-token")
			request.ContentLength = tt.length
			request.TransferEncoding = tt.transferEncoding
			if tt.contentLength != "" {
				request.Header.Set("Content-Length", tt.contentLength)
			}
			if tt.media != "" {
				request.Header.Set("Content-Type", tt.media)
			}
			response := serveContent(newContentHandler(controller, discardLogger()), request)
			assertAPIError(t, response, tt.wantStatus, tt.wantCode)
			if body.reads != 0 || controller.putCalls != 0 {
				t.Fatalf("body reads/controller calls = %d/%d, want 0/0", body.reads, controller.putCalls)
			}
		})
	}
}

func TestV2UploadStreamsDeclaredIdentityAndReturnsExactResults(t *testing.T) {
	for _, result := range []protocol.CacheUploadResult{protocol.CacheUploadPresent, protocol.CacheUploadCreated} {
		t.Run(string(result), func(t *testing.T) {
			identity := protocol.ContentIdentity{SHA256: v2Digest, Size: 3, Extension: "sfc"}
			controller := &fakeContentController{putResponse: protocol.CacheUploadResponse{Result: result, System: protocol.SystemSNES, Content: identity}}
			controller.put = exactUploadReader(controller.putResponse, nil)
			request := newV2Request(http.MethodPut, validUploadPath(), strings.NewReader("rom"), 3, "application/octet-stream")
			response := serveContent(newContentHandler(controller, discardLogger()), request)
			wantBody := fmt.Sprintf("{\"result\":%q,\"system\":\"snes\",\"content\":{\"sha256\":\"%s\",\"size\":3,\"extension\":\"sfc\"}}\n", result, v2Digest)
			if response.Code != http.StatusOK || response.Body.String() != wantBody {
				t.Fatalf("response = status %d, body %q", response.Code, response.Body.String())
			}
			if controller.putCalls != 1 || controller.putSystem != protocol.SystemSNES || controller.putContent != identity {
				t.Fatalf("put call = %d, %q, %#v", controller.putCalls, controller.putSystem, controller.putContent)
			}
		})
	}
}

func TestV2UploadPropagatesShortExcessReaderAndTypedFailures(t *testing.T) {
	tests := []struct {
		name       string
		body       io.Reader
		length     int64
		controller func(*fakeContentController)
		wantStatus int
		wantCode   protocol.ErrorCode
	}{
		{name: "short", body: strings.NewReader("ro"), length: 3, controller: func(f *fakeContentController) { f.put = exactUploadReader(protocol.CacheUploadResponse{}, nil) }, wantStatus: http.StatusBadRequest, wantCode: protocol.CodeTransferFailed},
		{name: "excess", body: strings.NewReader("romx"), length: 3, controller: func(f *fakeContentController) { f.put = exactUploadReader(protocol.CacheUploadResponse{}, nil) }, wantStatus: http.StatusBadRequest, wantCode: protocol.CodeTransferFailed},
		{name: "reader error", body: &observedReader{data: []byte("r"), err: errors.New("/Volumes/private/source failed")}, length: 3, controller: func(f *fakeContentController) { f.put = exactUploadReader(protocol.CacheUploadResponse{}, nil) }, wantStatus: http.StatusBadRequest, wantCode: protocol.CodeTransferFailed},
		{name: "digest mismatch", body: strings.NewReader("rom"), length: 3, controller: func(f *fakeContentController) {
			f.putErr = &protocol.APIError{Code: protocol.CodeDigestMismatch, Message: "uploaded content digest does not match its identity"}
		}, wantStatus: http.StatusUnprocessableEntity, wantCode: protocol.CodeDigestMismatch},
		{name: "capacity", body: strings.NewReader("rom"), length: 3, controller: func(f *fakeContentController) {
			f.putErr = &protocol.APIError{Code: protocol.CodeCacheFull, Message: "target cache has insufficient safe capacity"}
		}, wantStatus: http.StatusInsufficientStorage, wantCode: protocol.CodeCacheFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &fakeContentController{}
			tt.controller(controller)
			request := newV2Request(http.MethodPut, validUploadPath(), tt.body, tt.length, "application/octet-stream")
			response := serveContent(newContentHandler(controller, discardLogger()), request)
			assertAPIError(t, response, tt.wantStatus, tt.wantCode)
			if controller.putCalls != 1 {
				t.Fatalf("put calls = %d, want 1", controller.putCalls)
			}
		})
	}
}

func TestV2UploadBodyIsBoundedIndependentlyOfDeclaredLength(t *testing.T) {
	body := &repeatingReader{remaining: protocol.MaxContentBytes + (1 << 20)}
	controller := &fakeContentController{}
	controller.put = func(_ context.Context, _ protocol.System, _ protocol.ContentIdentity, reader io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
		_, err := io.Copy(io.Discard, reader)
		var tooLarge *http.MaxBytesError
		if !errors.As(err, &tooLarge) || tooLarge.Limit != protocol.MaxContentBytes+1 {
			t.Fatalf("reader error = %#v, want MaxBytesError limit %d", err, protocol.MaxContentBytes+1)
		}
		return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "content upload body is too large"}
	}
	request := newV2Request(http.MethodPut, validUploadPath(), body, 3, "application/octet-stream")
	response := serveContent(newContentHandler(controller, discardLogger()), request)
	assertAPIError(t, response, http.StatusBadRequest, protocol.CodeTransferFailed)
	if body.bytes > protocol.MaxContentBytes+2 {
		t.Fatalf("underlying body read %d bytes, want bounded near %d", body.bytes, protocol.MaxContentBytes+1)
	}
}

func TestV2UploadCanceledContextIsPreservedAndNotMappedToOrdinaryBadRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	controller := &fakeContentController{}
	controller.put = func(ctx context.Context, _ protocol.System, _ protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
		var one [1]byte
		_, _ = body.Read(one[:])
		cancel()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("controller context error = %v, want canceled", ctx.Err())
		}
		return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "content upload was canceled"}
	}
	request := newV2Request(http.MethodPut, validUploadPath(), strings.NewReader("rom"), 3, "application/octet-stream").WithContext(ctx)
	response := serveContent(newContentHandler(controller, discardLogger()), request)
	if response.Code != 499 {
		t.Fatalf("status = %d, want 499 for canceled client request; body=%s", response.Code, response.Body.String())
	}
	if controller.putCalls != 1 {
		t.Fatalf("put calls = %d, want 1", controller.putCalls)
	}
}

func TestV2ControllerResponsesMustMatchRequestedIdentity(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: v2Digest, Size: 3, Extension: "sfc"}
	tests := []struct {
		name     string
		response protocol.CacheUploadResponse
	}{
		{name: "missing result", response: protocol.CacheUploadResponse{System: protocol.SystemSNES, Content: identity}},
		{name: "unknown result", response: protocol.CacheUploadResponse{Result: "replaced", System: protocol.SystemSNES, Content: identity}},
		{name: "wrong system", response: protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: protocol.SystemMegaDrive, Content: identity}},
		{name: "wrong content", response: protocol.CacheUploadResponse{Result: protocol.CacheUploadCreated, System: protocol.SystemSNES, Content: protocol.ContentIdentity{SHA256: v2Digest, Size: 4, Extension: "sfc"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &fakeContentController{putResponse: tt.response}
			response := serveContent(newContentHandler(controller, discardLogger()), newV2Request(http.MethodPut, validUploadPath(), strings.NewReader("rom"), 3, "application/octet-stream"))
			assertAPIError(t, response, http.StatusInternalServerError, protocol.CodeInternal)
		})
	}
}

func TestV2ControllerErrorsAreTypedBoundedAndSanitized(t *testing.T) {
	privateDetails := "/media/fat/fogcast/cache/snes/private.sfc test-token synthetic-rom-secret " + strings.Repeat("x", 1<<20)
	controller := &fakeContentController{putErr: &protocol.APIError{Code: protocol.CodeInternal, Message: privateDetails}}
	response := serveContent(newContentHandler(controller, discardLogger()), newV2Request(http.MethodPut, validUploadPath(), strings.NewReader("rom"), 3, "application/octet-stream"))
	assertAPIError(t, response, http.StatusInternalServerError, protocol.CodeInternal)
	if response.Body.Len() > 1024 {
		t.Fatalf("error response length = %d, want at most 1024", response.Body.Len())
	}
	for _, secret := range []string{"/media/fat/fogcast/cache", "test-token", "synthetic-rom-secret", strings.Repeat("x", 128)} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("error response exposes %q: %s", secret, response.Body.String())
		}
	}
}

func TestV2LogsUseSanitizedRouteMetadataAndExcludeSecrets(t *testing.T) {
	privateSource := "/Volumes/private-library/synthetic.sfc"
	internalCachePath := "/media/fat/fogcast/cache/snes/private.sfc"
	romMarker := "synthetic-rom-secret"
	identity := protocol.ContentIdentity{SHA256: v2Digest, Size: int64(len(romMarker)), Extension: "sfc"}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	controller := &fakeContentController{
		putErr: &protocol.APIError{Code: protocol.CodeDigestMismatch, Message: "mismatch at " + internalCachePath},
	}
	request := newV2Request(http.MethodPut, "/v2/cache/snes/"+v2Digest+"?extension=sfc&source="+privateSource, strings.NewReader(romMarker), identity.Size, "application/octet-stream")
	response := serveContent(newContentHandler(controller, logger), request)
	assertAPIError(t, response, http.StatusBadRequest, protocol.CodeBadRequest)

	controller = &fakeContentController{putErr: &protocol.APIError{Code: protocol.CodeDigestMismatch, Message: "mismatch at " + internalCachePath}}
	request = newV2Request(http.MethodPut, validUploadPath(), strings.NewReader(romMarker), identity.Size, "application/octet-stream")
	response = serveContent(newContentHandler(controller, logger), request)
	assertAPIError(t, response, http.StatusUnprocessableEntity, protocol.CodeDigestMismatch)

	logText := logs.String()
	for _, want := range []string{
		`"method":"PUT"`,
		`"route":"/v2/cache/{system}/{sha256}"`,
		`"status":422`,
		`"system":"snes"`,
		`"digest":"` + v2Digest + `"`,
		fmt.Sprintf(`"size":%d`, identity.Size),
		`"error_code":"DIGEST_MISMATCH"`,
	} {
		if !strings.Contains(logText, want) {
			t.Errorf("logs missing %s: %s", want, logText)
		}
	}
	for _, secret := range []string{"test-token", privateSource, internalCachePath, romMarker, "source="} {
		if strings.Contains(logText, secret) {
			t.Errorf("logs expose %q: %s", secret, logText)
		}
	}
}

func TestV2LaunchLogsSafeIdentityWithoutLoggingBody(t *testing.T) {
	identity := protocol.ContentIdentity{SHA256: v2Digest, Size: 3, Extension: "sfc"}
	system := protocol.SystemSNES
	gameID, expected, observed := "snes-synthetic", "SNES", "SNES"
	controller := &fakeContentController{launchResponse: protocol.CachedLaunchResponse{
		Status:  protocol.Status{State: protocol.StateActive, GameID: &gameID, System: &system, ExpectedCore: &expected, ObservedCore: &observed},
		Content: identity,
	}}
	var logs bytes.Buffer
	response := serveContent(newContentHandler(controller, slog.New(slog.NewJSONHandler(&logs, nil))), newV2Request(http.MethodPost, "/v2/launch", strings.NewReader(validLaunchJSON()), int64(len(validLaunchJSON())), "application/json"))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	for _, want := range []string{
		`"method":"POST"`, `"route":"/v2/launch"`, `"status":200`, `"game_id":"snes-synthetic"`, `"system":"snes"`, `"digest":"` + v2Digest + `"`, `"size":3`, `"state":"active"`,
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs missing %s: %s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), `"content"`) || strings.Contains(logs.String(), validLaunchJSON()) {
		t.Fatalf("logs contain request body: %s", logs.String())
	}
}

func exactUploadReader(success protocol.CacheUploadResponse, successErr *protocol.APIError) func(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
	return func(ctx context.Context, _ protocol.System, content protocol.ContentIdentity, body io.Reader) (protocol.CacheUploadResponse, *protocol.APIError) {
		data, err := io.ReadAll(body)
		if err != nil {
			return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "content upload body cannot be read"}
		}
		if ctx.Err() != nil {
			return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "content upload was canceled"}
		}
		if int64(len(data)) != content.Size {
			return protocol.CacheUploadResponse{}, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "content upload length does not match its declaration"}
		}
		return success, successErr
	}
}

func validUploadPath() string {
	return "/v2/cache/snes/" + v2Digest + "?extension=sfc"
}

func validLaunchJSON() string {
	return `{"game_id":"snes-synthetic","system":"snes","content":{"sha256":"` + v2Digest + `","size":3,"extension":"sfc"}}`
}

func newContentHandler(content *fakeContentController, logger *slog.Logger) http.Handler {
	return httpapi.New(&fakeController{}, "test-token", "0.1.0", logger, httpapi.WithContent(content))
}

func newV2Request(method, path string, body io.Reader, contentLength int64, mediaType string) *http.Request {
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Authorization", "Bearer test-token")
	request.ContentLength = contentLength
	if mediaType != "" {
		request.Header.Set("Content-Type", mediaType)
	}
	return request
}

func serveContent(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code protocol.ErrorCode) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, status, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
	if !strings.HasSuffix(response.Body.String(), "\n") {
		t.Fatalf("body is not newline terminated: %q", response.Body.String())
	}
	var envelope protocol.ErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body=%q", err, response.Body.String())
	}
	if envelope.Error.Code != code || envelope.Error.Message == "" {
		t.Fatalf("error = %#v, want code %s with message", envelope.Error, code)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

type observedReader struct {
	data  []byte
	err   error
	reads int
}

func (r *observedReader) Read(p []byte) (int, error) {
	r.reads++
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

type repeatingReader struct {
	bytes     int64
	remaining int64
}

func (r *repeatingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	for index := range p {
		p[index] = 'x'
	}
	r.bytes += int64(len(p))
	r.remaining -= int64(len(p))
	return len(p), nil
}
