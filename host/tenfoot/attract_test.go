package tenfoot

import (
	"context"
	"encoding/json"
	"image"
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
				"idle_seconds": 1,
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

	armAttractSoon(app)

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

	armAttractSoon(app)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().Attract.Active {
			t.Fatal("attract ran while session active")
		}
		time.Sleep(5 * time.Millisecond)
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
	armAttractSoon(app)
	app.Tick(time.Now())
	if app.Snapshot().Attract.Active {
		t.Fatal("disabled attract became active")
	}

	app.SetAttractDisabled(false)
	app.Press(CmdSearch, time.Now())
	armAttractSoon(app)
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
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 200, G: 20, B: 20, A: 255})
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
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"cover":      handle,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
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
	armAttractSoon(app)
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
	if !opts.NoAttract || !opts.Hidden || !opts.NoAttractSet {
		t.Fatalf("opts = %#v", opts)
	}
}

func armAttractSoon(app *App) {
	app.mu.Lock()
	app.attractIdle = 20 * time.Millisecond
	app.attractIdleReady = true
	app.attractLoading = false
	app.attractGen++
	app.lastInput = time.Now().Add(-time.Hour)
	app.mu.Unlock()
}

type fakeAttractPlayer struct {
	img      *image.RGBA
	endAfter time.Duration
	started  time.Time
	closed   *atomic.Bool
}

func (f *fakeAttractPlayer) Frame() (*image.RGBA, bool, error) {
	if f.closed != nil && f.closed.Load() {
		return nil, false, errAttractVideoUnavailable
	}
	ended := f.endAfter > 0 && !f.started.IsZero() && time.Since(f.started) >= f.endAfter
	return f.img, ended, nil
}

func (f *fakeAttractPlayer) Close() {
	if f.closed != nil {
		f.closed.Store(true)
	}
}

func TestAppHydratesHostIdleBeforeEnteringAttract(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 20, G: 200, B: 20, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 300,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"cover":      handle,
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
		return len(snap.Games) >= 1 && !snap.Loading && snap.Attract.IdleSeconds == 300
	})

	app.mu.Lock()
	app.lastInput = time.Now().Add(-70 * time.Second)
	app.mu.Unlock()
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().Attract.Active {
			t.Fatal("attract entered against a 300s host idle after only 70s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAppPlaysVideoOnlyAttractEntries(t *testing.T) {
	video := strings.Repeat("cd", 32)
	frame := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range frame.Pix {
		frame.Pix[i] = 255
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.openAttractVideo = func(context.Context, *Client, string) (attractPlayer, error) {
		return &fakeAttractPlayer{img: frame, started: time.Now()}, nil
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.Title == "Mario" && snap.Attract.Video && snap.Attract.Image != nil
	})
}

func TestAppFallsBackToStillWhenAttractVideoFails(t *testing.T) {
	cover := strings.Repeat("ab", 32)
	video := strings.Repeat("cd", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 20, G: 20, B: 200, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"cover":      cover,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+cover:
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
	app.openAttractVideo = func(context.Context, *Client, string) (attractPlayer, error) {
		return nil, errAttractVideoUnavailable
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.Handle == cover && snap.Attract.Image != nil && !snap.Attract.Video
	})
}

func TestAppFallsBackToStillWhenAttractVideoEndsBeforeFirstFrame(t *testing.T) {
	cover := strings.Repeat("ab", 32)
	video := strings.Repeat("cd", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 20, G: 20, B: 200, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"cover":      cover,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+cover:
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
	app.openAttractVideo = func(context.Context, *Client, string) (attractPlayer, error) {
		return &fakeAttractPlayer{endAfter: time.Nanosecond, started: time.Now().Add(-time.Second)}, nil
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.Handle == cover && snap.Attract.Image != nil && !snap.Attract.Video
	})
}

func TestAppDismissTearsDownAttractVideo(t *testing.T) {
	video := strings.Repeat("cd", 32)
	frame := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var closed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.openAttractVideo = func(context.Context, *Client, string) (attractPlayer, error) {
		return &fakeAttractPlayer{img: frame, started: time.Now(), closed: &closed}, nil
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.Video
	})
	app.DismissAttract(time.Now())
	if !closed.Load() {
		t.Fatal("dismiss did not close attract video player")
	}
	if app.Snapshot().Attract.Active || app.Snapshot().Attract.Video {
		t.Fatal("attract video still active after dismiss")
	}
}

