package tenfoot

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAppNavigatesAndLaunchesThroughHostAPI(t *testing.T) {
	handle := strings.Repeat("cd", 32)
	var launches []string
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{
					{ID: "snes-mario", Title: "Mario", System: "snes", Launchable: true},
					{ID: "megadrive-sonic", Title: "Sonic", System: "megadrive", Launchable: true},
				},
			})
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			launches = append(launches, string(raw))
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"megadrive-sonic"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if len(snap.Games) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(app.Snapshot().Games) < 2 {
		t.Fatalf("games = %#v err=%s", app.Snapshot().Games, app.Snapshot().LoadErr)
	}

	now := time.Now()
	if app.Press(CommandFromButton(ButtonDPadRight), now) != CmdRight {
		t.Fatal("expected right command")
	}
	selected, ok := app.Selected()
	if !ok || selected.ID != "megadrive-sonic" {
		t.Fatalf("selected = %#v ok=%v", selected, ok)
	}
	app.Press(CommandFromButton(ButtonSouth), now)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().Launch.Phase == "ok" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap := app.Snapshot()
	if snap.Launch.Phase != "ok" || snap.Launch.HTTPStatus != 200 {
		t.Fatalf("launch = %#v launches=%v", snap.Launch, launches)
	}
	if len(launches) != 1 || launches[0] != `{"game_id":"megadrive-sonic"}` {
		t.Fatalf("launches = %#v", launches)
	}

	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if snap = app.Snapshot(); snap.CoverHits >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap.CoverHits < 1 {
		t.Fatalf("cover hits = %d", snap.CoverHits)
	}
}

func TestAppRecordsHostLaunchErrorWithoutTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/games" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{{ID: "gba-bond", Title: "Bond", System: "gba", Launchable: true}},
			})
			return
		}
		if r.URL.Path == "/api/v1/session/launch" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"code":"SOURCE_UNAVAILABLE","message":"game source is unavailable"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if len(app.Snapshot().Games) == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	app.Press(CmdSelect, time.Now())
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().Launch.Phase == "host" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap := app.Snapshot()
	if snap.Launch.Phase != "host" || snap.Launch.ErrorCode != "SOURCE_UNAVAILABLE" || snap.Launch.HTTPStatus != 500 {
		t.Fatalf("launch = %#v", snap.Launch)
	}
}

func mustPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
