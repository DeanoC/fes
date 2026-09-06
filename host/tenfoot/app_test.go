package tenfoot

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
				Presentation: &PresentationInfo{CoverArtworkID: handle},
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
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		if app.Snapshot().CoverHits >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if app.Snapshot().CoverHits < 1 {
		t.Fatalf("cover hits = %d", app.Snapshot().CoverHits)
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
	if !snap.GPUParked || snap.Session.State != "active" || snap.Session.GameID != "megadrive-sonic" {
		t.Fatalf("session = %#v parked=%v", snap.Session, snap.GPUParked)
	}
	if snap.CoverHits != 0 {
		t.Fatalf("parked cover hits = %d", snap.CoverHits)
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

func TestSnapshotSharesCatalogSlice(t *testing.T) {
	t.Parallel()
	app := catalogApp(3)
	snap := app.Snapshot()
	if len(snap.Games) != 3 {
		t.Fatalf("games = %d", len(snap.Games))
	}
	again := app.Snapshot()
	if &snap.Games[0] != &again.Games[0] {
		t.Fatal("Snapshot cloned the catalog")
	}
	app.games = []Game{availableGame("snes-only", "Only", "snes")}
	app.grid.SetCount(1)
	replaced := app.Snapshot()
	if len(replaced.Games) != 1 || replaced.Games[0].ID != "snes-only" {
		t.Fatalf("replaced = %#v", replaced.Games)
	}
	if &replaced.Games[0] == &snap.Games[0] {
		t.Fatal("replaced catalog still aliases the previous snapshot")
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

func TestAppLoadsCatalogCoverWithoutPresentationRoundTrip(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	var mu sync.Mutex
	presentationByID := map[string]int{}
	holdPresentation := make(chan struct{})
	var releaseOnce sync.Once
	releasePresentation := func() { releaseOnce.Do(func() { close(holdPresentation) }) }
	t.Cleanup(releasePresentation)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			games := []Game{
				availableGame("snes-mario", "Mario", "snes"),
				availableGame("snes-sonic", "Sonic", "snes"),
				availableGame("snes-zelda", "Zelda", "snes"),
			}
			for i := range games {
				games[i].Cover = handle
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/presentation/games/")
			mu.Lock()
			presentationByID[id]++
			mu.Unlock()
			<-holdPresentation
			_ = json.NewEncoder(w).Encode(Presentation{GameID: id, State: "offline"})
		case r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && snap.CoverHits >= 3
	})
	mu.Lock()
	got := cloneStringIntMap(presentationByID)
	mu.Unlock()
	if _, extra := got["snes-sonic"]; extra {
		t.Fatalf("prefetched presentation lookups = %#v", got)
	}
	if _, extra := got["snes-zelda"]; extra {
		t.Fatalf("prefetched presentation lookups = %#v", got)
	}
}

func TestAppCyclesPlatformAndSortAgainstHostAPI(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"platforms": []Platform{
					{ID: "snes", Label: "Super NES"},
					{ID: "megadrive", Label: "Mega Drive"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			mu.Lock()
			paths = append(paths, r.URL.RequestURI())
			mu.Unlock()
			platform := r.URL.Query().Get("platform")
			games := []Game{
				availableGame("snes-mario", "Mario", "snes"),
				availableGame("megadrive-sonic", "Sonic", "megadrive"),
			}
			if platform == "snes" {
				games = []Game{availableGame("snes-mario", "Mario", "snes")}
			}
			if platform == "megadrive" {
				games = []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && len(snap.Platforms) == 2 && !snap.Loading
	})

	app.Press(CmdFilterNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.PlatformID == "snes" && len(snap.Games) == 1 && snap.Games[0].ID == "snes-mario" && !snap.Loading
	})
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Sort == "recently_added" && !snap.Loading
	})
	found := false
	mu.Lock()
	snapshot := append([]string(nil), paths...)
	mu.Unlock()
	for _, path := range snapshot {
		if strings.Contains(path, "platform=snes") && strings.Contains(path, "sort=recently_added") && strings.Contains(path, "grouped=1") && strings.Contains(path, "availability=ready") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("paths = %#v", snapshot)
	}
}

func TestAppSearchQueriesHost(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/games" {
			mu.Lock()
			paths = append(paths, r.URL.RequestURI())
			mu.Unlock()
			q := r.URL.Query().Get("q")
			games := []Game{availableGame("snes-mario", "Mario", "snes"), availableGame("megadrive-sonic", "Sonic", "megadrive")}
			if q == "sonic" {
				games = []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			}
			if q != "" && q != "sonic" {
				games = nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && !snap.Loading
	})
	now := time.Now()
	app.Press(CmdSearch, now)
	if !app.SearchOpen() {
		t.Fatal("search should open")
	}
	app.TypeText("sonic", now)
	app.Tick(now.Add(searchDebounce + time.Millisecond))
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Query == "sonic" && len(snap.Games) == 1 && snap.Games[0].ID == "megadrive-sonic" && !snap.Loading
	})
	found := false
	mu.Lock()
	snapshot := append([]string(nil), paths...)
	mu.Unlock()
	for _, path := range snapshot {
		if strings.Contains(path, "q=sonic") && strings.Contains(path, "grouped=1") && strings.Contains(path, "availability=ready") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("paths = %#v", snapshot)
	}
	app.Press(CmdBack, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Query == "" && len(snap.Games) == 2 && !snap.Loading
	})
}

func focusSearchKey(t *testing.T, app *App, id string) {
	t.Helper()
	app.mu.Lock()
	ok := app.searchField.OSK.SelectID(id)
	app.mu.Unlock()
	if !ok {
		t.Fatalf("osk key %q not found", id)
	}
}

func oskType(t *testing.T, app *App, text string, now time.Time) {
	t.Helper()
	for _, r := range text {
		focusSearchKey(t, app, "char-"+string(r))
		app.Press(CmdSelect, now)
	}
}

func TestAppOSKTypesSearchQueryAndDoneCloses(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/games" {
			mu.Lock()
			paths = append(paths, r.URL.RequestURI())
			mu.Unlock()
			q := r.URL.Query().Get("q")
			games := []Game{availableGame("snes-mario", "Mario", "snes"), availableGame("megadrive-sonic", "Sonic", "megadrive")}
			if q == "sonic" {
				games = []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			}
			if q != "" && q != "sonic" {
				games = nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && !snap.Loading
	})
	focusBefore := app.Snapshot().Grid.Focus
	now := time.Now()
	app.Press(CmdSearch, now)
	snap := app.Snapshot()
	if !snap.SearchOpen || !snap.OSK.Open || snap.OSK.FocusID != "char-q" {
		t.Fatalf("osk open = %#v", snap.OSK)
	}
	app.Press(CmdRight, now)
	if app.Snapshot().Grid.Focus != focusBefore {
		t.Fatal("d-pad moved cover focus while OSK open")
	}
	if app.Snapshot().OSK.FocusID != "char-w" {
		t.Fatalf("osk focus = %q", app.Snapshot().OSK.FocusID)
	}
	oskType(t, app, "sonic", now)
	if got := app.Snapshot().Query; got != "sonic" {
		t.Fatalf("query = %q", got)
	}
	app.TypeText("!", now)
	if got := app.Snapshot().Query; got != "sonic!" {
		t.Fatalf("physical type = %q", got)
	}
	focusSearchKey(t, app, "bksp")
	app.Press(CmdSelect, now)
	if got := app.Snapshot().Query; got != "sonic" {
		t.Fatalf("bksp = %q", got)
	}
	app.Tick(now.Add(searchDebounce + time.Millisecond))
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Query == "sonic" && len(snap.Games) == 1 && snap.Games[0].ID == "megadrive-sonic" && !snap.Loading
	})
	focusSearchKey(t, app, "done")
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.SearchOpen && !snap.OSK.Open && snap.Query == "sonic"
	})
	found := false
	mu.Lock()
	snapshot := append([]string(nil), paths...)
	mu.Unlock()
	for _, path := range snapshot {
		if strings.Contains(path, "q=sonic") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("paths = %#v", snapshot)
	}
}