func TestAppCancelsInFlightAttractVideoFetchOnDismiss(t *testing.T) {
	video := strings.Repeat("cd", 32)
	started := make(chan struct{})
	reqErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+video:
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, 16))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			close(started)
			select {
			case <-r.Context().Done():
				reqErr <- r.Context().Err()
			case <-time.After(5 * time.Second):
				reqErr <- context.DeadlineExceeded
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.openAttractVideo = func(ctx context.Context, client *Client, handle string) (attractPlayer, error) {
		_, err := client.FetchVideoFile(ctx, handle)
		return nil, err
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	armAttractSoon(app)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		select {
		case <-started:
			deadline = time.Time{}
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if deadline != (time.Time{}) {
		select {
		case <-started:
		default:
			t.Fatal("attract video fetch did not start")
		}
	}
	app.DismissAttract(time.Now())
	select {
	case err := <-reqErr:
		if err != context.Canceled {
			t.Fatalf("in-flight fetch err = %v, want canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dismiss left the attract video fetch running")
	}
	if app.Snapshot().Attract.Active {
		t.Fatal("attract still active after dismiss")
	}
}

func TestAppDoesNotRestartAttractVideoFetchBeforeFirstFrame(t *testing.T) {
	video := strings.Repeat("cd", 32)
	started := make(chan struct{})
	reqErr := make(chan error, 1)
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+video:
			fetches.Add(1)
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, 16))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-started:
			default:
				close(started)
			}
			select {
			case <-r.Context().Done():
				reqErr <- r.Context().Err()
			case <-time.After(5 * time.Second):
				reqErr <- context.DeadlineExceeded
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.openAttractVideo = func(ctx context.Context, client *Client, handle string) (attractPlayer, error) {
		_, err := client.FetchVideoFile(ctx, handle)
		return nil, err
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	armAttractSoon(app)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		select {
		case <-started:
			deadline = time.Time{}
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if deadline != (time.Time{}) {
		select {
		case <-started:
		default:
			t.Fatal("attract video fetch did not start")
		}
	}
	app.mu.Lock()
	app.attractCycleAt = time.Now().Add(-time.Second)
	app.mu.Unlock()
	hold := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(hold) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	if fetches.Load() != 1 {
		t.Fatalf("fetches = %d, want 1 (load cap must not restart the clip)", fetches.Load())
	}
	select {
	case err := <-reqErr:
		t.Fatalf("in-flight fetch ended during load cap: %v", err)
	default:
	}
}

func TestAppShowsStillRowAfterFailedVideoOnly(t *testing.T) {
	cover := strings.Repeat("ab", 32)
	video := strings.Repeat("cd", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 20, G: 20, B: 200, A: 255})
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
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{
					{
						"game_id":    "snes-mario",
						"title":      "Mario",
						"platform":   "snes",
						"video":      video,
						"launchable": true,
					},
					{
						"game_id":    "megadrive-sonic",
						"title":      "Sonic",
						"platform":   "megadrive",
						"cover":      cover,
						"launchable": true,
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+cover:
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
	app.openAttractVideo = func(context.Context, *Client, string) (attractPlayer, error) {
		return nil, errAttractVideoUnavailable
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 2 && !snap.Loading
	})
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.Title == "Sonic" && snap.Attract.Image != nil && !snap.Attract.Video
	})
}

func TestAppHidesAttractWhenVideoOnlyFailsWithoutStill(t *testing.T) {
	video := strings.Repeat("cd", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"launchable": true,
				}},
			})
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
	armAttractSoon(app)
	deadline := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().Attract.Active {
			t.Fatal("failed video-only playlist activated attract")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestPendingSouthHoldDoesNotLaunchAttractItem(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 20, G: 20, B: 200, A: 255})
	var launches []string
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
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
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "megadrive-sonic",
					"title":      "Sonic",
					"platform":   "megadrive",
					"cover":      handle,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			launches = append(launches, string(raw))
			sessionJSON = `{"state":"active","game_id":"megadrive-sonic"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","game_id":"megadrive-sonic"}`)
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
		return len(snap.Games) >= 2 && !snap.Loading
	})
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.GameID == "megadrive-sonic"
	})

	held := map[Command]bool{CmdSelect: true}
	if !app.hold.Begin(CmdSelect, time.Now(), true) {
		t.Fatal("pending south hold")
	}
	if applyPressed(app, map[Command]bool{}, held, time.Now()) {
		t.Fatal("quit")
	}
	if app.Snapshot().Attract.Active {
		t.Fatal("attract should dismiss on pending south release")
	}
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	got := append([]string(nil), launches...)
	mu.Unlock()
	if len(got) != 0 {
		t.Fatalf("pending south launched %#v", got)
	}
	if got, ok := app.Selected(); !ok || got.ID != "snes-mario" {
		t.Fatalf("focus moved, selected=%#v ok=%v", got, ok)
	}
}

