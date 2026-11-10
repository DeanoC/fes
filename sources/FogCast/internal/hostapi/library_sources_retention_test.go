package hostapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

// A client that PUTs back the redacted GET document keeps the stored token.
func TestLibrarySourcesPUTKeepsRedactedTokenThroughService(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	dir := t.TempDir()
	config := filepath.Join(dir, "config.toml")
	data := fmt.Sprintf("base_url=%q\ntoken=%q\nrequest_timeout_seconds=5\nupload_timeout_seconds=2\n", target.URL, "test-token")
	if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := fogcast.Open(context.Background(), fogcast.Paths{Config: config, Index: filepath.Join(dir, "index.sqlite3"), Staging: filepath.Join(dir, "staging")}, target.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	handler := hostapi.New(service)
	call := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://127.0.0.1/api/v1/library/sources", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s sources: status=%d body=%s", method, w.Code, w.Body.String())
		}
		return w
	}
	token := "source-test-token-0123456789abcdef"
	call(http.MethodPut, `{"sources":[{"id":"powerboat","url":"http://192.0.2.1:8788","token":"`+token+`"}]}`)
	get := call(http.MethodGet, "")
	if strings.Contains(get.Body.String(), token) {
		t.Fatalf("GET leaked token: %s", get.Body.String())
	}
	// Echo the redacted GET document, then change only the URL.
	echo := call(http.MethodPut, get.Body.String())
	moved := call(http.MethodPut, `{"sources":[{"id":"powerboat","url":"http://192.0.2.9:8788"}]}`)
	for _, w := range []*httptest.ResponseRecorder{echo, moved} {
		if strings.Contains(w.Body.String(), token) {
			t.Fatalf("PUT leaked token: %s", w.Body.String())
		}
	}
	var result struct {
		Sources []fogcast.LibrarySourceConfig `json:"sources"`
	}
	if err := json.Unmarshal(moved.Body.Bytes(), &result); err != nil || len(result.Sources) != 1 || result.Sources[0].URL != "http://192.0.2.9:8788" {
		t.Fatalf("moved response=%s err=%v", moved.Body.String(), err)
	}
	if got := service.LibrarySources(); len(got) != 1 || got[0].Token != token || got[0].URL != "http://192.0.2.9:8788" {
		t.Fatalf("service sources=%+v", got)
	}
	loaded, err := fogcast.LoadConfig(config)
	if err != nil || len(loaded.LibrarySources) != 1 || loaded.LibrarySources[0].Token != token {
		t.Fatalf("persisted sources=%+v err=%v", loaded.LibrarySources, err)
	}
}
