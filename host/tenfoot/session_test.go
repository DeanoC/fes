package tenfoot

import (
	"encoding/json"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppPollsSessionParksAndStops(t *testing.T) {
	handle := strings.Repeat("cd", 32)
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	var launches, stops int
	var stopBody string
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	mario := availableGame("snes-mario", "Mario", "snes")
	mario.Cover = handle
	sonic := availableGame("megadrive-sonic", "Sonic", "megadrive")
	sonic.Cover = handle
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{mario, sonic},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			launches++
			sessionJSON = `{"state":"active","game_id":"snes-mario","system":"snes","execution":"fpga_native","media":"active"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","system":"snes","execution":"fpga_native","media":"active"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			stops++
			stopBody = string(raw)
			sessionJSON = `{"state":"idle","media":"stopped"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/presentation/games/")
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: id,
				State:  "ready",
				Presentation: &struct {
					CoverArtworkID string `json:"cover_artwork_id"`
					Summary        string `json:"summary"`
					Year           string `json:"year"`
					Genre          string `json:"genre"`
					Studio         string `json:"studio"`
				}{CoverArtworkID: handle},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 2 && !snap.Loading
	})
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.CoverHits >= 1
	})
	if app.Snapshot().GPUParked {
		t.Fatal("gpu parked while idle")
	}

	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active" && snap.Session.GameID == "snes-mario"
	})
	snap := app.Snapshot()
	if snap.CoverHits != 0 {
		t.Fatalf("active cover hits = %d", snap.CoverHits)
	}
	if snap.Session.Title != "Mario" {
		t.Fatalf("now playing title = %q", snap.Session.Title)
	}
	if snap.Session.Execution != "fpga_native" || snap.Session.Media != "active" {
		t.Fatalf("session overlays = %#v", snap.Session)
	}
	chrome := snap.ChromeLine()
	if !strings.Contains(chrome, "Now playing") || !strings.Contains(chrome, "Mario") || !strings.Contains(chrome, "active") {
		t.Fatalf("chrome = %q", chrome)
	}

	app.Press(CmdSelect, time.Now())
	app.Press(CmdRight, time.Now())
	mu.Lock()
	launchCount := launches
	mu.Unlock()
	if launchCount != 1 {
		t.Fatalf("second launch posted, launches=%d", launchCount)
	}

	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.GPUParked && snap.Session.State != "active"
	})
	mu.Lock()
	stopCount := stops
	body := stopBody
	mu.Unlock()
	if stopCount != 1 || body != "" {
		t.Fatalf("stops=%d body=%q", stopCount, body)
	}

	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.CoverHits >= 1
	})
	app.Press(CmdRight, time.Now())
	if got, ok := app.Selected(); !ok || got.ID != "megadrive-sonic" {
		t.Fatalf("browse after idle selected = %#v ok=%v", got, ok)
	}
}

func TestAppSessionPollObservesExternalStopAndPark(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			t.Errorf("stop posted during poll-only test")
			http.Error(w, "unexpected stop", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})

	mu.Lock()
	sessionJSON = `{"state":"active","game_id":"snes-mario","execution":"host_only"}`
	mu.Unlock()
	app.kickSessionPollLocked()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active" && snap.Session.GameID == "snes-mario"
	})

	mu.Lock()
	sessionJSON = `{"state":"idle"}`
	mu.Unlock()
	app.kickSessionPollLocked()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.GPUParked && snap.Session.State != "active"
	})
}

func TestAppStopKeyboardBindingAndBlockedLaunch(t *testing.T) {
	var mu sync.Mutex
	var stops int
	sessionJSON := `{"state":"idle"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			mu.Lock()
			stops++
			sessionJSON = `{"state":"idle","media":"stopped"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active"
	})
	if CommandFromKey("s") != CmdStop {
		t.Fatal("s should map to stop")
	}
	app.Press(CmdStop, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State != "active" && !snap.GPUParked
	})
	mu.Lock()
	n := stops
	mu.Unlock()
	if n != 1 {
		t.Fatalf("stops = %d", n)
	}
}

func TestAppIgnoresStaleSessionPollAfterLaunch(t *testing.T) {
	app, releaseHeldPoll := startHeldSessionApp(t, `{"state":"idle"}`)
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active"
	})
	releaseHeldPoll()
	time.Sleep(50 * time.Millisecond)
	snap := app.Snapshot()
	if !snap.GPUParked || snap.Session.State != "active" {
		t.Fatalf("stale idle poll unparked session = %#v parked=%v", snap.Session, snap.GPUParked)
	}
}