func TestAppSkipsAttractWhileLaunchInFlight(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"cover":      handle,
					"launchable": true,
				}},
			})
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
	app.launch.Phase = "launching"
	app.mu.Unlock()
	armAttractSoon(app)
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().Attract.Active {
			t.Fatal("attract ran during in-flight launch")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAttractBlockedIgnoresRejectedHostLaunchPhase(t *testing.T) {
	t.Parallel()
	app := NewApp(NewClient("", nil), 1280, 720, 10)
	if app.attractBlockedLocked() {
		t.Fatal("idle app blocked attract")
	}
	app.launch.Phase = "host"
	if app.attractBlockedLocked() {
		t.Fatal("rejected host launch permanently blocked attract")
	}
	app.launch.Phase = "error"
	if app.attractBlockedLocked() {
		t.Fatal("failed launch blocked attract")
	}
	app.launch.Phase = "launching"
	if !app.attractBlockedLocked() {
		t.Fatal("in-flight launch did not block attract")
	}
	app.launch.Phase = "idle"
	app.inputBusy = true
	if !app.attractBlockedLocked() {
		t.Fatal("in-flight input attach/detach did not block attract")
	}
}

func TestAppAllowsAttractAfterRejectedHostLaunch(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 200, G: 20, B: 20, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"cover":      handle,
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
	app.launch.Phase = "host"
	app.launch.Message = "host launch 500 SOURCE_UNAVAILABLE: game source is unavailable"
	app.mu.Unlock()
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active
	})
}

func TestAppRefreshesDecreasedHostIdleBeforeCachedDeadline(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 20, G: 180, B: 20, A: 255})
	var idle atomic.Int32
	idle.Store(300)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": idle.Load(),
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"cover":      handle,
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
		return len(snap.Games) >= 1 && !snap.Loading && snap.Attract.IdleSeconds == 300
	})
	idle.Store(1)
	app.mu.Lock()
	app.lastInput = time.Now().Add(-20 * time.Second)
	app.attractIdleAt = time.Now().Add(-time.Minute)
	app.mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.IdleSeconds == 1
	})
}

func TestAppFallsBackWhenAttractArtworkFails(t *testing.T) {
	backdrop := strings.Repeat("ab", 32)
	cover := strings.Repeat("cd", 32)
	pngBytes := mustPNG(t, 32, 16, color.RGBA{R: 180, G: 20, B: 20, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"backdrop":   backdrop,
					"cover":      cover,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+backdrop:
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+cover:
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
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.Handle == cover && snap.Attract.Image != nil
	})
}

