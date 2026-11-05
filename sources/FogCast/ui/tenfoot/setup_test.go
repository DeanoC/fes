package tenfoot

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// startFirstRun starts Home with the first-run gate on and prefs in dir.
func startFirstRun(t *testing.T, dir string, w, h int) *App {
	t.Helper()
	host := newRoomHost(t)
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
