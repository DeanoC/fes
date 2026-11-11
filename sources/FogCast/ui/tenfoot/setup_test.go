package tenfoot

import (
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// startFirstRun starts Home with the first-run gate on and prefs in dir.
func startFirstRun(t *testing.T, dir string, w, h int) *App {
	t.Helper()
	return startFirstRunWithHost(t, newRoomHost(t), dir, w, h)
}

func startFirstRunWithHost(t *testing.T, host *roomHost, dir string, w, h int) *App {
	t.Helper()
	app := NewApp(NewClient(host.server.URL, host.server.Client()), w, h, 50)
	app.SetPrefsPath(filepath.Join(dir, "tenfoot.json"))
	app.SetRooms(rooms.NewIndex(rooms.Examples()), t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom(installedCoresHomeID)
	app.SetKitLocal(&fakeLocalCores{cores: homeFixtureCores()}, &fakePadFeed{})
	app.SetFirstRun(true)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app
}

func waitRoom(t *testing.T, app *App, id string) Snapshot {
	t.Helper()
	return waitFor(t, app, id, func(s Snapshot) bool { return s.Room.Open && s.Room.ID == id && s.Room.Err == "" })
}

func setupPrefs(t *testing.T, dir string) tenfootPrefs {
	t.Helper()
	prefs, err := loadTenfootPrefs(filepath.Join(dir, "tenfoot.json"))
	if err != nil {
		t.Fatal(err)
	}
	return prefs
}

func TestFirstRunGoesWelcomeReadyThenHome(t *testing.T) {
	dir := t.TempDir()
	app := startFirstRun(t, dir, 1280, 720)
	snap := waitRoom(t, app, "example.welcome")
	if d := snap.Room.Destination; d.Kind != rooms.KindAction || d.LauncherAction != "setup_continue" {
		t.Fatalf("Welcome opens on %+v", d)
	}
	if _, ok := roomDestGeom(snap); ok {
		t.Fatal("a setup room drew the FES destination strip")
	}
	if !roomTextHas(snap, "Welcome to FES") || !roomTextHas(snap, "Step 1 of 2") {
		t.Fatal("Welcome copy or step tracker missing")
	}
	// Welcome is the first step: Back stays.
	app.HandleCommand(CmdBack, time.Now())
	waitRoom(t, app, "example.welcome")
	app.HandleCommand(CmdSelect, time.Now())
	snap = waitRoom(t, app, "example.setup-ready")
	if !roomTextHas(snap, "Step 2 of 2") {
		t.Fatal("Ready has no step tracker")
	}
	waitFor(t, app, "Ready rows", func(s Snapshot) bool {
		return roomTextHas(s, "You're ready to play") && roomTextHas(s, "Sinclair ZX81")
	})
	app.HandleCommand(CmdBack, time.Now())
	waitRoom(t, app, "example.welcome")
	app.HandleCommand(CmdSelect, time.Now())
	waitRoom(t, app, "example.setup-ready")
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "Home", homeFamilySettled)
	if p := setupPrefs(t, dir); !p.SetupDone || len(p.SetupSkipped) != 0 {
		t.Fatalf("Finish wrote %+v", p)
	}
	if roomTextHas(app.Snapshot(), "Setup isn't finished") {
		t.Fatal("finished setup still shows the banner")
	}
}

func TestFirstRunSkipShowsTheBannerAndFinishSetupResumes(t *testing.T) {
	dir := t.TempDir()
	app := startFirstRun(t, dir, 1280, 720)
	waitRoom(t, app, "example.welcome")
	app.HandleCommand(CmdRight, time.Now())
	snap := waitFor(t, app, "Skip copy", func(s Snapshot) bool { return roomTextHas(s, "Skip goes straight to Home.") })
	if d := snap.Room.Destination; d.LauncherAction != "setup_skip" {
		t.Fatalf("Skip setup focus %+v", d)
	}
	app.HandleCommand(CmdSelect, time.Now())
	snap = waitFor(t, app, "Home with the banner", func(s Snapshot) bool { return homeFamilySettled(s) && roomTextHas(s, "Setup isn't finished") })
	if p := setupPrefs(t, dir); !p.SetupDone || !slices.Equal(p.SetupSkipped, []string{"ready"}) {
		t.Fatalf("Skip wrote %+v", p)
	}
	// Up from the tiles reaches the banner; its Finish setup resumes at the
	// first skipped step.
	app.HandleCommand(CmdUp, time.Now())
	snap = app.Snapshot()
	if d := snap.Room.Destination; d.LauncherAction != "setup_resume" || heroDestinationCopy(snap).Primary != "Finish setup" {
		t.Fatalf("banner %+v", d)
	}
	app.HandleCommand(CmdSelect, time.Now())
	waitRoom(t, app, "example.setup-ready")
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "Home without the banner", func(s Snapshot) bool { return homeFamilySettled(s) && !roomTextHas(s, "Setup isn't finished") })
	if p := setupPrefs(t, dir); !p.SetupDone || len(p.SetupSkipped) != 0 {
		t.Fatalf("Finish after resume wrote %+v", p)
	}
}

