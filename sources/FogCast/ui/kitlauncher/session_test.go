package kitlauncher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

	game := hostclient.Game{ID: "sonic", Title: "Sonic", System: "megadrive", State: "available", RootOnline: true, Launchable: true}
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
	game := hostclient.Game{ID: "sonic", Title: "Sonic", System: "megadrive", State: "available", RootOnline: true, Launchable: true}

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
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{game}})
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
	var stopBody atomic.Value
	game := hostclient.Game{ID: "sonic", Title: "Sonic", System: "megadrive", State: "available", RootOnline: true, Launchable: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": state.Load().(string), "game_id": "sonic"})
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []map[string]any{{"id": game.System, "game_count": 1}}})
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{game}})
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		case "/api/v1/session/launch":
			launchCalls.Add(1)
			state.Store("active")
			_, _ = w.Write([]byte(`{"state":"active","game_id":"sonic","execution":"fpga_native"}`))
		case "/api/v1/session/stop":
			raw, _ := io.ReadAll(r.Body)
			stopBody.Store(string(raw))
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
	if body, _ := stopBody.Load().(string); body != "" {
		t.Fatalf("Select+Start stop body = %q, want explicit release", body)
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
	game := hostclient.Game{ID: "sonic", Title: "Sonic", System: "megadrive", State: "available", RootOnline: true, Launchable: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_ = json.NewEncoder(w).Encode(map[string]string{"state": state.Load().(string)})
		case "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "target": map[string]any{"reachable": true, "ready": true}})
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []map[string]any{{"id": game.System, "game_count": 1}}})
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{game}})
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
	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousOutput)
	game := hostclient.Game{ID: "fpga-browser-protocol-smoke-pong-20260918", Title: "Browser protocol smoke Pong 20260918", System: "fpga", State: "available", RootOnline: true, Launchable: true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []map[string]any{{"id": game.System, "game_count": 1}}})
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{game}})
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		case "/api/v1/session/launch":
			var body struct {
				GameID string `json:"game_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.GameID != game.ID {
				t.Errorf("submitted game_id=%q, decode=%v; want %q", body.GameID, err, game.ID)
			}
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
		if !m.Busy && strings.Contains(m.Message, "another session owns the kit") {
			message.Store(m.Message)
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	got, _ := message.Load().(string)
	if got != "another session owns the kit (Launch Browser protocol smoke Pong 20260918)" {
		t.Fatalf("safe API error = %q", got)
	}
	if strings.Contains(got, "Operation failed") || len(got) > 160 {
		t.Fatalf("unbounded/generic API error = %q", got)
	}
	for _, want := range []string{
		"kit session dispatch epoch=1 action=launch game_id=" + strconv.Quote(game.ID),
		"kit session result epoch=1 action=launch game_id=" + strconv.Quote(game.ID) + " http_status=409 code=\"KIT_LEASE_BLOCKED\"",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing trace %q in %q", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "another session owns the kit") {
		t.Fatal("trace included response message rather than safe result fields")
	}
}

func newSessionTestClient(t *testing.T, hostURL, agentURL string, game hostclient.Game) *Client {
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
	if err := client.Cache.SaveCatalog(CatalogSnapshot{Games: []hostclient.Game{game}}); err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRunClearsLabeledHostUnavailableAfterReconnect(t *testing.T) {
	game := hostclient.Game{ID: "fpga-pong", Title: "Browser Pong", System: "fpga", State: "available", RootOnline: true, Launchable: true}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var launches atomic.Int64
	observedLoss := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			if launches.Load() != 0 {
				select {
				case <-observedLoss:
				case <-ctx.Done():
					return
				}
			}
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		case "/api/v1/health":
			_, _ = w.Write([]byte(`{"ready":true,"target":{"reachable":true,"ready":true}}`))
		case "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []map[string]any{{"id": game.System, "game_count": 1}}})
		case "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{game}})
		case "/api/v1/library/attract":
			_, _ = w.Write([]byte(`{"idle_seconds":60,"items":[]}`))
		case "/api/v1/session/launch":
			launches.Add(1)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close() // Lost response, never permission to replay the POST.
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	defer cancel() // Unblock any handler before server.Close.
	client := newSessionTestClient(t, server.URL, "", game)
	var ready atomic.Bool
	pad := &offlineLaunchPad{ready: &ready}
	lost, recovered := false, false
	var recoveredMessage string
	if err := Run(ctx, client, func(m Model) {
		if m.Connected && m.TargetReady && len(m.Games) == 1 {
			ready.Store(true)
		}
		if !lost && !m.Connected && m.Message == "Host unavailable (Launch Browser Pong)" {
			lost = true
			close(observedLoss)
		}
		if lost && m.Connected && m.TargetReady && !m.Busy {
			recovered, recoveredMessage = true, m.Message
			cancel()
		}
	}, func() (Pad, error) { return pad, nil }); err != nil {
		t.Fatal(err)
	}
	if !lost || !recovered || recoveredMessage != "" {
		t.Fatalf("lost=%v recovered=%v message=%q", lost, recovered, recoveredMessage)
	}
	if launches.Load() != 1 {
		t.Fatalf("launches=%d; must not replay", launches.Load())
	}
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
	body := fmt.Sprintf("listen_address = %q\ntoken = %q\n", net.JoinHostPort(host, port), "agent-token")
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
