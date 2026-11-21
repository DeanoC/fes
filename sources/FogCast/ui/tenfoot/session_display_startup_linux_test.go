//go:build linux

package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
	"github.com/DeanoC/FogCast/ui/rooms"
)

type startupReads struct {
	values []menudisplay.StartupStatus
	err    error
	calls  int
}

func (s *startupReads) StartupStatus(context.Context) (menudisplay.StartupStatus, error) {
	s.calls++
	if s.err != nil {
		return menudisplay.StartupStatus{}, s.err
	}
	i := s.calls - 1
	if i >= len(s.values) {
		i = len(s.values) - 1
	}
	return s.values[i], nil
}
func startupApp(t *testing.T) *App {
	t.Helper()
	app := NewApp(nil, 1280, 720, 20)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{testRoomPack(t, colecoArcadeRoomID, "function load() end\nfunction draw() end")}), "")
	app.SetMenuDisplay(&gfx.MenuDisplay{})
	app.mu.Lock()
	app.localCores = &fakeLocalCores{}
	app.mu.Unlock()
	t.Cleanup(func() {
		app.mu.Lock()
		app.session = hostclient.SessionResult{}
		app.menuDisplay = nil
		app.mu.Unlock()
		app.Stop()
	})
	return app
}
func startupSession() hostclient.SessionResult {
	s := kitDisplaySession()
	s.System = string(protocol.SystemColecoVision)
	s.CorePackage.CoreID = colecoCoreID
	return s
}
func startupObserved(s hostclient.SessionResult, available bool, focus string) menudisplay.StartupStatus {
	return menudisplay.StartupStatus{State: "running_development", PackageID: s.CorePackage.PackageID, Generation: s.CorePackage.Generation, RuntimePID: 42, RuntimeStart: "first-process", Plane: &menudisplay.Status{Session: true, Available: available, Focus: focus, PackageID: s.CorePackage.PackageID, CoreGeneration: s.CorePackage.Generation, Generation: 1}}
}
func TestStartupDisplayAdoptsMatchingRuntimeState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		available bool
		focus     string
		want      protocol.SessionDisplay
	}{
		{"closed", false, "machine", protocol.SessionDisplayClosed}, {"shown", true, "machine", protocol.SessionDisplayShown}, {"focused", true, "launcher", protocol.SessionDisplayFocused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := startupApp(t)
			session := startupSession()
			observed := startupObserved(session, tc.available, tc.focus)
			reader := &startupReads{values: []menudisplay.StartupStatus{observed, observed}}
			err := app.reconcileDisplayStartupWith(context.Background(), reader, func(context.Context) (hostclient.SessionResult, error) { return session, nil })
			if err != nil {
				t.Fatal(err)
			}
			app.mu.Lock()
			defer app.mu.Unlock()
			if app.sessionDisplayBusy != tc.available || app.displayStartupFenced != tc.available || app.sessionDisplay != tc.want || app.session.CorePackage.PackageID != session.CorePackage.PackageID || reader.calls != 2 {
				t.Fatalf("state=%+v busy=%v calls=%d", app.sessionDisplay, app.sessionDisplayBusy, reader.calls)
			}
			if tc.want == protocol.SessionDisplayFocused && !app.roomDuringPlay {
				t.Fatal("focused plane did not open controls")
			}
			if tc.want == protocol.SessionDisplayShown && (app.displaySurroundOverride == nil || !*app.displaySurroundOverride || app.surround) {
				t.Fatal("shown override changed preference or was not retained")
			}
		})
	}
}
func TestStartupDisplayFailsClosedOnUncertainOrReplacedState(t *testing.T) {
	for _, scenario := range []string{"host-error", "package-mismatch", "target-mismatch", "missing-target-id", "missing-plane", "plane-error", "changed-generation", "changed-focus", "changed-runtime-incarnation", "runtime-error", "async-room"} {
		t.Run(scenario, func(t *testing.T) {
			app := startupApp(t)
			session := startupSession()
			first := startupObserved(session, true, "launcher")
			second := startupObserved(session, true, "launcher")
			switch scenario {
			case "package-mismatch":
				session.CorePackage.PackageID = strings.Repeat("b", 64)
			case "target-mismatch":
				app.kitID = "another-kit"
			case "missing-target-id":
				session.TargetID = ""
			case "missing-plane":
				first.Plane = nil
				second.Plane = nil
			case "plane-error":
				first.Plane.Error = &menudisplay.MenuError{Code: "io_failed"}
				second.Plane.Error = &menudisplay.MenuError{Code: "io_failed"}
			case "changed-generation":
				second.Generation++
			case "changed-focus":
				second.Plane.Focus = "machine"
			case "changed-runtime-incarnation":
				second.RuntimeStart = "replacement-process"
			case "async-room":
				app.SetRooms(rooms.NewIndex(nil), "")
				session.System = ""
				session.CorePackage.CoreID = ""
				session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}
			}
			reader := &startupReads{values: []menudisplay.StartupStatus{first, second}}
			if scenario == "runtime-error" {
				reader.err = errors.New("runtime unavailable")
			}
			err := app.reconcileDisplayStartupWith(context.Background(), reader, func(context.Context) (hostclient.SessionResult, error) {
				if scenario == "host-error" {
					return hostclient.SessionResult{}, errors.New("host unavailable")
				}
				return session, nil
			})
			if err == nil {
				t.Fatal("uncertain startup accepted")
			}
			app.mu.Lock()
			busy := app.sessionDisplayBusy
			app.mu.Unlock()
			if !busy {
				t.Fatal("uncertain startup removed input fence")
			}
		})
	}
}
func TestStartupDisplayIdleNeedsNoHostAndRechecks(t *testing.T) {
	app := startupApp(t)
	idle := menudisplay.StartupStatus{State: "idle"}
	reader := &startupReads{values: []menudisplay.StartupStatus{idle, idle}}
	if err := app.reconcileDisplayStartupWith(context.Background(), reader, nil); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 2 || app.sessionDisplayBusy {
		t.Fatal("idle startup not confirmed")
	}
}