func TestFirstRunMarkerOpensHomeAndRunSetupAgainStartsOver(t *testing.T) {
	dir := t.TempDir()
	if err := saveTenfootPrefs(filepath.Join(dir, "tenfoot.json"), tenfootPrefs{SafeAreaPct: DefaultSafeAreaPct, SetupDone: true}); err != nil {
		t.Fatal(err)
	}
	app := startFirstRun(t, dir, 1280, 720)
	waitFor(t, app, "Home", homeFamilySettled)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "setup-again", now)
	app.HandleCommand(CmdSelect, now)
	snap := waitRoom(t, app, "example.welcome")
	if snap.Settings.Open {
		t.Fatal("Run setup again left Settings open")
	}
	if p := setupPrefs(t, dir); !p.SetupDone {
		t.Fatal("Run setup again cleared the marker")
	}
}

func TestKitRoomsHomeEnablesTheFirstRunGate(t *testing.T) {
	for _, tc := range []struct {
		gfx, home string
		want      bool
	}{
		{"menu-display", "rooms", true},
		{"menu-display", "library", false},
		{"sdl", "rooms", false},
	} {
		opts := Options{GFX: tc.gfx, APIBase: "http://127.0.0.1:9", Home: tc.home, HomeSet: true, PrefsPath: filepath.Join(t.TempDir(), "tenfoot.json")}.normalized()
		app, err := configuredApp(opts)
		if err != nil {
			t.Fatal(err)
		}
		if app.setup.firstRun != tc.want {
			t.Fatalf("%s %s: first run %v", tc.gfx, tc.home, app.setup.firstRun)
		}
	}
}

func TestFirstRunGateIsOffUnlessEnabled(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "Home", homeFamilySettled)
}

func TestSetupWaitsForRunningOrStartingSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		busy func(*App)
	}{
		{"active host session", func(a *App) { a.session.State = "active" }},
		{"starting host session", func(a *App) { a.session.State = "launching" }},
		{"starting request", func(a *App) { a.launch.Phase, a.launch.Message = "launching", "Starting the machine" }},
		{"development load", func(a *App) { a.devLoadPhase = "loading" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			app := startFirstRun(t, dir, 1280, 720)
			waitRoom(t, app, "example.welcome")
			waitFor(t, app, "library loaded", func(s Snapshot) bool { return !s.Loading })
			app.mu.Lock()
			// The fake host keeps reporting idle; stop its poll and invalidate
			// an in-flight response before injecting the busy state.
			app.cancel()
			app.sessionGen++
			tc.busy(app)
			app.setup.skipped = []string{"welcome", "ready"}
			app.runSetupActionLocked("setup_continue")
			continueState, continueStatus := app.setup, app.status
			continueState.skipped = slices.Clone(app.setup.skipped)
			app.runSetupActionLocked("setup_skip")
			skipState := app.setup
			skipState.skipped = slices.Clone(app.setup.skipped)
			opened := app.openSetupStepLocked(1)
			app.runSetupAgainLocked()
			settingsStatus := app.settingsStatus
			app.mu.Unlock()
			if continueState.step != 0 || continueState.done || !slices.Equal(continueState.skipped, []string{"welcome", "ready"}) || continueStatus != setupBlockedStatus {
				t.Fatalf("blocked continue changed setup: %+v, status %q", continueState, continueStatus)
			}
			if skipState.step != 0 || skipState.done || !slices.Equal(skipState.skipped, []string{"welcome", "ready"}) {
				t.Fatalf("blocked skip changed setup: %+v", skipState)
			}
			if opened || settingsStatus != setupBlockedStatus {
				t.Fatalf("blocked open = %v, settings status = %q", opened, settingsStatus)
			}
			snap := app.Snapshot()
			if got := renderRoomLabelSource(t, snap, "nopanel-status"); got != setupBlockedStatus {
				app.mu.Lock()
				status, step, id, state, phase := app.status, app.setup.step, app.room.ID(), app.session.State, app.launch.Phase
				app.mu.Unlock()
				t.Fatalf("setup footer = %q, want %q; snapshot status=%q session=%q launch=%q room=%q; app status=%q step=%d room=%q session=%q launch=%q", got, setupBlockedStatus, snap.Status, snap.Session.State, snap.Launch.Phase, snap.Room.ID, status, step, id, state, phase)
			}
		})
	}
}

