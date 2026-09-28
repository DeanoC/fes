package tenfoot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

const homeRoomScript = `
loads = 0
function load()
  loads = loads + 1
end
function on_input(cmd)
  if cmd == "sort" then rooms.open("nested") return true end
  return false
end
function draw()
  gfx.rect(0, 0, 10, 10, "#fff")
end
`

const homeFromNestedScript = `
function on_input(cmd)
  if cmd == "filter_next" then rooms.home() return true end
  return false
end
function draw()
  gfx.rect(0, 0, 10, 10, "#fff")
end
`

func TestHomeRoomOpensAsRootAndHomeClearsStack(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "main-menu", homeRoomScript),
		testRoomPack(t, "nested", homeFromNestedScript),
	})
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(index, t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom("main-menu")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	now := time.Now()

	snap := waitFor(t, app, "home room root", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "main-menu" && !s.RoomPicker.Open && len(s.Room.Parents) == 0
	})
	if snap.Room.Err != "" {
		t.Fatalf("room err %q", snap.Room.Err)
	}
	app.mu.Lock()
	root := app.room
	loads := luaGlobalNumber(app, "loads")
	app.mu.Unlock()
	if root == nil || loads != 1 {
		t.Fatalf("root load loads=%v", loads)
	}

	app.HandleCommand(CmdSortCycle, now)
	waitFor(t, app, "nested", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "nested" && len(s.Room.Parents) == 1 && s.Room.Parents[0] == "main-menu"
	})
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "back to home room", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "main-menu" && !s.RoomPicker.Open && len(s.Room.Parents) == 0
	})
	app.mu.Lock()
	same := app.room == root
	loads = luaGlobalNumber(app, "loads")
	app.mu.Unlock()
	if !same || loads != 1 {
		t.Fatalf("back reloaded the home room same=%v loads=%v", same, loads)
	}

	app.HandleCommand(CmdBack, now)
	snap = app.Snapshot()
	if snap.Room.Open || !snap.RoomPicker.Open {
		t.Fatalf("back at home root should open the picker: room=%v picker=%v", snap.Room.Open, snap.RoomPicker.Open)
	}

	app.HandleCommand(CmdHome, now)
	waitFor(t, app, "home from picker", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "main-menu" && !s.RoomPicker.Open && len(s.Room.Parents) == 0
	})

	app.HandleCommand(CmdSortCycle, now)
	waitFor(t, app, "nested again", func(s Snapshot) bool { return s.Room.ID == "nested" })
	app.mu.Lock()
	root = app.roomStack[0]
	app.mu.Unlock()
	app.HandleCommand(CmdHome, now)
	waitFor(t, app, "home clears stack", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "main-menu" && !s.RoomPicker.Open && len(s.Room.Parents) == 0
	})
	app.mu.Lock()
	same = app.room == root
	stack := len(app.roomStack)
	app.mu.Unlock()
	if !same || stack != 0 {
		t.Fatalf("home did not restore the root room same=%v stack=%d", same, stack)
	}

	app.HandleCommand(CmdSortCycle, now)
	waitFor(t, app, "nested for rooms.home", func(s Snapshot) bool { return s.Room.ID == "nested" })
	app.HandleCommand(CmdFilterNext, now)
	waitFor(t, app, "rooms.home", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "main-menu" && len(s.Room.Parents) == 0 && !s.RoomPicker.Open
	})

	app.HandleCommand(CmdSortCycle, now)
	waitFor(t, app, "nested for settings home", func(s Snapshot) bool { return s.Room.ID == "nested" })
	app.HandleCommand(CmdSettings, now)
	if !app.Snapshot().Settings.Open {
		t.Fatal("settings")
	}
	for i := 0; i < settingsRowHome; i++ {
		app.HandleCommand(CmdDown, now)
	}
	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if snap.Settings.Open || !snap.Room.Open || snap.Room.ID != "main-menu" || len(snap.Room.Parents) != 0 || snap.RoomPicker.Open {
		t.Fatalf("settings home %+v parents=%v picker=%v settings=%v", snap.Room.ID, snap.Room.Parents, snap.RoomPicker.Open, snap.Settings.Open)
	}
}

func TestHomeRoomLibraryIgnoresHomeRoom(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "main-menu", homeRoomScript),
	})
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(index, t.TempDir())
	app.SetHomeRooms(false)
	app.SetHomeRoom("main-menu")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "library", func(s Snapshot) bool { return len(s.Games) == 2 && !s.Loading })
	snap := app.Snapshot()
	if snap.Room.Open || snap.RoomPicker.Open {
		t.Fatalf("library start opened rooms room=%v picker=%v", snap.Room.Open, snap.RoomPicker.Open)
	}
	time.Sleep(50 * time.Millisecond)
	if n := countUIPosts(h.uiPostsSnapshot(), "ui.home_room_fallback"); n != 0 {
		t.Fatalf("library mode emitted %d fallbacks", n)
	}
	app.HandleCommand(CmdHome, time.Now())
	snap = app.Snapshot()
	if !snap.RoomPicker.Open || snap.Room.Open {
		t.Fatalf("home=library should still toggle the picker: room=%v picker=%v", snap.Room.Open, snap.RoomPicker.Open)
	}
}