type retryStartupObserver struct {
	app         *App
	status      menudisplay.StartupStatus
	calls       int
	alwaysFail  bool
	sawUnfenced bool
}

func (s *retryStartupObserver) StartupStatus(context.Context) (menudisplay.StartupStatus, error) {
	s.app.mu.Lock()
	busy := s.app.sessionDisplayBusy
	s.app.mu.Unlock()
	if !busy {
		s.sawUnfenced = true
	}
	s.calls++
	if s.alwaysFail || s.calls == 1 {
		return menudisplay.StartupStatus{}, errors.New("temporarily unavailable")
	}
	return s.status, nil
}
func TestStartupDisplayRetryKeepsFenceUntilAdoption(t *testing.T) {
	app := startupApp(t)
	session := startupSession()
	observed := startupObserved(session, true, "machine")
	reader := &retryStartupObserver{app: app, status: observed}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := app.reconcileDisplayStartupRetryWith(ctx, reader, func(context.Context) (hostclient.SessionResult, error) { return session, nil }, testStartupTarget); err != nil {
		t.Fatal(err)
	}
	if reader.sawUnfenced || reader.calls != 3 {
		t.Fatalf("unfenced=%v calls=%d", reader.sawUnfenced, reader.calls)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !app.sessionDisplayBusy || !app.displayStartupFenced || app.sessionDisplay != protocol.SessionDisplayShown {
		t.Fatal("successful retry did not adopt and fence shown plane")
	}
}
func TestStartupDisplayRetryDeadlineLeavesFence(t *testing.T) {
	app := startupApp(t)
	reader := &retryStartupObserver{app: app, alwaysFail: true}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := app.reconcileDisplayStartupRetryWith(ctx, reader, nil, testStartupTarget)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retry error %v", err)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !app.sessionDisplayBusy || reader.sawUnfenced {
		t.Fatal("failed retry lost startup fence")
	}
}

type timeoutStartupObserver struct {
	app         *App
	status      menudisplay.StartupStatus
	calls       int
	sawUnfenced bool
}

