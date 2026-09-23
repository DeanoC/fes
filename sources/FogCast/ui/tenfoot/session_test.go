package tenfoot

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/shared"
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
				"games": []hostclient.Game{mario, sonic},
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
			_ = json.NewEncoder(w).Encode(hostclient.Presentation{
				GameID:       id,
				State:        "ready",
				Presentation: &hostclient.PresentationInfo{CoverArtworkID: handle},
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
	if stopCount != 1 || body != `{"retain_lease":true}` {
		t.Fatalf("stops=%d body=%q", stopCount, body)
	}
	if chrome := app.Snapshot().ChromeLine(); strings.Contains(chrome, "Now playing") {
		t.Fatalf("soft-stop left playing chrome %q", chrome)
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
				"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")},
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

func TestGPUParkRejectsPreParkCoverResults(t *testing.T) {
	t.Parallel()
	staleHandle := strings.Repeat("ab", 32)
	freshHandle := strings.Repeat("cd", 32)
	staleImg := image.NewRGBA(image.Rect(0, 0, 2, 2))
	freshImg := image.NewRGBA(image.Rect(0, 0, 4, 4))
	app := NewApp(nil, 800, 600, 10)
	parent, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	app.ctx = parent
	jobCtx := app.replaceLoadContextLocked()
	mario := availableGame("snes-mario", "Mario", "snes")
	app.games = []hostclient.Game{mario}
	app.grid.SetCount(1)
	app.covers[mario.ID] = &coverSlot{phase: coverArtwork, handle: staleHandle}
	staleGen := app.loadGen

	app.session.State = "active"
	app.syncGPUParkLocked()
	if !app.gpuParked {
		t.Fatal("session active should park")
	}
	if app.loadGen == staleGen {
		t.Fatal("park should advance cover generation")
	}
	if jobCtx.Err() == nil {
		t.Fatal("park should cancel in-flight cover job context")
	}
	freshGen := app.loadGen

	app.session.State = "idle"
	app.syncGPUParkLocked()
	if app.gpuParked {
		t.Fatal("idle session should unpark")
	}
	if app.loadGen != freshGen {
		t.Fatalf("unpark changed loadGen %d -> %d", freshGen, app.loadGen)
	}

	app.covers[mario.ID] = &coverSlot{phase: coverReady, handle: freshHandle, image: freshImg}
	app.details[mario.ID] = shared.FocusDetail{Summary: "fresh"}
	app.applyResult(workResult{
		kind:        workPresentation,
		gameID:      mario.ID,
		handle:      staleHandle,
		state:       "ready",
		summary:     "stale-pre-park",
		attribution: "Data from IGDB.com",
		gen:         staleGen,
	})
	app.applyResult(workResult{
		kind:   workArtwork,
		gameID: mario.ID,
		handle: staleHandle,
		image:  staleImg,
		gen:    staleGen,
	})
	if got := app.details[mario.ID].Summary; got != "fresh" {
		t.Fatalf("stale presentation applied after unpark: %q", got)
	}
	slot := app.covers[mario.ID]
	if slot == nil || slot.handle != freshHandle || slot.image != freshImg || slot.phase != coverReady {
		t.Fatalf("stale artwork applied after unpark: %#v", slot)
	}

	app.covers = map[string]*coverSlot{}
	app.details = map[string]shared.FocusDetail{}
	app.queueVisibleWork(time.Now())
	select {
	case item := <-app.jobs:
		if item.gen != freshGen {
			t.Fatalf("post-unpark work gen = %d, want %d (pre-park %d)", item.gen, freshGen, staleGen)
		}
		if item.gameID != mario.ID {
			t.Fatalf("queued game = %q", item.gameID)
		}
	default:
		t.Fatal("unpark should queue replacement cover work")
	}
}

func TestAppStopKeyboardBindingAndBlockedLaunch(t *testing.T) {
	var mu sync.Mutex
	var stops int
	sessionJSON := `{"state":"idle"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")},
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
				"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")},
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
				"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")},
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
				"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")},
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
				"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")},
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
				"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")},
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

func TestSessionChromeStateFixtures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, state, execution string
		stopping, retry        bool
		want                   string
	}{
		{name: "idle", state: "idle", want: "idle"},
		{name: "active", state: "active", want: "active"},
		{name: "stopping", state: "active", stopping: true, want: "stopping"},
		{name: "failed", state: "failed", want: "failed"},
		{name: "development", state: "active", execution: "fpga_development", want: "development"},
		{name: "retry-lock", state: "active", retry: true, want: "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionChromeState(tc.state, tc.execution, tc.stopping, tc.retry); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestFormatSessionEventAndLeaseLine(t *testing.T) {
	t.Parallel()
	line := formatSessionEvent(hostclient.SessionEvent{Event: "session.launch", State: "active", GameID: "snes-mario", System: "snes"})
	if !strings.Contains(line, "launch") || !strings.Contains(line, "active") || !strings.Contains(line, "snes-mario") || !strings.Contains(line, "snes") {
		t.Fatalf("event line = %q", line)
	}
	if strings.Contains(line, `"sequence"`) || strings.Contains(line, "{") {
		t.Fatalf("raw dump: %q", line)
	}
	lease := formatKitLeaseLine(KitLeaseStatus{
		State: "held", Owner: "fogcast@host", Purpose: "game", Generation: "abcdefghijklmnop", ExpiresInMS: 72000,
	})
	if !strings.Contains(lease, "held") || !strings.Contains(lease, "fogcast@host") || !strings.Contains(lease, "game") || !strings.Contains(lease, "gen abcdefgh") || !strings.Contains(lease, "72s") {
		t.Fatalf("lease line = %q", lease)
	}
	blocked := formatKitLeaseLine(KitLeaseStatus{State: "blocked", Reason: "cleanup failed"})
	if !strings.Contains(blocked, "blocked") || !strings.Contains(blocked, "cleanup failed") {
		t.Fatalf("blocked line = %q", blocked)
	}
}

func TestEventsRetainRetryStopIgnoresResolvedHistory(t *testing.T) {
	t.Parallel()
	if retain, _ := eventsRetainRetryStop([]hostclient.SessionEvent{
		{Sequence: 1, Event: "save_failed", State: "active"},
		{Sequence: 2, Event: "session.stop", State: "idle"},
	}); retain {
		t.Fatal("idle stop should clear historical save_failed")
	}
	if retain, ev := eventsRetainRetryStop([]hostclient.SessionEvent{
		{Sequence: 1, Event: "session.launch", State: "active"},
		{Sequence: 2, Event: "save_failed", State: "active", Progress: &hostclient.SessionProgress{Message: retryStopHint}},
	}); !retain || ev.Event != "save_failed" {
		t.Fatalf("unresolved save_failed retain=%v ev=%#v", retain, ev)
	}
}

func TestMergeSessionEventsAdvancesCursor(t *testing.T) {
	t.Parallel()
	rows, after := mergeSessionEvents(nil, []hostclient.SessionEvent{
		{Sequence: 1, Event: "session.launch", State: "active"},
		{Sequence: 2, Event: "session.stop", State: "idle"},
	}, 0)
	if after != 2 || len(rows) != 2 {
		t.Fatalf("first merge rows=%d after=%d", len(rows), after)
	}
	rows, after = mergeSessionEvents(rows, []hostclient.SessionEvent{
		{Sequence: 3, Event: "session.launch", State: "active", GameID: "pong"},
	}, after)
	if after != 3 || len(rows) != 3 || rows[2].GameID != "pong" {
		t.Fatalf("second merge rows=%#v after=%d", rows, after)
	}
	same, sameAfter := mergeSessionEvents(rows, []hostclient.SessionEvent{{Sequence: 3, Event: "session.launch", State: "active"}}, after)
	if sameAfter != 3 || len(same) != 3 {
		t.Fatalf("duplicate advanced cursor rows=%d after=%d", len(same), sameAfter)
	}
}

func TestAppPollsSessionEventsIntoSofaList(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	eventsJSON := `{"events":[{"sequence":1,"event":"session.launch","state":"active","game_id":"snes-mario","system":"snes"}]}`
	var afterVals []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session/events":
			mu.Lock()
			afterVals = append(afterVals, r.URL.Query().Get("after"))
			body := eventsJSON
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
		return len(snap.Session.Events) >= 1 && strings.Contains(snap.Session.Events[0], "launch")
	})
	mu.Lock()
	eventsJSON = `{"events":[{"sequence":2,"event":"session.stop","state":"idle","media":"stopped"}]}`
	mu.Unlock()
	app.kickSessionPollLocked()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		if len(snap.Session.Events) < 2 {
			return false
		}
		last := snap.Session.Events[len(snap.Session.Events)-1]
		return strings.Contains(last, "stop") && strings.Contains(last, "idle")
	})
	mu.Lock()
	after := append([]string(nil), afterVals...)
	mu.Unlock()
	if len(after) < 2 || after[0] != "0" {
		t.Fatalf("after cursor = %#v", after)
	}
	sawAdvance := false
	for _, v := range after[1:] {
		if v == "1" || v == "2" {
			sawAdvance = true
			break
		}
	}
	if !sawAdvance {
		t.Fatalf("after cursor never advanced: %#v", after)
	}
}

func TestAppKitLeaseStripFromSelectedTarget(t *testing.T) {
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
				"targets": []map[string]any{
					{"name": "dev", "address": "http://" + r.Host, "enabled": true, "agent_configured": true},
				},
				"libraries": []map[string]any{{"id": "snes", "system": "snes", "root": "/library/snes"}},
				"systems":   []map[string]any{{"id": "snes", "label": "SNES"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/kit/lease":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"state":         "held",
				"owner":         "fogcast@powerboat",
				"purpose":       "interactive game/development session",
				"generation":    "cafef00ddeadbeef",
				"expires_in_ms": 88000,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.KitLease.State == "held" && strings.Contains(snap.KitLease.Line, "fogcast@powerboat")
	})
	snap := app.Snapshot()
	if snap.KitLease.Owner != "fogcast@powerboat" || snap.KitLease.Purpose == "" || snap.KitLease.Generation == "" || snap.KitLease.Expires == "" {
		t.Fatalf("lease strip = %#v", snap.KitLease)
	}
	if !strings.Contains(snap.ChromeLine(), "lease held") {
		t.Fatalf("chrome missing lease: %q", snap.ChromeLine())
	}
}

func TestAppSaveFailedStopLockoutRetryAndClear(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	var launches, stops int
	failStop := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []hostclient.Game{
					availableGame("snes-mario", "Mario", "snes"),
					availableGame("megadrive-sonic", "Sonic", "megadrive"),
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			launches++
			sessionJSON = `{"state":"active","game_id":"snes-mario","system":"snes"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","system":"snes"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			mu.Lock()
			stops++
			fail := failStop
			if !fail {
				sessionJSON = `{"state":"idle","media":"stopped"}`
			}
			mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":{"code":"INTERNAL","message":"SNES save could not be written; retry Stop before leaving the game"}}`)
				return
			}
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
		return len(snap.Games) >= 2 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.GPUParked
	})
	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.RetryStop && snap.Session.LaunchLocked
	})
	snap := app.Snapshot()
	if snap.Session.Chrome != "failed" {
		t.Fatalf("chrome = %q", snap.Session.Chrome)
	}
	if !strings.Contains(snap.Session.RetryHint, "retry Stop") {
		t.Fatalf("retry hint = %q", snap.Session.RetryHint)
	}
	line := snap.NowPlayingLine()
	if !strings.Contains(line, "Save failed") || strings.Contains(line, "Now playing") || strings.Contains(line, "Completed") {
		t.Fatalf("save-failed chrome %q", line)
	}
	mu.Lock()
	sessionJSON = `{"state":"idle"}`
	launchCount := launches
	mu.Unlock()
	app.kickSessionPollLocked()
	time.Sleep(50 * time.Millisecond)
	app.Press(CmdSelect, time.Now())
	app.Press(CmdRight, time.Now())
	mu.Lock()
	if launches != launchCount {
		t.Fatalf("launch/replace during lockout launches=%d was=%d", launches, launchCount)
	}
	failStop = false
	mu.Unlock()
	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Session.RetryStop && !snap.Session.LaunchLocked && snap.Session.Chrome == "idle"
	})
	mu.Lock()
	if stops < 2 {
		t.Fatalf("retry stop not posted, stops=%d", stops)
	}
	mu.Unlock()
}

