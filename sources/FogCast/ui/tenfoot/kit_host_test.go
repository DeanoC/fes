package tenfoot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/testwait"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestKitHostUsesUnpairedHostInventoryInsteadOfEmbeddedCatalog(t *testing.T) {
	id := strings.Repeat("a", 64)
	pkg := localcores.Core{PackageID: id, CoreID: "fes.pong", Name: "Pong", Launchable: true}
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/kit/local-cores" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-FogCast-Target-ID") != "" {
			t.Error("local host request carried launcher pairing credentials")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"cores": []localcores.Core{pkg}})
	}))
	defer host.Close()
	app, err := configuredApp(Options{
		GFX: "menu-display", KitHost: true, APIBase: host.URL,
		APIToken: "paired-token", TargetID: "paired-target",
		PrefsPath: filepath.Join(t.TempDir(), "tenfoot.json"), Width: 1280, Height: 720,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Stop()
	if app.client.paired {
		t.Fatal("kit host was treated as paired or replaced by an embedded catalog")
	}
	cores, err := app.localCores.List(t.Context())
	if err != nil || len(cores) != 1 || cores[0].PackageID != id || cores[0].CoreID != "fes.pong" {
		t.Fatalf("host inventory = %+v, err = %v", cores, err)
	}
}

func TestKitHostStartupAndActiveSessionHoldDisplay(t *testing.T) {
	var available atomic.Bool
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/kit/local-cores":
			_, _ = w.Write([]byte(`{"cores":[]}`))
		case "/api/v1/session":
			if !available.Load() {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer host.Close()
	app, err := configuredApp(Options{GFX: "menu-display", KitHost: true, APIBase: host.URL, PrefsPath: filepath.Join(t.TempDir(), "tenfoot.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Stop()
	app.fetchSession(t.Context())
	if snap := app.Snapshot(); !snap.LocalCorePresentsPaused || !snap.LocalCoreLateAdopt {
		t.Fatal("unavailable host allowed startup presentation")
	}
	if !app.HandlePlayHIDScancode("a", 4, true, time.Now()) {
		t.Fatal("unavailable host allowed startup input")
	}
	available.Store(true)
	app.fetchSession(t.Context())
	if app.Snapshot().LocalCorePresentsPaused {
		t.Fatal("confirmed idle session kept startup display paused")
	}
	app.mu.Lock()
	app.session.State = "active"
	app.mu.Unlock()
	if snap := app.Snapshot(); !snap.LocalCorePresentsPaused || !snap.LocalCoreLateAdopt {
		t.Fatal("active host session allowed presentation over machine pixels")
	}
	app.mu.Lock()
	app.sessionDisplay = protocol.SessionDisplayShown
	app.mu.Unlock()
	if app.Snapshot().LocalCorePresentsPaused {
		t.Fatal("visible session plane did not allow presentation")
	}
}

func TestKitHostMenuBecomesReadyWhenHostSessionIsIdleWithoutTargetID(t *testing.T) {
	var available atomic.Bool
	httpClient := &http.Client{Transport: kitHostSessionTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/session":
			if !available.Load() {
				return kitHostResponse(r, http.StatusServiceUnavailable, `{"error":{"code":"TARGET_UNAVAILABLE","message":"host session starting"}}`), nil
			}
			return kitHostResponse(r, http.StatusOK, `{"state":"idle"}`), nil
		case "/api/v1/health":
			if !available.Load() {
				return kitHostResponse(r, http.StatusServiceUnavailable, `{"error":{"code":"TARGET_UNAVAILABLE","message":"target health starting"}}`), nil
			}
			return kitHostResponse(r, http.StatusOK, `{"ready":true,"target":{"reachable":true,"ready":true,"connection":{"state":"connected"}}}`), nil
		case "/api/v1/status":
			return kitHostResponse(r, http.StatusOK, `{"state":"idle"}`), nil
		default:
			return kitHostResponse(r, http.StatusNotFound, "not found"), nil
		}
	})}

	readyFile := filepath.Join(t.TempDir(), "launcher-ready.json")
	t.Setenv("FES_SOFTWARE_READY_FILE", readyFile)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", "test-bundle")
	app := NewApp(NewClient("http://kit-host.invalid", httpClient), 1280, 720, 50)
	app.kitHost, app.hostPresentsPaused, app.hostAwaitStatus = true, true, true
	logs := make(chan string, 16)
	app.kitMenuReadyLogf = func(format string, args ...any) { logs <- fmt.Sprintf(format, args...) }
	defer app.Stop()
	rec := &recordingMenu{}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	app.SetMenuDisplay(dev)
	diagCtx, stopDiagnostics := context.WithCancel(t.Context())
	defer stopDiagnostics()
	go app.pollKitMenuReadiness(diagCtx)
	if line := awaitKitMenuReadinessLog(t, logs, "host_session_seen=false", "host_health_seen=false", "host_await_status=true", "host_presents_paused=true", "runtime_status_known=false"); !strings.Contains(line, "elapsed=") {
		t.Fatalf("startup diagnostic omitted elapsed time: %s", line)
	}
	var hold presentHold

	// The host cannot prove that HDMI is idle while its session endpoint is
	// unavailable, so the initial display frame is suppressed and readiness stays off.
	app.fetchSession(t.Context())
	awaitKitMenuReadinessLog(t, logs, "host_session_error=", "503 TARGET_UNAVAILABLE")
	app.fetchHealth(t.Context())
	awaitKitMenuReadinessLog(t, logs, "host_health_error=", "503 TARGET_UNAVAILABLE")
	if skip, err := hold.skip(t.Context(), dev, app.Snapshot()); !skip || err != nil {
		t.Fatalf("unavailable host session: skip=%v err=%v", skip, err)
	}
	dev.Present()
	if readiness := dev.ReadinessDiagnostic(); readiness.Ready || readiness.HasPresented {
		t.Fatalf("unavailable host session produced menu readiness: %+v", readiness)
	}
	if _, err := os.Stat(readyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unavailable agent wrote launcher readiness: %v", err)
	}

	// A late session observation establishes idle. The kit host needs no
	// target ID to resume menu presentation; target ID is outside this predicate.
	available.Store(true)
	app.fetchSession(t.Context())
	app.fetchHealth(t.Context())
	app.mu.Lock()
	targetID := app.health.Connection.TargetID
	app.mu.Unlock()
	if targetID != "" {
		t.Fatalf("offline fixture unexpectedly supplied a target ID: %q", targetID)
	}
	if snap := app.Snapshot(); snap.LocalCorePresentsPaused || snap.LocalCoreLateAdopt {
		t.Fatalf("confirmed idle session left the menu held: %+v", snap)
	}
	if skip, err := hold.skip(t.Context(), dev, app.Snapshot()); skip || err != nil {
		t.Fatalf("confirmed idle session: skip=%v err=%v", skip, err)
	}
	dev.Present()
	ctx, cancel := context.WithTimeout(t.Context(), testwait.Timeout)
	defer cancel()
	if err := dev.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	readiness := dev.ReadinessDiagnostic()
	if !readiness.Ready || !readiness.HasPresented || !readiness.StatusKnown || !readiness.Available || readiness.Generation != readiness.PresentedGen || readiness.LastErr != "" {
		t.Fatalf("idle menu did not satisfy readiness: %+v", readiness)
	}
	data, err := os.ReadFile(readyFile)
	if err != nil || !strings.Contains(string(data), `"bundle_id":"test-bundle"`) {
		t.Fatalf("launcher readiness file %q, err=%v", data, err)
	}
}

func TestKitHostNeverReportsMenuReadyWhenSessionEndpointStaysUnavailable(t *testing.T) {
	httpClient := &http.Client{Transport: kitHostSessionTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/session":
			return kitHostResponse(r, http.StatusServiceUnavailable, `{"error":{"code":"TARGET_UNAVAILABLE","message":"agent unavailable"}}`), nil
		case "/api/v1/health":
			return kitHostResponse(r, http.StatusServiceUnavailable, `{"error":{"code":"TARGET_UNAVAILABLE","message":"target unavailable"}}`), nil
		case "/api/v1/status":
			return kitHostResponse(r, http.StatusOK, `{"state":"idle"}`), nil
		default:
			return kitHostResponse(r, http.StatusNotFound, "not found"), nil
		}
	})}

	readyFile := filepath.Join(t.TempDir(), "launcher-ready.json")
	t.Setenv("FES_SOFTWARE_READY_FILE", readyFile)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", "test-bundle")
	app := NewApp(NewClient("http://kit-host.invalid", httpClient), 1280, 720, 50)
	app.kitHost, app.hostPresentsPaused, app.hostAwaitStatus = true, true, true
	logs := make(chan string, 16)
	app.kitMenuReadyLogf = func(format string, args ...any) { logs <- fmt.Sprintf(format, args...) }
	defer app.Stop()
	rec := &recordingMenu{}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	app.SetMenuDisplay(dev)
	diagCtx, stopDiagnostics := context.WithCancel(t.Context())
	defer stopDiagnostics()
	go app.pollKitMenuReadiness(diagCtx)
	if line := awaitKitMenuReadinessLog(t, logs, "host_session_seen=false", "host_health_seen=false", "host_await_status=true", "host_presents_paused=true", "runtime_status_known=false"); !strings.Contains(line, "elapsed=") {
		t.Fatalf("startup diagnostic omitted elapsed time: %s", line)
	}
	var hold presentHold

	app.fetchSession(t.Context())
	awaitKitMenuReadinessLog(t, logs, "host_session_error=", "503 TARGET_UNAVAILABLE")
	app.fetchHealth(t.Context())
	awaitKitMenuReadinessLog(t, logs, "host_health_error=", "503 TARGET_UNAVAILABLE")
	if skip, err := hold.skip(t.Context(), dev, app.Snapshot()); !skip || err != nil {
		t.Fatalf("unavailable host session: skip=%v err=%v", skip, err)
	}
	dev.Present()
	readiness := dev.ReadinessDiagnostic()
	if readiness.Ready || readiness.HasPresented || readiness.LastErr != "" {
		t.Fatalf("stuck startup reported readiness or presented a frame: %+v", readiness)
	}
	if calls, _, _ := rec.snapshot(); calls != 0 {
		t.Fatalf("stuck startup presented %d frames", calls)
	}
	if _, err := os.Stat(readyFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stuck startup wrote launcher readiness: %v", err)
	}
}