func TestAppIgnoresStaleSessionPollAfterStop(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	hold := false
	started := make(chan string, 1)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	app := startSessionApp(t, &mu, &sessionJSON, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/session" {
			return false
		}
		mu.Lock()
		body := sessionJSON
		shouldHold := hold
		mu.Unlock()
		if shouldHold {
			select {
			case started <- body:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return true
			}
		}
		_, _ = io.WriteString(w, body)
		return true
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active"
	})

	mu.Lock()
	hold = true
	mu.Unlock()
	app.kickSessionPollLocked()
	select {
	case body := <-started:
		if !strings.Contains(body, `"active"`) {
			t.Fatalf("held active poll body = %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pre-stop poll")
	}

	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.GPUParked && snap.Session.State != "active"
	})
	close(release)
	time.Sleep(50 * time.Millisecond)
	snap := app.Snapshot()
	if snap.GPUParked || snap.Session.State == "active" {
		t.Fatalf("stale active poll re-parked session = %#v parked=%v", snap.Session, snap.GPUParked)
	}
}

func startHeldSessionApp(t *testing.T, heldBody string) (*App, func()) {
	t.Helper()
	var mu sync.Mutex
	sessionJSON := heldBody
	hold := false
	started := make(chan string, 1)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	app := startSessionApp(t, &mu, &sessionJSON, func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/session" {
			return false
		}
		mu.Lock()
		body := sessionJSON
		shouldHold := hold
		mu.Unlock()
		if shouldHold {
			select {
			case started <- body:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return true
			}
		}
		_, _ = io.WriteString(w, body)
		return true
	})
	mu.Lock()
	hold = true
	mu.Unlock()
	app.kickSessionPollLocked()
	select {
	case body := <-started:
		if body != heldBody {
			t.Fatalf("held poll body = %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for held session poll")
	}
	return app, func() {
		mu.Lock()
		hold = false
		mu.Unlock()
		select {
		case <-release:
		default:
			close(release)
		}
	}
}

func startSessionApp(t *testing.T, mu *sync.Mutex, sessionJSON *string, sessionGET func(http.ResponseWriter, *http.Request) bool) *App {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessionGET != nil && sessionGET(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := *sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			*sessionJSON = `{"state":"active","game_id":"snes-mario"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			mu.Lock()
			*sessionJSON = `{"state":"idle","media":"stopped"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	return app
}

func TestAppDoesNotInventGameIDForGameLessActiveSession(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			mu.Lock()
			sessionJSON = `{"state":"idle","media":"stopped"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.Session.GameID == "snes-mario"
	})
	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State != "active" && snap.Launch.GameID == ""
	})

	mu.Lock()
	sessionJSON = `{"state":"active","execution":"fpga_development"}`
	mu.Unlock()
	app.kickSessionPollLocked()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active"
	})
	snap := app.Snapshot()
	if snap.Session.GameID != "" || snap.Session.Title != "" {
		t.Fatalf("invented game id for game-less session = %#v", snap.Session)
	}
}

func TestAppDoesNotKeepLaunchGameIDWhenPollOmitsIt(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.Session.GameID == "snes-mario"
	})

	mu.Lock()
	sessionJSON = `{"state":"active","execution":"fpga_development"}`
	mu.Unlock()
	app.kickSessionPollLocked()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.Session.Execution == "fpga_development" && snap.Session.GameID == ""
	})
}

func TestAppFillsGameIDFromLaunchResponseOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok" && snap.Session.State == "active" && snap.Session.GameID == "snes-mario"
	})
}

func TestAppStatusPrefersStopErrorAndProgressOverLaunchOK(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario"}`
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":"TARGET_BUSY","message":"target is busy"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok" && snap.Session.State == "active"
	})
	if status := app.Snapshot().Status; !strings.Contains(status, "host accepted launch") {
		t.Fatalf("launch status = %q", status)
	}
	mu.Lock()
	sessionJSON = `{"state":"active","game_id":"snes-mario","progress":{"stage":"core","message":"loading core"}}`
	mu.Unlock()
	app.kickSessionPollLocked()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return strings.Contains(snap.Status, "loading core")
	})
	if status := app.Snapshot().Status; strings.Contains(status, "host accepted launch") {
		t.Fatalf("progress hidden behind launch ok: %q", status)
	}

	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return strings.Contains(snap.Status, "stop failed") || strings.Contains(snap.Status, "TARGET_BUSY")
	})
	snap := app.Snapshot()
	if strings.Contains(snap.Status, "host accepted launch") {
		t.Fatalf("stop error hidden behind launch ok: %q", snap.Status)
	}
	if !snap.GPUParked || snap.Session.State != "active" {
		t.Fatalf("failed stop should keep session active = %#v parked=%v", snap.Session, snap.GPUParked)
	}
}
