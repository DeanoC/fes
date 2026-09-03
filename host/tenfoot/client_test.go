package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListsGamesAndFollowsCursor(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.URL.Query().Get("cursor") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"games":       []Game{{ID: "snes-mario", Title: "Mario", System: "snes", Launchable: true}},
				"next_cursor": "page-2",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []Game{{ID: "megadrive-sonic", Title: "Sonic", System: "megadrive", Launchable: true}},
		})
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	games, err := client.FetchLibrary(context.Background(), GameListQuery{Limit: 200, Sort: "title"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0].ID != "snes-mario" || games[1].ID != "megadrive-sonic" {
		t.Fatalf("games = %#v", games)
	}
	if len(paths) != 2 || !strings.Contains(paths[0], "grouped=1") || !strings.Contains(paths[0], "availability=ready") || !strings.Contains(paths[0], "limit=200") || !strings.Contains(paths[0], "sort=title") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestListGamesPrefersLaunchableVariant(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []Game{{
				ID:         "snes-sonic-usa",
				Title:      "Sonic",
				System:     "snes",
				State:      "available",
				RootOnline: false,
				Launchable: true,
				Variants: []Game{
					{ID: "snes-sonic-usa", Title: "Sonic", System: "snes", State: "available", RootOnline: false, Launchable: true},
					{ID: "snes-sonic-japan", Title: "Sonic", System: "snes", State: "available", RootOnline: true, Launchable: true},
				},
			}},
		})
	}))
	t.Cleanup(server.Close)
	games, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), GameListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].ID != "snes-sonic-japan" {
		t.Fatalf("games = %#v", games)
	}
	if games[0].Variants != nil {
		t.Fatalf("variants leaked into catalog row: %#v", games[0].Variants)
	}
}

func TestClientPresentationArtworkAndLaunch(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ab", 32)
	var launchBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/games/snes-mario":
			_ = json.NewEncoder(w).Encode(Presentation{
				GameID: "snes-mario",
				State:  "ready",
				Presentation: &struct {
					CoverArtworkID string `json:"cover_artwork_id"`
					Summary        string `json:"summary"`
					Year           string `json:"year"`
					Genre          string `json:"genre"`
					Studio         string `json:"studio"`
				}{CoverArtworkID: handle, Summary: "jump", Year: "1985", Genre: "Platform", Studio: "Nintendo"},
				Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/presentation/artwork/"+handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png-bytes"))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			raw, _ := io.ReadAll(r.Body)
			launchBody = string(raw)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	pres, err := client.GamePresentation(context.Background(), "snes-mario")
	if err != nil || CoverHandle(Game{}, pres) != handle {
		t.Fatalf("presentation = %#v, %v", pres, err)
	}
	if got := pres.AttributionLabel(); got != "Data from IGDB.com" {
		t.Fatalf("attribution = %q", got)
	}
	data, ctype, err := client.Artwork(context.Background(), handle)
	if err != nil || ctype != "image/png" || string(data) != "png-bytes" {
		t.Fatalf("artwork = %q %q %v", data, ctype, err)
	}
	result, err := client.Launch(context.Background(), "snes-mario")
	if err != nil || result.HTTPStatus != 200 || result.State != "active" || result.GameID != "snes-mario" {
		t.Fatalf("launch = %#v, %v", result, err)
	}
	if launchBody != `{"game_id":"snes-mario"}` {
		t.Fatalf("launch body = %q", launchBody)
	}
}

func TestClientSessionAndStop(t *testing.T) {
	t.Parallel()
	var stopPath, stopBody string
	var stopCT string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{
				"state":"active",
				"game_id":"snes-mario",
				"system":"snes",
				"execution":"fpga_native",
				"media":"active",
				"progress":{"stage":"core","message":"running"},
				"input":{"state":"attached","ready":true}
			}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			stopPath = r.URL.Path
			stopBody = string(raw)
			stopCT = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped","execution":"fpga_native"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	got, err := client.Session(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.HTTPStatus != 200 || got.State != "active" || got.GameID != "snes-mario" || got.System != "snes" {
		t.Fatalf("session = %#v", got)
	}
	if got.Execution != "fpga_native" || got.Media != "active" {
		t.Fatalf("session overlays = %#v", got)
	}
	if got.Progress == nil || got.Progress.Stage != "core" || got.Progress.Message != "running" {
		t.Fatalf("progress = %#v", got.Progress)
	}
	if got.Input == nil || got.Input.State != "attached" || !got.Input.Ready {
		t.Fatalf("input = %#v", got.Input)
	}

	idleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(idleServer.Close)
	idle, err := NewClient(idleServer.URL, idleServer.Client()).Session(context.Background())
	if err != nil || idle.State != "idle" || idle.GameID != "" || idle.System != "" {
		t.Fatalf("idle session = %#v, %v", idle, err)
	}

	stopped, err := client.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stopped.HTTPStatus != 200 || stopped.State != "idle" || stopped.Media != "stopped" || stopped.Execution != "fpga_native" {
		t.Fatalf("stop = %#v", stopped)
	}
	if stopPath != "/api/v1/session/stop" || stopBody != "" {
		t.Fatalf("stop request path=%q body=%q", stopPath, stopBody)
	}
	if stopCT != "" {
		t.Fatalf("stop Content-Type = %q", stopCT)
	}
}

func TestClientRejectsMalformedSessionResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code int
		body string
	}{
		{name: "empty", code: 200, body: ""},
		{name: "204", code: 204, body: ""},
		{name: "empty object", code: 200, body: `{}`},
		{name: "unknown state", code: 200, body: `{"state":"nope"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			client := NewClient(server.URL, server.Client())
			if _, err := client.Session(context.Background()); err == nil {
				t.Fatal("Session accepted malformed body")
			}
			if _, err := client.Launch(context.Background(), "snes-mario"); err == nil {
				t.Fatal("Launch accepted malformed body")
			}
			if _, err := client.Stop(context.Background()); err == nil {
				t.Fatal("Stop accepted malformed body")
			}
		})
	}
}

func TestClientLaunchRejectsNonActiveSuccess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"state":"idle"}`)
	}))
	t.Cleanup(server.Close)
	if _, err := NewClient(server.URL, server.Client()).Launch(context.Background(), "snes-mario"); err == nil {
		t.Fatal("Launch accepted idle success")
	}
}

func TestClientStopRejectsNonIdleSuccess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario"}`)
	}))
	t.Cleanup(server.Close)
	if _, err := NewClient(server.URL, server.Client()).Stop(context.Background()); err == nil {
		t.Fatal("Stop accepted active success")
	}
}

