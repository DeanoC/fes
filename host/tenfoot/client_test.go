package tenfoot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

func TestClientHealthAndStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready":  true,
				"target": map[string]any{"reachable": false, "ready": false},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/status":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"code":"TARGET_UNAVAILABLE","message":"target status is unavailable"}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !health.Ready || health.TargetReachable || health.TargetReady {
		t.Fatalf("health = %#v", health)
	}
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Unavailable || status.HTTPStatus != 503 || status.ErrorCode != "TARGET_UNAVAILABLE" {
		t.Fatalf("status = %#v", status)
	}
}

func TestClientSessionInputAttachDetachEmptyBody(t *testing.T) {
	t.Parallel()
	var attachCT, detachCT, attachBody, detachBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session/input":
			_, _ = io.WriteString(w, `{"state":"detached","ready":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/input/attach":
			attachCT = r.Header.Get("Content-Type")
			attachBody = string(raw)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"attached","ready":true}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/input/detach":
			detachCT = r.Header.Get("Content-Type")
			detachBody = string(raw)
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"detached","ready":false}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	got, err := client.SessionInput(context.Background())
	if err != nil || got.State != "detached" {
		t.Fatalf("session input = %#v, %v", got, err)
	}
	attached, err := client.AttachInput(context.Background())
	if err != nil || attached.Input == nil || attached.Input.State != "attached" {
		t.Fatalf("attach = %#v, %v", attached, err)
	}
	if attachBody != "" || attachCT != "" {
		t.Fatalf("attach request body=%q ct=%q", attachBody, attachCT)
	}
	detached, err := client.DetachInput(context.Background())
	if err != nil || detached.Input == nil || detached.Input.State != "detached" {
		t.Fatalf("detach = %#v, %v", detached, err)
	}
	if detachBody != "" || detachCT != "" {
		t.Fatalf("detach request body=%q ct=%q", detachBody, detachCT)
	}
}

func TestHostTransportErrorIncludesClientTimeout(t *testing.T) {
	t.Parallel()
	if isHostTransportError(context.Canceled) || isHostTransportError(context.Cause(context.Background())) {
		t.Fatal("canceled or nil should not be host transport")
	}
	timeout := &url.Error{Op: "Get", URL: "http://127.0.0.1:8787/api/v1/health", Err: context.DeadlineExceeded}
	if !isHostTransportError(timeout) {
		t.Fatal("http client timeout should be host unreachable")
	}
	if !isHostTransportError(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded without cancel should be host transport")
	}
	if isHostTransportError(errors.New("host API status 404")) {
		t.Fatal("HTTP status error is not transport")
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

func TestClientCollectionMembershipAndCRUD(t *testing.T) {
	t.Parallel()
	var methods []string
	var upsertName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/collections/weekend-queue/snes-mario":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "collection": "weekend-queue", "member": true})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/library/collections/weekend-queue/snes-mario":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "snes-mario", "collection": "weekend-queue", "member": false})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/collections/weekend-queue":
			upsertName = r.URL.Query().Get("name")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "weekend-queue", "name": upsertName})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/library/collections/weekend-queue":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "weekend-queue"})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/collections/favorites":
			http.Error(w, `{"error":{"code":"BAD_REQUEST","message":"collection request is invalid"}}`, http.StatusBadRequest)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	if err := client.SetCollectionMember(context.Background(), "weekend-queue", "snes-mario", true); err != nil {
		t.Fatal(err)
	}
	if err := client.SetCollectionMember(context.Background(), "weekend-queue", "snes-mario", false); err != nil {
		t.Fatal(err)
	}
	got, err := client.UpsertCollection(context.Background(), "weekend-queue", "Weekend queue")
	if err != nil || got.ID != "weekend-queue" || got.Name != "Weekend queue" || upsertName != "Weekend queue" {
		t.Fatalf("upsert = %#v name=%q err=%v", got, upsertName, err)
	}
	if err := client.DeleteCollection(context.Background(), "weekend-queue"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpsertCollection(context.Background(), "favorites", "Favorites"); err == nil {
		t.Fatal("reserved upsert should fail")
	}
	want := "PUT /api/v1/library/collections/weekend-queue/snes-mario,DELETE /api/v1/library/collections/weekend-queue/snes-mario,PUT /api/v1/library/collections/weekend-queue,DELETE /api/v1/library/collections/weekend-queue,PUT /api/v1/library/collections/favorites"
	if strings.Join(methods, ",") != want {
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

func TestClientFetchVideoFileStreamsAcceptVideo(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ee", 32)
	payload := make([]byte, 32)
	copy(payload[4:8], []byte("ftyp"))
	var accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/presentation/artwork/"+handle {
			http.NotFound(w, r)
			return
		}
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)
	path, err := NewClient(server.URL, server.Client()).FetchVideoFile(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if accept != "video/*" {
		t.Fatalf("Accept = %q", accept)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("file = %q", got)
	}
}

func TestClientFetchVideoFileRejectsImageMIME(t *testing.T) {
	t.Parallel()
	handle := strings.Repeat("ff", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("not-a-video-file!!"))
	}))
	t.Cleanup(server.Close)
	path, err := NewClient(server.URL, server.Client()).FetchVideoFile(context.Background(), handle)
	if path != "" || err == nil {
		t.Fatalf("path=%q err=%v", path, err)
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

func TestClientLibrarySettingsGetAndPatch(t *testing.T) {
	t.Parallel()
	var patches []string
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/library/settings" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": 90,
				"preferred_regions":    []string{"usa", "japan"},
				"selected_target":      "dev",
				"targets": []map[string]any{
					{"name": "dev", "address": "http://192.0.2.10:8182", "enabled": true, "agent_configured": true, "agent": "secret"},
					{"name": "spare", "address": "", "enabled": false, "agent_configured": false},
				},
				"libraries": []map[string]any{{"id": "snes", "system": "snes", "root": "/library/snes"}},
				"systems":   []map[string]any{{"id": "snes", "label": "SNES"}},
			})
		case http.MethodPatch:
			raw, _ := io.ReadAll(r.Body)
			patches = append(patches, string(raw))
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("patch json: %v", err)
			}
			if _, ok := body["libraries"]; ok {
				t.Errorf("patch included libraries: %s", raw)
			}
			if _, ok := body["targets"]; ok {
				t.Errorf("patch included targets: %s", raw)
			}
			idle := 90
			regions := []any{"usa", "japan"}
			selected := "dev"
			if v, ok := body["attract_idle_seconds"].(float64); ok {
				idle = int(v)
			}
			if v, ok := body["preferred_regions"].([]any); ok {
				regions = v
			}
			if v, ok := body["selected_target"].(string); ok {
				selected = v
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"attract_idle_seconds": idle,
				"preferred_regions":    regions,
				"selected_target":      selected,
				"targets":              []map[string]any{{"name": "dev", "enabled": true, "agent_configured": true}},
				"libraries":            []map[string]any{},
				"systems":              []map[string]any{},
			})
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.URL, server.Client())
	got, err := client.LibrarySettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.AttractIdleSeconds != 90 || got.SelectedTarget != "dev" {
		t.Fatalf("settings = %#v", got)
	}
	if len(got.PreferredRegions) != 2 || got.PreferredRegions[0] != "usa" {
		t.Fatalf("regions = %#v", got.PreferredRegions)
	}
	if len(got.Targets) != 2 || got.Targets[0].Name != "dev" || !got.Targets[0].AgentConfigured || got.Targets[0].Address == "" {
		t.Fatalf("targets = %#v", got.Targets)
	}
	if len(got.Libraries) != 1 || len(got.Systems) != 1 {
		t.Fatalf("context = libs %#v systems %#v", got.Libraries, got.Systems)
	}

	idle := 75
	patched, err := client.PatchLibrarySettings(context.Background(), LibrarySettingsPatch{AttractIdleSeconds: &idle})
	if err != nil {
		t.Fatal(err)
	}
	if patched.AttractIdleSeconds != 75 {
		t.Fatalf("idle patch = %#v", patched)
	}
	regions := []string{"japan"}
	if _, err := client.PatchLibrarySettings(context.Background(), LibrarySettingsPatch{PreferredRegions: &regions}); err != nil {
		t.Fatal(err)
	}
	target := "spare"
	if _, err := client.PatchLibrarySettings(context.Background(), LibrarySettingsPatch{SelectedTarget: &target}); err != nil {
		t.Fatal(err)
	}
	if len(patches) != 3 {
		t.Fatalf("patches = %#v", patches)
	}
	if patches[0] != `{"attract_idle_seconds":75}` {
		t.Fatalf("idle body = %q", patches[0])
	}
	if patches[1] != `{"preferred_regions":["japan"]}` {
		t.Fatalf("regions body = %q", patches[1])
	}
	if patches[2] != `{"selected_target":"spare"}` {
		t.Fatalf("target body = %q", patches[2])
	}
	if strings.Join(methods, ",") != "GET,PATCH,PATCH,PATCH" {
		t.Fatalf("methods = %#v", methods)
	}
}

func TestClientLibrarySettingsPatchEmptyRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewClient("http://127.0.0.1:1", nil).PatchLibrarySettings(context.Background(), LibrarySettingsPatch{}); err == nil {
		t.Fatal("empty patch accepted")
	}
}

func TestClientLibrarySettingsGetError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"code":"SETTINGS_UNAVAILABLE","message":"library settings are unavailable"}}`)
	}))
	t.Cleanup(server.Close)
	if _, err := NewClient(server.URL, server.Client()).LibrarySettings(context.Background()); err == nil || !strings.Contains(err.Error(), "SETTINGS_UNAVAILABLE") {
		t.Fatalf("err = %v", err)
	}
}
