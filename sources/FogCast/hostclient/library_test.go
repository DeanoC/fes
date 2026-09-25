package hostclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientLibraryOperationsPreservePublicHTTPContract(t *testing.T) {
	handle := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer caller" || r.Header.Get("X-FogCast-Target-ID") != "target" {
			t.Errorf("identity headers = %q/%q", r.Header.Get("Authorization"), r.Header.Get("X-FogCast-Target-ID"))
		}
		if r.Header.Get("Accept") == "" {
			t.Error("missing Accept header")
		}
		switch r.URL.Path {
		case "/api/v1/games":
			query := r.URL.Query()
			if query.Get("grouped") != "1" || query.Get("availability") != "ready" || query.Get("platform") != "megadrive" || query.Get("sort") != "platform" || query.Get("limit") != "2" {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"games":[{"id":"pong","title":"Pong","system":"pong","launchable":true}],"next_cursor":"next"}`)
		case "/api/v1/platforms":
			_, _ = io.WriteString(w, `{"platforms":[{"id":"pong","label":"Pong","game_count":1,"online":true,"launchable":true}]}`)
		case "/api/v1/library/core-entries":
			_, _ = io.WriteString(w, `{"entries":[{"game_id":"fpga-pong","title":"Pong","core_id":"fes.pong","package_id":"`+handle+`"}]}`)
		case "/api/v1/core-packages":
			_, _ = io.WriteString(w, `{"packages":[{"package_id":"`+handle+`","descriptor":{"core":{"id":"fes.pong","name":"FES Pong","version":"1.0.0"}},"compatibility":"unknown"}]}`)
		case "/api/v1/library/cache":
			_, _ = io.WriteString(w, `{"rom":{"used_bytes":1,"max_bytes":2,"free_bytes":1,"reachable":true},"synced_unix":7}`)
		case "/api/v1/library/attract":
			if r.URL.Query().Get("limit") != "24" {
				t.Errorf("attract query = %q", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"items":[{"game_id":"pong","title":"Pong","platform":"pong","cover":"`+handle+`","launchable":true}],"idle_seconds":60}`)
		case "/api/v1/presentation/games/pong":
			_, _ = io.WriteString(w, `{"game_id":"pong","state":"ready","presentation":{"cover_artwork_id":"`+handle+`"}}`)
		case "/api/v1/presentation/artwork/" + handle:
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, "art")
		case "/api/v1/health":
			_, _ = io.WriteString(w, `{"ready":true,"target":{"reachable":true,"ready":true,"connection":{"state":"ready"}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	transport := &identityTransport{base: server.Client().Transport}
	client := NewClient(server.URL+"/", &http.Client{Transport: transport, Timeout: 5 * time.Second})
	games, next, err := client.ListGames(context.Background(), GameListQuery{Limit: 2, Platform: " megadrive", Sort: "system"})
	if err != nil || len(games) != 1 || games[0].ID != "pong" || next != "next" {
		t.Fatalf("games = %#v next=%q err=%v", games, next, err)
	}
	if platforms, err := client.Platforms(context.Background()); err != nil || len(platforms) != 1 {
		t.Fatalf("platforms = %#v err=%v", platforms, err)
	}
	library, err := client.CoreLibrary(context.Background())
	if err != nil || len(library.Entries) != 1 || len(library.Packages) != 1 {
		t.Fatalf("core library = %#v err=%v", library, err)
	}
	if cache, err := client.LibraryCache(context.Background()); err != nil || cache.ROM.MaxBytes != 2 {
		t.Fatalf("cache = %#v err=%v", cache, err)
	}
	if playlist, err := client.Attract(context.Background(), 0); err != nil || len(playlist.Items) != 1 {
		t.Fatalf("attract = %#v err=%v", playlist, err)
	}
	if presentation, err := client.GamePresentation(context.Background(), "pong"); err != nil || presentation.GameID != "pong" {
		t.Fatalf("presentation = %#v err=%v", presentation, err)
	}
	if health, err := client.Health(context.Background()); err != nil || !health.Ready || !health.TargetReady {
		t.Fatalf("health = %#v err=%v", health, err)
	}
	if artwork, contentType, err := client.Artwork(context.Background(), handle); err != nil || string(artwork) != "art" || contentType != "image/png" {
		t.Fatalf("artwork = %q/%q err=%v", artwork, contentType, err)
	}
}

func TestLaunchBlockClassifiesCatalogReadiness(t *testing.T) {
	t.Parallel()
	ready := Game{ID: "snes-mario", Title: "Mario", System: "snes", State: "available", RootOnline: true, Launchable: true}
	cases := []struct {
		name     string
		game     Game
		block    LaunchBlock
		eligible bool
	}{
		{name: "ready", game: ready, block: "", eligible: true},
		{name: "ready-here-keeps-offline", game: func() Game {
			game := ready
			game.Execution = ExecutionHostOnly
			here := true
			game.ReadyHere = &here
			game.RootOnline = false
			game.State = "missing"
			return game
		}(), block: LaunchSourceOffline, eligible: false},
		{name: "ready-here-keeps-catalog", game: func() Game {
			game := ready
			here := true
			game.ReadyHere = &here
			return game
		}(), block: "", eligible: true},
		{name: "browse-only", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "available", RootOnline: true, Launchable: false}, block: LaunchBrowseOnly, eligible: false},
		{name: "missing", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "missing", RootOnline: false, Launchable: true}, block: LaunchSourceOffline, eligible: false},
		{name: "offline", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "available", RootOnline: false, Launchable: true}, block: LaunchSourceOffline, eligible: false},
		{name: "invalid", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "invalid", RootOnline: true, Launchable: true}, block: LaunchUnreadable, eligible: false},
		{name: "not-ready", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "scanning", RootOnline: true, Launchable: true}, block: LaunchNotReady, eligible: false},
		{name: "browse-only-wins-over-offline", game: Game{ID: ready.ID, Title: ready.Title, System: ready.System, State: "missing", RootOnline: false, Launchable: false}, block: LaunchBrowseOnly, eligible: false},
		{name: "graphics-i-no-firmware", game: Game{ID: "fpga-graphics-i", Title: "Graphics I", System: "fpga", State: "available", RootOnline: true, Launchable: true}, block: "", eligible: true},
		{name: "frogger-missing-firmware", game: Game{ID: "fpga-frogger", Title: "Frogger", System: "fpga", State: "available", RootOnline: true, Launchable: true, FirmwareRequired: true}, block: LaunchMissingFirmware, eligible: false},
		{name: "frogger-ready-firmware", game: Game{ID: "fpga-frogger", Title: "Frogger", System: "fpga", State: "available", RootOnline: true, Launchable: true, FirmwareRequired: true, FirmwareReady: true}, block: "", eligible: true},
		{name: "fpga-coleco-package-ready", game: Game{ID: "fpga-donkey-kong", Title: "Donkey Kong", System: "coleco", State: "available", RootOnline: true, Launchable: true, FirmwareRequired: true, FirmwareReady: true}, block: "", eligible: true},
		{name: "raw-coleco-browse-only", game: Game{ID: "coleco-donkey-kong", Title: "Donkey Kong", System: "coleco", State: "available", RootOnline: true, Launchable: false}, block: LaunchBrowseOnly, eligible: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.game.LaunchBlock(); got != tc.block {
				t.Fatalf("LaunchBlock() = %q, want %q", got, tc.block)
			}
			if got := tc.game.LaunchEligible(); got != tc.eligible {
				t.Fatalf("LaunchEligible() = %v, want %v", got, tc.eligible)
			}
		})
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
	if game.ID != "snes-sonic-japan" || !game.Favorite || len(game.Collections) != 1 || game.Collections[0] != "weekend-queue" {
		t.Fatalf("game = %#v", game)
	}
	if game.Variants != nil {
		t.Fatalf("variants leaked into catalog row: %#v", game.Variants)
	}
}

func TestPreferLaunchableKeepsPackageAndSkipsDistant(t *testing.T) {
	t.Parallel()
	raw := Game{ID: "coleco-dk-cart", Title: "Donkey Kong", System: "coleco", State: "available", RootOnline: true, Launchable: false}
	pkg := Game{ID: "fpga-coleco-dk", Title: "Donkey Kong", System: "coleco", State: "available", RootOnline: true, Launchable: true, FirmwareRequired: true, FirmwareReady: true}
	group := raw
	group.Variants = []Game{raw, pkg}
	got := preferLaunchable(group)
	if got.ID != pkg.ID || !got.LaunchEligible() {
		t.Fatalf("package variant %+v", got)
	}
	distant := false
	pkg.ReadyHere = &distant
	pkg.ReadyBlock = string(LaunchDistant)
	pkg.NextAction = "fetch_here"
	group.Variants = []Game{raw, pkg}
	got = preferLaunchable(group)
	if got.LaunchEligible() || got.ID != raw.ID {
		t.Fatalf("distant package became playable %+v", got)
	}
	here := true
	pkg.ReadyHere = &here
	pkg.ReadyBlock = ""
	pkg.NextAction = ""
	group.Variants = []Game{raw, pkg}
	got = preferLaunchable(group)
	if got.ID != pkg.ID || !got.LaunchEligible() {
		t.Fatalf("ready package %+v", got)
	}
}

func TestPlacementUnresolvedIsNotReadyAndSelectedStaysEligible(t *testing.T) {
	t.Parallel()
	ready := true
	selected := Game{
		ID: "fpga-coleco-dk", Title: "Donkey Kong", System: "coleco",
		State: "available", RootOnline: true, Launchable: true,
		ReadyHere: &ready, Placement: PlacementSelected,
	}
	if !selected.LaunchEligible() || selected.LaunchBlock() != "" {
		t.Fatalf("selected %+v block %s", selected, selected.LaunchBlock())
	}
	blocked := false
	unresolved := selected
	unresolved.ReadyHere = &blocked
	unresolved.ReadyBlock = string(LaunchPlacementUnresolved)
	unresolved.Placement = PlacementUnresolved
	unresolved.NextAction = "unavailable"
	if unresolved.LaunchEligible() || unresolved.LaunchBlock() != LaunchPlacementUnresolved || unresolved.LaunchBlock() == LaunchVersionSkew || unresolved.LaunchBlock() == LaunchLeaseHeld {
		t.Fatalf("unresolved block %s", unresolved.LaunchBlock())
	}
	closed := unresolved
	closed.ReadyBlock = string(LaunchPlacementFailClosed)
	closed.Placement = PlacementFailClosed
	if closed.LaunchEligible() || closed.LaunchBlock() != LaunchPlacementFailClosed {
		t.Fatalf("fail closed block %s", closed.LaunchBlock())
	}
	skew := closed
	skew.ReadyBlock = string(LaunchVersionSkew)
	skew.Placement = PlacementFailClosed
	if skew.LaunchBlock() != LaunchVersionSkew {
		t.Fatalf("skew collapsed to %s", skew.LaunchBlock())
	}
	busy := unresolved
	busy.ReadyBlock = string(LaunchLeaseHeld)
	busy.Placement = PlacementUnresolved
	if busy.LaunchBlock() != LaunchLeaseHeld {
		t.Fatalf("in use collapsed to %s", busy.LaunchBlock())
	}
}

func TestStillHandlesReturnsNilWhenNoMedia(t *testing.T) {
	t.Parallel()
	if got := (AttractItem{}).StillHandles(); got != nil {
		t.Fatalf("StillHandles() = %#v, want nil", got)
	}
}

func TestListGamesPrefersLaunchableVariantCopiesFavorite(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"games":[{
			"id":"snes-sonic-usa","title":"Sonic","system":"snes","state":"available",
			"root_online":false,"launchable":true,"favorite":true,
			"collections":["weekend-queue"],
			"variants":[{"id":"snes-sonic-japan","title":"Sonic","system":"snes","state":"available","root_online":true,"launchable":true}]
		}]}`)
	}))
	defer server.Close()

	games, _, err := NewClient(server.URL, server.Client()).ListGames(context.Background(), GameListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || games[0].ID != "snes-sonic-japan" || !games[0].Favorite || len(games[0].Collections) != 1 || games[0].Collections[0] != "weekend-queue" {
		t.Fatalf("games = %#v", games)
	}
	if games[0].Variants != nil {
		t.Fatalf("variants leaked into catalog row: %#v", games[0].Variants)
	}
}