func TestAppBacksOffWhenAttractArtworkExhausted(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	var attractCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			attractCalls.Add(1)
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
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/artwork/"):
			http.NotFound(w, r)
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
	armAttractSoon(app)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if attractCalls.Load() >= 1 && !app.Snapshot().Attract.Active {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if app.Snapshot().Attract.Active {
		t.Fatal("exhausted artwork left attract active")
	}
	afterHide := attractCalls.Load()
	if afterHide < 1 {
		t.Fatal("did not fetch attract playlist")
	}
	spin := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(spin) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	if got := attractCalls.Load(); got > afterHide+1 {
		t.Fatalf("attract refetch loop: before=%d after=%d", afterHide, got)
	}
}

func TestPumpAttractVideoIgnoresIdleFrame(t *testing.T) {
	t.Parallel()
	first := image.NewRGBA(image.Rect(0, 0, 2, 2))
	app := NewApp(NewClient("", nil), 1280, 720, 10)
	app.attractActive = true
	app.attractVideo = true
	app.attractPlayer = &seqAttractPlayer{frames: []*image.RGBA{first, nil, nil}}
	now := time.Now()
	app.pumpAttractVideoLocked(now)
	if app.attractFrameSeq != 1 || app.attractImage != first {
		t.Fatalf("first frame seq=%d image=%v", app.attractFrameSeq, app.attractImage != nil)
	}
	app.pumpAttractVideoLocked(now)
	app.pumpAttractVideoLocked(now)
	if app.attractFrameSeq != 1 {
		t.Fatalf("idle ticks bumped FrameSeq to %d", app.attractFrameSeq)
	}
}

func TestTickAttractSkipsCatchUpVideoAfterPlaybackCap(t *testing.T) {
	t.Parallel()
	first := image.NewRGBA(image.Rect(0, 0, 2, 2))
	later := image.NewRGBA(image.Rect(0, 0, 2, 2))
	player := &seqAttractPlayer{frames: []*image.RGBA{first, later, later}}
	app := NewApp(NewClient("", nil), 1280, 720, 10)
	t.Cleanup(app.Stop)
	app.attractActive = true
	app.attractVideo = true
	app.attractPlayer = player
	app.attractHandle = strings.Repeat("aa", 32)
	app.attractItems = []AttractItem{
		{GameID: "a", Title: "A", Video: strings.Repeat("aa", 32), Launchable: true},
		{GameID: "b", Title: "B", Video: strings.Repeat("bb", 32), Launchable: true},
	}
	now := time.Unix(1, 0)
	app.pumpAttractVideoLocked(now)
	if player.calls != 1 || app.attractFrameSeq != 1 {
		t.Fatalf("first frame calls=%d seq=%d", player.calls, app.attractFrameSeq)
	}
	app.tickAttractLocked(now.Add(maxAttractVideo + time.Second))
	if player.calls != 1 {
		t.Fatalf("expired cap still pumped Frame, calls=%d", player.calls)
	}
	if app.attractIndex != 1 {
		t.Fatalf("index = %d, want next item", app.attractIndex)
	}
}

type seqAttractPlayer struct {
	frames []*image.RGBA
	i      int
	calls  int
}

func (s *seqAttractPlayer) Frame() (*image.RGBA, bool, error) {
	s.calls++
	if s.i >= len(s.frames) {
		return nil, false, nil
	}
	img := s.frames[s.i]
	s.i++
	return img, false, nil
}

func (s *seqAttractPlayer) Close() {}

func TestAppStopClosesQueuedAttractVideo(t *testing.T) {
	t.Parallel()
	var closed atomic.Bool
	app := NewApp(NewClient("", nil), 1280, 720, 10)
	app.attractResults <- attractResult{
		player: &fakeAttractPlayer{closed: &closed},
		video:  true,
	}
	app.Stop()
	if !closed.Load() {
		t.Fatal("queued attract video was not closed on Stop")
	}
}

func TestAppRestartsAttractVideoFromCachedFile(t *testing.T) {
	video := strings.Repeat("cd", 32)
	frame := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range frame.Pix {
		frame.Pix[i] = 255
	}
	var fetches atomic.Int32
	var cached atomic.Int32
	var hadImage atomic.Bool
	var sawBlank atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+video:
			t.Error("clip restart fetched video bytes again")
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.openAttractVideo = func(context.Context, *Client, string) (attractPlayer, error) {
		fetches.Add(1)
		return &fileAttractPlayer{
			inner: &fakeAttractPlayer{img: frame, endAfter: 20 * time.Millisecond, started: time.Now()},
			path:  "cached-attract-clip",
		}, nil
	}
	app.openAttractCached = func(path string) (attractPlayer, error) {
		if path != "cached-attract-clip" {
			t.Errorf("cached path = %q", path)
		}
		cached.Add(1)
		return &fileAttractPlayer{
			inner: &fakeAttractPlayer{img: frame, endAfter: 20 * time.Millisecond, started: time.Now()},
			path:  path,
			keep:  true,
		}, nil
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	armAttractSoon(app)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if snap.Attract.Image != nil {
			hadImage.Store(true)
		}
		if hadImage.Load() && snap.Attract.Active && snap.Attract.Image == nil {
			sawBlank.Store(true)
		}
		if cached.Load() >= 1 && fetches.Load() == 1 && snap.Attract.Active && snap.Attract.Image != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fetches.Load() != 1 {
		t.Fatalf("fetches = %d, want 1", fetches.Load())
	}
	if cached.Load() < 1 {
		t.Fatal("clip restart did not reopen the cached file")
	}
	if sawBlank.Load() {
		t.Fatal("clip restart blanked the attract stage")
	}
}

func TestAppRestartsSingleAttractVideoWhenClipEnds(t *testing.T) {
	video := strings.Repeat("cd", 32)
	frame := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var opens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id":    "snes-mario",
					"title":      "Mario",
					"platform":   "snes",
					"video":      video,
					"launchable": true,
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.openAttractVideo = func(context.Context, *Client, string) (attractPlayer, error) {
		opens.Add(1)
		return &fakeAttractPlayer{img: frame, endAfter: 20 * time.Millisecond, started: time.Now()}, nil
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) >= 1 && !snap.Loading
	})
	armAttractSoon(app)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Attract.Active && snap.Attract.Video && opens.Load() >= 2
	})
}

func TestPlayableAttractItemsKeepsVideoOnly(t *testing.T) {
	t.Parallel()
	cover := strings.Repeat("ab", 32)
	video := strings.Repeat("cd", 32)
	items := playableAttractItems([]AttractItem{
		{Title: "video", Video: video},
		{Title: "still", Cover: cover},
		{Title: "empty"},
	})
	if len(items) != 2 || items[0].Title != "video" || items[1].Title != "still" {
		t.Fatalf("items = %#v", items)
	}
}
