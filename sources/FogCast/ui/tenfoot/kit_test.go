package tenfoot

import (
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestKitHealthLine(t *testing.T) {
	t.Parallel()
	if got := kitHealthLine(true, false, hostclient.HealthResult{}); got != "host unreachable" {
		t.Fatalf("host unreachable = %q", got)
	}
	if got := kitHealthLine(false, false, hostclient.HealthResult{}); got != "" {
		t.Fatalf("unknown = %q", got)
	}
	if got := kitHealthLine(false, true, hostclient.HealthResult{Ready: false}); got != "host not ready" {
		t.Fatalf("host not ready = %q", got)
	}
	if got := kitHealthLine(false, true, hostclient.HealthResult{Ready: true}); got != "kit unreachable" {
		t.Fatalf("kit unreachable = %q", got)
	}
	if got := kitHealthLine(false, true, hostclient.HealthResult{Ready: true, TargetReachable: true}); got != "kit not ready" {
		t.Fatalf("kit not ready = %q", got)
	}
	if got := kitHealthLine(false, true, hostclient.HealthResult{Ready: true, TargetReachable: true, TargetReady: true}); got != "" {
		t.Fatalf("healthy = %q", got)
	}
}

func TestConnectionLineKeepsTargetLifecycleDistinct(t *testing.T) {
	t.Parallel()
	tests := []struct {
		connection hostclient.TargetConnection
		want       string
	}{
		{hostclient.TargetConnection{State: "connecting", Message: "looking for den-kit"}, "connecting · looking for den-kit"},
		{hostclient.TargetConnection{State: "disconnected", Message: "target not found"}, "disconnected · target not found"},
		{hostclient.TargetConnection{State: "ready", Address: "192.0.2.4:8182"}, "ready · 192.0.2.4:8182"},
		{hostclient.TargetConnection{State: "active", Address: "192.0.2.4:8182"}, "active · 192.0.2.4:8182"},
		{hostclient.TargetConnection{State: "busy", Owner: "living-room"}, "busy · owned by living-room"},
		{hostclient.TargetConnection{State: "recovery-required", Message: "cleanup failed"}, "recovery required · cleanup failed"},
	}
	for _, tt := range tests {
		if got := connectionLine(tt.connection); got != tt.want {
			t.Errorf("connectionLine(%#v) = %q, want %q", tt.connection, got, tt.want)
		}
	}
}

func TestChromeLinePrefixesKitHealth(t *testing.T) {
	t.Parallel()
	browse := Snapshot{
		ViewLabel: "All",
		Status:    "12 titles · All · All · Title",
		Health:    HealthSnapshot{Line: "kit unreachable"},
	}
	chrome := browse.ChromeLine()
	if !strings.HasPrefix(chrome, "kit unreachable") {
		t.Fatalf("browse chrome = %q", chrome)
	}
	if !strings.Contains(chrome, "All") {
		t.Fatalf("browse chrome dropped view: %q", chrome)
	}
	playing := Snapshot{
		GPUParked: true,
		Session:   SessionSnapshot{State: "active", Title: "Mario", InputState: "detached"},
		Health:    HealthSnapshot{Line: "host unreachable"},
	}
	got := playing.ChromeLine()
	if !strings.HasPrefix(got, "host unreachable") || !strings.Contains(got, "Now playing") || !strings.Contains(got, "Mario") {
		t.Fatalf("now-playing chrome = %q", got)
	}
}

func TestRemoteInputGates(t *testing.T) {
	t.Parallel()
	active := hostclient.SessionResult{
		State:     "active",
		Execution: "fpga_native",
		Input:     &hostclient.SessionInput{State: "detached"},
	}
	if !remoteInputCanAttach(active) || remoteInputCanDetach(active) {
		t.Fatalf("detached attach/detach = attach:%v detach:%v", remoteInputCanAttach(active), remoteInputCanDetach(active))
	}
	active.Input.State = "attached"
	if remoteInputCanAttach(active) || !remoteInputCanDetach(active) {
		t.Fatalf("attached attach/detach = attach:%v detach:%v", remoteInputCanAttach(active), remoteInputCanDetach(active))
	}
	active.Input.State = "starting"
	if remoteInputCanAttach(active) || remoteInputCanDetach(active) {
		t.Fatal("starting should be busy")
	}
	active.Input.State = "reconnecting"
	if remoteInputCanAttach(active) || remoteInputCanDetach(active) {
		t.Fatal("reconnecting should be busy")
	}
	active.Input.State = "failed"
	if !remoteInputCanAttach(active) {
		t.Fatal("failed should allow attach")
	}
	active.Execution = "host_emulator"
	active.Input.State = "detached"
	if remoteInputCanAttach(active) || remoteInputCanDetach(active) {
		t.Fatal("non-fpga should not offer input")
	}
	active.Execution = "fpga_native"
	active.State = "idle"
	if remoteInputCanAttach(active) {
		t.Fatal("idle should not offer input")
	}
}

func TestAppPollsHealthIntoChrome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready":  true,
				"target": map[string]any{"reachable": false, "ready": false},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"idle"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Health.Line == "kit unreachable" && strings.HasPrefix(snap.ChromeLine(), "kit unreachable")
	})
}

