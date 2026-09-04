package tenfoot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTenfootPrefsAttractRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "FogCast", "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, Layout: "grid", AttractEnabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if prefsAttractEnabled(got) {
		t.Fatalf("attract should be off: %#v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"attract_enabled": false`) {
		t.Fatalf("json = %s", raw)
	}
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, Layout: "shelf"}); err != nil {
		t.Fatal(err)
	}
	got, err = loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if !prefsAttractEnabled(got) {
		t.Fatalf("missing attract_enabled should default on: %#v", got)
	}
}

func TestOptionsNormalizedLoadsAttractPref(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, Layout: "grid", AttractEnabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	opts := Options{PrefsPath: path}.normalized()
	if !opts.NoAttract || opts.NoAttractSet {
		t.Fatalf("pref off = %#v", opts)
	}
	opts = Options{PrefsPath: path, NoAttract: true, NoAttractSet: true}.normalized()
	if !opts.NoAttract || !opts.NoAttractSet {
		t.Fatalf("flag override = %#v", opts)
	}
}

func TestOptionsNormalizedEnvOverridesAttractPref(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, AttractEnabled: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOGCAST_TENFOOT_NO_ATTRACT", "1")
	opts := Options{PrefsPath: path}.normalized()
	if !opts.NoAttract || !opts.NoAttractSet {
		t.Fatalf("env override = %#v", opts)
	}
}

func catalogSettingsApp(t *testing.T, n int) *App {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/settings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"attract_idle_seconds": 60,
			"preferred_regions":    []string{"usa", "world", "europe", "japan"},
			"selected_target":      "dev",
			"targets":              []map[string]any{{"name": "dev"}},
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, n)
	app.games = make([]Game, n)
	for i := 0; i < n; i++ {
		app.games[i] = Game{ID: "g" + strconv.Itoa(i), Title: "Game"}
	}
	app.grid.SetCount(n)
	return app
}

func TestAppSettingsOverlayOpenCloseAndLocalPrefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app := catalogSettingsApp(t, 8)
	app.SetPrefsPath(path)
	app.SetSafeAreaPct(0.05)
	app.ConfigureAttract(false, false)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	if !app.SettingsOpen() {
		t.Fatal("settings should open")
	}
	snap := app.Snapshot()
	if !snap.Settings.Open || snap.Settings.Index != 0 || len(snap.Settings.Rows) != settingsRowCount {
		t.Fatalf("snapshot = %#v", snap.Settings)
	}
	app.HandleCommand(CmdRight, now)
	if app.Snapshot().Grid.Mode != LayoutShelf {
		t.Fatalf("layout = %s", app.Snapshot().Grid.Mode)
	}
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdRight, now)
	if app.Snapshot().SafeAreaPct < 0.054 {
		t.Fatalf("safe-area = %v", app.Snapshot().SafeAreaPct)
	}
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	if app.Snapshot().Attract.Active {
		t.Fatal("attract started while settings open")
	}
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != "shelf" || prefsAttractEnabled(got) {
		t.Fatalf("prefs = %#v", got)
	}
	if got.SafeAreaPct < 0.054 {
		t.Fatalf("safe-area pref = %v", got.SafeAreaPct)
	}
	app.HandleCommand(CmdBack, now)
	if app.SettingsOpen() {
		t.Fatal("east should close")
	}
	app.HandleCommand(CmdSettings, now)
	app.HandleCommand(CmdSettings, now)
	if app.SettingsOpen() {
		t.Fatal("guide toggle should close")
	}
}

func TestAppSettingsDoesNotArmAttract(t *testing.T) {
	app := catalogSettingsApp(t, 3)
	app.attractIdle = 20 * time.Millisecond
	app.attractIdleReady = true
	app.lastInput = time.Unix(0, 0)
	app.HandleCommand(CmdSettings, time.Unix(1, 0))
	if !app.SettingsOpen() {
		t.Fatal("open")
	}
	app.Tick(time.Unix(1, 0).Add(time.Second))
	if app.Snapshot().Attract.Active {
		t.Fatal("attract ran while settings open")
	}
}