func TestSetupBlockedStatusSurvivesLibraryResponse(t *testing.T) {
	for _, tc := range []struct {
		name       string
		httpStatus int
		want       string
	}{
		{"success", 0, "2 titles · All · All · Title"},
		{"failure", http.StatusInternalServerError, "Can't reach the library right now."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := newRoomHost(t)
			gate := make(chan struct{})
			arrived := make(chan struct{}, 1)
			host.mu.Lock()
			host.state, host.gamesGate, host.gamesArrived, host.gamesStatus = "launching", gate, arrived, tc.httpStatus
			host.mu.Unlock()
			t.Cleanup(func() { close(gate) })
			app := startFirstRunWithHost(t, host, t.TempDir(), 1280, 720)
			waitRoom(t, app, "example.welcome")
			select {
			case <-arrived:
			case <-time.After(5 * time.Second):
				t.Fatal("library request did not arrive")
			}
			waitFor(t, app, "busy host session", func(s Snapshot) bool { return s.Session.State == "launching" })
			app.mu.Lock()
			app.runSetupActionLocked("setup_continue")
			app.mu.Unlock()
			if snap := app.Snapshot(); snap.Status != setupBlockedStatus {
				t.Fatalf("blocked status = %q", snap.Status)
			}
			// Release the real in-flight response after the warning is set.
			close(gate)
			gate = make(chan struct{}) // cleanup closes this only on success
			snap := waitFor(t, app, "library response", func(s Snapshot) bool { return !s.Loading })
			if got := renderRoomLabelSource(t, snap, "nopanel-status"); got != setupBlockedStatus {
				t.Fatalf("footer after library response = %q, want %q", got, setupBlockedStatus)
			}
			host.mu.Lock()
			host.state = "idle"
			host.mu.Unlock()
			select {
			case app.sessionKick <- struct{}{}:
			default:
			}
			snap = waitFor(t, app, "idle host session", func(s Snapshot) bool { return s.Session.State == "idle" })
			if snap.Status != tc.want {
				t.Fatalf("status after idle = %q, want %q", snap.Status, tc.want)
			}
			if got := renderRoomLabelSource(t, snap, "nopanel-status"); got == setupBlockedStatus {
				t.Fatalf("footer remains blocked after idle: %q", got)
			}
			if snap.Room.ID != "example.welcome" {
				t.Fatalf("blocked setup advanced to %q", snap.Room.ID)
			}
		})
	}
}