func TestAppHistoricalSaveFailedDoesNotLockIdleSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session/events":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"events": []map[string]any{
					{"sequence": 4, "event": "save_failed", "state": "active", "game_id": "snes-mario"},
					{"sequence": 5, "event": "session.stop", "state": "idle", "media": "stopped"},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
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
		return !snap.Loading && len(snap.Session.Events) >= 1
	})
	snap := app.Snapshot()
	if snap.Session.RetryStop || snap.Session.LaunchLocked || snap.GPUParked {
		t.Fatalf("historical save_failed locked idle sofa = %#v parked=%v", snap.Session, snap.GPUParked)
	}
}

func TestShellExitReleasesIdleRetainedLease(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	var stopBodies []string
	mario := availableGame("snes-mario", "Mario", "snes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{mario}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario","system":"snes","execution":"fpga_native"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","system":"snes","execution":"fpga_native"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			stopBodies = append(stopBodies, string(raw))
			sessionJSON = `{"state":"idle","media":"stopped"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	defer app.Stop()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.Session.GameID == "snes-mario"
	})
	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Session.State != "active"
	})
	mu.Lock()
	bodies := append([]string(nil), stopBodies...)
	mu.Unlock()
	if len(bodies) != 1 || bodies[0] != `{"retain_lease":true}` {
		t.Fatalf("soft-stop bodies = %#v", bodies)
	}

	app.Stop()
	mu.Lock()
	bodies = append([]string(nil), stopBodies...)
	mu.Unlock()
	if len(bodies) != 2 || bodies[0] != `{"retain_lease":true}` || bodies[1] != "" {
		t.Fatalf("shell-exit bodies = %#v", bodies)
	}
}

func TestShellExitDoesNotReleaseActiveOrUnownedSession(t *testing.T) {
	var mu sync.Mutex
	var stops int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop" {
			mu.Lock()
			stops++
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"idle"}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())

	active := NewApp(client, 800, 600, 10)
	active.session.State = "active"
	active.Stop()

	idle := NewApp(client, 800, 600, 10)
	idle.session.State = "idle"
	idle.Stop()

	mu.Lock()
	n := stops
	mu.Unlock()
	if n != 0 {
		t.Fatalf("stops = %d, want no release for an active play or an unowned idle shell", n)
	}
}

func TestActiveSessionKeepsRetainedIdleLease(t *testing.T) {
	app := NewApp(nil, 800, 600, 10)
	app.retainedIdleLease = true
	for _, state := range []string{"active", "launching"} {
		app.applySessionLocked(hostclient.SessionResult{State: state, GameID: "nes-still", System: "nes"})
		if !app.retainedIdleLease {
			t.Fatalf("session %q cleared retainedIdleLease", state)
		}
	}
	if app.session.State != "launching" {
		t.Fatalf("session state = %q", app.session.State)
	}
}

func TestShellExitReleasesIdleGrantWhenAnotherPlayIsActive(t *testing.T) {
	t.Run("promoted play", func(t *testing.T) {
		assertShellExitReleasesRetainedGrant(t, true)
	})
	t.Run("relaunch", func(t *testing.T) {
		assertShellExitReleasesRetainedGrant(t, false)
	})
}

func assertShellExitReleasesRetainedGrant(t *testing.T, promote bool) {
	t.Helper()
	var mu sync.Mutex
	phase := "idle"
	var bodies []string
	mario := availableGame("snes-mario", "Mario", "snes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{mario}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			current := phase
			mu.Unlock()
			switch current {
			case "playing":
				_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","system":"snes","execution":"fpga_native"}`)
			case "promoted":
				_, _ = io.WriteString(w, `{"state":"active","game_id":"nes-still","system":"nes","execution":"fpga_native"}`)
			default:
				_, _ = io.WriteString(w, `{"state":"idle"}`)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
			mu.Lock()
			current := phase
			mu.Unlock()
			if current == "promoted" || current == "playing" {
				_, _ = io.WriteString(w, `{"sessions":[{"target":"kit-a","state":"active"}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"sessions":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			phase = "playing"
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","system":"snes","execution":"fpga_native"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			bodies = append(bodies, string(raw))
			body := string(raw)
			if body == `{"retain_lease":true}` {
				if promote {
					phase = "promoted"
				} else {
					phase = "idle"
				}
				mu.Unlock()
				_, _ = io.WriteString(w, `{"state":"idle","media":"stopped"}`)
				return
			}
			current := phase
			mu.Unlock()
			if body == `{"release_idle":true}` && (current == "playing" || current == "promoted") {
				_, _ = io.WriteString(w, `{"state":"active","game_id":"nes-still","system":"nes"}`)
				return
			}
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	defer app.Stop()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.Session.GameID == "snes-mario"
	})
	app.Press(CmdBack, time.Now())
	if promote {
		waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
			return snap.Session.State == "active" && snap.Session.GameID == "nes-still"
		})
	} else {
		waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
			return snap.Session.State != "active"
		})
		app.Press(CmdSelect, time.Now())
		waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
			return snap.Session.State == "active" && snap.Session.GameID == "snes-mario"
		})
	}
	app.mu.Lock()
	kept := app.retainedIdleLease
	app.mu.Unlock()
	if !kept {
		t.Fatal("active play cleared retainedIdleLease before shell exit")
	}

	app.Stop()
	mu.Lock()
	got := append([]string(nil), bodies...)
	mu.Unlock()
	if len(got) != 2 || got[0] != `{"retain_lease":true}` || got[1] != `{"release_idle":true}` {
		t.Fatalf("stop bodies = %#v, want retain_lease then release_idle", got)
	}
	app.mu.Lock()
	kept = app.retainedIdleLease
	app.mu.Unlock()
	if kept {
		t.Fatal("successful release_idle left retainedIdleLease set")
	}
}

func TestShellExitDoesNotEmptyBodyStopPromotedPlay(t *testing.T) {
	cases := []struct {
		name     string
		session  string
		sessions string
	}{
		{name: "promoted session", session: `{"state":"active","game_id":"nes-still"}`},
		{name: "session idle with surviving play", session: `{"state":"idle"}`, sessions: `{"sessions":[{"target":"kit-a","state":"active","game_id":"nes-still"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var bodies []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
					_, _ = io.WriteString(w, tc.session)
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sessions":
					if tc.sessions == "" {
						http.NotFound(w, r)
						return
					}
					_, _ = io.WriteString(w, tc.sessions)
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
					raw, _ := io.ReadAll(r.Body)
					mu.Lock()
					bodies = append(bodies, string(raw))
					mu.Unlock()
					_, _ = io.WriteString(w, tc.session)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
			app.retainedIdleLease = true
			app.session.State = "idle"
			app.Stop()
			mu.Lock()
			got := append([]string(nil), bodies...)
			mu.Unlock()
			if len(got) != 1 || got[0] != `{"release_idle":true}` {
				t.Fatalf("stop bodies = %#v, want release_idle and no empty-body Stop", got)
			}
		})
	}
}

func TestShellExitReleasesInFlightSoftStop(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	started := make(chan struct{})
	releaseStop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			bodies = append(bodies, string(raw))
			mu.Unlock()
			if string(raw) == `{"retain_lease":true}` {
				close(started)
				<-releaseStop
			}
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.session.State = "active"
	app.session.GameID = "snes-mario"
	app.Press(CmdBack, time.Now())
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("soft-stop was not posted")
	}
	exited := make(chan struct{})
	go func() {
		app.Stop()
		close(exited)
	}()
	select {
	case <-exited:
		t.Fatal("shell exit finished while Soft-stop was still in flight")
	case <-time.After(200 * time.Millisecond):
	}
	mu.Lock()
	if len(bodies) != 1 || bodies[0] != `{"retain_lease":true}` {
		t.Fatalf("bodies before soft-stop completed = %#v", bodies)
	}
	mu.Unlock()
	close(releaseStop)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("shell exit did not finish after Soft-stop")
	}
	mu.Lock()
	got := append([]string(nil), bodies...)
	mu.Unlock()
	if len(got) != 2 || got[0] != `{"retain_lease":true}` || got[1] != "" {
		t.Fatalf("shell-exit bodies = %#v", got)
	}
}

