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
	"strconv"
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
					availableGame("snes-mario", "Mario", "snes"),
					availableGame("megadrive-sonic", "Sonic", "megadrive"),
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
				"games": []Game{availableGame("gba-bond", "Bond", "gba")},
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

func TestLaunchBlockReasonMirrorsWebUI(t *testing.T) {
	t.Parallel()
	ready := availableGame("snes-mario", "Mario", "snes")
	cases := []struct {
		name string
		game Game
		want string
	}{
		{name: "ready", game: ready, want: ""},
		{name: "browse-only", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "available", RootOnline: true, Launchable: false}, want: "This platform is browse-only on this host."},
		{name: "missing", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "missing", RootOnline: false, Launchable: true}, want: "This game's source is offline."},
		{name: "offline", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "available", RootOnline: false, Launchable: true}, want: "This game's source is offline."},
		{name: "invalid", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "invalid", RootOnline: true, Launchable: true}, want: "This ROM can't be read."},
		{name: "not-ready", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "scanning", RootOnline: true, Launchable: true}, want: "This game isn't ready to launch."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := launchBlockReason(tc.game); got != tc.want {
				t.Fatalf("launchBlockReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAppRejectsUnavailableTitlesWithoutHostLaunch(t *testing.T) {
	cases := []struct {
		name string
		game Game
		want string
	}{
		{name: "browse-only", game: Game{ID: "ps1-browse", Title: "Browse", System: "psx", State: "available", RootOnline: true, Launchable: false}, want: "This platform is browse-only on this host."},
		{name: "offline", game: Game{ID: "snes-offline", Title: "Offline", System: "snes", State: "available", RootOnline: false, Launchable: true}, want: "This game's source is offline."},
		{name: "missing", game: Game{ID: "snes-missing", Title: "Missing", System: "snes", State: "missing", RootOnline: false, Launchable: true}, want: "This game's source is offline."},
		{name: "invalid", game: Game{ID: "snes-bad", Title: "Bad", System: "snes", State: "invalid", RootOnline: true, Launchable: true}, want: "This ROM can't be read."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var launches int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/games" {
					_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{tc.game}})
					return
				}
				if r.URL.Path == "/api/v1/session/launch" {
					launches++
					w.WriteHeader(http.StatusOK)
					_, _ = io.WriteString(w, `{"state":"active"}`)
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
			app.Tick(time.Now())
			snap := app.Snapshot()
			if snap.Launch.Phase != "error" || snap.Launch.Message != tc.want || snap.Launch.HTTPStatus != 0 {
				t.Fatalf("launch = %#v", snap.Launch)
			}
			if launches != 0 {
				t.Fatalf("host launch called %d times", launches)
			}
		})
	}
}

func TestAppEvictsDecodedCoversOutsidePrefetchWindow(t *testing.T) {
	handle := strings.Repeat("cd", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	games := make([]Game, 80)
	for i := range games {
		games[i] = availableGame("snes-"+strconv.Itoa(i), "Game "+strconv.Itoa(i), "snes")
		games[i].Cover = handle
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 80)
	app.Start(t.Context())
	t.Cleanup(app.Stop)

	deadline := time.Now().Add(3 * time.Second)
	var firstID string
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap := app.Snapshot()
		if len(snap.Games) == 80 && snap.CoverHits >= 1 {
			firstID = snap.Games[0].ID
			if _, ok := snap.Covers[firstID]; ok {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap := app.Snapshot()
	if firstID == "" || snap.Covers[firstID] == nil {
		t.Fatalf("expected decoded cover for first title, hits=%d games=%d err=%s", snap.CoverHits, len(snap.Games), snap.LoadErr)
	}

	for i := 0; i < 16; i++ {
		app.Press(CmdDown, time.Now())
		app.Tick(time.Now())
	}
	for time.Now().Before(deadline.Add(2 * time.Second)) {
		app.Tick(time.Now())
		snap = app.Snapshot()
		start, end := snap.Grid.PrefetchRange(prefetchRows)
		if _, kept := snap.Covers[firstID]; !kept && snap.CoverHits >= 1 {
			if snap.CoverHits > end-start {
				t.Fatalf("cover hits %d exceed prefetch window [%d,%d)", snap.CoverHits, start, end)
			}
			for id := range snap.Covers {
				if !gameIDInRange(snap.Games, start, end, id) {
					t.Fatalf("cover %s outside prefetch window [%d,%d)", id, start, end)
				}
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("first cover %s still present after scroll, covers=%d focus=%d", firstID, len(app.Snapshot().Covers), app.Snapshot().Grid.Focus)
}

func availableGame(id, title, system string) Game {
	return Game{ID: id, Title: title, System: system, Launchable: true, State: "available", RootOnline: true}
}

func gameIDInRange(games []Game, start, end int, id string) bool {
	if start < 0 {
		start = 0
	}
	if end > len(games) {
		end = len(games)
	}
	for i := start; i < end; i++ {
		if games[i].ID == id {
			return true
		}
	}
	return false
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
