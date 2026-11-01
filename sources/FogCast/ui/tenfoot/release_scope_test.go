package tenfoot

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

func TestCoreLibraryListsOnlyReleaseCoresUnlessAllShown(t *testing.T) {
	app := NewApp(nil, 1280, 720, 10)
	t.Cleanup(app.Stop)
	app.coreLibrary = coreLibraryState{Open: true, Online: true, Cores: []hostclient.AvailableCore{
		{CoreReference: hostclient.CoreReference{CoreID: "fes.c64"}, Label: "C64"},
		{CoreReference: hostclient.CoreReference{CoreID: "fes.coleco"}, Label: "ColecoVision"},
		{CoreReference: hostclient.CoreReference{CoreID: "fes.pc386"}, Label: "PC 386"},
	}}
	listed := func() []string {
		app.mu.Lock()
		defer app.mu.Unlock()
		var ids []string
		for _, row := range app.coreLibraryRowsLocked() {
			if row.Core != nil {
				ids = append(ids, row.Core.CoreID)
			}
		}
		return ids
	}
	if got := listed(); len(got) != 1 || got[0] != "fes.c64" {
		t.Fatalf("v1 catalog = %v", got)
	}
	app.SetAllCores(true)
	if got := listed(); len(got) != 3 {
		t.Fatalf("all-cores catalog = %v", got)
	}
}

func TestHomeRecentsHideGamesForCoresOutsideTheRelease(t *testing.T) {
	app := NewApp(nil, 1280, 720, 10)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.homeRecentsLoaded = true
	app.homeRecents = []hostclient.Game{
		{ID: "zx", Title: "3D Monster Maze", CoreID: "fes.zx81"},
		{ID: "pong", Title: "Pong", CoreID: "fes.pong"},
		{ID: "rom", Title: "Scanned ROM"},
	}
	ids := func() []string {
		var out []string
		for _, game := range app.shownRecentsLocked() {
			out = append(out, game.ID)
		}
		return out
	}
	if got := ids(); len(got) != 2 || got[0] != "zx" || got[1] != "rom" {
		t.Fatalf("v1 recents = %v", got)
	}
	if _, ok := app.homeRecentByIDLocked("pong"); ok {
		t.Fatal("a hidden recent can still be chosen")
	}
	app.mu.Unlock()
	app.SetAllCores(true)
	app.mu.Lock()
	defer app.mu.Unlock()
	if got := ids(); len(got) != 3 {
		t.Fatalf("all-cores recents = %v", got)
	}
}

func TestAllCoresSettingTogglesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app := NewApp(nil, 1280, 720, 20)
	t.Cleanup(app.Stop)
	app.SetPrefsPath(path)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "all-cores", now)
	if row := app.Snapshot().Settings.Rows[settingsRowAllCores]; row.Value != "Off" {
		t.Fatalf("row = %#v", row)
	}
	app.HandleCommand(CmdSelect, now)
	app.mu.Lock()
	row := app.settingsRowsLocked()[settingsRowAllCores]
	app.mu.Unlock()
	if row.Value != "On" {
		t.Fatalf("toggled row = %#v", row)
	}
	got, err := loadTenfootPrefs(path)
	if err != nil || !got.AllCores {
		t.Fatalf("prefs = %#v err=%v", got, err)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !app.allCores {
		t.Fatal("toggle did not show all cores")
	}
}

func TestAllCoresChangeFromSettingsRebuildsHome(t *testing.T) {
	cores := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: tenfootPongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
	}}
	app := startHomeLounge(t, cores, 1280, 720)
	waitFor(t, app, "home room", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == installedCoresHomeID })
	app.mu.Lock()
	before := app.room
	app.mu.Unlock()
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "all-cores", now)
	app.HandleCommand(CmdSelect, now)
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.settingsOpen || app.room == nil || app.room == before || !app.room.ReleaseOnly() || app.room.ID() != installedCoresHomeID {
		t.Fatalf("settings open %v, room %p (was %p), release only %v", app.settingsOpen, app.room, before, app.room != nil && app.room.ReleaseOnly())
	}
}
