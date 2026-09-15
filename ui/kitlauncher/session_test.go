package kitlauncher

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

func TestRunOfflineInputDoesNotUseHostlessTarget(t *testing.T) {
	var targetRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		http.Error(w, "target must not be contacted", http.StatusServiceUnavailable)
	}))
	defer target.Close()

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "host unavailable", http.StatusServiceUnavailable)
	}))
	defer host.Close()

	game := tenfoot.Game{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true}
	client := newSessionTestClient(t, host.URL, target.URL, game)
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	var offline atomic.Bool
	pad := &offlineLaunchPad{ready: &offline}
	if err := Run(ctx, client, func(m Model) {
		if !m.Connected && len(m.Games) == 1 {
			offline.Store(true)
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	if pad.presses.Load() != 2 {
		t.Fatalf("offline input presses = %d, want cached browse input to remain usable", pad.presses.Load())
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("offline UI contacted target %d times", got)
	}
}

func TestRunReconnectsBeforeAllowingLaunch(t *testing.T) {
	var hostUp atomic.Bool
	var state atomic.Value
	state.Store("idle")
	var launchCalls atomic.Int64
	var targetRequests atomic.Int64
	game := tenfoot.Game{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		http.Error(w, "target must not be contacted", http.StatusServiceUnavailable)
	}))
	defer target.Close()
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostUp.Load() {
			http.Error(w, "host unavailable", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/api/v1/session":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": state.Load().(string)})
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []map[string]any{{"id": game.System, "game_count": 1}}})
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []tenfoot.Game{game}})
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		case "/api/v1/session/launch":
			launchCalls.Add(1)
			state.Store("active")
			_, _ = w.Write([]byte(`{"state":"active","game_id":"sonic","execution":"fpga_native"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer host.Close()

	client := newSessionTestClient(t, host.URL, target.URL, game)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var offline, connectedReady atomic.Bool
	pad := &reconnectPad{offline: &offline, connected: &connectedReady, hostUp: &hostUp}
	if err := Run(ctx, client, func(m Model) {
		if !m.Connected && len(m.Games) == 1 {
			offline.Store(true)
		}
		if m.Connected && m.TargetReady && m.Session.State == "idle" && len(m.Games) == 1 {
			connectedReady.Store(true)
		}
		if launchCalls.Load() == 1 {
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	if pad.offlinePresses.Load() != 2 {
		t.Fatalf("offline presses = %d, want cached browse attempt", pad.offlinePresses.Load())
	}
	if !hostUp.Load() || !connectedReady.Load() {
		t.Fatalf("host did not reconnect before launch: host=%v ready=%v", hostUp.Load(), connectedReady.Load())
	}
	if got := launchCalls.Load(); got != 1 {
		t.Fatalf("session launches = %d, want one after reconnect", got)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("UI contacted target %d times during loss/reconnect", got)
	}
}

func TestRunActiveStopThenRelaunchUsesSessionAPI(t *testing.T) {
	var state atomic.Value
	state.Store("idle")
	var launchCalls, stopCalls atomic.Int64
	game := tenfoot.Game{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": state.Load().(string), "game_id": "sonic"})
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []map[string]any{{"id": game.System, "game_count": 1}}})
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []tenfoot.Game{game}})
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		case "/api/v1/session/launch":
			launchCalls.Add(1)
			state.Store("active")
			_, _ = w.Write([]byte(`{"state":"active","game_id":"sonic","execution":"fpga_native"}`))
		case "/api/v1/session/stop":
			stopCalls.Add(1)
			state.Store("idle")
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newSessionTestClient(t, server.URL, "", game)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var initialReady, idleAfterStop atomic.Bool
	pad := &sessionCyclePad{initialReady: &initialReady, idleReady: &idleAfterStop, launches: &launchCalls, stops: &stopCalls}
	if err := Run(ctx, client, func(m Model) {
		if m.Connected && m.TargetReady && m.Session.State == "idle" && !m.Busy && launchCalls.Load() == 0 {
			initialReady.Store(true)
		}
		if launchCalls.Load() == 1 && stopCalls.Load() == 1 && m.Session.State == "idle" && !m.Busy {
			idleAfterStop.Store(true)
		}
		if launchCalls.Load() == 2 {
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	if got := launchCalls.Load(); got != 2 {
		t.Fatalf("session launches = %d, want initial launch and relaunch", got)
	}
	if got := stopCalls.Load(); got != 1 {
		t.Fatalf("session stops = %d, want one persistent stop", got)
	}
	if !idleAfterStop.Load() {
		t.Fatal("missing authoritative idle transition after stop")
	}
}

func TestRunDoesNotRelaunchUntilDelayedStopConfirmsIdle(t *testing.T) {
	var state atomic.Value
	state.Store("idle")
	var launchCalls, stopCalls atomic.Int64
	stopStarted := make(chan struct{})
	releaseStop := make(chan struct{})
	var stopOnce sync.Once
	var prematureLaunch atomic.Bool
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	game := tenfoot.Game{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": state.Load().(string)})
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "target": map[string]any{"reachable": true, "ready": true}})
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []map[string]any{{"id": game.System, "game_count": 1}}})
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []tenfoot.Game{game}})
		case "/api/v1/library/attract":
			_ = json.NewEncoder(w).Encode(map[string]any{"idle_seconds": 60, "items": []any{}})
		case "/api/v1/session/launch":
			if launchCalls.Add(1) == 2 {
				cancel()
			}
			state.Store("active")
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "active", "game_id": game.ID})
		case "/api/v1/session/stop":
			stopCalls.Add(1)
			stopOnce.Do(func() { close(stopStarted) })
			select {
			case <-releaseStop:
			case <-r.Context().Done():
				return
			}
			state.Store("idle")
			_ = json.NewEncoder(w).Encode(map[string]string{"state": "idle"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	go func() {
		select {
		case <-stopStarted:
			time.Sleep(150 * time.Millisecond)
			if launchCalls.Load() != 1 {
				prematureLaunch.Store(true)
			}
			close(releaseStop)
		case <-ctx.Done():
		}
	}()

	client := newSessionTestClient(t, server.URL, "", game)
	pad := &delayedStopPad{ready: new(atomic.Bool), launches: &launchCalls, stops: &stopCalls}
	if err := Run(ctx, client, func(m Model) {
		if m.Connected && m.TargetReady && m.Session.State == "idle" && !m.Busy {
			pad.ready.Store(true)
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	if prematureLaunch.Load() {
		t.Fatal("relaunch was admitted before delayed stop confirmed idle")
	}
	if got := launchCalls.Load(); got != 2 {
		t.Fatalf("session launches = %d, want initial launch and post-idle relaunch", got)
	}
	if got := stopCalls.Load(); got != 1 {
		t.Fatalf("session stops = %d, want one delayed stop", got)
	}
}

func TestRunDisplaysBoundedSessionAPIError(t *testing.T) {
	game := tenfoot.Game{ID: "sonic", Title: "Sonic", System: "megadrive", Launchable: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []map[string]any{{"id": game.System, "game_count": 1}}})
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []tenfoot.Game{game}})
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		case "/api/v1/session/launch":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"KIT_LEASE_BLOCKED","message":"another session owns the kit"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newSessionTestClient(t, server.URL, "", game)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var ready atomic.Bool
	pad := &offlineLaunchPad{ready: &ready}
	var message atomic.Value
	if err := Run(ctx, client, func(m Model) {
		if m.Connected && m.TargetReady && len(m.Games) == 1 {
			ready.Store(true)
		}
		if !m.Busy && strings.Contains(m.Message, "KIT_LEASE_BLOCKED") {
			message.Store(m.Message)
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	got, _ := message.Load().(string)
	if got != "KIT_LEASE_BLOCKED: another session owns the kit" {
		t.Fatalf("safe API error = %q", got)
	}
	if strings.Contains(got, "Operation failed") || len(got) > 160 {
		t.Fatalf("unbounded/generic API error = %q", got)
	}
}

func newSessionTestClient(t *testing.T, hostURL, agentURL string, game tenfoot.Game) *Client {
	t.Helper()
	dir := t.TempDir()
	cfg := writeKitConfig(t, dir, hostURL)
	if agentURL != "" {
		writeAgentTestConfig(t, cfg.path, agentURL)
	}
	client := NewClient(cfg)
	if client.Cache == nil {
		t.Fatal("session fixture cache unavailable")
	}
	if err := client.Cache.SaveCatalog(CatalogSnapshot{Games: []tenfoot.Game{game}}); err != nil {
		t.Fatal(err)
	}
	return client
}

func writeAgentTestConfig(t *testing.T, launcherPath, agentURL string) {
	t.Helper()
	u, err := url.Parse(agentURL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("listen_address = %q\ntoken = %q\nmister_process_comm = %q\ncommand_pipe = %q\ncore_name_file = %q\nmenu_rbf = %q\nmgl_directory = %q\n", net.JoinHostPort(host, port), "agent-token", "/dev/null", "/dev/null", "/dev/null", "/dev/null", "/tmp")
	if err := os.WriteFile(filepath.Join(filepath.Dir(launcherPath), "agent.toml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

type offlineLaunchPad struct {
	ready   *atomic.Bool
	presses atomic.Int64
}

func (p *offlineLaunchPad) Poll() ([]remoteinput.Event, error) {
	if p.ready == nil || !p.ready.Load() {
		return nil, nil
	}
	if p.presses.Load() >= 2 {
		return nil, nil
	}
	p.presses.Add(1)
	e, _ := remoteinput.NormalizeGamepad("a", true)
	return []remoteinput.Event{e}, nil
}

func (*offlineLaunchPad) Close() error { return nil }

type reconnectPad struct {
	offline, connected *atomic.Bool
	hostUp             *atomic.Bool
	offlinePresses     atomic.Int64
	connectedPresses   atomic.Int64
}

func (p *reconnectPad) Poll() ([]remoteinput.Event, error) {
	if p.offline != nil && p.offline.Load() {
		if p.offlinePresses.Load() < 2 {
			n := p.offlinePresses.Add(1)
			if n == 2 && p.hostUp != nil {
				p.hostUp.Store(true)
			}
			e, _ := remoteinput.NormalizeGamepad("a", true)
			return []remoteinput.Event{e}, nil
		}
	}
	if p.connected != nil && p.connected.Load() {
		if p.connectedPresses.Load() >= 2 {
			return nil, nil
		}
		n := p.connectedPresses.Add(1)
		_ = n
		e, _ := remoteinput.NormalizeGamepad("a", true)
		return []remoteinput.Event{e}, nil
	}
	return nil, nil
}

func (*reconnectPad) Close() error { return nil }

type sessionCyclePad struct {
	initialReady, idleReady         *atomic.Bool
	launches, stops                 *atomic.Int64
	initialPresses, relaunchPresses atomic.Int64
}

func (p *sessionCyclePad) Poll() ([]remoteinput.Event, error) {
	if p.initialReady != nil && p.initialReady.Load() && p.launches.Load() == 0 {
		n := p.initialPresses.Add(1)
		if n <= 2 {
			e, _ := remoteinput.NormalizeGamepad("a", true)
			return []remoteinput.Event{e}, nil
		}
	}
	if p.launches.Load() == 1 && p.stops.Load() == 0 {
		selectPress, _ := remoteinput.NormalizeGamepad("select", true)
		startPress, _ := remoteinput.NormalizeGamepad("start", true)
		return []remoteinput.Event{selectPress, startPress}, nil
	}
	if p.idleReady != nil && p.idleReady.Load() && p.launches.Load() == 1 && p.stops.Load() == 1 {
		if p.relaunchPresses.Add(1) == 1 {
			e, _ := remoteinput.NormalizeGamepad("a", true)
			return []remoteinput.Event{e}, nil
		}
	}
	return nil, nil
}

func (*sessionCyclePad) Close() error { return nil }

type delayedStopPad struct {
	ready           *atomic.Bool
	launches, stops *atomic.Int64
	initialPresses  atomic.Int64
}

func (p *delayedStopPad) Poll() ([]remoteinput.Event, error) {
	if p.ready != nil && p.ready.Load() && p.launches.Load() == 0 {
		if p.initialPresses.Add(1) <= 2 {
			e, _ := remoteinput.NormalizeGamepad("a", true)
			return []remoteinput.Event{e}, nil
		}
	}
	if p.launches.Load() == 1 && p.stops.Load() == 0 {
		selectPress, _ := remoteinput.NormalizeGamepad("select", true)
		startPress, _ := remoteinput.NormalizeGamepad("start", true)
		return []remoteinput.Event{selectPress, startPress}, nil
	}
	if p.launches.Load() == 1 && p.stops.Load() == 1 {
		e, _ := remoteinput.NormalizeGamepad("a", true)
		return []remoteinput.Event{e}, nil
	}
	return nil, nil
}

func (*delayedStopPad) Close() error { return nil }