type kitHostSessionTransport func(*http.Request) (*http.Response, error)

func (f kitHostSessionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func kitHostResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: request,
	}
}

func awaitKitMenuReadinessLog(t *testing.T, logs <-chan string, fragments ...string) string {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	var observed []string
	for {
		select {
		case line := <-logs:
			observed = append(observed, line)
			matched := true
			for _, fragment := range fragments {
				if !strings.Contains(line, fragment) {
					matched = false
					break
				}
			}
			if matched {
				return line
			}
		case <-deadline.C:
			t.Fatalf("no readiness diagnostic contained %q; observed %q", fragments, observed)
		}
	}
}

func TestKitHostStartupAndActiveSessionDoNotPresentOverGame(t *testing.T) {
	rec := &recordingMenu{}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	app := NewApp(nil, 1280, 720, 50)
	defer app.Stop()
	app.SetMenuDisplay(dev)
	app.kitHost, app.hostPresentsPaused, app.hostAwaitStatus = true, true, true
	var hold presentHold
	if skip, err := hold.skip(t.Context(), dev, app.Snapshot()); !skip || err != nil {
		t.Fatalf("unknown startup session: skip=%v err=%v", skip, err)
	}
	dev.Present()
	if calls, _, _ := rec.snapshot(); calls != 0 {
		t.Fatalf("unknown session presented %d frames", calls)
	}
	app.mu.Lock()
	app.hostAwaitStatus, app.hostPresentsPaused = false, false
	app.mu.Unlock()
	if skip, err := hold.skip(t.Context(), dev, app.Snapshot()); skip || err != nil {
		t.Fatalf("confirmed idle session: skip=%v err=%v", skip, err)
	}
	dev.Present()
	ctx, cancel := context.WithTimeout(context.Background(), testwait.Timeout)
	defer cancel()
	if err := dev.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	before, _, _ := rec.snapshot()
	if before != 1 {
		t.Fatalf("idle presented %d frames, want 1", before)
	}
	app.mu.Lock()
	app.session.State = "active"
	app.mu.Unlock()
	if skip, err := hold.skip(t.Context(), dev, app.Snapshot()); !skip || err != nil {
		t.Fatalf("active session: skip=%v err=%v", skip, err)
	}
	dev.Present()
	if calls, _, _ := rec.snapshot(); calls != before {
		t.Fatalf("active session presented %d extra frames", calls-before)
	}
}

