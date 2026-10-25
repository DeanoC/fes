package tenfoot

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/rooms"
)

func installedTestRoom(t *testing.T, root, id, title, script string) rooms.Pack {
	t.Helper()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "room.toml"), []byte("id = '"+id+"'\ntitle = '"+title+"'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.lua"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	pack, err := rooms.Install(root, source)
	if err != nil {
		t.Fatal(err)
	}
	return pack
}

func looseTestRoom(t *testing.T, root, id, title, script string) rooms.Pack {
	t.Helper()
	dir := filepath.Join(root, "loose-"+id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "room.toml"), []byte("id = '"+id+"'\ntitle = '"+title+"'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	pack := rooms.LoadPackFS(os.DirFS(dir), dir)
	if pack.Err != nil {
		t.Fatal(pack.Err)
	}
	return pack
}

func TestInstalledRoomLiveUpdateAndFailedUpdateKeepWorkingInstance(t *testing.T) {
	root := t.TempDir()
	first := installedTestRoom(t, root, "live", "First", "function load() store.set('keep', 'v1') end; function unload() store.set('on_exit', 'saved') end; function draw() gfx.text('old', 1, 1) end")
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{first}), root)
	t.Cleanup(func() { app.mu.Lock(); defer app.mu.Unlock(); app.closeAllRoomsLocked() })
	app.mu.Lock()
	defer app.mu.Unlock()
	app.openRoomLocked("live")
	original := app.room
	if original == nil || original.Title() != "First" {
		t.Fatalf("first load: %s", app.status)
	}
	second := installedTestRoom(t, root, "live", "Second", "function load() assert(store.get('keep') == 'v1'); store.set('keep', 'v2') end; function draw() gfx.text('new', 1, 1) end")
	app.session.State = "active"
	app.openInstalledRoomLocked(second, true, "")
	if app.room != original {
		t.Fatal("room replacement disturbed active play")
	}
	app.session.State = "idle"
	app.openRoomPickerLocked()
	app.activateHomeRowLocked(RoomPickerRow{ID: "live", Kind: HomeKindRoom})
	if app.room == original || app.room.Revision() != second.Revision {
		t.Fatalf("not reloaded: %s", app.status)
	}
	if frame := app.room.Frame(); len(frame.Ops) != 1 || frame.Ops[0].Text != "new" {
		t.Fatal("updated Lua was not rendered")
	}
	working := app.room
	for _, script := range []string{"function load() store.set('bad', true); error('broken load') end", "function draw() error('broken draw') end"} {
		installedTestRoom(t, root, "live", "Broken", script)
		app.openRoomPickerLocked()
		app.activateHomeRowLocked(RoomPickerRow{ID: "live", Kind: HomeKindRoom})
		if app.room != working || !strings.Contains(app.status, "Room update failed") {
			t.Fatalf("working instance lost: %s", app.status)
		}
		state, err := os.ReadFile(app.roomStorePathLocked("live"))
		if err != nil || strings.Contains(string(state), "bad") || !strings.Contains(string(state), "v2") || !strings.Contains(string(state), "on_exit") {
			t.Fatalf("trial changed persistent state: %s %v", state, err)
		}
		packs, err := rooms.LoadDir(root)
		if err != nil || packs[0].Revision != second.Revision {
			t.Fatalf("rollback failed: %v %v", packs, err)
		}
	}
}

func TestInstalledRoomStartupFailureRestoresPreviousWithoutRestart(t *testing.T) {
	root := t.TempDir()
	first := installedTestRoom(t, root, "home", "Working", "function draw() end")
	bad := installedTestRoom(t, root, "home", "Bad", "function load() error('oops') end")
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{bad}), root)
	app.SetHomeRooms(true)
	app.SetHomeRoom("home")
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.closeAllRoomsLocked()
	if !app.openHomeRoomAsRootLocked() || app.room == nil || app.room.Revision() != first.Revision {
		t.Fatalf("startup rollback: %s", app.status)
	}
}

