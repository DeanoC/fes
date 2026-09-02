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
