package kitlauncher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestCatalogSystemsUsesPlatformsThenFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"megadrive","game_count":2},{"id":"snes","game_count":0},{"id":"pong","game_count":1}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ids := catalogSystems(context.Background(), NewClient(Config{API: server.URL}))
	if len(ids) != 2 || ids[0] != "megadrive" || ids[1] != "pong" {
		t.Fatalf("platforms %v", ids)
	}

	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer fallback.Close()
	ids = catalogSystems(context.Background(), NewClient(Config{API: fallback.URL}))
	if len(ids) != 3 || ids[0] != "pong" || ids[1] != "megadrive" || ids[2] != "snes" {
		t.Fatalf("fallback %v", ids)
	}
}

func TestLoadStripUsesRecentsAndFavorites(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/games" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("collection") {
		case "recents":
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true},{"id":"mario","title":"Mario","system":"snes","launchable":true}]}`))
		case "favorites":
			_, _ = w.Write([]byte(`{"games":[{"id":"mario","title":"Mario","system":"snes","favorite":true,"launchable":true},{"id":"pong","title":"Pong","system":"pong","favorite":true,"launchable":true}]}`))
		default:
			t.Errorf("unexpected collection %q", r.URL.Query().Get("collection"))
			_, _ = w.Write([]byte(`{"games":[]}`))
		}
	}))
	defer server.Close()
	games, label, recents := loadStrip(context.Background(), NewClient(Config{API: server.URL}))
	if label != "Recent / Favorites" || len(games) != 3 {
		t.Fatalf("strip %q n=%d ids=%v", label, len(games), ids(games))
	}
	if games[0].ID != "sonic" || games[1].ID != "mario" || games[2].ID != "pong" {
		t.Fatalf("order %v", ids(games))
	}
	if len(recents) != 2 || recents[0].ID != "sonic" {
		t.Fatalf("recents %v", ids(recents))
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"games":[]}`))
	}))
	defer empty.Close()
	games, label, recents = loadStrip(context.Background(), NewClient(Config{API: empty.URL}))
	if label != "" || len(games) != 0 || len(recents) != 0 {
		t.Fatalf("empty strip %q n=%d recents=%d", label, len(games), len(recents))
	}

	recentOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("collection") == "recents" {
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","launchable":true}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer recentOnly.Close()
	games, label, recents = loadStrip(context.Background(), NewClient(Config{API: recentOnly.URL}))
	if label != "Recent" || len(games) != 1 || games[0].ID != "sonic" || len(recents) != 1 {
		t.Fatalf("recent-only %q n=%d recents=%d", label, len(games), len(recents))
	}
}

func TestLoadCatalogFiltersByPlatform(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"pong","game_count":1},{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			platform := r.URL.Query().Get("platform")
			switch platform {
			case "pong":
				_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","system":"pong","launchable":true}]}`))
			case "megadrive":
				_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true}]}`))
			default:
				t.Errorf("unexpected platform %q", platform)
				_, _ = w.Write([]byte(`{"games":[]}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	games, err := loadCatalog(context.Background(), NewClient(Config{API: server.URL}))
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || games[0].ID != "pong" || games[1].ID != "sonic" {
		t.Fatalf("games %v", games)
	}
}

func TestUnavailableHostStillRendersAndExits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	draws := 0
	err := Run(ctx, NewClient(Config{API: server.URL}), func(Model) { draws++ }, func() (Pad, error) { return nil, errors.New("no pad") })
	if err != nil {
		t.Fatal(err)
	}
	if draws == 0 {
		t.Fatal("connecting screen never rendered")
	}
}

type pressPad struct{ n int }

func (p *pressPad) Poll() ([]remoteinput.Event, error) {
	if p.n >= 2 {
		return nil, nil
	}
	p.n++
	e, _ := remoteinput.NormalizeGamepad("a", true)
	return []remoteinput.Event{e}, nil
}
func (*pressPad) Close() error { return nil }

func TestLaunchPresentsLoadingBeforeDispatch(t *testing.T) {
	var loading atomic.Bool
	launched := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","launchable":true}]}`))
		case "/api/v1/session/launch":
			if !loading.Load() {
				t.Error("launch dispatched before loading frame")
			}
			close(launched)
			_, _ = w.Write([]byte(`{"state":"active","game_id":"pong"}`))
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ready := false
	_ = Run(ctx, NewClient(Config{API: server.URL}), func(m Model) {
		if m.Busy && m.Message == "Loading game" {
			loading.Store(true)
		}
		if len(m.Games) > 0 {
			ready = true
		}
	}, func() (Pad, error) {
		if !ready {
			return nil, errors.New("wait")
		}
		return &pressPad{}, nil
	})
	select {
	case <-launched:
	default:
		t.Fatal("launch not attempted")
	}
}