func TestSetupDeferredOfflineShelfRetainsRefreshFailureNotice(t *testing.T) {
	host := newRoomHost(t)
	host.mu.Lock()
	host.state = "launching"
	host.mu.Unlock()
	app := startFirstRunWithHost(t, host, t.TempDir(), 1280, 720)
	waitRoom(t, app, "example.welcome")
	waitFor(t, app, "initial library and busy session", func(s Snapshot) bool {
		return !s.Loading && len(s.Games) > 0 && s.Session.State == "launching"
	})
	host.mu.Lock()
	host.gamesStatus = http.StatusInternalServerError
	host.mu.Unlock()
	const want = "Saved. Couldn't refresh the library."
	app.mu.Lock()
	app.runSetupActionLocked("setup_continue")
	app.reloadCatalogWithNoticeLocked("Saved.")
	app.mu.Unlock()
	waitFor(t, app, "deferred offline refresh notice", func(s Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return !s.Loading && app.setup.deferredLibraryStatus == want
	})
	host.mu.Lock()
	host.state = "idle"
	host.mu.Unlock()
	select {
	case app.sessionKick <- struct{}{}:
	default:
	}
	snap := waitFor(t, app, "idle session", func(s Snapshot) bool { return s.Session.State == "idle" })
	if snap.Status != want {
		t.Fatalf("status after idle = %q, want %q", snap.Status, want)
	}
}

