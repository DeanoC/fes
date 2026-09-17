package hostapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

type coreMediaAPIService struct {
	coreLibraryAPIService
	calls   int
	created bool
	err     error
	args    []string
}

func (s *coreMediaAPIService) ImportCoreMedia(_ context.Context, size int64, body io.Reader) (catalog.CoreMedia, bool, error) {
	s.calls++
	data, err := protocol.ReadDevelopmentMedia(size, body)
	if err != nil {
		return catalog.CoreMedia{}, false, err
	}
	digest := sha256.Sum256(data)
	return catalog.CoreMedia{MediaID: hex.EncodeToString(digest[:]), Size: size}, s.created, s.err
}
func (s *coreMediaAPIService) CoreMedia(_ context.Context, id string) (catalog.CoreMedia, error) {
	s.calls++
	return catalog.CoreMedia{MediaID: id, Size: 3}, s.err
}
func (s *coreMediaAPIService) CreateCoreMediaEntry(_ context.Context, title, pkg, role, id string) (catalog.CoreEntry, error) {
	s.calls++
	s.args = []string{title, pkg, role, id}
	return catalog.CoreEntry{Title: title, PackageID: pkg, MediaRole: role, MediaID: id}, s.err
}
func (s *coreMediaAPIService) SelectCoreEntryMedia(_ context.Context, game, pkg, expected, role, id string) (catalog.CoreEntry, error) {
	s.calls++
	s.args = []string{game, pkg, expected, role, id}
	return catalog.CoreEntry{GameID: game, PackageID: pkg, MediaRole: role, MediaID: id}, s.err
}
func (s *coreMediaAPIService) TargetConnection() fogcast.TargetConnection {
	return fogcast.TargetConnection{TargetID: launcherID, State: "ready"}
}
func mediaRequest(handler http.Handler, method, path, body, content string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1"
	if content != "" {
		req.Header.Set("Content-Type", content)
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	return out
}

func TestCoreMediaImportHeadersAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     int64
		content  []string
		transfer []string
		body     string
		want     int
		calls    int
	}{
		{"one", 1, []string{"application/octet-stream"}, nil, "x", 201, 1},
		{"maximum", 16384, []string{"application/octet-stream"}, nil, strings.Repeat("x", 16384), 201, 1},
		{"empty", 0, []string{"application/octet-stream"}, nil, "", 400, 0},
		{"oversize", 16385, []string{"application/octet-stream"}, nil, "x", 400, 0},
		{"unknown", -1, []string{"application/octet-stream"}, nil, "x", 400, 0},
		{"missing type", 1, nil, nil, "x", 400, 0},
		{"wrong type", 1, []string{"text/plain"}, nil, "x", 400, 0},
		{"parameter", 1, []string{"application/octet-stream; charset=utf-8"}, nil, "x", 400, 0},
		{"duplicate", 1, []string{"application/octet-stream", "application/octet-stream"}, nil, "x", 400, 0},
		{"chunked", 1, []string{"application/octet-stream"}, []string{"chunked"}, "x", 400, 0},
		{"short", 2, []string{"application/octet-stream"}, nil, "x", 400, 1},
		{"long", 1, []string{"application/octet-stream"}, nil, "xx", 400, 1},
		{"bounded reader", 1, []string{"application/octet-stream"}, nil, strings.Repeat("x", 16385), 400, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &coreMediaAPIService{created: true}
			req := httptest.NewRequest("POST", "/api/v1/core-media", strings.NewReader(tc.body))
			req.Host = "127.0.0.1"
			req.ContentLength = tc.size
			req.TransferEncoding = tc.transfer
			req.Header["Content-Type"] = tc.content
			out := httptest.NewRecorder()
			hostapi.New(s).ServeHTTP(out, req)
			if out.Code != tc.want || s.calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", out.Code, s.calls, out.Body)
			}
			if out.Code == 201 {
				var got catalog.CoreMedia
				if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil || got.Size != tc.size || len(got.MediaID) != 64 {
					t.Fatalf("metadata=%+v err=%v", got, err)
				}
			}
		})
	}
	s := &coreMediaAPIService{}
	out := mediaRequest(hostapi.New(s), "POST", "/api/v1/core-media", "abc", "application/octet-stream")
	if out.Code != 200 || s.calls != 1 {
		t.Fatalf("deduplicated=%d calls=%d", out.Code, s.calls)
	}
}