type silentPad struct{}

func (*silentPad) Poll() ([]remoteinput.Event, error) { return nil, nil }
func (*silentPad) Close() error                       { return nil }

type gatedPad struct {
	release *atomic.Bool
	sent    atomic.Bool
}

func (p *gatedPad) Poll() ([]remoteinput.Event, error) {
	if !p.release.Load() || p.sent.Load() {
		return nil, nil
	}
	p.sent.Store(true)
	e, _ := remoteinput.NormalizeGamepad("dpad-right", true)
	return []remoteinput.Event{e}, nil
}
func (*gatedPad) Close() error { return nil }

func attractTestHandler(handle string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"snes","game_count":1},{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"mario","title":"Mario","system":"snes","launchable":true},{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true}]}`))
		case "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"idle_seconds": 1,
				"items": []map[string]any{{
					"game_id": "mario", "title": "Mario", "platform": "snes",
					"backdrop": handle, "launchable": true,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}
}

func TestRunEntersAttractFromHostPlaylist(t *testing.T) {
	handle := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(attractTestHandler(handle))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var once atomic.Bool
	var title atomic.Value
	_ = Run(ctx, NewClient(Config{API: server.URL}), func(m Model) {
		if !m.AttractActive {
			return
		}
		view := m.AttractView(time.Now())
		title.Store(view.Title)
		if view.Title == "Mario" && once.CompareAndSwap(false, true) {
			cancel()
		}
	}, func() (Pad, error) { return &silentPad{}, nil })
	if !once.Load() {
		t.Fatalf("attract did not arm from host idle_seconds title=%v", title.Load())
	}
}

func TestRunDismissesAttractOnPadAndKeepsFocus(t *testing.T) {
	handle := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(attractTestHandler(handle))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var release atomic.Bool
	var armed, dismissed atomic.Bool
	var focusBefore, focusAfter atomic.Int64
	focusBefore.Store(-1)
	_ = Run(ctx, NewClient(Config{API: server.URL}), func(m Model) {
		if m.AttractActive && !armed.Load() {
			focusBefore.Store(int64(m.Focus))
			armed.Store(true)
			release.Store(true)
		}
		if armed.Load() && !m.AttractActive && !m.Busy {
			focusAfter.Store(int64(m.Focus))
			dismissed.Store(true)
			cancel()
		}
	}, func() (Pad, error) { return &gatedPad{release: &release}, nil })
	if !armed.Load() {
		t.Fatal("attract did not arm")
	}
	if !dismissed.Load() {
		t.Fatal("pad did not dismiss attract")
	}
	if focusBefore.Load() != focusAfter.Load() {
		t.Fatalf("focus %d -> %d", focusBefore.Load(), focusAfter.Load())
	}
}

type bPressPad struct{ n int }

func (p *bPressPad) Poll() ([]remoteinput.Event, error) {
	if p.n >= 2 {
		return nil, nil
	}
	name := "a"
	if p.n == 1 {
		name = "dpad-down"
	}
	p.n++
	e, _ := remoteinput.NormalizeGamepad(name, true)
	return []remoteinput.Event{e}, nil
}
func (*bPressPad) Close() error { return nil }

func TestRunFetchesPresentationWhileDetailOpen(t *testing.T) {
	handle := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case r.URL.Path == "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case r.URL.Path == "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"snes","game_count":1}]}`))
		case r.URL.Path == "/api/v1/games":
			if r.URL.Query().Get("collection") != "" {
				_, _ = w.Write([]byte(`{"games":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"games":[{"id":"mario","title":"Mario","system":"snes","year":"1990","genre":"Action","launchable":true}]}`))
		case r.URL.Path == "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/presentation/games/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"game_id": "mario",
				"state":   "ready",
				"presentation": map[string]any{
					"year": "1985", "genre": "Platform", "studio": "Nintendo",
					"screenshot_ids": []string{handle},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var ready atomic.Bool
	var gotStudio atomic.Bool
	_ = Run(ctx, NewClient(Config{API: server.URL}), func(m Model) {
		if len(m.Games) > 0 {
			ready.Store(true)
		}
		if m.DetailOpen && m.FocusDetail().Studio == "Nintendo" && m.FocusDetail().Year == "1985" {
			gotStudio.Store(true)
			cancel()
		}
	}, func() (Pad, error) {
		if !ready.Load() {
			return nil, errors.New("wait")
		}
		return &bPressPad{}, nil
	})
	if !gotStudio.Load() {
		t.Fatal("detail presentation was not applied")
	}
}