func TestAppOSKIgnoresLayoutAndPagesCharset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/games" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	now := time.Now()
	layout := app.Snapshot().Grid.Mode
	safe := app.Snapshot().SafeAreaPct
	app.Press(CmdSearch, now)
	app.Press(CmdLayoutCycle, now)
	app.Press(CmdSafeAreaIn, now)
	app.Press(CmdFilterNext, now)
	snap := app.Snapshot()
	if snap.Grid.Mode != layout {
		t.Fatalf("layout changed while OSK open: %s", snap.Grid.Mode)
	}
	if snap.SafeAreaPct != safe {
		t.Fatalf("safe-area changed while OSK open: %v", snap.SafeAreaPct)
	}
	if snap.PlatformID != "" {
		t.Fatalf("shoulder cycled platform: %q", snap.PlatformID)
	}
	if snap.OSK.Page != oskPageSymbols {
		t.Fatalf("shoulder should page OSK, page=%d", snap.OSK.Page)
	}
	app.Press(CmdBack, now)
	if app.SearchOpen() {
		t.Fatal("empty B should close OSK")
	}
}

func TestAppReloadClearsPendingSearch(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var launches []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			paths = append(paths, r.URL.RequestURI())
			mu.Unlock()
			q := r.URL.Query().Get("q")
			games := []Game{availableGame("snes-mario", "Mario", "snes"), availableGame("megadrive-sonic", "Sonic", "megadrive")}
			if q == "sonic" {
				games = []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			}
			if q != "" && q != "sonic" {
				games = nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			launches = append(launches, string(raw))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"megadrive-sonic"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && !snap.Loading
	})
	now := time.Now()
	app.Press(CmdSearch, now)
	app.TypeText("sonic", now)
	app.Press(CmdSortCycle, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Query == "sonic" && snap.Sort == "recently_added" && len(snap.Games) == 1 && snap.Games[0].ID == "megadrive-sonic" && !snap.Loading
	})
	mu.Lock()
	afterSort := len(paths)
	mu.Unlock()
	app.Tick(now.Add(searchDebounce + time.Millisecond))
	time.Sleep(50 * time.Millisecond)
	app.Tick(time.Now())
	mu.Lock()
	afterDebounce := append([]string(nil), paths...)
	mu.Unlock()
	if len(afterDebounce) != afterSort {
		t.Fatalf("debounce reloaded after sort already applied query, paths=%v", afterDebounce)
	}
	app.Press(CmdSearch, time.Now())
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok"
	})
	mu.Lock()
	got := append([]string(nil), launches...)
	laterPaths := append([]string(nil), paths...)
	mu.Unlock()
	if len(got) != 1 || got[0] != `{"game_id":"megadrive-sonic"}` {
		t.Fatalf("select launched after pending search cleared, got %v", got)
	}
	if len(laterPaths) != afterSort {
		t.Fatalf("select started a redundant reload, paths=%v", laterPaths)
	}
}

func TestAppFocusDetailUsesPresentation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			game := availableGame("snes-mario", "Mario", "snes")
			game.Year = "1990"
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
		case r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"platforms": []Platform{{ID: "snes", Label: "Super NES"}},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &PresentationInfo{Summary: "Jump on turtles.", Year: "1985", Genre: "Platform"},
				Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
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
		d := snap.FocusDetail
		return d.Title == "Mario" && d.Platform == "Super NES" && d.Year == "1985" && d.Genre == "Platform" && d.Summary == "Jump on turtles." && d.Attribution == "Data from IGDB.com" && d.MetaLine() == "Super NES  ·  1985  ·  Platform  ·  Data from IGDB.com"
	})
}

func TestPresentationRetryDelayDoublesUntilCap(t *testing.T) {
	t.Parallel()
	if got := presentationRetryDelay(1); got != presentationRetryMin {
		t.Fatalf("first = %s", got)
	}
	if got := presentationRetryDelay(2); got != 2*presentationRetryMin {
		t.Fatalf("second = %s", got)
	}
	if got := presentationRetryDelay(8); got != presentationRetryMax {
		t.Fatalf("capped = %s", got)
	}
}

func TestAppBacksOffFailedPresentationRetries(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	for _, tc := range []struct {
		name       string
		withCover  bool
		wantCovers bool
		offline    bool
	}{
		{name: "coverFailed", withCover: false},
		{name: "coverReady", withCover: true, wantCovers: true},
		{name: "coverFailedOffline", offline: true},
		{name: "coverReadyOffline", withCover: true, wantCovers: true, offline: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var presentationGets int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/games":
					game := availableGame("snes-mario", "Mario", "snes")
					if tc.withCover {
						game.Cover = handle
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
				case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
					mu.Lock()
					presentationGets++
					mu.Unlock()
					if tc.offline {
						_ = json.NewEncoder(w).Encode(Presentation{GameID: "snes-mario", State: "offline"})
						return
					}
					http.Error(w, "temporary", http.StatusInternalServerError)
				case tc.withCover && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(pngBytes)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
			app.Start(t.Context())
			t.Cleanup(app.Stop)
			waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
				mu.Lock()
				n := presentationGets
				mu.Unlock()
				if n < 1 || len(snap.Games) != 1 || snap.FocusDetail.Summary != "" {
					return false
				}
				if tc.wantCovers && snap.CoverHits < 1 {
					return false
				}
				return true
			})
			mu.Lock()
			afterFirst := presentationGets
			mu.Unlock()
			hold := time.Now().Add(presentationRetryMin / 2)
			for time.Now().Before(hold) {
				app.Tick(time.Now())
				time.Sleep(5 * time.Millisecond)
			}
			mu.Lock()
			duringBackoff := presentationGets
			mu.Unlock()
			if duringBackoff != afterFirst {
				t.Fatalf("presentation GETs during backoff: first=%d later=%d", afterFirst, duringBackoff)
			}
			waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
				mu.Lock()
				n := presentationGets
				mu.Unlock()
				return n > afterFirst
			})
		})
	}
}

func TestAppDoesNotRequeueFailedArtwork(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		name    string
		artwork func(http.ResponseWriter)
	}{
		{name: "notFound", artwork: func(w http.ResponseWriter) { http.NotFound(w, nil) }},
		{name: "corrupt", artwork: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("not-an-image"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var artworkGets int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/games":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"games": []Game{availableGame("snes-mario", "Mario", "snes")},
					})
				case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
					_ = json.NewEncoder(w).Encode(Presentation{
						GameID: "snes-mario",
						State:  "ready",
						Presentation: &PresentationInfo{CoverArtworkID: handle, Summary: "Jump on turtles."},
					})
				case r.URL.Path == "/api/v1/presentation/artwork/"+handle:
					mu.Lock()
					artworkGets++
					mu.Unlock()
					tc.artwork(w)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
			app.Start(t.Context())
			t.Cleanup(app.Stop)
			waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
				mu.Lock()
				n := artworkGets
				mu.Unlock()
				return n >= 1 && len(snap.Games) == 1 && snap.CoverHits == 0
			})
			mu.Lock()
			afterFirst := artworkGets
			mu.Unlock()
			hold := time.Now().Add(200 * time.Millisecond)
			for time.Now().Before(hold) {
				app.Tick(time.Now())
				time.Sleep(5 * time.Millisecond)
			}
			mu.Lock()
			later := artworkGets
			mu.Unlock()
			if later != afterFirst {
				t.Fatalf("artwork GETs after failure: first=%d later=%d", afterFirst, later)
			}
			if afterFirst != 1 {
				t.Fatalf("artwork GETs before hold = %d, want 1", afterFirst)
			}
		})
	}
}

