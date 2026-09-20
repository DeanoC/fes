package tenfoot

import (
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func TestOpenDevelopmentRBFFileRejectsEmptyAndDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.rbf")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openDevelopmentRBFFile(empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty file err = %v", err)
	}
	if _, _, err := openDevelopmentRBFFile(dir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory err = %v", err)
	}
	if err := developmentRBFSizeError(protocol.MaxDevelopmentRBFBytes + 1); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize err = %v", err)
	}
	okPath := filepath.Join(dir, "core.rbf")
	if err := os.WriteFile(okPath, []byte("rbf"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, size, err := openDevelopmentRBFFile(okPath)
	if err != nil || size != 3 {
		t.Fatalf("open = size %d err %v", size, err)
	}
	_ = file.Close()
}

func TestDiagnosticNowPlayingLineIsNotPlay(t *testing.T) {
	t.Parallel()
	line := diagnosticNowPlayingLine(SessionSnapshot{
		Chrome:           sessionChromeDevelopment,
		Execution:        "fpga_development",
		DevelopmentState: "fpga_development",
		Diagnostic:       true,
	})
	if strings.Contains(line, "Now playing") {
		t.Fatalf("play copy leaked: %q", line)
	}
	if !strings.Contains(line, diagnosticLabel) || !strings.Contains(line, "not a game session") || !strings.Contains(line, "HDMI/input may be down") {
		t.Fatalf("diagnostic copy missing: %q", line)
	}
}

func TestAppDevelopmentPathOSKLoadsAndStops(t *testing.T) {
	dir := t.TempDir()
	rbf := filepath.Join(dir, "core.rbf")
	payload := []byte("development-rbf")
	if err := os.WriteFile(rbf, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	var uploads int
	var gotCT string
	var gotLen int64
	var gotBody []byte
	var stops int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
				"libraries":            []map[string]any{},
				"systems":              []map[string]any{{"id": "snes", "label": "SNES"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/development-rbf":
			mu.Lock()
			uploads++
			gotCT = r.Header.Get("Content-Type")
			gotLen = r.ContentLength
			raw, _ := io.ReadAll(r.Body)
			gotBody = raw
			sessionJSON = `{"state":"active","execution":"fpga_development","development":true}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","execution":"fpga_development","development":true}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			mu.Lock()
			stops++
			sessionJSON = `{"state":"idle"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			t.Errorf("catalog launch during development test")
			http.Error(w, "unexpected launch", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	row := settingsRowByID(t, app, "development-rbf")
	if !strings.Contains(row.Label, "DIAGNOSTIC") || strings.Contains(strings.ToLower(row.Label), "play") {
		t.Fatalf("row = %#v", row)
	}
	focusSettingsRow(t, app, "development-rbf", now)
	if !strings.Contains(app.Snapshot().Settings.Hint, "not a game session") {
		t.Fatalf("hint = %q", app.Snapshot().Settings.Hint)
	}
	app.HandleCommand(CmdSelect, now)
	if !app.OSKOpen() {
		t.Fatal("path OSK")
	}
	if prompt := app.Snapshot().OSK.Prompt; !strings.Contains(prompt, "DIAGNOSTIC") {
		t.Fatalf("prompt = %q", prompt)
	}
	app.TypeText(rbf, now)
	app.ConfirmSearch(now)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active" && snap.Session.Diagnostic
	})
	snap := app.Snapshot()
	if !snap.Session.DevelopmentActive || snap.Session.Chrome != sessionChromeDevelopment {
		t.Fatalf("session = %#v", snap.Session)
	}
	chrome := snap.ChromeLine()
	if strings.Contains(chrome, "Now playing") || !strings.Contains(chrome, diagnosticLabel) || !strings.Contains(chrome, "not a game session") {
		t.Fatalf("chrome = %q", chrome)
	}
	mu.Lock()
	ct, length, body, n := gotCT, gotLen, append([]byte(nil), gotBody...), uploads
	mu.Unlock()
	if n != 1 || ct != "application/octet-stream" || length != int64(len(payload)) || string(body) != string(payload) {
		t.Fatalf("upload ct=%q len=%d body=%q n=%d", ct, length, body, n)
	}
	app.Press(CmdSelect, now)
	mu.Lock()
	if uploads != 1 {
		t.Fatalf("launch/replace during development uploads=%d", uploads)
	}
	mu.Unlock()
	app.Press(CmdBack, now)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.GPUParked && snap.Session.State != "active"
	})
	mu.Lock()
	if stops != 1 {
		t.Fatalf("stops=%d", stops)
	}
	mu.Unlock()
	if status := app.Snapshot().Status; strings.Contains(status, "DIAGNOSTIC RBF loaded") {
		t.Fatalf("idle still showing loaded: %q", status)
	}
}

func TestAppDevelopmentPathOSKRejectsEmptyFileBeforeUpload(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.rbf")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var uploads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
				"libraries":            []map[string]any{},
				"systems":              []map[string]any{{"id": "snes", "label": "SNES"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/development-rbf":
			uploads++
			http.Error(w, "should not upload", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	focusSettingsRow(t, app, "development-rbf", now)
	app.HandleCommand(CmdSelect, now)
	app.TypeText(empty, now)
	app.ConfirmSearch(now)
	if !app.OSKOpen() {
		t.Fatal("OSK should stay open after reject")
	}
	if uploads != 0 {
		t.Fatalf("uploads=%d", uploads)
	}
	if status := app.Snapshot().Status; !strings.Contains(status, "empty") {
		t.Fatalf("status = %q", status)
	}
}

func TestAppDevelopmentLoadRespectsLeaseBlockedAndRetryStop(t *testing.T) {
	rbf := filepath.Join(t.TempDir(), "core.rbf")
	if err := os.WriteFile(rbf, []byte("rbf"), 0o644); err != nil {
		t.Fatal(err)
	}
	var uploads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
				"libraries":            []map[string]any{},
				"systems":              []map[string]any{{"id": "snes", "label": "SNES"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/development-rbf":
			uploads++
			http.Error(w, "should not upload", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	app.mu.Lock()
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "blocked", ErrorCode: "KIT_LEASE_BLOCKED", Reason: "cleanup failed"}
	app.mu.Unlock()
	focusSettingsRow(t, app, "development-rbf", now)
	app.HandleCommand(CmdSelect, now)
	if app.OSKOpen() {
		t.Fatal("path OSK opened despite blocked lease")
	}
	if status := app.Snapshot().Status; !strings.Contains(status, "blocked") && !strings.Contains(status, "lease") {
		t.Fatalf("blocked status = %q", status)
	}
	if strings.Contains(strings.ToLower(app.Snapshot().Status), "takeover") {
		t.Fatalf("auto-takeover copy: %q", app.Snapshot().Status)
	}
	app.HandleCommand(CmdBack, now)

	app.mu.Lock()
	app.kitLeaseHave = false
	app.kitLease = KitLeaseStatus{}
	app.retryStopLock = true
	app.retryStopHint = retryStopHint
	app.session.State = "active"
	app.syncGPUParkLocked()
	app.mu.Unlock()
	app.HandleCommand(CmdSettings, now)
	if app.SettingsOpen() {
		t.Fatal("settings opened during retry-Stop lockout")
	}
	if uploads != 0 {
		t.Fatalf("uploads=%d", uploads)
	}
}

func TestAppPollDevelopmentSessionShowsDiagnosticChrome(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"active","execution":"fpga_development","development_session_state":"fpga_development"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.Diagnostic && snap.GPUParked
	})
	snap := app.Snapshot()
	if !snap.Session.DevelopmentActive || snap.Session.DevelopmentState != "fpga_development" {
		t.Fatalf("session = %#v", snap.Session)
	}
	if strings.Contains(snap.NowPlayingLine(), "Now playing") {
		t.Fatalf("now playing = %q", snap.NowPlayingLine())
	}
}

func TestAppFailedDevelopmentLoadDoesNotMaskLaterGameSession(t *testing.T) {
	app := NewApp(NewClient("http://127.0.0.1:1", nil), 800, 600, 4)
	app.games = []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}
	app.grid.SetCount(1)
	app.mu.Lock()
	app.devLoadPhase = "error"
	app.devLoadMessage = "diagnostic RBF failed: boom"
	app.status = app.devLoadMessage
	app.mu.Unlock()
	if !strings.Contains(app.Snapshot().Status, "diagnostic RBF failed") {
		t.Fatalf("idle should keep failure: %q", app.Snapshot().Status)
	}
	app.mu.Lock()
	app.applySessionLocked(hostclient.SessionResult{State: "active", GameID: "snes-mario", System: "snes", Execution: "fpga_native"})
	app.mu.Unlock()
	snap := app.Snapshot()
	if strings.Contains(snap.Status, "diagnostic RBF failed") {
		t.Fatalf("game session masked by stale diagnostic: %q", snap.Status)
	}
	if snap.Session.State != "active" || snap.Session.Diagnostic {
		t.Fatalf("session = %#v", snap.Session)
	}
}

func TestAppSuccessfulDevelopmentLoadClearsOnIdle(t *testing.T) {
	app := NewApp(NewClient("http://127.0.0.1:1", nil), 800, 600, 4)
	app.mu.Lock()
	app.devLoadPhase = "ok"
	app.devLoadMessage = "DIAGNOSTIC RBF loaded · not a game session"
	app.status = app.devLoadMessage
	app.session = hostclient.SessionResult{State: "active", Execution: "fpga_development", Development: true}
	app.mu.Unlock()
	if !strings.Contains(app.Snapshot().NowPlayingLine(), diagnosticLabel) {
		t.Fatalf("missing diagnostic chrome: %q", app.Snapshot().NowPlayingLine())
	}
	app.mu.Lock()
	app.applySessionLocked(hostclient.SessionResult{State: "idle"})
	app.mu.Unlock()
	snap := app.Snapshot()
	if strings.Contains(snap.Status, "DIAGNOSTIC RBF loaded") {
		t.Fatalf("idle kept loaded copy: %q", snap.Status)
	}
	if snap.Session.Diagnostic || snap.GPUParked {
		t.Fatalf("idle still diagnostic/parked = %#v parked=%v", snap.Session, snap.GPUParked)
	}
}