func (s *timeoutStartupObserver) StartupStatus(ctx context.Context) (menudisplay.StartupStatus, error) {
	s.app.mu.Lock()
	busy := s.app.sessionDisplayBusy
	s.app.mu.Unlock()
	if !busy {
		s.sawUnfenced = true
	}
	s.calls++
	if s.calls == 1 {
		<-ctx.Done()
		return menudisplay.StartupStatus{}, ctx.Err()
	}
	return s.status, nil
}
func TestStartupDisplayRetryBoundsEachAttempt(t *testing.T) {
	app := startupApp(t)
	reader := &timeoutStartupObserver{app: app, status: menudisplay.StartupStatus{State: "idle"}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := app.reconcileDisplayStartupRetryWithTimeout(ctx, reader, nil, testStartupTarget, 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 3 || reader.sawUnfenced {
		t.Fatalf("calls=%d unfenced=%v", reader.calls, reader.sawUnfenced)
	}
}
func TestStartupDisplayRetryClearsPartialAdoptionWhenRuntimeBecomesIdle(t *testing.T) {
	app := startupApp(t)
	app.SetRooms(rooms.NewIndex(nil), "")
	session := startupSession()
	session.System = ""
	session.CorePackage.CoreID = ""
	session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}
	active := startupObserved(session, true, "launcher")
	idle := menudisplay.StartupStatus{State: "idle"}
	reader := &startupReads{values: []menudisplay.StartupStatus{active, active, idle, idle}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := app.reconcileDisplayStartupRetryWith(ctx, reader, func(context.Context) (hostclient.SessionResult, error) { return session, nil }, testStartupTarget); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.session.State != "idle" || app.sessionDisplay != protocol.SessionDisplayClosed || app.sessionDisplayBusy || app.displaySurroundOverride != nil || app.roomDuringPlay {
		t.Fatalf("stale partial adoption: session=%q display=%+v busy=%v", app.session.State, app.sessionDisplay, app.sessionDisplayBusy)
	}
}

func (a *App) reconcileDisplayStartupWith(ctx context.Context, observer startupDisplayObserver, read startupSessionReader) error {
	if a == nil || observer == nil || ctx == nil {
		return errors.New("startup display observation is unavailable")
	}
	if err := a.beginStartupDisplayFence(); err != nil {
		return err
	}
	return a.observeStartupDisplayFenced(ctx, observer, read, testStartupTarget)
}

func testStartupTarget(context.Context) (startupTargetIdentity, error) {
	return startupTargetIdentity{ID: "kit-id", PeerPID: 10, PeerStart: "agent-start"}, nil
}

func TestStartupDisplayRejectsOtherOrChangingLocalTarget(t *testing.T) {
	for _, scenario := range []string{"missing", "missing-peer", "other-kit-same-package", "changed-id", "changed-agent", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			app := startupApp(t)
			session := startupSession()
			observed := startupObserved(session, true, "machine")
			reader := &startupReads{values: []menudisplay.StartupStatus{observed, observed}}
			calls := 0
			target := func(context.Context) (startupTargetIdentity, error) {
				calls++
				switch scenario {
				case "missing":
					return startupTargetIdentity{}, nil
				case "missing-peer":
					return startupTargetIdentity{ID: "kit-id"}, nil
				case "other-kit-same-package":
					return startupTargetIdentity{ID: "another-kit", PeerPID: 10, PeerStart: "agent-start"}, nil
				case "changed-id":
					if calls > 1 {
						return startupTargetIdentity{ID: "another-kit", PeerPID: 10, PeerStart: "agent-start"}, nil
					}
				case "changed-agent":
					if calls > 1 {
						return startupTargetIdentity{ID: "kit-id", PeerPID: 10, PeerStart: "new-agent"}, nil
					}
				case "unavailable":
					return startupTargetIdentity{}, errors.New("agent unavailable")
				}
				return startupTargetIdentity{ID: "kit-id", PeerPID: 10, PeerStart: "agent-start"}, nil
			}
			if err := app.beginStartupDisplayFence(); err != nil {
				t.Fatal(err)
			}
			err := app.observeStartupDisplayFenced(context.Background(), reader, func(context.Context) (hostclient.SessionResult, error) { return session, nil }, target)
			if err == nil {
				t.Fatal("unverified target accepted")
			}
			app.mu.Lock()
			defer app.mu.Unlock()
			if !app.sessionDisplayBusy {
				t.Fatal("identity failure removed fence")
			}
		})
	}
}