func TestHomeRefreshesUpdatedRoomAndKeepsWorkingInstanceOnFailedTrial(t *testing.T) {
	root := t.TempDir()
	first := installedTestRoom(t, root, "home-update", "First", "function draw() end")
	installedTestRoom(t, root, "home-child", "Child", "function draw() end")
	packs, err := rooms.LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex(packs), root)
	app.SetHomeRooms(true)
	app.SetHomeRoom("home-update")
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.closeAllRoomsLocked()
	if !app.openHomeRoomAsRootLocked() || app.room == nil || app.room.Revision() != first.Revision {
		t.Fatalf("initial home room: %s", app.status)
	}
	original := app.room

	second := installedTestRoom(t, root, "home-update", "Second", "function draw() end")
	if !app.openHomeRoomAsRootLocked() || app.room == original || app.room.Revision() != second.Revision {
		t.Fatalf("home did not load the changed selection: %s", app.status)
	}
	working := app.room
	app.openNestedRoomLocked("home-child", "")
	if app.room == nil || app.room.ID() != "home-child" || len(app.roomStack) != 1 || app.roomStack[0] != working {
		t.Fatalf("nested room did not keep the current home: %s", app.status)
	}

	installedTestRoom(t, root, "home-update", "Broken", "function load() error('broken home update') end")
	if !app.openHomeRoomAsRootLocked() || app.room != working || len(app.roomStack) != 0 || !strings.Contains(app.status, "Room update failed") {
		t.Fatalf("failed home trial disturbed the working room: %s", app.status)
	}
	packs, err = rooms.LoadDir(root)
	restored := false
	for _, pack := range packs {
		if pack.ID == "home-update" && pack.Revision == second.Revision {
			restored = true
		}
	}
	if err != nil || !restored {
		t.Fatalf("failed home trial did not restore previous selection: %v %v", packs, err)
	}
}

func TestInstalledHomeKeepsChildOpenedDuringLoad(t *testing.T) {
	root := t.TempDir()
	home := installedTestRoom(t, root, "home-nav", "Home", "function load() rooms.open('home-nav-child') end; function draw() end")
	child := installedTestRoom(t, root, "home-nav-child", "Child", "function draw() end")
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{home, child}), root)
	app.SetHomeRooms(true)
	app.SetHomeRoom("home-nav")
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.closeAllRoomsLocked()

	if !app.openHomeRoomAsRootLocked() || app.room == nil || app.room.ID() != "home-nav-child" {
		t.Fatalf("home child navigation was discarded: room=%v status=%s", app.room, app.status)
	}
	if len(app.roomStack) != 1 || app.roomStack[0].ID() != "home-nav" || app.roomStack[0].Revision() != home.Revision {
		t.Fatalf("home parent stack was not preserved: %+v", app.roomStack)
	}
}

func TestRollbackRevealsLooseRoomAndPlayGuardKeepsCurrentRoom(t *testing.T) {
	root := t.TempDir()
	looseTestRoom(t, root, "live", "Loose", "function draw() end")
	installed := installedTestRoom(t, root, "live", "Installed", "function draw() end")
	packs, err := rooms.LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex(packs), root)
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.closeAllRoomsLocked()
	app.openRoomLocked("live")
	working := app.room
	if working == nil || working.Revision() != installed.Revision {
		t.Fatalf("installed room was not opened: %s", app.status)
	}
	app.session.State = "active"
	if err := rooms.Rollback(root, "live", ""); err != nil {
		t.Fatal(err)
	}
	app.openRoomPickerLocked()
	row := app.homeRoomRowLocked("live")
	if row.Invalid || row.Label != "Loose" {
		t.Fatalf("rollback did not restore loose room: %#v", row)
	}
	app.activateHomeRowLocked(row)
	if app.room != working || !app.roomPickerOpen || !strings.Contains(app.status, "Stop the running machine") {
		t.Fatalf("loose selection disturbed live room or picker: %s", app.status)
	}
}