func TestClientLaunchRecordsHostError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"SOURCE_UNAVAILABLE","message":"game source is unavailable"}}`)
	}))
	t.Cleanup(server.Close)
	result, err := NewClient(server.URL, server.Client()).Launch(context.Background(), "gba-test")
	if err != nil {
		t.Fatal(err)
	}
	if result.HTTPStatus != 500 || result.ErrorCode != "SOURCE_UNAVAILABLE" {
		t.Fatalf("result = %#v", result)
	}
}

func TestClientListsGamesWithPlatformSortAndSearch(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		_ = json.NewEncoder(w).Encode(map[string]any{"games": []Game{}})
	}))
	t.Cleanup(server.Close)
	_, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), GameListQuery{
		Limit:    50,
		Platform: "snes",
		Sort:     "system",
		Q:        "mario & luigi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %#v", paths)
	}
	for _, want := range []string{"grouped=1", "availability=ready", "platform=snes", "sort=platform", "q=mario"} {
		if !strings.Contains(paths[0], want) {
			t.Fatalf("missing %q in %s", want, paths[0])
		}
	}
}

func TestClientListsPlatforms(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/platforms" {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"platforms": []Platform{{ID: "snes", Label: "Super NES", GameCount: 3, Launchable: true}},
		})
	}))
	t.Cleanup(server.Close)
	platforms, err := NewClient(server.URL, server.Client()).Platforms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(platforms) != 1 || platforms[0].ID != "snes" || platforms[0].Label != "Super NES" {
		t.Fatalf("platforms = %#v", platforms)
	}
}

func TestPresentationAttributionLabel(t *testing.T) {
	t.Parallel()
	igdb := Presentation{Attribution: &PresentationAttribution{Provider: "igdb", Label: "Data from IGDB.com"}}
	if got := igdb.AttributionLabel(); got != "Data from IGDB.com" {
		t.Fatalf("igdb = %q", got)
	}
	launchbox := Presentation{Attribution: &PresentationAttribution{Provider: "launchbox", Label: "Data from LaunchBox Games Database"}}
	if got := launchbox.AttributionLabel(); got != "Data from LaunchBox Games Database" {
		t.Fatalf("launchbox = %q", got)
	}
	unknown := Presentation{Attribution: &PresentationAttribution{Provider: "steam", Label: "Steam"}}
	if got := unknown.AttributionLabel(); got != "" {
		t.Fatalf("unknown = %q", got)
	}
	if got := (Presentation{}).AttributionLabel(); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestClientListsGamesWithCollection(t *testing.T) {
	t.Parallel()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []Game{{ID: "snes-mario", Title: "Mario", Favorite: true, Collections: []string{"weekend-queue"}}},
		})
	}))
	t.Cleanup(server.Close)
	games, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), GameListQuery{
		Limit:      50,
		Collection: "favorites",
		Sort:       "title",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || !games[0].Favorite || len(games[0].Collections) != 1 || games[0].Collections[0] != "weekend-queue" {
		t.Fatalf("games = %#v", games)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %#v", paths)
	}
	for _, want := range []string{"grouped=1", "availability=ready", "collection=favorites", "sort=title"} {
		if !strings.Contains(paths[0], want) {
			t.Fatalf("missing %q in %s", want, paths[0])
		}
	}
}

func TestClientListsCollectionsAndSetFavorite(t *testing.T) {
	t.Parallel()
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"collections": []Collection{{ID: "weekend-queue", Name: "Weekend queue"}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/favorites/snes-mario":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "favorite": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/library/favorites/snes-mario":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "favorite": false})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	collections, err := client.Collections(context.Background())
	if err != nil || len(collections) != 1 || collections[0].ID != "weekend-queue" || collections[0].Name != "Weekend queue" {
		t.Fatalf("collections = %#v, %v", collections, err)
	}
	if err := client.SetFavorite(context.Background(), "snes-mario", true); err != nil {
		t.Fatal(err)
	}
	if err := client.SetFavorite(context.Background(), "snes-mario", false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "GET /api/v1/library/collections,PUT /api/v1/library/favorites/snes-mario,DELETE /api/v1/library/favorites/snes-mario" {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestPreferLaunchableCopiesFavorite(t *testing.T) {
	t.Parallel()
	game := preferLaunchable(Game{
		ID:          "snes-sonic-usa",
		Title:       "Sonic",
		System:      "snes",
		State:       "available",
		RootOnline:  false,
		Launchable:  true,
		Favorite:    true,
		Collections: []string{"weekend-queue"},
		Variants: []Game{
			{ID: "snes-sonic-japan", Title: "Sonic", System: "snes", State: "available", RootOnline: true, Launchable: true},
		},
	})
	if game.ID != "snes-sonic-japan" || !game.Favorite || len(game.Collections) != 1 {
		t.Fatalf("game = %#v", game)
	}
}

func TestClientAttractPlaylist(t *testing.T) {
	t.Parallel()
	backdrop := strings.Repeat("ab", 32)
	cover := strings.Repeat("cd", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/attract" {
			t.Errorf("path = %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("limit") != "24" {
			t.Errorf("limit = %s", r.URL.Query().Get("limit"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"idle_seconds": 45,
			"items": []map[string]any{{
				"game_id":    "snes-mario",
				"title":      "Mario",
				"platform":   "snes",
				"backdrop":   backdrop,
				"cover":      cover,
				"launchable": true,
			}},
		})
	}))
	t.Cleanup(server.Close)
	playlist, err := NewClient(server.URL, server.Client()).Attract(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if playlist.IdleSeconds != 45 || len(playlist.Items) != 1 {
		t.Fatalf("playlist = %#v", playlist)
	}
	item := playlist.Items[0]
	if item.StillHandle() != backdrop {
		t.Fatalf("still handle = %q want backdrop", item.StillHandle())
	}
}

func TestAttractStillHandlePrefersBackdropThenCoverThenMarquee(t *testing.T) {
	t.Parallel()
	backdrop := strings.Repeat("aa", 32)
	cover := strings.Repeat("bb", 32)
	marquee := strings.Repeat("cc", 32)
	video := strings.Repeat("dd", 32)
	item := AttractItem{Video: video, Cover: cover, Backdrop: backdrop, Marquee: marquee}
	if item.StillHandle() != backdrop {
		t.Fatalf("got %q", item.StillHandle())
	}
	item.Backdrop = ""
	if item.StillHandle() != cover {
		t.Fatalf("cover fallback = %q", item.StillHandle())
	}
	item.Cover = ""
	if item.StillHandle() != marquee {
		t.Fatalf("marquee fallback = %q", item.StillHandle())
	}
	item.Marquee = ""
	if item.StillHandle() != "" {
		t.Fatalf("video must not be a still handle, got %q", item.StillHandle())
	}
}

func TestNormalizeHandleRejectsShortValues(t *testing.T) {
	t.Parallel()
	if got := normalizeHandle("abc"); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := CoverHandle(Game{Cover: "not-a-handle"}, Presentation{}); got != "" {
		t.Fatalf("cover handle = %q", got)
	}
}