func TestAppDoesNotRequeueFailedArtworkDuringOfflineDetailRetry(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	var mu sync.Mutex
	var artworkGets, presentationGets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			game := availableGame("snes-mario", "Mario", "snes")
			game.Cover = handle
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			mu.Lock()
			presentationGets++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-mario",
				State:  "offline",
				Presentation: &PresentationInfo{CoverArtworkID: handle},
			})
		case r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			mu.Lock()
			artworkGets++
			mu.Unlock()
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
		mu.Lock()
		art := artworkGets
		pres := presentationGets
		mu.Unlock()
		return art >= 1 && pres >= 1 && len(snap.Games) == 1 && snap.CoverHits == 0
	})
	mu.Lock()
	afterArt := artworkGets
	afterPres := presentationGets
	mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		n := presentationGets
		mu.Unlock()
		return n > afterPres
	})
	hold := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(hold) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	laterArt := artworkGets
	laterPres := presentationGets
	mu.Unlock()
	if laterArt != afterArt {
		t.Fatalf("artwork GETs after offline detail retry: first=%d later=%d presentation=%d", afterArt, laterArt, laterPres)
	}
	if afterArt != 1 {
		t.Fatalf("artwork GETs before retry = %d, want 1", afterArt)
	}
}

func TestAppAdoptsChangedPresentationCoverWhenReady(t *testing.T) {
	oldHandle := strings.Repeat("ab", 32)
	newHandle := strings.Repeat("cd", 32)
	oldPNG := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	newPNG := mustPNG(t, 8, 12, color.RGBA{R: 200, G: 40, B: 20, A: 255})
	for _, tc := range []struct {
		name      string
		nextCover string
		wantHits  bool
	}{
		{name: "replaced", nextCover: newHandle, wantHits: true},
		{name: "removed", nextCover: "", wantHits: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			recovered := false
			artworkByHandle := map[string]int{}
			var presentationGets int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/games":
					game := availableGame("snes-mario", "Mario", "snes")
					game.Cover = oldHandle
					_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
				case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
					mu.Lock()
					presentationGets++
					done := recovered
					cover := oldHandle
					if done {
						cover = tc.nextCover
					}
					mu.Unlock()
					if !done {
						_ = json.NewEncoder(w).Encode(Presentation{GameID: "snes-mario", State: "offline"})
						return
					}
					pres := Presentation{
						GameID: "snes-mario",
						State:  "ready",
						Presentation: &PresentationInfo{CoverArtworkID: cover, Summary: "Jump on turtles."},
						Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
					}
					_ = json.NewEncoder(w).Encode(pres)
				case r.URL.Path == "/api/v1/presentation/artwork/"+oldHandle:
					mu.Lock()
					artworkByHandle[oldHandle]++
					mu.Unlock()
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(oldPNG)
				case r.URL.Path == "/api/v1/presentation/artwork/"+newHandle:
					mu.Lock()
					artworkByHandle[newHandle]++
					mu.Unlock()
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(newPNG)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
			app.Start(t.Context())
			t.Cleanup(app.Stop)
			waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
				mu.Lock()
				art := artworkByHandle[oldHandle]
				pres := presentationGets
				mu.Unlock()
				img := snap.Covers["snes-mario"]
				return art >= 1 && pres >= 1 && len(snap.Games) == 1 && snap.CoverHits >= 1 && img != nil && img.RGBAAt(0, 0) == color.RGBA{R: 20, G: 80, B: 200, A: 255}
			})
			mu.Lock()
			afterOld := artworkByHandle[oldHandle]
			recovered = true
			mu.Unlock()
			waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
				if snap.FocusDetail.Summary != "Jump on turtles." {
					return false
				}
				if tc.wantHits {
					img := snap.Covers["snes-mario"]
					return snap.CoverHits >= 1 && img != nil && img.RGBAAt(0, 0) == color.RGBA{R: 200, G: 40, B: 20, A: 255}
				}
				return snap.CoverHits == 0 && snap.Covers["snes-mario"] == nil
			})
			hold := time.Now().Add(200 * time.Millisecond)
			for time.Now().Before(hold) {
				app.Tick(time.Now())
				time.Sleep(5 * time.Millisecond)
			}
			mu.Lock()
			laterOld := artworkByHandle[oldHandle]
			newGets := artworkByHandle[newHandle]
			mu.Unlock()
			if laterOld != afterOld {
				t.Fatalf("stale artwork retried: first=%d later=%d", afterOld, laterOld)
			}
			if afterOld != 1 {
				t.Fatalf("old artwork GETs = %d, want 1", afterOld)
			}
			if tc.wantHits && newGets < 1 {
				t.Fatalf("replacement artwork GETs = %d", newGets)
			}
			if !tc.wantHits && newGets != 0 {
				t.Fatalf("removed cover fetched replacement artwork: %d", newGets)
			}
		})
	}
}

func TestAppAdoptsChangedPresentationCoverOnRetry(t *testing.T) {
	oldHandle := strings.Repeat("ab", 32)
	newHandle := strings.Repeat("cd", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	for _, tc := range []struct {
		name      string
		nextCover string
		wantHits  bool
	}{
		{name: "replaced", nextCover: newHandle, wantHits: true},
		{name: "removed", nextCover: "", wantHits: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			recovered := false
			artworkByHandle := map[string]int{}
			var presentationGets int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/games":
					game := availableGame("snes-mario", "Mario", "snes")
					game.Cover = oldHandle
					_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
				case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
					mu.Lock()
					presentationGets++
					done := recovered
					cover := oldHandle
					if done {
						cover = tc.nextCover
					}
					mu.Unlock()
					pres := Presentation{
						GameID: "snes-mario",
						State:  "ready",
						Presentation: &PresentationInfo{CoverArtworkID: cover},
					}
					if done {
						pres.Presentation.Summary = "Jump on turtles."
						pres.Attribution = &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"}
					}
					_ = json.NewEncoder(w).Encode(pres)
				case r.URL.Path == "/api/v1/presentation/artwork/"+oldHandle:
					mu.Lock()
					artworkByHandle[oldHandle]++
					mu.Unlock()
					http.NotFound(w, r)
				case r.URL.Path == "/api/v1/presentation/artwork/"+newHandle:
					mu.Lock()
					artworkByHandle[newHandle]++
					mu.Unlock()
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(pngBytes)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
			app.Start(t.Context())
			t.Cleanup(app.Stop)
			waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
				mu.Lock()
				art := artworkByHandle[oldHandle]
				pres := presentationGets
				mu.Unlock()
				return art >= 1 && pres >= 1 && len(snap.Games) == 1 && snap.CoverHits == 0
			})
			mu.Lock()
			afterOld := artworkByHandle[oldHandle]
			recovered = true
			mu.Unlock()
			waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
				if snap.FocusDetail.Summary != "Jump on turtles." {
					return false
				}
				if tc.wantHits {
					return snap.CoverHits >= 1
				}
				return true
			})
			hold := time.Now().Add(200 * time.Millisecond)
			for time.Now().Before(hold) {
				app.Tick(time.Now())
				time.Sleep(5 * time.Millisecond)
			}
			mu.Lock()
			laterOld := artworkByHandle[oldHandle]
			newGets := artworkByHandle[newHandle]
			mu.Unlock()
			if laterOld != afterOld {
				t.Fatalf("stale artwork retried: first=%d later=%d", afterOld, laterOld)
			}
			if afterOld != 1 {
				t.Fatalf("old artwork GETs = %d, want 1", afterOld)
			}
			if tc.wantHits && newGets < 1 {
				t.Fatalf("replacement artwork GETs = %d", newGets)
			}
			if !tc.wantHits && newGets != 0 {
				t.Fatalf("removed cover fetched replacement artwork: %d", newGets)
			}
		})
	}
}

func TestAppRetriesPresentationDetailsAfterFailedFetch(t *testing.T) {
	var mu sync.Mutex
	fail := true
	var presentationGets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			mu.Lock()
			presentationGets++
			shouldFail := fail
			mu.Unlock()
			if shouldFail {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &PresentationInfo{Summary: "Jump on turtles.", Year: "1985", Genre: "Platform"},
				Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
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
		mu.Lock()
		n := presentationGets
		mu.Unlock()
		return len(snap.Games) == 1 && n >= 1 && snap.FocusDetail.Summary == ""
	})
	mu.Lock()
	fail = false
	mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		d := snap.FocusDetail
		return d.Summary == "Jump on turtles." && d.Year == "1985" && d.Genre == "Platform" && d.Attribution == "Data from IGDB.com"
	})
}