func TestSetupOfflineNoticeAfterIdleBeforeSnapshot(t *testing.T) {
	app := startFirstRun(t, t.TempDir(), 1280, 720)
	waitRoom(t, app, "example.welcome")
	waitFor(t, app, "library loaded", func(s Snapshot) bool { return !s.Loading })
	app.mu.Lock()
	app.cancel()
	app.sessionGen++
	app.session.State = "launching"
	app.runSetupActionLocked("setup_continue")
	app.setLibraryStatusLocked(savedListOfflineCopy)
	app.session.State = "idle"
	// The refresh goroutine's notice arrives before any idle Snapshot.
	app.setLibraryRefreshNoticeLocked("Saved.")
	app.mu.Unlock()
	const want = "Saved. Couldn't refresh the library."
	if got := app.Snapshot().Status; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestSkippingLaterStepRecordsCurrentStep(t *testing.T) {
	dir := t.TempDir()
	app := startFirstRun(t, dir, 1280, 720)
	waitRoom(t, app, "example.welcome")
	app.HandleCommand(CmdSelect, time.Now())
	waitRoom(t, app, "example.setup-ready")
	app.mu.Lock()
	app.runSetupActionLocked("setup_skip")
	app.mu.Unlock()
	snap := waitFor(t, app, "Home with unfinished setup", func(s Snapshot) bool {
		return homeFamilySettled(s) && roomTextHas(s, "Setup isn't finished")
	})
	if p := setupPrefs(t, dir); !p.SetupDone || !slices.Equal(p.SetupSkipped, []string{"ready"}) {
		t.Fatalf("Skip on Ready wrote %+v", p)
	}
	app.HandleCommand(CmdUp, time.Now())
	if d := app.Snapshot().Room.Destination; d.LauncherAction != "setup_resume" {
		t.Fatalf("unfinished banner destination after %+v: %+v", snap.Room.ID, d)
	}
	app.HandleCommand(CmdSelect, time.Now())
	waitRoom(t, app, "example.setup-ready")
}

func TestSetupBlockedStatusClearsWhenSessionStops(t *testing.T) {
	app := startFirstRun(t, t.TempDir(), 1280, 720)
	waitRoom(t, app, "example.welcome")
	app.mu.Lock()
	app.session.State = "active"
	app.runSetupActionLocked("setup_continue")
	app.session.State = "idle"
	app.mu.Unlock()
	if got := renderRoomLabelSource(t, app.Snapshot(), "nopanel-status"); got == setupBlockedStatus {
		t.Fatal("setup still says to stop the machine after it stopped")
	}
}

func TestFirstRunMaximumSafeAreaRender(t *testing.T) {
	dir := t.TempDir()
	app := startFirstRun(t, dir, 1280, 720)
	waitRoom(t, app, "example.welcome")
	app.SetSafeAreaPct(maxSafeAreaPct)
	snap := waitFor(t, app, "768x432 Welcome", func(s Snapshot) bool {
		return s.Room.Width == 768 && s.Room.Height == 432 && roomTextHas(s, "Welcome to FES")
	})
	assertSetupTextClearOfButtons(t, snap)
	writeHomeScreenshot(t, snap, "welcome-max-safe")
	app.HandleCommand(CmdRight, time.Now())
	snap = waitFor(t, app, "Skip copy", func(s Snapshot) bool { return roomTextHas(s, "Skip goes straight to Home.") })
	assertSetupTextClearOfButtons(t, snap)
	writeHomeScreenshot(t, snap, "welcome-skip-max-safe")
	app.mu.Lock()
	previousSession, previousStatus := app.session.State, app.status
	app.session.State = "active"
	app.runSetupActionLocked("setup_skip")
	app.mu.Unlock()
	snap = app.Snapshot()
	if got := renderRoomLabelSource(t, snap, "nopanel-status"); got != setupBlockedStatus {
		t.Fatalf("small setup footer = %q, want %q", got, setupBlockedStatus)
	}
	writeHomeScreenshot(t, snap, "welcome-blocked-max-safe")
	app.mu.Lock()
	app.session.State, app.status = previousSession, previousStatus
	app.mu.Unlock()
	app.HandleCommand(CmdLeft, time.Now())
	app.HandleCommand(CmdSelect, time.Now())
	snap = waitFor(t, app, "768x432 Ready", func(s Snapshot) bool {
		return s.Room.ID == "example.setup-ready" && s.Room.Width == 768 && s.Room.Height == 432 && roomTextHas(s, "Atari ST") && !roomTextHas(s, "Checking…")
	})
	assertSetupTextClearOfButtons(t, snap)
	// Every detail must finish before the next family row starts.
	var details []rooms.Op
	var names []rooms.Op
	for _, op := range snap.Room.Frame.Ops {
		if op.Kind != rooms.OpText {
			continue
		}
		switch op.Text {
		case "Sinclair ZX81", "ZX Spectrum", "Atari ST", "Commodore 64":
			names = append(names, op)
		case "No games yet", "No games yet  ·  needs your firmware files", "Not on this kit":
			details = append(details, op)
		}
	}
	if len(names) != 4 || len(details) != 4 {
		t.Fatalf("Ready rows: %d names, %d details", len(names), len(details))
	}
	for i := 0; i < len(names)-1; i++ {
		if details[i].Y+float32(gfx.TextHeight(details[i].Size)) > names[i+1].Y {
			t.Errorf("detail %q overlaps next row %q", details[i].Text, names[i+1].Text)
		}
	}
	writeHomeScreenshot(t, snap, "ready-max-safe")
}

func TestFirstRunIntermediateSafeAreaRender(t *testing.T) {
	for _, tc := range []struct {
		name string
		pct  float64
		h    int
	}{
		{"intermediate", 0.15, 504},
		{"below compact boundary", 0.085, 598},
		{"at compact boundary", 0.083, 600},
		{"above compact boundary", 0.082, 602},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := startFirstRun(t, t.TempDir(), 1280, 720)
			waitRoom(t, app, "example.welcome")
			app.SetSafeAreaPct(tc.pct)
			waitFor(t, app, "resized Welcome", func(s Snapshot) bool { return s.Room.Height == tc.h })
			app.HandleCommand(CmdRight, time.Now())
			snap := waitFor(t, app, "Skip copy", func(s Snapshot) bool { return roomTextHas(s, "Skip goes straight to Home.") })
			assertSetupTextClearOfButtons(t, snap)
			writeHomeScreenshot(t, snap, "welcome-skip-"+tc.name)
			app.HandleCommand(CmdLeft, time.Now())
			app.HandleCommand(CmdSelect, time.Now())
			snap = waitFor(t, app, "Ready", func(s Snapshot) bool {
				return s.Room.ID == "example.setup-ready" && s.Room.Height == tc.h && roomTextHas(s, "Atari ST") && !roomTextHas(s, "Checking…")
			})
			assertSetupTextClearOfButtons(t, snap)
		})
	}
}

func assertSetupTextClearOfButtons(t *testing.T, snap Snapshot) {
	t.Helper()
	if len(snap.Room.Frame.Hits) < 2 {
		t.Fatal("setup buttons have no hit regions")
	}
	for _, op := range snap.Room.Frame.Ops {
		if op.Kind != rooms.OpText {
			continue
		}
		switch op.Text {
		case "A  Continue", "Skip setup", "A  Finish", "B  Back":
			continue
		}
		width := gfx.MeasureText(op.Text, op.Size)
		if op.MaxW > 0 && width > op.MaxW {
			width = op.MaxW
		}
		x := op.X
		if op.Align == "right" && op.MaxW > 0 {
			x += float32(op.MaxW - width)
		}
		for _, hit := range snap.Room.Frame.Hits {
			if x < hit.X+hit.W && x+float32(width) > hit.X && op.Y < hit.Y+hit.H && op.Y+float32(gfx.TextHeight(op.Size)) > hit.Y {
				t.Errorf("text %q overlaps button %q", op.Text, hit.ID)
			}
		}
	}
}

