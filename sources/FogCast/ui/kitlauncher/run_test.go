package kitlauncher

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	err := Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(Model) { draws++ }, func() (Pad, error) { return nil, errors.New("no pad") })
	if err != nil {
		t.Fatal(err)
	}
	if draws == 0 {
		t.Fatal("connecting screen never rendered")
	}
}

func TestRunPaintsLocalCatalogBeforeHostHTTP(t *testing.T) {
	var gamesHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/games") {
			gamesHits.Add(1)
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	cfg := writeKitConfig(t, t.TempDir(), server.URL)
	handle := strings.Repeat("ab", 32)
	client := NewClient(cfg)
	if client.Cache == nil {
		t.Fatal("expected launcher cache beside config")
	}
	if err := client.Cache.SaveCatalog(CatalogSnapshot{
		Games: []hostclient.Game{{ID: "sonic", Title: "Sonic", System: "megadrive", Cover: handle, Launchable: true}},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var painted atomic.Bool
	var firstCount atomic.Int64
	err := Run(ctx, client, func(m Model) {
		if firstCount.Add(1) == 1 {
			if len(m.Catalog) != 1 || m.Catalog[0].ID != "sonic" || m.Catalog[0].Cover != handle {
				t.Errorf("first paint catalog %#v", m.Catalog)
			}
			if m.Message != OfflineMessage {
				t.Errorf("first paint message %q", m.Message)
			}
			if gamesHits.Load() != 0 {
				t.Errorf("first paint waited on games HTTP hits=%d", gamesHits.Load())
			}
			painted.Store(len(m.Catalog) == 1 && m.Catalog[0].ID == "sonic")
			cancel()
		}
	}, func() (Pad, error) { return nil, errors.New("no pad") })
	if err != nil {
		t.Fatal(err)
	}
	if !painted.Load() {
		t.Fatal("did not paint local catalog before host HTTP")
	}
	if gamesHits.Load() != 0 {
		t.Fatalf("catalog HTTP hits=%d", gamesHits.Load())
	}
}

func TestRunOfflineFooterUsesLocalLibraryCopy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer server.Close()
	cfg := writeKitConfig(t, t.TempDir(), server.URL)
	client := NewClient(cfg)
	if err := client.Cache.SaveCatalog(CatalogSnapshot{
		Games: []hostclient.Game{{ID: "mario", Title: "Mario", System: "snes", Launchable: true}},
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var painted, offline atomic.Bool
	err := Run(ctx, client, func(m Model) {
		if len(m.Catalog) == 1 && m.Catalog[0].ID == "mario" {
			painted.Store(true)
		}
		if m.Message == OfflineMessage && !m.Connected {
			offline.Store(true)
		}
		if painted.Load() && offline.Load() {
			cancel()
		}
	}, func() (Pad, error) { return nil, errors.New("no pad") })
	if err != nil {
		t.Fatal(err)
	}
	if !painted.Load() {
		t.Fatal("local catalog was not painted")
	}
	if !offline.Load() {
		t.Fatal("offline footer was not honest")
	}
}

func TestRunEmptyCacheStaysOfflineWithoutHang(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer server.Close()
	cfg := writeKitConfig(t, t.TempDir(), server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var draws atomic.Int64
	var offline atomic.Bool
	err := Run(ctx, NewClient(cfg), func(m Model) {
		draws.Add(1)
		if len(m.Catalog) != 0 {
			t.Errorf("empty cache painted catalog %#v", m.Catalog)
		}
		if m.Message == OfflineMessage {
			offline.Store(true)
			cancel()
		}
	}, func() (Pad, error) { return nil, errors.New("no pad") })
	if err != nil {
		t.Fatal(err)
	}
	if draws.Load() == 0 {
		t.Fatal("empty cache hung without a frame")
	}
	if !offline.Load() {
		t.Fatal("empty cache did not label offline")
	}
}

func TestRunPersistsHostCatalogForNextBoot(t *testing.T) {
	packageIDs := []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","cover":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","launchable":true}]}`))
		case "/api/v1/library/core-entries":
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []map[string]string{
				{"game_id": "fpga-pong", "title": "Pong", "core_id": "fes.pong", "package_id": packageIDs[0]},
				{"game_id": "fpga-zx81", "title": "ZX81", "core_id": "fes.zx81", "package_id": packageIDs[1]},
				{"game_id": "fpga-coleco", "title": "Coleco", "core_id": "fes.coleco", "package_id": packageIDs[2]},
			}})
		case "/api/v1/core-packages":
			_ = json.NewEncoder(w).Encode(map[string]any{"packages": []map[string]any{
				{"package_id": packageIDs[0], "descriptor": map[string]any{"core": map[string]string{"id": "fes.pong", "name": "FES Pong", "version": "1.0.0"}}, "compatibility": "unknown"},
				{"package_id": packageIDs[1], "descriptor": map[string]any{"core": map[string]string{"id": "fes.zx81", "name": "FES ZX81", "version": "1.0.0"}}, "compatibility": "unknown"},
				{"package_id": packageIDs[2], "descriptor": map[string]any{"core": map[string]string{"id": "fes.coleco", "name": "FES Coleco", "version": "1.0.0"}}, "compatibility": "unknown"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := writeKitConfig(t, t.TempDir(), server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := NewClient(cfg)
	var coreReady atomic.Bool
	err := Run(ctx, client, func(m Model) {
		if m.Connected && len(m.Catalog) == 1 && m.Catalog[0].ID == "sonic" && len(m.CoreStatuses) == 3 {
			coreReady.Store(true)
			cancel()
		}
	}, func() (Pad, error) { return nil, errors.New("no pad") })
	if err != nil {
		t.Fatal(err)
	}
	snap, ok := client.Cache.LoadCatalog()
	if !ok || len(snap.Games) != 1 || snap.Games[0].ID != "sonic" || snap.Games[0].Cover == "" {
		t.Fatalf("persisted snapshot %#v ok=%v", snap, ok)
	}
	if !coreReady.Load() {
		t.Fatal("core status was not applied with catalog refresh")
	}
}

func TestRunKeepsCatalogWhenCoreStatusUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[{"id":"megadrive","game_count":1}]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[{"id":"sonic","title":"Sonic","system":"megadrive","launchable":true}]}`))
		case "/api/v1/library/core-entries", "/api/v1/core-packages":
			http.Error(w, "core inventory unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var observed atomic.Bool
	err := Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(m Model) {
		if m.Connected && len(m.Catalog) == 1 && m.CoreStatusUnavailable {
			observed.Store(true)
			cancel()
		}
	}, func() (Pad, error) { return nil, errors.New("no pad") })
	if err != nil {
		t.Fatal(err)
	}
	if !observed.Load() {
		t.Fatal("core status failure discarded ordinary catalog")
	}
}