func TestAppRetriesPresentationDetailsAfterOfflineState(t *testing.T) {
	var mu sync.Mutex
	offline := true
	var presentationGets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			mu.Lock()
			presentationGets++
			stillOffline := offline
			mu.Unlock()
			if stillOffline {
				_ = json.NewEncoder(w).Encode(Presentation{GameID: "snes-mario", State: "offline"})
				return
			}
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &PresentationInfo{Summary: "Jump on turtles.", Year: "1985", Genre: "Platform"},
				Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
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
		mu.Lock()
		n := presentationGets
		mu.Unlock()
		return len(snap.Games) == 1 && n >= 1 && snap.FocusDetail.Summary == ""
	})
	mu.Lock()
	offline = false
	mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		d := snap.FocusDetail
		return d.Summary == "Jump on turtles." && d.Year == "1985" && d.Genre == "Platform" && d.Attribution == "Data from IGDB.com"
	})
}

func TestAppRetriesReadyPresentationWithoutAttribution(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	var mu sync.Mutex
	readyOffline := true
	var presentationGets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			game := availableGame("snes-mario", "Mario", "snes")
			game.Cover = handle
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			mu.Lock()
			presentationGets++
			fallback := readyOffline
			mu.Unlock()
			pres := Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &PresentationInfo{CoverArtworkID: handle},
			}
			if !fallback {
				pres.Presentation.Summary = "Jump on turtles."
				pres.Presentation.Year = "1985"
				pres.Presentation.Genre = "Platform"
				pres.Attribution = &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"}
			}
			_ = json.NewEncoder(w).Encode(pres)
		case r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		n := presentationGets
		mu.Unlock()
		return len(snap.Games) == 1 && n >= 1 && snap.CoverHits >= 1 && snap.FocusDetail.Summary == "" && snap.FocusDetail.Attribution == ""
	})
	mu.Lock()
	afterFirst := presentationGets
	mu.Unlock()
	hold := time.Now().Add(presentationRetryMin / 2)
	for time.Now().Before(hold) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	duringBackoff := presentationGets
	mu.Unlock()
	if duringBackoff != afterFirst {
		t.Fatalf("presentation GETs during backoff: first=%d later=%d", afterFirst, duringBackoff)
	}
	mu.Lock()
	readyOffline = false
	mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		d := snap.FocusDetail
		return d.Summary == "Jump on turtles." && d.Year == "1985" && d.Genre == "Platform" && d.Attribution == "Data from IGDB.com"
	})
}

func TestAppCachesDisabledPresentationWithoutRetry(t *testing.T) {
	var mu sync.Mutex
	var presentationGets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			mu.Lock()
			presentationGets++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(Presentation{GameID: "snes-mario", State: "disabled"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		n := presentationGets
		mu.Unlock()
		return len(snap.Games) == 1 && n >= 1
	})
	mu.Lock()
	afterFirst := presentationGets
	mu.Unlock()
	hold := time.Now().Add(presentationRetryMin + 50*time.Millisecond)
	for time.Now().Before(hold) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	later := presentationGets
	mu.Unlock()
	if later != afterFirst {
		t.Fatalf("disabled presentation retried: first=%d later=%d", afterFirst, later)
	}
}

func TestAppCachesTerminalPresentationWithLocalMediaWithoutRetry(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	for _, state := range []string{"disabled", "unconfigured", "no_match", "ambiguous"} {
		t.Run(state, func(t *testing.T) {
			var mu sync.Mutex
			var presentationGets int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/games":
					game := availableGame("snes-mario", "Mario", "snes")
					game.Cover = handle
					_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
				case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
					mu.Lock()
					presentationGets++
					mu.Unlock()
					_ = json.NewEncoder(w).Encode(Presentation{
						GameID: "snes-mario",
						State:  state,
						Presentation: &PresentationInfo{CoverArtworkID: handle},
					})
				case r.URL.Path == "/api/v1/presentation/artwork/"+handle:
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(pngBytes)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
			app.Start(t.Context())
			t.Cleanup(app.Stop)
			waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
				mu.Lock()
				n := presentationGets
				mu.Unlock()
				return len(snap.Games) == 1 && snap.CoverHits >= 1 && n >= 1
			})
			mu.Lock()
			afterFirst := presentationGets
			mu.Unlock()
			hold := time.Now().Add(presentationRetryMin + 50*time.Millisecond)
			for time.Now().Before(hold) {
				app.Tick(time.Now())
				time.Sleep(5 * time.Millisecond)
			}
			mu.Lock()
			later := presentationGets
			mu.Unlock()
			if later != afterFirst {
				t.Fatalf("%s presentation retried: first=%d later=%d", state, afterFirst, later)
			}
		})
	}
}

func TestPresentationCompleteTreatsReadyWithoutAttributionAsOffline(t *testing.T) {
	t.Parallel()
	if presentationComplete("offline", "") {
		t.Fatal("offline should retry")
	}
	if presentationComplete("ready", "") {
		t.Fatal("ready without attribution should retry")
	}
	if presentationComplete("ready", "   ") {
		t.Fatal("ready with blank attribution should retry")
	}
	if !presentationComplete("ready", "Data from IGDB.com") {
		t.Fatal("ready with validated attribution is complete")
	}
	for _, state := range []string{"disabled", "unconfigured", "no_match", "ambiguous"} {
		if !presentationComplete(state, "") {
			t.Fatalf("%s remains complete without attribution", state)
		}
	}
}

func TestRestoreCatalogFocusKeepsIDThenClamps(t *testing.T) {
	t.Parallel()
	page1 := []Game{availableGame("a", "A", "snes")}
	page2 := append(append([]Game{}, page1...), availableGame("b", "B", "snes"))
	all := append(append([]Game{}, page2...), availableGame("c", "C", "snes"))
	if got := restoreCatalogFocus(page1, "c", 2); got != 0 {
		t.Fatalf("page1 clamp = %d", got)
	}
	if got := restoreCatalogFocus(page2, "c", 2); got != 1 {
		t.Fatalf("page2 clamp = %d want last present index", got)
	}
	if got := restoreCatalogFocus(all, "c", 2); got != 2 {
		t.Fatalf("found id = %d", got)
	}
	if got := restoreCatalogFocus(page2, "c", 2); got != 1 {
		t.Fatalf("missing id clamp = %d", got)
	}
	if got := restoreCatalogFocus(all, "missing", 1); got != 1 {
		t.Fatalf("keep original index = %d", got)
	}
}

func TestApplyCatalogPageRetainsNavDirtyAfterReturnToRestoredIndex(t *testing.T) {
	t.Parallel()
	app := NewApp(nil, 1280, 720, 10)
	page1 := []Game{availableGame("a", "A", "snes")}
	page2 := []Game{availableGame("a", "A", "snes"), availableGame("b", "B", "snes")}
	app.loading = true
	app.keepFocusID = "c"
	app.keepFocusIndex = 2
	pinned := true
	lastFocus := -1
	app.applyCatalogPageLocked(page1, "c", 2, &pinned, &lastFocus)
	if !pinned || lastFocus != 0 || app.grid.Focus != 0 {
		t.Fatalf("page1 restore pinned=%v last=%d focus=%d", pinned, lastFocus, app.grid.Focus)
	}
	app.applyCatalogPageLocked(page2, "c", 2, &pinned, &lastFocus)
	if !pinned || lastFocus != 1 || app.grid.Focus != 1 {
		t.Fatalf("page2 clamp pinned=%v last=%d focus=%d", pinned, lastFocus, app.grid.Focus)
	}
	app.grid.Focus = 0
	app.navDirty = true
	app.grid.Focus = lastFocus
	app.applyCatalogPageLocked(page2, "c", 2, &pinned, &lastFocus)
	if pinned {
		t.Fatal("returning to restored index must unpin pagination focus")
	}
	if !app.navDirty {
		t.Fatal("returning to restored index must not clear navDirty")
	}
	app.captureCatalogFocusLocked()
	if app.keepFocusID != "b" || app.keepFocusIndex != 1 {
		t.Fatalf("recapture keepID=%q keepIndex=%d", app.keepFocusID, app.keepFocusIndex)
	}
}