func TestKitHostWaitsForDisplayHandoffBeforeLaunching(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	defer app.Stop()
	app.kitHost = true
	app.menuDisplay = &gfx.MenuDisplay{}
	app.mu.Lock()
	app.startLaunchGameLocked(hostclient.Game{ID: "game", Title: "Game", State: "available", RootOnline: true, Launchable: true})
	app.mu.Unlock()
	if !app.Snapshot().LocalCorePresentsPaused || requests.Load() != 0 {
		t.Fatal("host launch was not held for the display")
	}
	app.completeLocalLaunchHandoff(errors.New("display unavailable"))
	if requests.Load() != 0 || app.Snapshot().LocalCorePresentsPaused || app.Snapshot().Launch.Phase != "error" {
		t.Fatal("failed display handoff did not abort the launch")
	}
}

func TestKitHostLostLaunchReplyWaitsForHostStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/session/launch" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		if r.URL.Path == "/api/v1/session" {
			_, _ = w.Write([]byte(`{"state":"idle"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	defer app.Stop()
	app.kitHost = true
	app.menuDisplay = &gfx.MenuDisplay{}
	app.mu.Lock()
	app.startLaunchGameLocked(hostclient.Game{ID: "game", Title: "Game", State: "available", RootOnline: true, Launchable: true})
	app.mu.Unlock()
	app.completeLocalLaunchHandoff(nil)
	deadline := time.Now().Add(testwait.Timeout)
	for app.Snapshot().Launch.Phase == "launching" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !app.Snapshot().LocalCorePresentsPaused {
		t.Fatal("lost launch reply resumed presents before reconciliation")
	}
	app.fetchSession(context.Background())
	if app.Snapshot().LocalCorePresentsPaused {
		t.Fatal("confirmed idle session did not resume presents")
	}
}

func TestKitHostPreparesCartridgeBeforeLaunchingHostEntry(t *testing.T) {
	packageID := strings.Repeat("b", 64)
	started := make(chan struct{})
	release := make(chan struct{})
	launched := make(chan string, 1)
	var prepares atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/local-core-entry":
			var req struct {
				PackageID string `json:"package_id"`
				GameID    string `json:"game_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.PackageID != "" || req.GameID != "cartridge" {
				t.Errorf("preparation request = %+v", req)
			}
			prepares.Add(1)
			close(started)
			<-release
			_ = json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "prepared", CoreID: "fes.sms", PackageID: packageID})
		case "/api/v1/games/prepared":
			_ = json.NewEncoder(w).Encode(hostclient.Game{ID: "prepared", Title: "Game", State: "available", RootOnline: true, Launchable: true})
		case "/api/v1/session/launch":
			var req struct {
				GameID string `json:"game_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			launched <- req.GameID
			_, _ = w.Write([]byte(`{"state":"active"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	defer app.Stop()
	app.kitHost = true
	app.mu.Lock()
	app.startKitHostEntryLocked("", "cartridge", "Game")
	app.mu.Unlock()
	select {
	case <-started:
	case <-time.After(testwait.Timeout):
		t.Fatal("host preparation did not start")
	}
	app.mu.Lock()
	app.startKitHostEntryLocked("", "cartridge", "Game")
	app.mu.Unlock()
	close(release)
	select {
	case gameID := <-launched:
		if gameID != "prepared" || prepares.Load() != 1 {
			t.Fatalf("launched %q after %d preparations", gameID, prepares.Load())
		}
	case <-time.After(testwait.Timeout):
		t.Fatal("prepared host entry was not launched")
	}
}

func TestKitHostReattachedTitleLookupDoesNotBlockOrOverwriteReplacement(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResponded := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/games/first":
			close(firstStarted)
			<-releaseFirst
			_ = json.NewEncoder(w).Encode(hostclient.Game{ID: "first", Title: "Old title"})
			close(firstResponded)
		case "/api/v1/games/second":
			_ = json.NewEncoder(w).Encode(hostclient.Game{ID: "second", Title: "Current title"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	defer app.Stop()
	app.kitHost = true
	app.mu.Lock()
	app.applySessionLocked(hostclient.SessionResult{ID: "first-session", State: "active", GameID: "first"})
	app.mu.Unlock()
	select {
	case <-firstStarted:
	case <-time.After(testwait.Timeout):
		t.Fatal("title lookup did not start")
	}
	// The first lookup remains blocked while the replacement is installed.
	app.mu.Lock()
	app.applySessionLocked(hostclient.SessionResult{ID: "second-session", State: "active", GameID: "second"})
	app.mu.Unlock()
	deadline := time.Now().Add(testwait.Timeout)
	for app.Snapshot().Session.Title != "Current title" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := app.Snapshot().Session.Title; got != "Current title" {
		t.Fatalf("replacement title = %q", got)
	}
	close(releaseFirst)
	select {
	case <-firstResponded:
	case <-time.After(testwait.Timeout):
		t.Fatal("stale lookup did not return")
	}
	// Give the client goroutine an opportunity to process the late response.
	time.Sleep(20 * time.Millisecond)
	if got := app.Snapshot().Session.Title; got != "Current title" {
		t.Fatalf("stale lookup overwrote replacement title: %q", got)
	}
}

func TestKitHostReattachedTitleRetriesAfterHostFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(hostclient.Game{ID: "game", Title: "Cartridge"})
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	defer app.Stop()
	app.kitHost = true
	session := hostclient.SessionResult{ID: "session", State: "active", GameID: "game"}
	app.mu.Lock()
	app.applySessionLocked(session)
	app.mu.Unlock()
	deadline := time.Now().Add(testwait.Timeout)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		finished := !app.sessionTitlePending && !app.sessionTitleNext.IsZero()
		app.mu.Unlock()
		if finished {
			break
		}
		time.Sleep(time.Millisecond)
	}
	app.mu.Lock()
	if app.sessionTitlePending || app.sessionTitleNext.IsZero() {
		app.mu.Unlock()
		t.Fatal("failed lookup did not schedule a retry")
	}
	app.sessionTitleNext = time.Time{}
	app.applySessionLocked(session)
	app.mu.Unlock()
	for app.Snapshot().Session.Title != "Cartridge" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := app.Snapshot().Session.Title; got != "Cartridge" || requests.Load() != 2 {
		t.Fatalf("title=%q requests=%d", got, requests.Load())
	}
}

func TestKitHostReattachedSessionWithoutGameIDDoesNotInventTitle(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 50)
	defer app.Stop()
	app.kitHost = true
	app.mu.Lock()
	app.applySessionLocked(hostclient.SessionResult{ID: "session", State: "active"})
	pending := app.sessionTitlePending
	app.mu.Unlock()
	if title := app.Snapshot().Session.Title; title != "" || pending || requests.Load() != 0 {
		t.Fatalf("title=%q pending=%v requests=%d", title, pending, requests.Load())
	}
}