func TestClientMutationPreservesHeadersTimeoutsAndReadFailureStatus(t *testing.T) {
	const status = http.StatusBadGateway
	transport := mutationTransport{responses: map[string]*http.Response{}}
	client := NewClient("http://host", &http.Client{Transport: transport, Timeout: 5 * time.Second})
	if client.launchHTTP.Timeout != 60*time.Second || client.stopHTTP.Timeout != 60*time.Second {
		t.Fatalf("mutation timeouts = %s/%s", client.launchHTTP.Timeout, client.stopHTTP.Timeout)
	}

	transport.responses["/api/v1/session/launch"] = syntheticResponse(status, maxResponseBytes+1)
	launch, err := client.Launch(context.Background(), "pong")
	if !errors.Is(err, ErrResponseTooLarge) || launch.HTTPStatus != status {
		t.Fatalf("launch = %#v err=%v", launch, err)
	}
	transport.responses["/api/v1/session/stop"] = syntheticResponse(status, 8)
	stop, err := client.Stop(context.Background())
	if !errors.Is(err, ErrResponseTruncated) || stop.HTTPStatus != status {
		t.Fatalf("stop = %#v err=%v", stop, err)
	}
}

func TestClientEditionPreferencesSaveAndLoad(t *testing.T) {
	var puts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/edition-preferences":
			_, _ = io.WriteString(w, `{"preferences":[{"query":"super mario bros","platform":"nes","game_id":"nes-smb-usa","chosen_at":1}]}`)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/edition-preferences":
			puts++
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"game_id":"nes-smb-jp"`) || !strings.Contains(string(body), `"query":"Super Mario Bros."`) {
				t.Errorf("put body = %s", body)
			}
			_, _ = io.WriteString(w, `{"query":"super mario bros","platform":"nes","game_id":"nes-smb-jp","chosen_at":2}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client())
	listed, err := client.EditionPreferences(context.Background())
	if err != nil || len(listed) != 1 || listed[0].GameID != "nes-smb-usa" {
		t.Fatalf("list = %#v err=%v", listed, err)
	}
	saved, err := client.SetEditionPreference(context.Background(), "Super Mario Bros.", "nes", "nes-smb-jp")
	if err != nil || saved.GameID != "nes-smb-jp" || puts != 1 {
		t.Fatalf("save = %#v puts=%d err=%v", saved, puts, err)
	}
}

type identityTransport struct {
	base http.RoundTripper
}

func (t *identityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer caller")
	clone.Header.Set("X-FogCast-Target-ID", "target")
	return t.base.RoundTrip(clone)
}

type mutationTransport struct {
	responses map[string]*http.Response
}

func (t mutationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response := t.responses[req.URL.Path]
	if response == nil {
		return nil, errors.New("unexpected request " + req.URL.Path)
	}
	response.Request = req
	return response, nil
}

func syntheticResponse(status int, contentLength int64) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader("{}")),
		ContentLength: contentLength,
	}
}
