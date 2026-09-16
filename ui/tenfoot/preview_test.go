package tenfoot

import (
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
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

func TestAppSessionPreviewStartsAndTearsDown(t *testing.T) {
	jpeg := mustJPEG(t, 8, 6, color.RGBA{R: 40, G: 80, B: 120, A: 255})
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	var previewLive atomic.Int32
	var launches, stops atomic.Int32
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
			launches.Add(1)
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario","system":"snes","execution":"fpga_native","media":"active"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","system":"snes","execution":"fpga_native","media":"active"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			stops.Add(1)
			mu.Lock()
			sessionJSON = `{"state":"idle"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session/preview":
			previewLive.Add(1)
			defer previewLive.Add(-1)
			w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fogcast-frame")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			writeMJPEGPart(w, jpeg)
			if flusher != nil {
				flusher.Flush()
			}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active" && snap.Preview.Image != nil && snap.Preview.Label == "Preview"
	})
	snap := app.Snapshot()
	if snap.Preview.Label != previewLabel {
		t.Fatalf("preview label = %q", snap.Preview.Label)
	}
	if strings.Contains(strings.ToLower(snap.ChromeLine()), "hdmi mirror") {
		t.Fatalf("chrome claimed mirror quality: %q", snap.ChromeLine())
	}
	if previewLive.Load() < 1 {
		t.Fatal("expected a live preview stream while session is active")
	}

	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.GPUParked && snap.Session.State != "active" && snap.Preview.Image == nil && !snap.Preview.Live
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if previewLive.Load() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if previewLive.Load() != 0 {
		t.Fatalf("preview stream still live after stop: %d", previewLive.Load())
	}
	if launches.Load() != 1 || stops.Load() != 1 {
		t.Fatalf("launches=%d stops=%d", launches.Load(), stops.Load())
	}
}

func TestAppPreviewUnavailableDoesNotBlockLaunchOrStop(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"idle"}`
	var launches, stops, previews atomic.Int32
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
			launches.Add(1)
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario","execution":"fpga_native"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","execution":"fpga_native"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			stops.Add(1)
			mu.Lock()
			sessionJSON = `{"state":"idle"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session/preview":
			previews.Add(1)
			http.Error(w, "session preview is inactive", http.StatusServiceUnavailable)
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
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active"
	})
	if launches.Load() != 1 {
		t.Fatalf("launch blocked, launches=%d", launches.Load())
	}
	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.GPUParked && snap.Session.State != "active"
	})
	if stops.Load() != 1 {
		t.Fatalf("stop blocked, stops=%d", stops.Load())
	}
	if previews.Load() < 1 {
		t.Fatal("expected preview probe")
	}
	snap := app.Snapshot()
	if snap.Preview.Image != nil {
		t.Fatal("unavailable preview still has an image")
	}
}

func TestAppPreviewMissingRouteIsGraceful(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"active","game_id":"snes-mario","execution":"fpga_native"}`
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session/preview":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Session.State == "active"
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		app.mu.Lock()
		unavail := app.previewUnavailable
		app.mu.Unlock()
		if unavail {
			snap := app.Snapshot()
			if snap.Preview.Image != nil || snap.Preview.Live {
				t.Fatalf("404 preview still live: %#v", snap.Preview)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("404 preview was not marked unavailable")
}

func TestGPUParkCancelsPreviewStream(t *testing.T) {
	jpeg := mustJPEG(t, 4, 3, color.RGBA{R: 9, G: 8, B: 7, A: 255})
	var previewLive atomic.Int32
	var previewGens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/preview" {
			http.NotFound(w, r)
			return
		}
		previewGens.Add(1)
		previewLive.Add(1)
		defer previewLive.Add(-1)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fogcast-frame")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeMJPEGPart(w, jpeg)
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	app.mu.Lock()
	app.session.State = "active"
	app.syncGPUParkLocked()
	app.mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.GPUParked && snap.Preview.Image != nil
	})
	if previewLive.Load() != 1 {
		t.Fatalf("live streams = %d", previewLive.Load())
	}

	app.mu.Lock()
	app.session.State = "idle"
	app.syncGPUParkLocked()
	app.mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.GPUParked && snap.Preview.Image == nil && !snap.Preview.Live
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if previewLive.Load() == 0 {
			if previewGens.Load() < 1 {
				t.Fatal("park never opened a preview stream")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("zombie preview stream after unpark: live=%d", previewLive.Load())
}

func TestAppCloseTearsDownPreview(t *testing.T) {
	var previewLive atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/preview" {
			http.NotFound(w, r)
			return
		}
		previewLive.Add(1)
		defer previewLive.Add(-1)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fogcast-frame")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeMJPEGPart(w, mustJPEG(t, 2, 2, color.RGBA{A: 255}))
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 640, 480, 10)
	app.Start(t.Context())
	app.mu.Lock()
	app.session.State = "active"
	app.syncGPUParkLocked()
	app.mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Preview.Live || snap.Preview.Image != nil
	})
	app.Stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if previewLive.Load() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("preview still live after app.Stop: %d", previewLive.Load())
}

func TestPreviewStopsForGPUNeedingOverlayAndAttract(t *testing.T) {
	var previewLive atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/preview" {
			http.NotFound(w, r)
			return
		}
		previewLive.Add(1)
		defer previewLive.Add(-1)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fogcast-frame")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeMJPEGPart(w, mustJPEG(t, 2, 2, color.RGBA{A: 255}))
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.session.State = "active"
	app.syncGPUParkLocked()
	app.mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return snap.Preview.Image != nil
	})

	app.mu.Lock()
	app.settingsOpen = true
	app.syncPreviewLocked()
	app.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if previewLive.Load() == 0 && !app.Snapshot().Preview.Live {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("preview still live with settings overlay: %d", previewLive.Load())
}

func TestPreviewNotStartedWithoutAppStart(t *testing.T) {
	t.Parallel()
	app := NewApp(nil, 800, 600, 10)
	app.session.State = "active"
	app.syncGPUParkLocked()
	if app.previewLive || app.previewCancel != nil {
		t.Fatal("preview started without App.Start")
	}
}