func TestAppKeepsFocusAcrossPaginatedReload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		games := []Game{
			availableGame("snes-alpha", "Alpha", "snes"),
			availableGame("snes-bravo", "Bravo", "snes"),
			availableGame("snes-charlie", "Charlie", "snes"),
		}
		cursor := r.URL.Query().Get("cursor")
		idx := 0
		if cursor != "" {
			idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
		}
		if idx < 0 || idx >= len(games) {
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
			return
		}
		next := ""
		if idx+1 < len(games) {
			next = "p" + strconv.Itoa(idx+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games":       []Game{games[idx]},
			"next_cursor": next,
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	if !app.focusIndex(2) {
		t.Fatal("focus charlie")
	}
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-charlie" {
		t.Fatalf("selected before reload = %#v ok=%v", selected, ok)
	}
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading && snap.Sort == "recently_added"
	})
	selected, ok = app.Selected()
	if !ok || selected.ID != "snes-charlie" {
		t.Fatalf("selected after paginated reload = %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppPaginationKeepsQueryWhileSearchPending(t *testing.T) {
	bravoGate := make(chan struct{})
	closeOnce := func(ch chan struct{}) func() {
		var once sync.Once
		return func() { once.Do(func() { close(ch) }) }
	}
	releaseBravo := closeOnce(bravoGate)
	t.Cleanup(releaseBravo)
	type gameCall struct {
		q      string
		cursor string
	}
	var mu sync.Mutex
	var calls []gameCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("q")
		cursor := r.URL.Query().Get("cursor")
		mu.Lock()
		calls = append(calls, gameCall{q: q, cursor: cursor})
		mu.Unlock()
		if q == "sonic" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")},
			})
			return
		}
		games := []Game{
			availableGame("snes-alpha", "Alpha", "snes"),
			availableGame("snes-bravo", "Bravo", "snes"),
			availableGame("snes-charlie", "Charlie", "snes"),
		}
		idx := 0
		if cursor != "" {
			idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
		}
		if idx < 0 || idx >= len(games) {
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
			return
		}
		if idx == 1 {
			<-bravoGate
		}
		next := ""
		if idx+1 < len(games) {
			next = "p" + strconv.Itoa(idx+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games":       []Game{games[idx]},
			"next_cursor": next,
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && snap.Loading && snap.Games[0].ID == "snes-alpha"
	})
	now := time.Now()
	app.Press(CmdSearch, now)
	app.TypeText("sonic", now)
	if got := app.Snapshot().Query; got != "sonic" {
		t.Fatalf("typed query = %q", got)
	}
	releaseBravo()
	deadline := time.Now().Add(2 * time.Second)
	var snap Snapshot
	for time.Now().Before(deadline) {
		app.Tick(now)
		snap = app.Snapshot()
		if len(snap.Games) == 3 && !snap.Loading {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(snap.Games) != 3 || snap.Loading {
		t.Fatalf("paginated load mixed search text: status=%q err=%q games=%d loading=%v", snap.Status, snap.LoadErr, len(snap.Games), snap.Loading)
	}
	if snap.Games[0].ID != "snes-alpha" || snap.Games[1].ID != "snes-bravo" || snap.Games[2].ID != "snes-charlie" {
		t.Fatalf("games = %#v", snap.Games)
	}
	mu.Lock()
	got := append([]gameCall(nil), calls...)
	mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("expected 3 unfiltered pages, calls=%#v", got)
	}
	for i, call := range got[:3] {
		if call.q != "" {
			t.Fatalf("page %d used pending search q=%q cursor=%q calls=%#v", i, call.q, call.cursor, got)
		}
	}
	if got[0].cursor != "" || got[1].cursor != "p1" || got[2].cursor != "p2" {
		t.Fatalf("unfiltered cursors = %#v", got[:3])
	}
	app.Tick(now.Add(searchDebounce + time.Millisecond))
	waitSnapshot(t, app, 2*time.Second, func(s Snapshot) bool {
		return s.Query == "sonic" && !s.Loading && len(s.Games) == 1 && s.Games[0].ID == "megadrive-sonic"
	})
}

func TestAppKeepsUserNavDuringInitialPagination(t *testing.T) {
	bravoGate := make(chan struct{})
	charlieGate := make(chan struct{})
	closeOnce := func(ch chan struct{}) func() {
		var once sync.Once
		return func() { once.Do(func() { close(ch) }) }
	}
	releaseBravo := closeOnce(bravoGate)
	releaseCharlie := closeOnce(charlieGate)
	t.Cleanup(releaseBravo)
	t.Cleanup(releaseCharlie)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		games := []Game{
			availableGame("snes-alpha", "Alpha", "snes"),
			availableGame("snes-bravo", "Bravo", "snes"),
			availableGame("snes-charlie", "Charlie", "snes"),
		}
		cursor := r.URL.Query().Get("cursor")
		idx := 0
		if cursor != "" {
			idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
		}
		if idx < 0 || idx >= len(games) {
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
			return
		}
		switch idx {
		case 1:
			<-bravoGate
		case 2:
			<-charlieGate
		}
		next := ""
		if idx+1 < len(games) {
			next = "p" + strconv.Itoa(idx+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games":       []Game{games[idx]},
			"next_cursor": next,
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && snap.Loading
	})
	releaseBravo()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && snap.Loading
	})
	app.Press(CmdRight, time.Now())
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-bravo" {
		t.Fatalf("user nav during pagination = %#v ok=%v", selected, ok)
	}
	releaseCharlie()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	selected, ok = app.Selected()
	if !ok || selected.ID != "snes-bravo" {
		t.Fatalf("later pages must not snap focus back to 0, got %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppKeepsUserNavDuringPaginatedReload(t *testing.T) {
	bravoGate := make(chan struct{})
	charlieGate := make(chan struct{})
	closeOnce := func(ch chan struct{}) func() {
		var once sync.Once
		return func() { once.Do(func() { close(ch) }) }
	}
	releaseBravo := closeOnce(bravoGate)
	releaseCharlie := closeOnce(charlieGate)
	t.Cleanup(releaseBravo)
	t.Cleanup(releaseCharlie)
	var mu sync.Mutex
	var gameCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		games := []Game{
			availableGame("snes-alpha", "Alpha", "snes"),
			availableGame("snes-bravo", "Bravo", "snes"),
			availableGame("snes-charlie", "Charlie", "snes"),
		}
		cursor := r.URL.Query().Get("cursor")
		idx := 0
		if cursor != "" {
			idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
		}
		if idx < 0 || idx >= len(games) {
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
			return
		}
		mu.Lock()
		gameCalls++
		call := gameCalls
		mu.Unlock()
		if call > 3 {
			switch idx {
			case 1:
				<-bravoGate
			case 2:
				<-charlieGate
			}
		}
		next := ""
		if idx+1 < len(games) {
			next = "p" + strconv.Itoa(idx+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games":       []Game{games[idx]},
			"next_cursor": next,
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	if !app.focusIndex(2) {
		t.Fatal("focus charlie")
	}
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Sort == "recently_added" && len(snap.Games) == 1 && snap.Loading
	})
	releaseBravo()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && snap.Loading
	})
	if !app.focusIndex(0) {
		t.Fatal("user moved to alpha during pagination")
	}
	releaseCharlie()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-alpha" {
		t.Fatalf("later pages must not overwrite user nav, got %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppBlocksLaunchWhileCatalogReloading(t *testing.T) {
	var mu sync.Mutex
	var launches int
	holdReload := make(chan struct{})
	var releaseOnce sync.Once
	releaseReload := func() { releaseOnce.Do(func() { close(holdReload) }) }
	t.Cleanup(releaseReload)
	var gamesCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			mu.Lock()
			gamesCalls++
			call := gamesCalls
			mu.Unlock()
			if call > 1 {
				<-holdReload
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{
					availableGame("snes-alpha", "Alpha", "snes"),
					availableGame("snes-bravo", "Bravo", "snes"),
				},
			})
		case r.URL.Path == "/api/v1/session/launch":
			mu.Lock()
			launches++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-alpha"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && !snap.Loading
	})
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Loading && snap.Sort == "recently_added"
	})
	app.Press(CmdSelect, time.Now())
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	got := launches
	mu.Unlock()
	if got != 0 {
		t.Fatalf("launch POSTed during reload, count=%d", got)
	}
	if phase := app.Snapshot().Launch.Phase; phase == "launching" || phase == "ok" {
		t.Fatalf("launch phase during reload = %q", phase)
	}
	releaseReload()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 2
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok"
	})
	mu.Lock()
	got = launches
	mu.Unlock()
	if got != 1 {
		t.Fatalf("expected one launch after reload, got %d", got)
	}
}

func TestAppClampsFocusWhenReloadedIDMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"platforms": []Platform{{ID: "snes", Label: "Super NES"}, {ID: "megadrive", Label: "Mega Drive"}},
			})
		case r.URL.Path == "/api/v1/games":
			platform := r.URL.Query().Get("platform")
			games := []Game{
				availableGame("snes-alpha", "Alpha", "snes"),
				availableGame("snes-bravo", "Bravo", "snes"),
				availableGame("megadrive-charlie", "Charlie", "megadrive"),
			}
			if platform == "snes" {
				games = games[:2]
			}
			cursor := r.URL.Query().Get("cursor")
			idx := 0
			if cursor != "" {
				idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
			}
			if idx < 0 || idx >= len(games) {
				_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
				return
			}
			next := ""
			if idx+1 < len(games) {
				next = "p" + strconv.Itoa(idx+1)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games":       []Game{games[idx]},
				"next_cursor": next,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && len(snap.Platforms) == 2 && !snap.Loading
	})
	if !app.focusIndex(2) {
		t.Fatal("focus charlie")
	}
	app.Press(CmdFilterNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.PlatformID == "snes" && len(snap.Games) == 2 && !snap.Loading
	})
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-bravo" {
		t.Fatalf("missing id should clamp to last present, got %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppSelectClosesSearchFlushesBeforeLaunch(t *testing.T) {
	var mu sync.Mutex
	var launches []string
	holdSearch := make(chan struct{})
	var releaseOnce sync.Once
	releaseSearch := func() { releaseOnce.Do(func() { close(holdSearch) }) }
	t.Cleanup(releaseSearch)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			q := r.URL.Query().Get("q")
			if q != "" {
				<-holdSearch
			}
			games := []Game{availableGame("snes-mario", "Mario", "snes"), availableGame("megadrive-sonic", "Sonic", "megadrive")}
			if q == "sonic" {
				games = []Game{availableGame("megadrive-sonic", "Sonic", "megadrive")}
			}
			if q != "" && q != "sonic" {
				games = nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"games": games})
		case r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			launches = append(launches, string(raw))
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"megadrive-sonic"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && !snap.Loading
	})
	now := time.Now()
	app.Press(CmdSearch, now)
	app.TypeText("sonic", now)
	focusSearchKey(t, app, "done")
	app.Press(CmdSelect, now)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.SearchOpen && snap.Loading && snap.Query == "sonic"
	})
	app.Press(CmdSelect, now)
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	got := append([]string(nil), launches...)
	mu.Unlock()
	if len(got) != 0 {
		t.Fatalf("launch POSTed before pending search applied, launches=%v", got)
	}
	if phase := app.Snapshot().Launch.Phase; phase == "launching" || phase == "ok" {
		t.Fatalf("launch phase while search pending/reloading = %q", phase)
	}
	releaseSearch()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && snap.Games[0].ID == "megadrive-sonic"
	})
	app.Press(CmdSelect, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Launch.Phase == "ok"
	})
	mu.Lock()
	got = append([]string(nil), launches...)
	mu.Unlock()
	if len(got) != 1 || got[0] != `{"game_id":"megadrive-sonic"}` {
		t.Fatalf("expected sonic launch after search applied, got %v", got)
	}
}

func TestAppCancelsSupersededCatalogRequest(t *testing.T) {
	var mu sync.Mutex
	var gamesCalls int
	blocked := make(chan struct{})
	canceled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		gamesCalls++
		call := gamesCalls
		mu.Unlock()
		if call == 2 {
			close(blocked)
			select {
			case <-r.Context().Done():
				select {
				case canceled <- struct{}{}:
				default:
				}
				return
			case <-time.After(3 * time.Second):
				http.Error(w, "superseded games GET was not canceled", http.StatusGatewayTimeout)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []Game{availableGame("snes-alpha", "Alpha", "snes")},
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 1 && !snap.Loading
	})
	app.Press(CmdSortCycle, time.Now())
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("reload did not reach games GET")
	}
	app.Press(CmdSortCycle, time.Now())
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("superseded games GET was not canceled")
	}
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && snap.Sort == "platform" && snap.LoadErr == ""
	})
}

func TestAppCancelsSupersededCoverWork(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	blocked := make(chan struct{})
	canceled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			game := availableGame("snes-alpha", "Alpha", "snes")
			game.Cover = handle
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
		case r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			select {
			case <-blocked:
			default:
				close(blocked)
				select {
				case <-r.Context().Done():
					select {
					case canceled <- struct{}{}:
					default:
					}
					return
				case <-time.After(3 * time.Second):
					http.Error(w, "superseded artwork GET was not canceled", http.StatusGatewayTimeout)
					return
				}
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.Tick(time.Now())
		select {
		case <-blocked:
			goto artworkStarted
		default:
		}
		if !time.Now().Before(deadline) {
			t.Fatal("cover work did not reach artwork GET")
		}
		time.Sleep(5 * time.Millisecond)
	}
artworkStarted:
	app.Press(CmdSortCycle, time.Now())
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("superseded artwork GET was not canceled")
	}
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && snap.CoverHits >= 1 && snap.LoadErr == ""
	})
}

func TestAppCancelsSupersededPresentationWork(t *testing.T) {
	handle := strings.Repeat("cd", 32)
	pngBytes := mustPNG(t, 8, 12, color.RGBA{R: 20, G: 80, B: 200, A: 255})
	blocked := make(chan struct{})
	canceled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{availableGame("snes-alpha", "Alpha", "snes")}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			select {
			case <-blocked:
			default:
				close(blocked)
				select {
				case <-r.Context().Done():
					select {
					case canceled <- struct{}{}:
					default:
					}
					return
				case <-time.After(3 * time.Second):
					http.Error(w, "superseded presentation GET was not canceled", http.StatusGatewayTimeout)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-alpha",
				State:  "ready",
				Presentation: &PresentationInfo{
					CoverArtworkID: handle,
					Summary:        "Alpha",
				},
				Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
			})
		case r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.Tick(time.Now())
		select {
		case <-blocked:
			goto presentationStarted
		default:
		}
		if !time.Now().Before(deadline) {
			t.Fatal("cover work did not reach presentation GET")
		}
		time.Sleep(5 * time.Millisecond)
	}
presentationStarted:
	app.Press(CmdSortCycle, time.Now())
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("superseded presentation GET was not canceled")
	}
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1 && snap.CoverHits >= 1 && snap.LoadErr == ""
	})
}

func TestReplaceLoadContextDrainsJobsAndCancelsMediaContext(t *testing.T) {
	t.Parallel()
	app := NewApp(nil, 800, 600, 10)
	parent, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	app.ctx = parent
	first := app.replaceLoadContextLocked()
	if first.Err() != nil {
		t.Fatal("fresh job ctx should be live")
	}
	for i := 0; i < coverJobBuffer; i++ {
		select {
		case app.jobs <- workItem{kind: workArtwork, gameID: "stale-" + strconv.Itoa(i), gen: 1}:
		default:
			t.Fatal("jobs buffer should accept fill")
		}
	}
	if got := len(app.jobs); got != coverJobBuffer {
		t.Fatalf("queued = %d want %d", got, coverJobBuffer)
	}
	second := app.replaceLoadContextLocked()
	if first.Err() == nil {
		t.Fatal("replaced job ctx should be canceled")
	}
	if second.Err() != nil {
		t.Fatal("new job ctx should be live")
	}
	if second == first {
		t.Fatal("replace should install a new job ctx")
	}
	if app.jobCtx != second {
		t.Fatal("jobCtx should be the replacement")
	}
	if got := len(app.jobs); got != 0 {
		t.Fatalf("stale jobs remaining = %d", got)
	}
	select {
	case <-app.jobs:
		t.Fatal("drained jobs channel still readable")
	default:
	}
}

