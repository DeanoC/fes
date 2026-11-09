package hostapi_test

import (
	"context"
	"encoding/json"
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

type librarySourceConfigService struct {
	fakeService
	sources   []fogcast.LibrarySourceConfig
	sets      int
	refreshID string
}

func (s *librarySourceConfigService) LibrarySources() []fogcast.LibrarySourceConfig {
	return append([]fogcast.LibrarySourceConfig{}, s.sources...)
}

func (s *librarySourceConfigService) SetLibrarySources(_ context.Context, sources []fogcast.LibrarySourceConfig) error {
	s.sets++
	if len(sources) > 0 && sources[0].ID == "invalid" {
		return &protocol.APIError{Code: protocol.CodeBadRequest}
	}
	s.sources = append([]fogcast.LibrarySourceConfig{}, sources...)
	return nil
}

func (s *librarySourceConfigService) RefreshLibrarySource(_ context.Context, id string) (fogcast.LibrarySourceRefresh, error) {
	s.refreshID = id
	if id == "missing" {
		return fogcast.LibrarySourceRefresh{}, &protocol.APIError{Code: protocol.CodeBadRequest}
	}
	return fogcast.LibrarySourceRefresh{SourceID: id, Entries: []catalog.CoreEntry{{GameID: "zx81-game"}}, Skipped: 1}, nil
}

func TestLibrarySourcesAPI(t *testing.T) {
	s := &librarySourceConfigService{fakeService: fakeService{connection: fogcast.TargetConnection{TargetID: launcherID}}}
	handler := hostapi.New(s)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	decodeSources := func(w *httptest.ResponseRecorder) []fogcast.LibrarySourceConfig {
		t.Helper()
		var result struct {
			Sources []fogcast.LibrarySourceConfig `json:"sources"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatalf("sources response: status=%d body=%s", w.Code, w.Body.String())
		}
		return result.Sources
	}
	path := "/api/v1/library/sources"
	if got := decodeSources(call(http.MethodGet, path, "")); len(got) != 0 {
		t.Fatalf("initial sources: %+v", got)
	}
	want := []fogcast.LibrarySourceConfig{{ID: "powerboat", URL: "http://192.0.2.1:8788"}}
	body := `{"sources":[{"id":"powerboat","url":"http://192.0.2.1:8788"}]}`
	if got := decodeSources(call(http.MethodPut, path, body)); !reflect.DeepEqual(got, want) {
		t.Fatalf("PUT sources = %+v", got)
	}
	if got := decodeSources(call(http.MethodGet, path, "")); !reflect.DeepEqual(got, want) {
		t.Fatalf("GET sources = %+v", got)
	}
	w := call(http.MethodPost, path+"/powerboat/refresh", "")
	var refreshed fogcast.LibrarySourceRefresh
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &refreshed) != nil || refreshed.SourceID != "powerboat" || refreshed.Skipped != 1 || len(refreshed.Entries) != 1 || refreshed.Entries[0].GameID != "zx81-game" || s.refreshID != "powerboat" {
		t.Fatalf("refresh: status=%d body=%s called=%q", w.Code, w.Body.String(), s.refreshID)
	}
	for _, bad := range []string{`{}`, `{"sources":null}`, `{"sources":`, `{"sources":[{"id":"invalid","url":"http://x"}]}`} {
		w = call(http.MethodPut, path, bad)
		if w.Code != http.StatusBadRequest || !reflect.DeepEqual(s.sources, want) {
			t.Fatalf("bad PUT %s: status=%d sources=%+v", bad, w.Code, s.sources)
		}
	}
	if w = call(http.MethodPost, path+"/missing/refresh", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("refresh failure: status=%d body=%s", w.Code, w.Body.String())
	}
	unsupported := hostapi.New(&fakeService{})
	w = httptest.NewRecorder()
	unsupported.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("unsupported: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLibrarySourcesAPINotExposedOnPairedOrSourceListener(t *testing.T) {
	s := &librarySourceConfigService{fakeService: fakeService{connection: fogcast.TargetConnection{TargetID: launcherID}}}
	paired, err := hostapi.NewLauncherHandler(hostapi.New(s), hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	source := hostapi.NewLibrarySourceHandler(&cassetteSource{})
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/library/sources", ""},
		{http.MethodPut, "/api/v1/library/sources", `{"sources":[]}`},
		{http.MethodPost, "/api/v1/library/sources/powerboat/refresh", ""},
	} {
		for name, handler := range map[string]http.Handler{"paired": paired, "source": source} {
			w := kitCall(handler, tc.method, tc.path, launcherToken, launcherID, tc.body)
			if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s %s: status=%d body=%s", name, tc.method, tc.path, w.Code, w.Body.String())
			}
		}
	}
	if s.sets != 0 || s.refreshID != "" {
		t.Fatalf("restricted listener reached service: sets=%d refresh=%q", s.sets, s.refreshID)
	}
}