func TestStartupFrameVerifierRejectsChangedIncarnationAndIdentity(t *testing.T) {
	for _, scenario := range []string{"unchanged", "runtime-restarted-same-package", "different-target", "host-session-replaced"} {
		t.Run(scenario, func(t *testing.T) {
			app := startupApp(t)
			session := startupSession()
			before := startupObserved(session, true, "machine")
			after := startupObserved(session, true, "machine")
			if scenario == "runtime-restarted-same-package" {
				after.RuntimeStart = "new-process"
			}
			reader := &startupReads{values: []menudisplay.StartupStatus{before, before, after, after}}
			targetCalls := 0
			target := func(context.Context) (startupTargetIdentity, error) {
				targetCalls++
				if scenario == "different-target" && targetCalls > 2 {
					return startupTargetIdentity{ID: "other-kit", PeerPID: 10, PeerStart: "agent-start"}, nil
				}
				return startupTargetIdentity{ID: "kit-id", PeerPID: 10, PeerStart: "agent-start"}, nil
			}
			hostCalls := 0
			host := func(context.Context) (hostclient.SessionResult, error) {
				hostCalls++
				if scenario == "host-session-replaced" && hostCalls > 1 {
					replaced := session
					replaced.ID = "another-session"
					return replaced, nil
				}
				return session, nil
			}
			if err := app.beginStartupDisplayFence(); err != nil {
				t.Fatal(err)
			}
			if err := app.observeStartupDisplayFenced(context.Background(), reader, host, target); err != nil {
				t.Fatal(err)
			}
			app.mu.Lock()
			verify := app.startupFrameVerify
			app.mu.Unlock()
			if verify == nil {
				t.Fatal("visible startup has no verifier")
			}
			err := verify(context.Background())
			if (err != nil) != (scenario != "unchanged") {
				t.Fatalf("verify = %v", err)
			}
			app.mu.Lock()
			defer app.mu.Unlock()
			if !app.sessionDisplayBusy || !app.displayStartupFenced {
				t.Fatal("verifier prematurely released input fence")
			}
		})
	}
}

func TestStartupFocusedNonColecoWaitsForDelayedPackage(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/core-packages/") {
			http.NotFound(w, r)
			return
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = fmt.Fprintf(w, `{"package_id":"%s","descriptor":{"core":{"id":"fes.zx81"}}}`, strings.Repeat("a", 64))
	}))
	t.Cleanup(server.Close)
	app, _ := kitDisplayApp(t, server)
	app.SetMenuDisplay(&gfx.MenuDisplay{})
	session := kitDisplaySession()
	session.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: "fes.keyboard", Major: 1}, {ID: "fes.memory.hps-ddr", Major: 1}, {ID: "fes.video.session-display", Major: 1}}
	observed := startupObserved(session, true, "launcher")
	reader := &startupReads{values: []menudisplay.StartupStatus{observed, observed}}
	app.mu.Lock()
	app.session = hostclient.SessionResult{}
	app.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- app.reconcileDisplayStartupRetryWith(ctx, reader, func(context.Context) (hostclient.SessionResult, error) { return session, nil }, testStartupTarget)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("lookup did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("adopted before metadata: %v", err)
	default:
	}
	app.mu.Lock()
	busy := app.sessionDisplayBusy
	app.mu.Unlock()
	if !busy {
		t.Fatal("startup fence was dropped during lookup")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	opened, fenced := app.roomDuringPlay, app.displayStartupFenced
	app.session = hostclient.SessionResult{}
	app.sessionDisplayBusy = false
	app.displayStartupFenced = false
	app.menuDisplay = nil
	app.mu.Unlock()
	if !opened || !fenced {
		t.Fatalf("opened=%v fenced=%v", opened, fenced)
	}
}