func writeKitConfig(t *testing.T, dir, api string) Config {
	t.Helper()
	p := filepath.Join(dir, "launcher.json")
	body := `{"api":"` + api + `","token":"12345678901234567890123456789012","target_id":"73dc9f5f-1a12-4a95-a820-a9b4e600769a","hps_framebuffer":true}`
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
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

type customSessionPad struct {
	polls int
}

func (p *customSessionPad) Poll() ([]remoteinput.Event, error) {
	p.polls++
	if p.polls < 2 {
		return nil, nil
	}
	// Keep the chord held across polls. Session state arrives on the host
	// poll, which is independent of the local socket.
	a, _ := remoteinput.NormalizeGamepad("a", true)
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	startPress, _ := remoteinput.NormalizeGamepad("start", true)
	return []remoteinput.Event{a, selectPress, startPress}, nil
}
func (*customSessionPad) Close() error { return nil }

func TestRunForwardsCapableCustomPaddleAndKeepsStopChord(t *testing.T) {
	frames := make(chan protocol.InputFrame, 4)
	ln, path := listenLocalInput(t, frames)
	defer ln.Close()
	stopped := make(chan struct{}, 1)
	var hostInput atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"active","execution":"fpga_development","input":{"state":"detached","ready":false},"core_package":{"generation":9,"gamepad":true}}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_, _ = w.Write([]byte(`{"platforms":[]}`))
		case "/api/v1/games":
			_, _ = w.Write([]byte(`{"games":[]}`))
		case "/api/v1/launcher/input":
			hostInput.Add(1)
			http.Error(w, "kit pads must not post here", http.StatusConflict)
		case "/api/v1/session/stop":
			select {
			case stopped <- struct{}{}:
			default:
			}
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := NewClient(Config{API: server.URL})
	client.localCore = func(context.Context) (bool, error) { return true, nil }
	client.localInputPath = path
	_ = Run(ctx, client, func(Model) {}, func() (Pad, error) { return &customSessionPad{}, nil })
	frame := awaitFrame(t, frames)
	if frame.Code != uint16(remoteinput.ButtonA) || frame.Player != 0 {
		t.Fatalf("frame %+v", frame)
	}
	if hostInput.Load() != 0 {
		t.Fatalf("host input posts = %d", hostInput.Load())
	}
	select {
	case <-stopped:
	default:
		t.Fatal("custom session Stop chord was not dispatched")
	}
}

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
			_, _ = w.Write([]byte(`{"games":[{"id":"pong","title":"Pong","state":"available","root_online":true,"launchable":true}]}`))
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
	_ = Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(m Model) {
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
	_ = Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(m Model) {
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
	_ = Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(m Model) {
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
	_ = Run(ctx, NewClient(Config{API: server.URL, HPSFramebuffer: true}), func(m Model) {
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