func TestFirstRunNativeRender(t *testing.T) {
	for _, size := range []struct{ w, h int }{{1280, 720}, {1152, 648}, {1024, 600}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			dir := t.TempDir()
			app := startFirstRun(t, dir, size.w, size.h)
			writeHomeScreenshot(t, waitRoom(t, app, "example.welcome"), "welcome")
			app.HandleCommand(CmdRight, time.Now())
			writeHomeScreenshot(t, waitFor(t, app, "Skip copy", func(s Snapshot) bool { return roomTextHas(s, "Skip goes straight to Home.") }), "welcome-skip")
			app.HandleCommand(CmdLeft, time.Now())
			app.HandleCommand(CmdSelect, time.Now())
			waitRoom(t, app, "example.setup-ready")
			snap := waitFor(t, app, "Ready rows", func(s Snapshot) bool { return roomTextHas(s, "Atari ST") && !roomTextHas(s, "Checking…") })
			writeHomeScreenshot(t, snap, "ready")
			app.HandleCommand(CmdBack, time.Now())
			waitRoom(t, app, "example.welcome")
			app.HandleCommand(CmdRight, time.Now())
			app.HandleCommand(CmdSelect, time.Now())
			snap = waitFor(t, app, "Home with the banner", func(s Snapshot) bool { return homeFamilySettled(s) && roomTextHas(s, "Setup isn't finished") })
			writeHomeScreenshot(t, snap, "setup-skipped")
		})
	}
}

func TestFirstRunHomeCommandStaysInSetup(t *testing.T) {
	dir := t.TempDir()
	app := startFirstRun(t, dir, 1280, 720)
	waitRoom(t, app, "example.welcome")
	app.HandleCommand(CmdSelect, time.Now())
	waitRoom(t, app, "example.setup-ready")
	// The Home command (GUIDE, h, long-press B) keeps setup open on its
	// current step until it is finished or skipped.
	app.HandleCommand(CmdHome, time.Now())
	waitRoom(t, app, "example.setup-ready")
	if p, err := loadTenfootPrefs(filepath.Join(dir, "tenfoot.json")); err == nil && p.SetupDone {
		t.Fatal("leaving for Home wrote the marker")
	}
}

func TestSkippingRunSetupAgainKeepsTheFinishedSetup(t *testing.T) {
	dir := t.TempDir()
	if err := saveTenfootPrefs(filepath.Join(dir, "tenfoot.json"), tenfootPrefs{SafeAreaPct: DefaultSafeAreaPct, SetupDone: true}); err != nil {
		t.Fatal(err)
	}
	app := startFirstRun(t, dir, 1280, 720)
	waitFor(t, app, "Home", homeFamilySettled)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "setup-again", now)
	app.HandleCommand(CmdSelect, now)
	waitRoom(t, app, "example.welcome")
	app.HandleCommand(CmdRight, time.Now())
	app.HandleCommand(CmdSelect, time.Now())
	snap := waitFor(t, app, "Home", homeFamilySettled)
	if roomTextHas(snap, "Setup isn't finished") {
		t.Fatal("skipping Run setup again brought the banner back")
	}
	if p := setupPrefs(t, dir); !p.SetupDone || len(p.SetupSkipped) != 0 {
		t.Fatalf("prefs %+v", p)
	}
}

func TestLibraryStatusOutsideSetupRemainsImmediate(t *testing.T) {
	app := &App{status: "previous"}
	app.setLibraryStatusLocked("library updated")
	if app.status != "library updated" || app.setup.deferredLibraryStatus != "" {
		t.Fatalf("status = %q, deferred = %q", app.status, app.setup.deferredLibraryStatus)
	}
}
