package tenfoot

import (
	"encoding/json"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppEntersAttractAndDismissesOnInput(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 200, G: 20, B: 20, A: 255})
	var attractCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{
					availableGame("snes-mario", "Mario", "snes"),
					availableGame("megadrive-sonic", "Sonic", "megadrive"),
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			attractCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 60,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"backdrop":   handle,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})

	app.mu.Lock()
	app.attractIdle = 20 * time.Millisecond
	app.lastInput = time.Now().Add(-time.Second)
	app.mu.Unlock()

	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.Title == "Mario" && snap.Attract.Image != nil
	})
	if attractCalls.Load() < 1 {
		t.Fatal("did not GET /api/v1/library/attract")
	}

	app.Press(CmdRight, time.Now())
	snap := app.Snapshot()
	if snap.Attract.Active {
		t.Fatal("attract still active after input")
	}
	if got, ok := app.Selected(); !ok || got.ID != "snes-mario" {
		t.Fatalf("dismiss should not also move focus, selected=%#v ok=%v", got, ok)
	}
}

func TestAppSkipsAttractWhileSessionActive(t *testing.T) {
	var attractCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			attractCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "idle_seconds": 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.GPUParked
	})

	app.mu.Lock()
	app.attractIdle = time.Millisecond
	app.lastInput = time.Now().Add(-time.Hour)
	app.mu.Unlock()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().Attract.Active {
			t.Fatal("attract ran while session active")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if attractCalls.Load() != 0 {
		t.Fatalf("attract API called %d times while session active", attractCalls.Load())
	}
}

func TestAppSkipsAttractWhenDisabledOrModal(t *testing.T) {
	var attractCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/library/attract" {
			attractCalls.Add(1)
		}
		if r.URL.Path == "/api/v1/games" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.SetAttractDisabled(true)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1
	})
	app.mu.Lock()
	app.attractIdle = time.Millisecond
	app.lastInput = time.Now().Add(-time.Hour)
	app.mu.Unlock()
	app.Tick(time.Now())
	if app.Snapshot().Attract.Active {
		t.Fatal("disabled attract became active")
	}

	app.SetAttractDisabled(false)
	app.Press(CmdSearch, time.Now())
	app.mu.Lock()
	app.lastInput = time.Now().Add(-time.Hour)
	app.mu.Unlock()
	app.Tick(time.Now())
	if app.Snapshot().Attract.Active {
		t.Fatal("attract ran while search open")
	}
	if attractCalls.Load() != 0 {
		t.Fatalf("attract API called %d times while blocked", attractCalls.Load())
	}
}

func TestAppAttractSelectLaunchesLaunchableItem(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	var launches []string
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 60,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"cover":      handle,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			launches = append(launches, string(raw))
			sessionJSON = `{"state":"active","game_id":"snes-mario"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
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

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	app.mu.Lock()
	app.attractIdle = 20 * time.Millisecond
	app.lastInput = time.Now().Add(-time.Second)
	app.mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok" || snap.Session.State == "active"
	})
	mu.Lock()
	got := append([]string(nil), launches...)
	mu.Unlock()
	if len(got) != 1 || got[0] != `{"game_id":"snes-mario"}` {
		t.Fatalf("launches = %#v", got)
	}
	if app.Snapshot().Attract.Active {
		t.Fatal("attract still active after select launch")
	}
}

func TestOptionsSmokeDisablesAttract(t *testing.T) {
	t.Parallel()
	opts := Options{Smoke: true, APIBase: "http://127.0.0.1:8787"}.normalized()
	if !opts.NoAttract || !opts.Hidden {
		t.Fatalf("opts = %#v", opts)
	}
}