func TestWorkerRejectsStaleMediaJobBeforeIO(t *testing.T) {
	handle := strings.Repeat("ab", 32)
	staleHandle := strings.Repeat("ef", 32)
	stalePresID := "snes-stale-pres"
	var staleArtworkGets, stalePresentationGets atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/games":
			game := availableGame("snes-mario", "Mario", "snes")
			game.Cover = handle
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{game}})
		case r.URL.Path == "/api/v1/presentation/games/"+stalePresID:
			stalePresentationGets.Add(1)
			http.NotFound(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/presentation/games/")
			_ = json.NewEncoder(w).Encode(Presentation{GameID: id, State: "offline"})
		case r.URL.Path == "/api/v1/presentation/artwork/"+staleHandle:
			staleArtworkGets.Add(1)
			http.NotFound(w, r)
		case r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 1
	})
	app.mu.Lock()
	staleGen := app.loadGen
	app.loadGen++
	app.mu.Unlock()
	jobs := []workItem{
		{kind: workArtwork, gameID: "snes-stale-art", handle: staleHandle, gen: staleGen},
		{kind: workPresentation, gameID: stalePresID, handle: staleHandle, gen: staleGen},
	}
	for _, item := range jobs {
		select {
		case app.jobs <- item:
		case <-time.After(time.Second):
			t.Fatal("jobs channel blocked")
		}
	}
	hold := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(hold) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	if staleArtworkGets.Load() != 0 {
		t.Fatalf("stale artwork job performed I/O, gets=%d", staleArtworkGets.Load())
	}
	if stalePresentationGets.Load() != 0 {
		t.Fatalf("stale presentation job performed I/O, gets=%d", stalePresentationGets.Load())
	}
}

func TestAppOverlappingReloadKeepsPreClearFocus(t *testing.T) {
	holdReload := make(chan struct{})
	var releaseOnce sync.Once
	releaseReload := func() { releaseOnce.Do(func() { close(holdReload) }) }
	t.Cleanup(releaseReload)
	var mu sync.Mutex
	var gameCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		gameCalls++
		call := gameCalls
		mu.Unlock()
		if call > 1 {
			<-holdReload
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []Game{
				availableGame("snes-alpha", "Alpha", "snes"),
				availableGame("snes-bravo", "Bravo", "snes"),
				availableGame("snes-charlie", "Charlie", "snes"),
			},
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	if !app.focusIndex(2) {
		t.Fatal("focus charlie")
	}
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Loading && len(snap.Games) == 0 && snap.Sort == "recently_added"
	})
	app.Press(CmdSortCycle, time.Now())
	releaseReload()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 3 && snap.Sort == "platform"
	})
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-charlie" {
		t.Fatalf("overlapping reload dropped keep focus, got %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppKeepsFocusWhenCyclingDuringPartialReload(t *testing.T) {
	bravoGate := make(chan struct{})
	charlieGate := make(chan struct{})
	closeOnce := func(ch chan struct{}) func() {
		var once sync.Once
		return func() { once.Do(func() { close(ch) }) }
	}
	releaseBravo := closeOnce(bravoGate)
	releaseCharlie := closeOnce(charlieGate)
	t.Cleanup(releaseBravo)
	t.Cleanup(releaseCharlie)
	var mu sync.Mutex
	var gameCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		games := []Game{
			availableGame("snes-alpha", "Alpha", "snes"),
			availableGame("snes-bravo", "Bravo", "snes"),
			availableGame("snes-charlie", "Charlie", "snes"),
		}
		cursor := r.URL.Query().Get("cursor")
		idx := 0
		if cursor != "" {
			idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
		}
		if idx < 0 || idx >= len(games) {
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
			return
		}
		mu.Lock()
		gameCalls++
		call := gameCalls
		mu.Unlock()
		if call > 3 {
			switch idx {
			case 1:
				<-bravoGate
			case 2:
				<-charlieGate
			}
		}
		next := ""
		if idx+1 < len(games) {
			next = "p" + strconv.Itoa(idx+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games":       []Game{games[idx]},
			"next_cursor": next,
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	if !app.focusIndex(2) {
		t.Fatal("focus charlie")
	}
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Sort == "recently_added" && len(snap.Games) == 1 && snap.Loading
	})
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Sort == "platform" && len(snap.Games) == 1 && snap.Loading
	})
	releaseBravo()
	releaseCharlie()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 3 && snap.Sort == "platform"
	})
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-charlie" {
		t.Fatalf("partial reload recapture dropped keep focus, got %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppRecapturesFocusWhenUserMovesDuringPartialReload(t *testing.T) {
	bravoGate := make(chan struct{})
	charlieGate := make(chan struct{})
	closeOnce := func(ch chan struct{}) func() {
		var once sync.Once
		return func() { once.Do(func() { close(ch) }) }
	}
	releaseBravo := closeOnce(bravoGate)
	releaseCharlie := closeOnce(charlieGate)
	t.Cleanup(releaseBravo)
	t.Cleanup(releaseCharlie)
	var mu sync.Mutex
	var gameCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		games := []Game{
			availableGame("snes-alpha", "Alpha", "snes"),
			availableGame("snes-bravo", "Bravo", "snes"),
			availableGame("snes-charlie", "Charlie", "snes"),
		}
		cursor := r.URL.Query().Get("cursor")
		idx := 0
		if cursor != "" {
			idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
		}
		if idx < 0 || idx >= len(games) {
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
			return
		}
		mu.Lock()
		gameCalls++
		call := gameCalls
		mu.Unlock()
		if call > 3 {
			switch idx {
			case 1:
				<-bravoGate
			case 2:
				<-charlieGate
			}
		}
		next := ""
		if idx+1 < len(games) {
			next = "p" + strconv.Itoa(idx+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games":       []Game{games[idx]},
			"next_cursor": next,
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	if !app.focusIndex(2) {
		t.Fatal("focus charlie")
	}
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Sort == "recently_added" && len(snap.Games) == 1 && snap.Loading
	})
	releaseBravo()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && snap.Loading
	})
	if !app.focusIndex(0) {
		t.Fatal("user moved to alpha during pagination")
	}
	app.Press(CmdSortCycle, time.Now())
	releaseCharlie()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 3 && snap.Sort == "platform"
	})
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-alpha" {
		t.Fatalf("user nav during partial reload should recapture, got %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppRecapturesFocusWhenUserReturnsToRestoredIndex(t *testing.T) {
	bravoGate := make(chan struct{})
	charlieGate := make(chan struct{})
	closeOnce := func(ch chan struct{}) func() {
		var once sync.Once
		return func() { once.Do(func() { close(ch) }) }
	}
	releaseBravo := closeOnce(bravoGate)
	releaseCharlie := closeOnce(charlieGate)
	t.Cleanup(releaseBravo)
	t.Cleanup(releaseCharlie)
	var mu sync.Mutex
	var gameCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		games := []Game{
			availableGame("snes-alpha", "Alpha", "snes"),
			availableGame("snes-bravo", "Bravo", "snes"),
			availableGame("snes-charlie", "Charlie", "snes"),
		}
		cursor := r.URL.Query().Get("cursor")
		idx := 0
		if cursor != "" {
			idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
		}
		if idx < 0 || idx >= len(games) {
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
			return
		}
		mu.Lock()
		gameCalls++
		call := gameCalls
		mu.Unlock()
		if call > 3 {
			switch idx {
			case 1:
				<-bravoGate
			case 2:
				<-charlieGate
			}
		}
		next := ""
		if idx+1 < len(games) {
			next = "p" + strconv.Itoa(idx+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games":       []Game{games[idx]},
			"next_cursor": next,
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	if !app.focusIndex(2) {
		t.Fatal("focus charlie")
	}
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Sort == "recently_added" && len(snap.Games) == 1 && snap.Loading
	})
	releaseBravo()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && snap.Loading && snap.Grid.Focus == 1
	})
	app.Press(CmdLeft, time.Now())
	if got := app.Snapshot().Grid.Focus; got != 0 {
		t.Fatalf("move away focus=%d", got)
	}
	app.Press(CmdRight, time.Now())
	if got := app.Snapshot().Grid.Focus; got != 1 {
		t.Fatalf("return to restored index focus=%d", got)
	}
	app.Press(CmdSortCycle, time.Now())
	releaseCharlie()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 3 && snap.Sort == "platform"
	})
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-bravo" {
		t.Fatalf("return to restored index should recapture bravo, got %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppKeepsUserNavWhenReturningToClampedIndexDuringPagination(t *testing.T) {
	bravoGate := make(chan struct{})
	charlieGate := make(chan struct{})
	closeOnce := func(ch chan struct{}) func() {
		var once sync.Once
		return func() { once.Do(func() { close(ch) }) }
	}
	releaseBravo := closeOnce(bravoGate)
	releaseCharlie := closeOnce(charlieGate)
	t.Cleanup(releaseBravo)
	t.Cleanup(releaseCharlie)
	var mu sync.Mutex
	var gameCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		games := []Game{
			availableGame("snes-alpha", "Alpha", "snes"),
			availableGame("snes-bravo", "Bravo", "snes"),
			availableGame("snes-charlie", "Charlie", "snes"),
		}
		cursor := r.URL.Query().Get("cursor")
		idx := 0
		if cursor != "" {
			idx, _ = strconv.Atoi(strings.TrimPrefix(cursor, "p"))
		}
		if idx < 0 || idx >= len(games) {
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
			return
		}
		mu.Lock()
		gameCalls++
		call := gameCalls
		mu.Unlock()
		if call > 3 {
			switch idx {
			case 1:
				<-bravoGate
			case 2:
				<-charlieGate
			}
		}
		next := ""
		if idx+1 < len(games) {
			next = "p" + strconv.Itoa(idx+1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games":       []Game{games[idx]},
			"next_cursor": next,
		})
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.pageLimit = 1
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 3 && !snap.Loading
	})
	if !app.focusIndex(2) {
		t.Fatal("focus charlie")
	}
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Sort == "recently_added" && len(snap.Games) == 1 && snap.Loading
	})
	releaseBravo()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return len(snap.Games) == 2 && snap.Loading && snap.Grid.Focus == 1
	})
	app.Press(CmdLeft, time.Now())
	if got := app.Snapshot().Grid.Focus; got != 0 {
		t.Fatalf("move away focus=%d", got)
	}
	app.Press(CmdRight, time.Now())
	if got := app.Snapshot().Grid.Focus; got != 1 {
		t.Fatalf("return to restored index focus=%d", got)
	}
	releaseCharlie()
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.Loading && len(snap.Games) == 3 && snap.Sort == "recently_added"
	})
	selected, ok := app.Selected()
	if !ok || selected.ID != "snes-bravo" {
		t.Fatalf("later pages must not snap after return to clamped index, got %#v ok=%v focus=%d", selected, ok, app.Snapshot().Grid.Focus)
	}
}