func TestInstalledNestedRoomKeepsParent(t *testing.T) {
	root := t.TempDir()
	parent := installedTestRoom(t, root, "parent", "Parent", "function draw() end")
	child := installedTestRoom(t, root, "child", "Child", "function draw() end")
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{parent, child}), root)
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.closeAllRoomsLocked()
	app.openRoomLocked("parent")
	original := app.room
	app.openNestedRoomLocked("child", "")
	if app.room == nil || app.room.ID() != "child" || len(app.roomStack) != 1 || app.roomStack[0] != original {
		t.Fatalf("nested room lost parent: %s", app.status)
	}
}

func TestNestedRoomReceivesInitialGameID(t *testing.T) {
	root := t.TempDir()
	parent := installedTestRoom(t, root, "parent", "Parent", "function on_input(cmd) if cmd == 'select' then rooms.open('child', {game_id = 'c64-second'}) return true end return false end; function draw() end")
	child := installedTestRoom(t, root, "child", "Child", "function draw() end")
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex([]rooms.Pack{parent, child}), root)
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.closeAllRoomsLocked()
	app.openRoomLocked("parent")
	if app.room == nil || !app.room.Input("select") {
		t.Fatal("parent did not handle selection")
	}
	app.applyRoomActionsLocked()
	if app.room == nil || app.room.ID() != "child" {
		t.Fatalf("child room did not open: %s", app.status)
	}
	if err := app.room.CheckGlobal("room.initial_game_id == 'c64-second'"); err != nil {
		t.Fatalf("child did not receive its selected game: %v", err)
	}
}

func TestFirstPartyHomeUpdatesLuaAndAssetsInTheSameLauncher(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	manifest, err := os.ReadFile("../rooms/examples/fes-cores/room.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "room.toml"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	install := func(label string) rooms.Pack {
		t.Helper()
		script := "function draw() gfx.text('" + label + "', 1, 1) end"
		for name, data := range map[string]string{"main.lua": script, "art.txt": label + " artwork"} {
			if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		pack, err := rooms.Install(root, source)
		if err != nil {
			t.Fatal(err)
		}
		return pack
	}
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex(rooms.Examples()), root)
	app.SetHomeRooms(true)
	app.SetHomeRoom("example.fes-cores")
	app.mu.Lock()
	defer app.mu.Unlock()
	defer app.closeAllRoomsLocked()
	if !app.openHomeRoomAsRootLocked() || app.room == nil {
		t.Fatalf("built-in Home: %s", app.status)
	}
	original := app.room
	first := install("first")
	if app.room != original {
		t.Fatal("install replaced the running room before reopen")
	}
	reopen := func(want rooms.Pack, label string) {
		t.Helper()
		app.openRoomPickerLocked()
		app.activateHomeRowLocked(RoomPickerRow{ID: "example.fes-cores", Kind: HomeKindRoom})
		if app.room == nil || app.room.Revision() != want.Revision {
			t.Fatalf("Home did not load installed snapshot: %s", app.status)
		}
		frame := app.room.Frame()
		if len(frame.Ops) != 1 || frame.Ops[0].Text != label {
			t.Fatalf("updated Lua did not render: %#v", frame.Ops)
		}
	}
	reopen(first, "first")
	working := app.room
	second := install("second")
	if app.room != working {
		t.Fatal("asset update disturbed the working instance")
	}
	oldArt, err := fs.ReadFile(first.FS, "art.txt")
	if err != nil || string(oldArt) != "first artwork" {
		t.Fatalf("old asset changed: %q %v", oldArt, err)
	}
	newArt, err := fs.ReadFile(second.FS, "art.txt")
	if err != nil || string(newArt) != "second artwork" {
		t.Fatalf("new asset missing: %q %v", newArt, err)
	}
	reopen(second, "second")
	if err := rooms.Rollback(root, "example.fes-cores", ""); err != nil {
		t.Fatal(err)
	}
	reopen(first, "first")
}