func TestAppAttachDetachGamepadAndErrorRevert(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"detached"}}`
	var attaches, detaches int
	failAttach := true
	block := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready":  true,
				"target": map[string]any{"reachable": true, "ready": true},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/input/attach":
			mu.Lock()
			attaches++
			fail := failAttach
			mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"error":{"code":"TARGET_BUSY","message":"/private/path"}}`)
				return
			}
			select {
			case block <- struct{}{}:
			default:
			}
			<-release
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"attached","ready":true}}`
			mu.Unlock()
			_, _ = io.WriteString(w, sessionJSON)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/input/detach":
			mu.Lock()
			detaches++
			sessionJSON = `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"detached"}}`
			mu.Unlock()
			_, _ = io.WriteString(w, sessionJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		server.Close()
	})
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.Session.InputState == "detached" && snap.Session.InputHint == "X attach"
	})
	prior := app.Snapshot().Session.InputState
	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return strings.Contains(snap.Status, "input attach failed")
	})
	snap := app.Snapshot()
	if snap.Session.InputState != prior {
		t.Fatalf("failed attach mutated input %q -> %q", prior, snap.Session.InputState)
	}
	if strings.Contains(snap.Status, "/private/path") {
		t.Fatalf("leaked host message: %q", snap.Status)
	}

	mu.Lock()
	failAttach = false
	mu.Unlock()
	app.Press(CmdSortCycle, time.Now())
	select {
	case <-block:
	case <-time.After(2 * time.Second):
		t.Fatal("attach did not start")
	}
	app.Press(CmdSortCycle, time.Now())
	app.Press(CmdSortCycle, time.Now())
	close(release)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.InputState == "attached" && !snap.Session.InputBusy && strings.Contains(snap.Status, "input attached")
	})
	mu.Lock()
	attachCount := attaches
	mu.Unlock()
	if attachCount != 2 {
		t.Fatalf("attaches = %d, want 2 (one failed, one success; busy should not double-fire)", attachCount)
	}

	app.Press(CmdSortCycle, time.Now())
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.InputState == "detached" && strings.Contains(snap.Status, "input detached")
	})
	mu.Lock()
	detachCount := detaches
	mu.Unlock()
	if detachCount != 1 {
		t.Fatalf("detaches = %d", detachCount)
	}
}

func TestAppQueuesStopUntilInputMutationFinishes(t *testing.T) {
	var mu sync.Mutex
	sessionJSON := `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"detached"}}`
	var attaches, stops int
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			mu.Lock()
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/input/attach":
			mu.Lock()
			attaches++
			mu.Unlock()
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			mu.Lock()
			sessionJSON = `{"state":"active","game_id":"snes-mario","execution":"fpga_native","input":{"state":"attached","ready":true}}`
			body := sessionJSON
			mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			mu.Lock()
			stops++
			sessionJSON = `{"state":"idle","media":"stopped"}`
			mu.Unlock()
			_, _ = io.WriteString(w, `{"state":"idle","media":"stopped"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		server.Close()
	})
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.Session.InputHint == "X attach"
	})
	app.Press(CmdSortCycle, time.Now())
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("attach did not start")
	}
	app.Press(CmdBack, time.Now())
	mu.Lock()
	stopCount := stops
	mu.Unlock()
	if stopCount != 0 {
		t.Fatalf("stop posted during attach, n=%d", stopCount)
	}
	close(release)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return !snap.GPUParked && snap.Session.State != "active"
	})
	mu.Lock()
	stopCount = stops
	attachCount := attaches
	mu.Unlock()
	if attachCount != 1 || stopCount != 1 {
		t.Fatalf("attaches=%d stops=%d", attachCount, stopCount)
	}
}

func TestAppIgnoresInputToggleWhenNotFPGANative(t *testing.T) {
	var attaches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, `{"state":"active","game_id":"snes-mario","execution":"host_emulator","input":{"state":"detached"}}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/input/attach":
			attaches++
			http.Error(w, "should not attach", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	app := NewApp(NewClient(server.URL, server.Client()), 800, 600, 10)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitSnapshot(t, app, 2*time.Second, func(snap Snapshot) bool {
		return snap.Session.State == "active" && snap.GPUParked
	})
	app.Press(CmdSortCycle, time.Now())
	time.Sleep(50 * time.Millisecond)
	if attaches != 0 {
		t.Fatalf("attach posted for host_emulator, n=%d", attaches)
	}
}