func TestAppRetriesFailedPlatformListAndSurfacesStatus(t *testing.T) {
	var mu sync.Mutex
	var platformGets int
	fail := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/platforms":
			mu.Lock()
			platformGets++
			shouldFail := fail
			mu.Unlock()
			if shouldFail {
				http.Error(w, "temporary", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"platforms": []Platform{{ID: "snes", Label: "Super NES"}, {ID: "megadrive", Label: "Mega Drive"}},
			})
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
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
		return len(snap.Games) == 1 && len(snap.Platforms) == 0 && strings.Contains(snap.Status, "platform list failed")
	})
	app.Press(CmdFilterNext, time.Now())
	if id := app.Snapshot().PlatformID; id != "" {
		t.Fatalf("shoulder cycled with empty platforms, platform=%q", id)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	waitSnapshot(t, app, 3*time.Second, func(snap Snapshot) bool {
		return len(snap.Platforms) == 2 && !strings.Contains(snap.Status, "platform list failed")
	})
	app.Press(CmdFilterNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.PlatformID == "snes" && !snap.Loading
	})
}

func TestAppBacksOffFailedPlatformListRetriesUntilShoulderKick(t *testing.T) {
	var mu sync.Mutex
	var platformGets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/platforms":
			mu.Lock()
			platformGets++
			mu.Unlock()
			http.Error(w, "temporary", http.StatusInternalServerError)
		case r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games": []Game{availableGame("snes-mario", "Mario", "snes")},
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
		mu.Lock()
		n := platformGets
		mu.Unlock()
		return n >= 1 && strings.Contains(snap.Status, "platform list failed")
	})
	mu.Lock()
	afterFirst := platformGets
	mu.Unlock()
	hold := time.Now().Add(presentationRetryMin / 2)
	for time.Now().Before(hold) {
		app.Tick(time.Now())
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	duringBackoff := platformGets
	mu.Unlock()
	if duringBackoff != afterFirst {
		t.Fatalf("platform GETs during backoff: first=%d later=%d", afterFirst, duringBackoff)
	}
	app.Press(CmdFilterNext, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		mu.Lock()
		n := platformGets
		mu.Unlock()
		return n > afterFirst
	})
}

func TestFocusDetailMetaLineAndWrap(t *testing.T) {
	t.Parallel()
	got := FocusDetail{Platform: "Super NES", Year: "1985", Genre: "Platform"}.MetaLine()
	if got != "Super NES  ·  1985  ·  Platform" {
		t.Fatalf("meta = %q", got)
	}
	got = FocusDetail{Platform: "Super NES", Year: "1985", Genre: "Platform", Attribution: "Data from IGDB.com"}.MetaLine()
	if got != "Super NES  ·  1985  ·  Platform  ·  Data from IGDB.com" {
		t.Fatalf("attributed meta = %q", got)
	}
	facts := FocusDetail{Platform: "Super NES", Year: "1985", Genre: "Platform", Attribution: "Data from IGDB.com"}.MetaFacts()
	if facts != "Super NES  ·  1985  ·  Platform" {
		t.Fatalf("facts = %q", facts)
	}
	lines := wrapWords("Jump on turtles and save the princess from the castle.", 20, 2)
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "Jump") {
		t.Fatalf("lines = %#v", lines)
	}
}

func TestLayoutDetailMetaReservesAttribution(t *testing.T) {
	t.Parallel()
	d := FocusDetail{
		Platform:    "Super Nintendo Entertainment System",
		Year:        "1985",
		Genre:       "Platform",
		Attribution: "Data from IGDB.com",
	}
	const maxWidth = 280
	facts, factsX, factsW, attr, attrX, attrW := layoutDetailMeta(d, 24, maxWidth, 16)
	if attr != "Data from IGDB.com" {
		t.Fatalf("attr = %q", attr)
	}
	if facts == "" || factsW < 1 || attrW < 1 {
		t.Fatalf("layout facts=%q factsW=%d attrW=%d", facts, factsW, attrW)
	}
	if attrX <= factsX || factsX != 24 {
		t.Fatalf("positions factsX=%d attrX=%d", factsX, attrX)
	}
	if factsW+detailMetaGap+attrW != maxWidth {
		t.Fatalf("widths facts=%d gap=%d attr=%d max=%d", factsW, detailMetaGap, attrW, maxWidth)
	}
	face := goRegularFace(t, 16)
	if fitLabel(face, attr, attrW) != attr {
		t.Fatalf("attribution clipped to %q", fitLabel(face, attr, attrW))
	}
	clippedFacts := fitLabel(face, facts, factsW)
	if strings.Contains(clippedFacts, "IGDB") {
		t.Fatalf("facts include attribution: %q", clippedFacts)
	}
}

func waitSnapshot(t *testing.T, app *App, d time.Duration, ok func(Snapshot) bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	var snap Snapshot
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		snap = app.Snapshot()
		if ok(snap) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout status=%q err=%q games=%d platform=%q sort=%q query=%q collection=%q view=%q detail=%#v", snap.Status, snap.LoadErr, len(snap.Games), snap.PlatformID, snap.Sort, snap.Query, snap.Collection, snap.ViewLabel, snap.FocusDetail)
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

func cloneStringIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