func TestAppSettingsEastDiscardsHostDrafts(t *testing.T) {
	var mu sync.Mutex
	var patches int
	idle := 60
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": idle,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}, {"name": "spare"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/library/settings":
			mu.Lock()
			patches++
			mu.Unlock()
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if v, ok := body["attract_idle_seconds"].(float64); ok {
				idle = int(v)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": idle,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}, {"name": "spare"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 8)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading && snap.Settings.Rows[settingsRowIdle].Value == "60s"
	})
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdDown, now)
	if app.Snapshot().Settings.Index != settingsRowIdle {
		t.Fatalf("index = %d", app.Snapshot().Settings.Index)
	}
	app.HandleCommand(CmdRight, now)
	if app.Snapshot().Settings.Rows[settingsRowIdle].Value != "75s" {
		t.Fatalf("draft = %q", app.Snapshot().Settings.Rows[settingsRowIdle].Value)
	}
	app.HandleCommand(CmdBack, now)
	mu.Lock()
	gotPatches := patches
	mu.Unlock()
	if gotPatches != 0 {
		t.Fatalf("east patched %d times", gotPatches)
	}
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	if app.Snapshot().Settings.Rows[settingsRowIdle].Value != "60s" {
		t.Fatalf("draft leaked = %q", app.Snapshot().Settings.Rows[settingsRowIdle].Value)
	}
}

func TestAppSettingsPatchIdleRegionsAndTarget(t *testing.T) {
	var mu sync.Mutex
	state := map[string]any{
		"attract_idle_seconds": 60,
		"preferred_regions":    []string{"usa", "world", "europe", "japan"},
		"selected_target":      "dev",
		"targets":              []map[string]any{{"name": "dev", "enabled": true, "agent_configured": true}, {"name": "spare", "enabled": false, "agent_configured": false}},
		"libraries":            []map[string]any{{"id": "snes", "system": "snes", "root": "/snes"}},
		"systems":              []map[string]any{{"id": "snes", "label": "SNES"}},
	}
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/games" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{{ID: "g0", Title: "Game", Launchable: true}},
			})
			return
		}
		if r.URL.Path != "/api/v1/library/settings" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			mu.Lock()
			_ = json.NewEncoder(w).Encode(state)
			mu.Unlock()
			return
		}
		if r.Method != http.MethodPatch {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		for k, v := range body {
			state[k] = v
		}
		_ = json.NewEncoder(w).Encode(state)
		mu.Unlock()
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 8)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading && snap.Settings.LibraryCount == 1
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Settings.Busy && strings.Contains(snap.Status, "idle 75s")
	})
	if app.attractIdleSeconds != 75 {
		t.Fatalf("attract idle not hydrated: %d", app.attractIdleSeconds)
	}
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Settings.Busy
	})
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Settings.Busy && strings.Contains(snap.Status, "target spare")
	})
	mu.Lock()
	got := append([]string(nil), bodies...)
	mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("bodies = %#v", got)
	}
	if got[0] != `{"attract_idle_seconds":75}` || !strings.Contains(got[1], `"preferred_regions"`) || got[2] != `{"selected_target":"spare"}` {
		t.Fatalf("bodies = %#v", got)
	}
}

func TestAppSettingsPatchKeepsPriorOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}, {"name": "spare"}},
			})
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"error":{"code":"SESSION_ACTIVE","message":"selected target cannot change while a session is active"}}`)
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 8)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	for i := 0; i < settingsRowTarget; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Settings.Busy && strings.Contains(snap.Status, "selected target cannot change")
	})
	if app.Snapshot().Settings.Rows[settingsRowTarget].Value != "dev" {
		t.Fatalf("target reverted = %q", app.Snapshot().Settings.Rows[settingsRowTarget].Value)
	}
}

func TestAppSettingsAttractFlagOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app := catalogSettingsApp(t, 4)
	app.SetPrefsPath(path)
	app.ConfigureAttract(true, true)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdDown, now)
	if app.Snapshot().Settings.Rows[settingsRowAttract].Value != "Off (flag)" {
		t.Fatalf("value = %q", app.Snapshot().Settings.Rows[settingsRowAttract].Value)
	}
	app.HandleCommand(CmdSelect, now)
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if prefsAttractEnabled(got) {
		t.Fatalf("toggle should persist off: %#v", got)
	}
	if !app.attractDisabled {
		t.Fatal("flag must keep attract disabled")
	}
}

func TestAppLayoutCycleAndSafeAreaWorkWithSettingsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app := catalogApp(6)
	app.SetPrefsPath(path)
	app.SetSafeAreaPct(0.05)
	app.HandleCommand(CmdLayoutCycle, time.Now())
	app.HandleCommand(CmdSafeAreaIn, time.Now())
	if app.Snapshot().Grid.Mode != LayoutShelf {
		t.Fatalf("mode = %s", app.Snapshot().Grid.Mode)
	}
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != "shelf" || got.SafeAreaPct < 0.054 {
		t.Fatalf("prefs = %#v", got)
	}
}

func TestPersistLayoutKeepsAttractPref(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, Layout: "grid", AttractEnabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	app := catalogApp(4)
	app.SetPrefsPath(path)
	app.HandleCommand(CmdLayoutCycle, time.Now())
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if prefsAttractEnabled(got) || got.Layout != "shelf" {
		t.Fatalf("prefs = %#v", got)
	}
}

func TestTogglePreferredRegionKeepsOne(t *testing.T) {
	t.Parallel()
	got, ok := togglePreferredRegion([]string{"usa"}, "usa")
	if ok || strings.Join(got, ",") != "usa" {
		t.Fatalf("removed last: %v %#v", ok, got)
	}
	got, ok = togglePreferredRegion([]string{"usa", "japan"}, "usa")
	if !ok || strings.Join(got, ",") != "japan" {
		t.Fatalf("toggle off = %v %#v", ok, got)
	}
	got, ok = togglePreferredRegion([]string{"usa"}, "japan")
	if !ok || strings.Join(got, ",") != "usa,japan" {
		t.Fatalf("toggle on = %v %#v", ok, got)
	}
}

func TestAttractForcedOnlyWhenDisabled(t *testing.T) {
	t.Parallel()
	if (Options{NoAttract: false, NoAttractSet: true}).attractForced() {
		t.Fatal("explicit -no-attract=false must not force attract off")
	}
	if !(Options{NoAttract: true, NoAttractSet: true}).attractForced() {
		t.Fatal("flag/env disable must force attract off")
	}
	if (Options{NoAttract: true, NoAttractSet: false}).attractForced() {
		t.Fatal("saved pref off is not a CLI force")
	}
}

func TestOptionsNormalizedExplicitFalseOverridesSavedOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, AttractEnabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	opts := Options{PrefsPath: path, NoAttract: false, NoAttractSet: true}.normalized()
	if opts.NoAttract || !opts.NoAttractSet || opts.attractForced() {
		t.Fatalf("explicit false = %#v", opts)
	}
	app := NewApp(nil, 1280, 720, 4)
	app.SetPrefsPath(path)
	app.ConfigureAttract(opts.NoAttract, opts.attractForced())
	if app.attractDisabled || app.attractForcedOff || !app.attractPrefEnabled {
		t.Fatalf("runtime = disabled=%v forced=%v pref=%v", app.attractDisabled, app.attractForcedOff, app.attractPrefEnabled)
	}
}

func TestAppSettingsCloseIgnoresFailedGetStatus(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings" {
			close(started)
			<-release
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":"SETTINGS_UNAVAILABLE","message":"library settings are unavailable"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	app.status = "browse ready"
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("GET did not start")
	}
	app.HandleCommand(CmdBack, now)
	if app.SettingsOpen() {
		t.Fatal("should close")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(app.Snapshot().Status, "settings failed") {
			t.Fatalf("closed overlay adopted GET error: %q", app.Snapshot().Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if app.Snapshot().Status != "browse ready" {
		t.Fatalf("status = %q", app.Snapshot().Status)
	}
}

func TestAppSettingsPatchAppliesAfterClose(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/library/settings":
			close(started)
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 75,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("PATCH did not start")
	}
	app.HandleCommand(CmdBack, now)
	if app.SettingsOpen() {
		t.Fatal("should close")
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if app.attractIdleSeconds == 75 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("idle after close = %d", app.attractIdleSeconds)
}

func TestAppSettingsIdlePatchInvalidatesAttractFetch(t *testing.T) {
	attractStarted := make(chan struct{})
	attractRelease := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			close(attractStarted)
			<-attractRelease
			_ = json.NewEncoder(w).Encode(map[string]any{"idle_seconds": 5, "items": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 75,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	app.mu.Lock()
	app.startAttractFetchLocked(false)
	app.mu.Unlock()
	select {
	case <-attractStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("attract GET did not start")
	}
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Settings.Busy && app.attractIdleSeconds == 75
	})
	close(attractRelease)
	time.Sleep(50 * time.Millisecond)
	if app.attractIdleSeconds != 75 {
		t.Fatalf("stale attract GET overwrote idle: %d", app.attractIdleSeconds)
	}
}

func TestAppSettingsStalePatchDoesNotClearNewBusy(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondStarted := make(chan struct{})
	secondRelease := make(chan struct{})
	var mu sync.Mutex
	n := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/library/settings":
			mu.Lock()
			n++
			id := n
			mu.Unlock()
			raw, _ := io.ReadAll(r.Body)
			idle := 75
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if v, ok := body["attract_idle_seconds"].(float64); ok {
				idle = int(v)
			}
			if id == 1 {
				close(firstStarted)
				<-firstRelease
			} else {
				close(secondStarted)
				<-secondRelease
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": idle,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first PATCH did not start")
	}
	app.HandleCommand(CmdBack, now)
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("second PATCH did not start")
	}
	if !app.Snapshot().Settings.Busy {
		t.Fatal("new save should be busy")
	}
	close(firstRelease)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := app.Snapshot()
		if !snap.Settings.Busy {
			t.Fatal("stale PATCH cleared the new save busy flag")
		}
		if snap.Settings.Rows[settingsRowIdle].Value != "90s" {
			t.Fatalf("stale PATCH overwrote draft: %q", snap.Settings.Rows[settingsRowIdle].Value)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(secondRelease)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Settings.Busy && app.attractIdleSeconds == 90
	})
}

func TestClampSettingsIdleAllowsHostMinimum(t *testing.T) {
	t.Parallel()
	if got := clampSettingsIdle(1); got != 1 {
		t.Fatalf("1 = %d", got)
	}
	if got := clampSettingsIdle(4); got != 4 {
		t.Fatalf("4 = %d", got)
	}
	if got := clampSettingsIdle(0); got != minSettingsIdleSeconds {
		t.Fatalf("0 = %d", got)
	}
	if got := clampSettingsIdle(-12); got != minSettingsIdleSeconds {
		t.Fatalf("negative = %d", got)
	}
	if minSettingsIdleSeconds != 1 {
		t.Fatalf("floor = %d want 1", minSettingsIdleSeconds)
	}
}

func TestAppSettingsLeftOnLowIdleDoesNotJumpToFive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/library/settings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"attract_idle_seconds": 3,
			"preferred_regions":    []string{"usa"},
			"selected_target":      "dev",
			"targets":              []map[string]any{{"name": "dev"}},
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading && snap.Settings.Rows[settingsRowIdle].Value == "3s"
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdLeft, now)
	if got := app.Snapshot().Settings.Rows[settingsRowIdle].Value; got != "1s" {
		t.Fatalf("left from 3s = %q, want 1s (not 5s)", got)
	}
}

func TestAppSettingsRehydratesAfterSaveCompletesDuringReopen(t *testing.T) {
	var mu sync.Mutex
	idle := 60
	var gets, patches int
	getRelease := make(chan struct{})
	patchStarted := make(chan struct{})
	patchRelease := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-getRelease:
		default:
			close(getRelease)
		}
		select {
		case <-patchRelease:
		default:
			close(patchRelease)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			mu.Lock()
			gets++
			n := gets
			current := idle
			mu.Unlock()
			if n >= 2 {
				<-getRelease
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": current,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/library/settings":
			mu.Lock()
			patches++
			mu.Unlock()
			close(patchStarted)
			<-patchRelease
			mu.Lock()
			idle = 75
			current := idle
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": current,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading && snap.Settings.Rows[settingsRowIdle].Value == "60s"
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	select {
	case <-patchStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("PATCH did not start")
	}
	app.HandleCommand(CmdBack, now)
	app.HandleCommand(CmdSettings, now)
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := gets
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reopen GET did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(patchRelease)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if app.attractIdleSeconds == 75 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if app.attractIdleSeconds != 75 {
		t.Fatalf("PATCH did not apply idle, got %d", app.attractIdleSeconds)
	}
	close(getRelease)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading && app.settingsHydrated && snap.Settings.Rows[settingsRowIdle].Value == "75s"
	})
	if got := app.Snapshot().Settings.Rows[settingsRowIdle].Value; got == "—" {
		t.Fatal("overlay stayed unhydrated")
	}
}

func TestAppSettingsPreservesHostIdleAboveTenMinutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/settings" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"attract_idle_seconds": 3600,
			"preferred_regions":    []string{"usa"},
			"selected_target":      "dev",
			"targets":              []map[string]any{{"name": "dev"}},
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading && snap.Settings.Rows[settingsRowIdle].Value == "3600s"
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	if app.Snapshot().Settings.Rows[settingsRowIdle].Value != "3615s" {
		t.Fatalf("right from 3600 = %q", app.Snapshot().Settings.Rows[settingsRowIdle].Value)
	}
}

func TestAppSettingsBusyBlocksHostRowEdits(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/library/settings":
			close(started)
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 75,
				"preferred_regions":    []string{"usa"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.games = []Game{{ID: "g0", Title: "Game"}}
	app.grid.SetCount(1)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	for i := 0; i < settingsRowIdle; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("PATCH did not start")
	}
	app.HandleCommand(CmdRight, now)
	if app.Snapshot().Settings.Rows[settingsRowIdle].Value != "75s" {
		t.Fatalf("busy edit = %q", app.Snapshot().Settings.Rows[settingsRowIdle].Value)
	}
	close(release)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Settings.Busy
	})
}

func TestAppSettingsRegionPatchReloadsCatalog(t *testing.T) {
	var mu sync.Mutex
	var games int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			mu.Lock()
			games++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{{ID: "g0", Title: "Game", Launchable: true}},
			})
		case r.URL.Path == "/api/v1/library/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 60,
				"preferred_regions":    []string{"usa", "world", "europe", "japan"},
				"selected_target":      "dev",
				"targets":              []map[string]any{{"name": "dev"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 4)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Settings.Open && !snap.Settings.Loading
	})
	for i := 0; i < settingsRowRegions; i++ {
		app.HandleCommand(CmdDown, now)
	}
	mu.Lock()
	before := games
	mu.Unlock()
	app.HandleCommand(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		defer mu.Unlock()
		return games > before
	})
}