func TestKitHostReattachesAfterHostAndLauncherRestart(t *testing.T) {
	var available atomic.Bool
	var stops atomic.Int32
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/session":
			if !available.Load() {
				http.Error(w, "host restarting", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"id":"session-1","state":"active","target":"kit","target_id":"kit-1","core_package":{"package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation":7}}`))
		case "/api/v1/session/stop":
			stops.Add(1)
			_, _ = w.Write([]byte(`{"state":"idle"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer host.Close()
	newLauncher := func() *App {
		app, err := configuredApp(Options{GFX: "menu-display", KitHost: true, APIBase: host.URL, PrefsPath: filepath.Join(t.TempDir(), "tenfoot.json")})
		if err != nil {
			t.Fatal(err)
		}
		return app
	}
	assertAttached := func(app *App) {
		t.Helper()
		app.fetchSession(t.Context())
		app.mu.Lock()
		session := app.session
		waiting := app.hostAwaitStatus
		app.mu.Unlock()
		if waiting || session.State != "active" || session.ID != "session-1" || !packageStopIdentity(session) || !app.Snapshot().LocalCorePresentsPaused {
			t.Fatalf("session not safely reattached: %+v waiting=%v", session, waiting)
		}
	}
	available.Store(true)
	app := newLauncher()
	assertAttached(app)
	available.Store(false)
	app.fetchSession(t.Context())
	if app.Snapshot().Session.State != "active" || !app.Snapshot().LocalCorePresentsPaused {
		t.Fatal("host outage discarded the running session or resumed display")
	}
	available.Store(true)
	assertAttached(app)
	app.Stop()
	newApp := newLauncher()
	defer newApp.Stop()
	assertAttached(newApp)
	if stops.Load() != 0 {
		t.Fatal("reattachment stopped the running session")
	}
}

func TestKitHostStartupAcceptsTerminalSessionStates(t *testing.T) {
	for _, tc := range []struct {
		state           string
		waiting, paused bool
	}{
		{"idle", false, false},
		{"active", false, true},
		{"local", false, true},
		{"failed", false, false},
		{"launching", true, true},
		{"stopping", true, true},
	} {
		t.Run(tc.state, func(t *testing.T) {
			host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/session" {
					http.NotFound(w, r)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"state": tc.state})
			}))
			defer host.Close()
			app, err := configuredApp(Options{GFX: "menu-display", KitHost: true, APIBase: host.URL, PrefsPath: filepath.Join(t.TempDir(), "tenfoot.json")})
			if err != nil {
				t.Fatal(err)
			}
			defer app.Stop()
			app.fetchSession(t.Context())
			app.mu.Lock()
			waiting := app.hostAwaitStatus
			app.mu.Unlock()
			if waiting != tc.waiting || app.Snapshot().LocalCorePresentsPaused != tc.paused {
				t.Fatalf("state %s: waiting=%v paused=%v", tc.state, waiting, app.Snapshot().LocalCorePresentsPaused)
			}
		})
	}
}
