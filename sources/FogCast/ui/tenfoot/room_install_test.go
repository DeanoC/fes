package tenfoot

import (
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

func TestInstalledRoomLiveUpdateAndFailedUpdateKeepWorkingInstance(t *testing.T) {
	root := t.TempDir()
	first := installedTestRoom(t, root, "live", "First", "function draw() gfx.text('old', 1, 1) end")
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
	second := installedTestRoom(t, root, "live", "Second", "function draw() gfx.text('new', 1, 1) end")
	app.openRoomPickerLocked()
	app.activateHomeRowLocked(RoomPickerRow{ID: "live", Kind: HomeKindRoom})
	if app.room == original || app.room.Revision() != second.Revision {
		t.Fatalf("not reloaded: %s", app.status)
	}
	working := app.room
	for _, script := range []string{"function load() store.set('bad', true); error('broken load') end", "function draw() error('broken draw') end"} {
		installedTestRoom(t, root, "live", "Broken", script)
		app.openRoomPickerLocked()
		app.activateHomeRowLocked(RoomPickerRow{ID: "live", Kind: HomeKindRoom})
		if app.room != working || !strings.Contains(app.status, "Room update failed") {
			t.Fatalf("working instance lost: %s", app.status)
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
	app.openNestedRoomLocked("child")
	if app.room == nil || app.room.ID() != "child" || len(app.roomStack) != 1 || app.roomStack[0] != original {
		t.Fatalf("nested room lost parent: %s", app.status)
	}
}
