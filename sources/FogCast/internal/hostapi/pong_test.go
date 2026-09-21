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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/protocol"
)

func TestRawPongIsNotSeededOrLaunchedThroughPublicSessionAPI(t *testing.T) {
	testPongPublicSession(t, false)
}
func testPongPublicSession(t *testing.T, delayedStop bool) {
	var statusMu sync.Mutex
	var releases atomic.Int32
	status := protocol.Status{State: protocol.StateIdle}
	launches := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing target authentication")
		}
		statusMu.Lock()
		defer statusMu.Unlock()
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(protocol.Health{APIVersion: "v1", Ready: true})
			return
		case "/v1/kit/lease":
			state := "free"
			if launches > 0 && releases.Load() == 0 {
				state = "held"
			}
			json.NewEncoder(w).Encode(map[string]string{"state": state, "generation": "test"})
			return
		case "/v1/kit/claim":
			json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"state": "held", "generation": "test", "expires_at": time.Now().Add(time.Minute), "expires_in_ms": 60000}, "token": "test-lease"})
			return
		case "/v1/kit/release":
			releases.Add(1)
			json.NewEncoder(w).Encode(map[string]string{"state": "free"})
			return
		case "/v1/launch", "/v2/launch":
			launches++
			t.Error("retired raw launch reached target")
		case "/v1/stop":
			status = protocol.Status{State: protocol.StateIdle}
			if delayedStop {
				statusMu.Unlock()
				<-r.Context().Done()
				statusMu.Lock()
				return
			}
		case "/v1/status":
		case "/v2/cache":
			if r.Method != http.MethodGet {
				t.Errorf("unexpected target operation %s %s", r.Method, r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(protocol.CacheIndex{Entries: []protocol.CacheIndexEntry{}})
			return
		default:
			t.Errorf("unexpected target operation %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}))
	defer target.Close()
	dir := t.TempDir()
	config := filepath.Join(dir, "config.toml")
	data := fmt.Sprintf("base_url=%q\ntoken='test-token'\nrequest_timeout_seconds=5\nupload_timeout_seconds=2\n", target.URL)
	if err := os.WriteFile(config, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := fogcast.Open(context.Background(), fogcast.Paths{Config: config, Index: filepath.Join(dir, "index.sqlite3"), Staging: filepath.Join(dir, "staging")}, target.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	handler := hostapi.New(service)
	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/api/v1/games?platform=pong&grouped=1&availability=ready", nil))
	if get.Code != 200 || strings.Contains(get.Body.String(), `"id":"pong"`) {
		t.Fatalf("discovery=%d %s", get.Code, get.Body.String())
	}
	post := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/session/launch", strings.NewReader(`{"game_id":"pong"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(post, request)
	if post.Code != http.StatusNotFound || launches != 0 {
		t.Fatalf("retired raw game launch=%d %s (%d dispatches)", post.Code, post.Body.String(), launches)
	}
}