func TestCoreMediaMetadata(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		path, body  string
		want, calls int
	}{
		{id, "", 200, 1}, {id, "{}", 400, 0}, {"bad", "", 400, 0}, {strings.ToUpper(id), "", 400, 0},
	} {
		s := &coreMediaAPIService{}
		out := mediaRequest(hostapi.New(s), "GET", "/api/v1/core-media/"+tc.path, tc.body, "")
		if out.Code != tc.want || s.calls != tc.calls {
			t.Fatalf("status=%d calls=%d %s", out.Code, s.calls, out.Body)
		}
		if out.Code == 200 && out.Body.String() != "{\"media_id\":\""+id+"\",\"size\":3}\n" {
			t.Fatalf("metadata=%s", out.Body)
		}
	}
}

func TestCoreMediaEntryCreateAndSelectShapes(t *testing.T) {
	pkg, id := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, tc := range []struct {
		name, method, path, body string
		want                     int
		args                     []string
	}{
		{"legacy", "POST", "", `{"title":"Core","package_id":"` + pkg + `"}`, 201, nil},
		{"create", "POST", "", `{"title":"Core","package_id":"` + pkg + `","media_role":"blob","media_id":"` + id + `"}`, 201, []string{"Core", pkg, "blob", id}},
		{"set", "PUT", "/core-test/media", `{"expected_package_id":"` + pkg + `","expected_media_id":"","media_role":"blob","media_id":"` + id + `"}`, 200, []string{"core-test", pkg, "", "blob", id}},
		{"clear", "PUT", "/core-test/media", `{"expected_package_id":"` + pkg + `","expected_media_id":"` + id + `","media_role":"","media_id":""}`, 200, []string{"core-test", pkg, id, "", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &coreMediaAPIService{}
			out := mediaRequest(hostapi.New(s), tc.method, "/api/v1/library/core-entries"+tc.path, tc.body, "application/json")
			if out.Code != tc.want || !reflect.DeepEqual(s.args, tc.args) {
				t.Fatalf("status=%d args=%v body=%s", out.Code, s.args, out.Body)
			}
			if tc.args != nil {
				var got catalog.CoreEntry
				if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil || got.MediaID != tc.args[len(tc.args)-1] {
					t.Fatalf("entry=%+v err=%v", got, err)
				}
			}
		})
	}
	// The old service interface remains sufficient for media-free creation.
	out := mediaRequest(hostapi.New(&coreLibraryAPIService{}), "POST", "/api/v1/library/core-entries", `{"title":"Core","package_id":"`+pkg+`"}`, "application/json")
	if out.Code != 201 {
		t.Fatal(out.Body)
	}
}

func TestCoreMediaSelectionRejectsIncompleteOrMalformedCAS(t *testing.T) {
	pkg, id := strings.Repeat("a", 64), strings.Repeat("b", 64)
	base := map[string]any{"expected_package_id": pkg, "expected_media_id": "", "media_role": "blob", "media_id": id}
	for _, tc := range []struct {
		name, key string
		value     any
		remove    bool
	}{
		{"missing package", "expected_package_id", nil, true},
		{"null package", "expected_package_id", nil, false},
		{"empty package", "expected_package_id", "", false},
		{"missing media", "expected_media_id", nil, true},
		{"null media", "expected_media_id", nil, false},
		{"numeric media", "expected_media_id", 1, false},
		{"bad expected media", "expected_media_id", "bad", false},
		{"bad role", "media_role", "cartridge", false},
		{"missing role", "media_role", nil, true},
		{"missing id", "media_id", nil, true},
		{"bad id", "media_id", "bad", false},
		{"unknown", "path", "/private", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := map[string]any{}
			for k, v := range base {
				value[k] = v
			}
			if tc.remove {
				delete(value, tc.key)
			} else {
				value[tc.key] = tc.value
			}
			body, _ := json.Marshal(value)
			s := &coreMediaAPIService{}
			out := mediaRequest(hostapi.New(s), "PUT", "/api/v1/library/core-entries/core-test/media", string(body), "application/json")
			if out.Code != 400 || s.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", out.Code, s.calls, out.Body)
			}
		})
	}
	for _, body := range []string{
		`{"package_id":"` + pkg + `","media_id":"` + id + `"}`,
		`{"package_id":"` + pkg + `","media_role":"blob"}`,
		`{"package_id":"` + pkg + `","media_role":"cartridge","media_id":"` + id + `"}`,
		`{"package_id":"` + pkg + `","media_role":"blob","media_id":"bad"}`,
	} {
		s := &coreMediaAPIService{}
		out := mediaRequest(hostapi.New(s), "POST", "/api/v1/library/core-entries", body, "application/json")
		if out.Code != 400 || s.calls != 0 {
			t.Fatalf("status=%d calls=%d body=%s", out.Code, s.calls, out.Body)
		}
	}
	for _, body := range []string{`{}`, `null`, `{} {}`, `{`} {
		s := &coreMediaAPIService{}
		out := mediaRequest(hostapi.New(s), "PUT", "/api/v1/library/core-entries/core-test/media", body, "application/json")
		if out.Code != 400 || s.calls != 0 {
			t.Fatalf("status=%d calls=%d", out.Code, s.calls)
		}
	}
}

