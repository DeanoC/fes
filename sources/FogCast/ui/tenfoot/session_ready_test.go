package tenfoot

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func TestPausedSessionReadinessRequiresFreshAppliedHostAndRenderProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher-ready.json")
	t.Setenv("FES_SOFTWARE_READY_FILE", path)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", "bundle")
	var failed atomic.Bool
	client := &http.Client{Transport: kitHostSessionTransport(func(r *http.Request) (*http.Response, error) {
		if failed.Load() {
			return nil, errors.New("host unavailable")
		}
		return kitHostResponse(r, 200, `{"id":"play","state":"active","game_id":"pong","target_id":"kit","core_package":{"package_id":"`+strings.Repeat("a", 64)+`","generation":7}}`), nil
	})}
	app := NewApp(NewClient("http://host.invalid", client), 1280, 720, 50)
	app.kitHost = true
	defer app.Stop()
	exists := func(want bool) {
		t.Helper()
		_, err := os.Stat(path + ".session")
		if (err == nil) != want {
			t.Fatalf("session marker exists=%v want=%v", err == nil, want)
		}
	}
	app.fetchSession(t.Context())
	app.reportSessionReady(false, app.sessionObservation)
	exists(false)
	app.reportSessionReady(true, app.sessionObservation)
	exists(true)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("paused adoption created frame readiness")
	}
	app.reportSessionReady(true, app.sessionObservation-1)
	exists(false)
	app.reportSessionReady(true, app.sessionObservation)
	exists(true)
	failed.Store(true)
	app.fetchSession(t.Context())
	exists(false)
	app.reportSessionReady(true, app.sessionObservation)
	exists(false)
	failed.Store(false)
	app.fetchSession(t.Context())
	app.reportSessionReady(true, app.sessionObservation)
	exists(true)
	app.mu.Lock()
	app.sessionObservedAt = time.Now().Add(-6 * time.Second)
	app.mu.Unlock()
	app.reportSessionReady(true, app.sessionObservation)
	exists(false)
	app.fetchSession(t.Context())
	app.reportSessionReady(true, app.sessionObservation)
	exists(true)
	app.mu.Lock()
	app.applySessionLocked(hostclient.SessionResult{State: "idle"})
	app.mu.Unlock()
	exists(false)
	app.reportSessionReady(true, app.sessionObservation)
	exists(false)
}