func TestHomeRoomFallbackPickerAndDiagnostic(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		assertHomeRoomFallback(t, "zz-missing-home", "missing", rooms.NewIndex([]rooms.Pack{
			testRoomPack(t, "arcade", homeRoomScript),
		}))
	})
	t.Run("invalid id", func(t *testing.T) {
		assertHomeRoomFallback(t, "ZZ-Bad", "invalid id", rooms.NewIndex([]rooms.Pack{
			testRoomPack(t, "arcade", homeRoomScript),
		}))
	})
	t.Run("invalid pack", func(t *testing.T) {
		bad := rooms.Pack{Manifest: rooms.Manifest{ID: "zz-invalid-pack", Title: "Broken pack"}, Err: errors.New("broken manifest")}
		assertHomeRoomFallback(t, "zz-invalid-pack", "invalid", rooms.NewIndex([]rooms.Pack{
			testRoomPack(t, "arcade", homeRoomScript),
			bad,
		}))
	})
	t.Run("open failed", func(t *testing.T) {
		ghost := rooms.Pack{Manifest: rooms.Manifest{ID: "zz-ghost-open", Title: "Ghost room"}}
		assertHomeRoomFallback(t, "zz-ghost-open", "open failed", rooms.NewIndex([]rooms.Pack{ghost}))
	})
}

func assertHomeRoomFallback(t *testing.T, id, reason string, index *rooms.Index) {
	t.Helper()
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(index, t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom(id)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	snap := waitFor(t, app, "fallback picker", func(s Snapshot) bool {
		return s.RoomPicker.Open && !s.Room.Open
	})
	if strings.Contains(snap.Status, id) || strings.Contains(snap.Room.Err, id) || strings.Contains(snap.LoadErr, id) {
		t.Fatalf("tv text leaked %q status=%q err=%q load=%q", id, snap.Status, snap.Room.Err, snap.LoadErr)
	}
	ev := waitForUIPost(t, h, "ui.home_room_fallback")
	if ev.Detail["room"] != id || ev.Detail["reason"] != reason {
		t.Fatalf("diagnostic %+v want room=%s reason=%s", ev.Detail, id, reason)
	}
	for i := 0; i < 8; i++ {
		app.Tick(time.Now())
	}
	time.Sleep(40 * time.Millisecond)
	if n := countUIPosts(h.uiPostsSnapshot(), "ui.home_room_fallback"); n != 1 {
		t.Fatalf("fallback events %d, want one per start", n)
	}
}

func TestHomeRoomSurvivesPrefsSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{
		SafeAreaPct: DefaultSafeAreaPct,
		Layout:      "grid",
		Home:        "rooms",
		HomeRoom:    "main-menu",
		PinnedRooms: []string{"arcade"},
	}); err != nil {
		t.Fatal(err)
	}
	app := NewApp(nil, 1280, 720, 10)
	app.SetPrefsPath(path)
	app.SetHomeRooms(true)
	app.SetSafeAreaPct(0.05)
	app.mu.Lock()
	app.persistPrefsLocked("safe-area")
	app.mu.Unlock()
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.HomeRoom != "main-menu" || got.Home != "rooms" || len(got.PinnedRooms) != 1 || got.PinnedRooms[0] != "arcade" {
		t.Fatalf("prefs after save %+v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"home_room": "main-menu"`) {
		t.Fatalf("json dropped home_room: %s", raw)
	}
}

func TestHomeRoomFlagPrefEnvPrecedence(t *testing.T) {
	t.Setenv("FOGCAST_HOME_ROOM", "")
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{
		SafeAreaPct: DefaultSafeAreaPct,
		Layout:      "grid",
		Home:        "rooms",
		HomeRoom:    "from-pref",
	}); err != nil {
		t.Fatal(err)
	}
	opts := Options{PrefsPath: path}.normalized()
	if opts.HomeRoom != "from-pref" || opts.Home != "rooms" {
		t.Fatalf("pref %#v", opts.HomeRoom)
	}
	opts = Options{PrefsPath: path, HomeRoom: "from-flag"}.normalized()
	if opts.HomeRoom != "from-flag" {
		t.Fatalf("flag %#v", opts.HomeRoom)
	}
	t.Setenv("FOGCAST_HOME_ROOM", "from-env")
	opts = Options{PrefsPath: path}.normalized()
	if opts.HomeRoom != "from-pref" {
		t.Fatalf("pref should beat env, got %#v", opts.HomeRoom)
	}
	opts = Options{PrefsPath: filepath.Join(t.TempDir(), "missing.json")}.normalized()
	if opts.HomeRoom != "from-env" {
		t.Fatalf("env fallback %#v", opts.HomeRoom)
	}
	opts = Options{PrefsPath: path, HomeRoom: "from-flag"}.normalized()
	if opts.HomeRoom != "from-flag" {
		t.Fatalf("flag should beat env %#v", opts.HomeRoom)
	}
}

func TestConfiguredAppAppliesHomeRoom(t *testing.T) {
	t.Setenv("FOGCAST_HOME_ROOM", "from-env")
	t.Setenv("FOGCAST_THEME", "")
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	opts := Options{
		APIBase:   "http://127.0.0.1:9",
		PrefsPath: path,
		Home:      "rooms",
		HomeRoom:  "main-menu",
		Theme:     "default",
		Width:     1280,
		Height:    720,
	}.normalized()
	app, err := configuredApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if !app.homeRooms || app.homeRoom != "main-menu" {
		t.Fatalf("homeRooms=%v homeRoom=%q", app.homeRooms, app.homeRoom)
	}
}