func TestShellExitRetriesFailedLeaseRelease(t *testing.T) {
	t.Run("api error", func(t *testing.T) {
		assertShellExitReleaseRetries(t, func(w http.ResponseWriter, attempt int) bool {
			if attempt < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"error":{"code":"MISTER_UNAVAILABLE","message":"lease release failed"}}`)
				return false
			}
			_, _ = io.WriteString(w, `{"state":"idle"}`)
			return true
		})
	})
	t.Run("transport error", func(t *testing.T) {
		assertShellExitReleaseRetries(t, func(w http.ResponseWriter, attempt int) bool {
			if attempt == 1 {
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Fatalf("response cannot hijack")
				}
				conn, _, err := hj.Hijack()
				if err != nil {
					t.Fatal(err)
				}
				_ = conn.Close()
				return false
			}
			_, _ = io.WriteString(w, `{"state":"idle"}`)
			return true
		})
	})
}

func assertShellExitReleaseRetries(t *testing.T, respond func(http.ResponseWriter, int) bool) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			attempts++
			n := attempts
			bodies = append(bodies, string(raw))
			mu.Unlock()
			respond(w, n)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.retainedIdleLease = true
	app.session.State = "idle"
	app.Stop()
	mu.Lock()
	got := append([]string(nil), bodies...)
	mu.Unlock()
	if len(got) < 2 {
		t.Fatalf("release attempts = %#v, want a retry", got)
	}
	for i, body := range got {
		if body != "" {
			t.Fatalf("attempt %d body = %q, want empty-body release", i, body)
		}
	}
	if got[len(got)-1] != "" || len(got) > shellLeaseReleaseAttempts {
		t.Fatalf("release attempts = %#v", got)
	}
}

func TestAppSaveFailedEventLocksLaunchUntilStop(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"active","game_id":"snes-mario"}`
	eventsJSON := `{"events":[{"sequence":4,"event":"save_failed","state":"active","game_id":"snes-mario","system":"snes","progress":{"message":"retry Stop before leaving the game"}}]}`
	var launches, stops int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session/events":
			mu.Lock()
			body := eventsJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			launches++
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			mu.Lock()
			stops++
			sessionJSON = `{"state":"idle","media":"stopped"}`
			eventsJSON = `{"events":[]}`
			mu.Unlock()
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
		return snap.Session.RetryStop && snap.Session.LaunchLocked
	})
	app.Press(CmdSelect, time.Now())
	mu.Lock()
	if launches != 0 {
		t.Fatalf("launch during save_failed lockout: %d", launches)
	}
	mu.Unlock()
	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Session.RetryStop && snap.Session.State != "active"
	})
	mu.Lock()
	n := stops
	mu.Unlock()
	if n != 1 {
		t.Fatalf("stops = %d", n)
	}
}