func TestCoreMediaClearRequiresExplicitNonNullPair(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	for _, suffix := range []string{
		"", `,"media_role":""`, `,"media_id":""`,
		`,"media_role":null,"media_id":""`,
		`,"media_role":"","media_id":null`,
		`,"media_role":null,"media_id":null`,
		`,"media_role":null`, `,"media_id":null`,
		`,"media_role":"","media_id":""`,
	} {
		t.Run(suffix, func(t *testing.T) {
			s := &coreMediaAPIService{}
			body := `{"expected_package_id":"` + pkg + `","expected_media_id":"` + pkg + `"` + suffix + `}`
			out := mediaRequest(hostapi.New(s), "PUT", "/api/v1/library/core-entries/core-test/media", body, "application/json")
			wantStatus, wantCalls := 400, 0
			if suffix == `,"media_role":"","media_id":""` {
				wantStatus, wantCalls = 200, 1
			}
			if out.Code != wantStatus || s.calls != wantCalls {
				t.Fatalf("status=%d calls=%d body=%s", out.Code, s.calls, out.Body)
			}
			if wantCalls == 1 && !reflect.DeepEqual(s.args, []string{"core-test", pkg, pkg, "", ""}) {
				t.Fatalf("clear args=%v", s.args)
			}
		})
	}
}

func TestCoreMediaErrorsAreReturnedWithoutReplay(t *testing.T) {
	pkg := strings.Repeat("a", 64)
	for _, tc := range []struct {
		err  error
		want int
		code string
	}{
		{&protocol.APIError{Code: protocol.CodeBadRequest}, 400, "BAD_REQUEST"},
		{&protocol.APIError{Code: protocol.CodeROMNotFound}, 404, "ROM_NOT_FOUND"},
		{&protocol.APIError{Code: protocol.CodeStaleRevision}, 409, "STALE_REVISION"},
		{&protocol.APIError{Code: protocol.CodeMiSTerUnavailable}, 503, "MISTER_UNAVAILABLE"},
		{errors.New("private storage path"), 500, "INTERNAL"},
	} {
		for _, op := range []struct{ method, path, body, content string }{
			{"POST", "/api/v1/core-media", "abc", "application/octet-stream"},
			{"GET", "/api/v1/core-media/" + pkg, "", ""},
			{"POST", "/api/v1/library/core-entries", `{"title":"Core","package_id":"` + pkg + `","media_role":"blob","media_id":"` + pkg + `"}`, "application/json"},
			{"PUT", "/api/v1/library/core-entries/core-test/media", `{"expected_package_id":"` + pkg + `","expected_media_id":"","media_role":"","media_id":""}`, "application/json"},
		} {
			s := &coreMediaAPIService{err: tc.err}
			out := mediaRequest(hostapi.New(s), op.method, op.path, op.body, op.content)
			if out.Code != tc.want || s.calls != 1 || !strings.Contains(out.Body.String(), tc.code) || strings.Contains(out.Body.String(), "private storage path") {
				t.Fatalf("status=%d calls=%d body=%s", out.Code, s.calls, out.Body)
			}
		}
	}
}

func TestCoreMediaManagementIsHostOnly(t *testing.T) {
	s := &coreMediaAPIService{}
	handler, err := hostapi.NewLauncherHandler(hostapi.New(s), hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/core-media"}, {"GET", "/api/v1/core-media/" + strings.Repeat("a", 64)},
		{"POST", "/api/v1/library/core-entries"}, {"PUT", "/api/v1/library/core-entries/core-test/media"},
	} {
		req := launcherRequest(tc.method, "http://192.0.2.1:8789"+tc.path, strings.NewReader("{}"))
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != 404 || s.calls != 0 {
			t.Fatalf("status=%d calls=%d body=%s", out.Code, s.calls, out.Body)
		}
	}
}
