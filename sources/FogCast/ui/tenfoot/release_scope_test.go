package tenfoot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
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

func TestHomeRecentsPaginatePastHeldBackCores(t *testing.T) {
	var cursorMu sync.Mutex
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		cursorMu.Lock()
		cursors = append(cursors, cursor)
		cursorMu.Unlock()
		games := make([]hostclient.Game, homeRecentLimit)
		coreID := "fes.pong"
		next := "v1-recents"
		if cursor != "" {
			coreID = "fes.zx81"
			next = ""
		}
		for i := range games {
			games[i] = hostclient.Game{ID: coreID + "-" + strconv.Itoa(i), Title: "Recent", CoreID: coreID}
		}
		_ = json.NewEncoder(w).Encode(hostclient.GamePage{Games: games, NextCursor: next})
	}))
	defer server.Close()

	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.homeRecentsGen = 1
	app.mu.Unlock()
	app.loadHomeRecents(context.Background(), 1)

	cursorMu.Lock()
	gotCursors := append([]string(nil), cursors...)
	cursorMu.Unlock()
	if len(gotCursors) != 2 || gotCursors[0] != "" || gotCursors[1] != "v1-recents" {
		t.Fatalf("recent cursors = %v", gotCursors)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.homeRecents) != homeRecentLimit {
		t.Fatalf("eligible recent count = %d, want %d", len(app.homeRecents), homeRecentLimit)
	}
	for _, game := range app.homeRecents {
		if game.CoreID != "fes.zx81" {
			t.Fatalf("held-back recent reached Home: %+v", game)
		}
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

func TestAllCoresChangeWithLibraryHomeReloadsOpenRoom(t *testing.T) {
	cores := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: tenfootPongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
	}}
	app := startHomeLoungeInWithAllCores(t, t.TempDir(), cores, 1280, 720, false)
	waitFor(t, app, "v1 Home room", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == installedCoresHomeID })
	app.mu.Lock()
	before := app.room
	app.mu.Unlock()
	app.SetHomeRooms(false)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "all-cores", now)
	app.HandleCommand(CmdSelect, now)
	if snap := app.Snapshot(); !snap.RoomPicker.Open || snap.Room.Open {
		t.Fatalf("all-cores change did not close stale room and show Library Home: %+v", snap)
	}
	focusHomeRow(t, app, HomeKindRoom, installedCoresHomeID)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "refreshed room", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == installedCoresHomeID })
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.room == before || app.room.ReleaseOnly() {
		t.Fatalf("room was not reopened with all cores: before=%p after=%p release only=%v", before, app.room, app.room != nil && app.room.ReleaseOnly())
	}
}

func TestOptionsNormalizedLoadsAllCoresFromEnvironmentAndPrefs(t *testing.T) {
	t.Setenv("FOGCAST_TENFOOT_ALL_CORES", "")
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{AllCores: true}); err != nil {
		t.Fatal(err)
	}
	if opts := (Options{PrefsPath: path}).normalized(); !opts.AllCores {
		t.Fatal("tenfoot.json all_cores was ignored at startup")
	}
	t.Setenv("FOGCAST_TENFOOT_ALL_CORES", "1")
	if opts := (Options{PrefsPath: filepath.Join(t.TempDir(), "missing.json")}).normalized(); !opts.AllCores {
		t.Fatal("FOGCAST_TENFOOT_ALL_CORES was ignored at startup")
	}
}
