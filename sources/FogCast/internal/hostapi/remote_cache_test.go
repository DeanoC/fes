package hostapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

// Exercise the public loopback API against the real shared catalog, including
// explicit cleanup, rejected invalid requests and listener restrictions.
func TestRemoteCassetteCacheAPI(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer target.Close()
	root := t.TempDir()
	config := filepath.Join(root, "config.toml")
	if err := os.WriteFile(config, []byte("base_url = "+strconv.Quote(target.URL)+"\ntoken = \"test-token\"\nrequest_timeout_seconds = 5\nupload_timeout_seconds = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, "index.sqlite3")
	service, err := fogcast.Open(ctx, fogcast.Paths{Config: config, Index: index, Staging: filepath.Join(root, "staging")}, target.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	store, err := catalog.Open(index)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	media, created, err := store.ImportRemoteCassetteStream(ctx, 4, strings.NewReader("tape"))
	if err != nil || !created {
		t.Fatalf("fixture: %v %v", created, err)
	}
	handler := hostapi.New(service)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		return w
	}
	w := call(http.MethodGet, "/api/v1/library/sources/cache", "")
	var usage catalog.RemoteCacheUsage
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &usage) != nil || len(usage.Candidates) != 1 || usage.Candidates[0].MediaID != media.MediaID {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/v1/library/sources/cache?limit=0", "/api/v1/library/sources/cache?after=wrong"} {
		if w = call(http.MethodGet, path, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("bad GET: %d %s", w.Code, w.Body.String())
		}
	}
	if w = call(http.MethodPost, "/api/v1/library/sources/cache/cleanup", `{"media_ids":["bad"]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad POST: %d %s", w.Code, w.Body.String())
	}
	w = call(http.MethodPost, "/api/v1/library/sources/cache/cleanup", `{"media_ids":["`+media.MediaID+`"]}`)
	var result catalog.RemoteCacheCleanup
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Deleted) != 1 || result.LogicalBytes != 4 {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	ids := make([]string, catalog.RemoteCachePageLimit)
	for i := range ids {
		ids[i] = fmt.Sprintf("%064x", i+1)
	}
	body, err := json.Marshal(map[string]any{"media_ids": ids})
	if err != nil {
		t.Fatal(err)
	}
	w = call(http.MethodPost, "/api/v1/library/sources/cache/cleanup", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("maximum cleanup batch rejected: %d %s", w.Code, w.Body.String())
	}
	restrictedService := &librarySourceConfigService{fakeService: fakeService{connection: fogcast.TargetConnection{TargetID: launcherID}}}
	paired, err := hostapi.NewLauncherHandler(hostapi.New(restrictedService), hostapi.LauncherConfig{Token: launcherToken, TargetID: launcherID})
	if err != nil {
		t.Fatal(err)
	}
	source := hostapi.NewLibrarySourceHandler(&cassetteSource{}, launcherToken)
	for _, tc := range []struct{ method, path, body string }{{http.MethodGet, "/api/v1/library/sources/cache", ""}, {http.MethodPost, "/api/v1/library/sources/cache/cleanup", `{"media_ids":[]}`}} {
		for name, handler := range map[string]http.Handler{"paired": paired, "source": source} {
			w := kitCall(handler, tc.method, tc.path, launcherToken, launcherID, tc.body)
			if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s route exposed: %d %s", name, w.Code, w.Body.String())
			}
		}
	}
}
