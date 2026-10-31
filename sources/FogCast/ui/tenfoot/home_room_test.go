package tenfoot

import (
	"context"
	"errors"
	"github.com/DeanoC/FogCast/internal/localcores"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	app.SetRooms(index, "")
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
	app.SetRooms(index, "")
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
	for _, id := range []string{systemRoomID, hardwareRoomID} {
		t.Run(id, func(t *testing.T) {
			assertHomeRoomFallback(t, id, "internal setup", rooms.NewIndex([]rooms.Pack{
				testRoomPack(t, id, homeRoomScript),
				testRoomPack(t, "arcade", homeRoomScript),
			}))
		})
	}
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
	app.SetRooms(index, "")
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

func TestInstalledCoresHomeDefault(t *testing.T) {
	for _, tc := range []struct {
		name, backend, backendEnv, home, flag, pref, env, want string
	}{
		{name: "kit rooms", backend: "menu-display", home: "rooms", want: "example.fes-cores"},
		{name: "environment backend", backendEnv: "menu-display", home: "rooms", want: "example.fes-cores"},
		{name: "explicit room", backend: "menu-display", home: "rooms", flag: "chosen", pref: "saved", env: "environment", want: "chosen"},
		{name: "saved room", backend: "menu-display", home: "rooms", pref: "saved", env: "environment", want: "saved"},
		{name: "environment room", backend: "menu-display", home: "rooms", env: "environment", want: "environment"},
		{name: "kit library", backend: "menu-display", home: "library"},
		{name: "host SDL", backend: "sdl", home: "rooms"},
		{name: "framebuffer", backend: "linuxfb", home: "rooms"},
		{name: "software", backend: "software", home: "rooms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TENFOOT_GFX", tc.backendEnv)
			t.Setenv("FOGCAST_HOME_ROOM", tc.env)
			path := filepath.Join(t.TempDir(), "tenfoot.json")
			if err := saveTenfootPrefs(path, tenfootPrefs{Home: tc.home, HomeRoom: tc.pref}); err != nil {
				t.Fatal(err)
			}
			opts := Options{PrefsPath: path, GFX: tc.backend, HomeRoom: tc.flag}.normalized()
			if opts.HomeRoom != tc.want {
				t.Fatalf("home room = %q, want %q", opts.HomeRoom, tc.want)
			}
		})
	}
}

func TestInstalledCoresDefaultOpensAndBackKeepsHomePicker(t *testing.T) {
	t.Setenv("FOGCAST_HOME_ROOM", "")
	h := newRoomHost(t)
	opts := Options{GFX: "menu-display", Home: "rooms", HomeSet: true, PrefsPath: filepath.Join(t.TempDir(), "tenfoot.json")}.normalized()
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(opts.PrefsPath)
	app.SetRooms(rooms.NewIndex(rooms.Examples()), t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom(opts.HomeRoom)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "installed cores root", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "example.fes-cores" && s.Room.Err == ""
	})
	app.HandleCommand(CmdBack, time.Now())
	snap := app.Snapshot()
	if snap.Room.Open || !snap.RoomPicker.Open {
		t.Fatalf("Back must expose Home picker: room=%v picker=%v", snap.Room.Open, snap.RoomPicker.Open)
	}
}

// The initial launcher inventory succeeds, but the home room's independent
// read races startup. A later successful read must populate the same room.
type startupRoomCores struct {
	fakeLocalCores
	listMu    sync.Mutex
	listCalls int
}

func (f *startupRoomCores) List(ctx context.Context) ([]localcores.Core, error) {
	f.listMu.Lock()
	f.listCalls++
	call := f.listCalls
	f.listMu.Unlock()
	if call == 2 {
		return nil, localcores.ErrUnavailable
	}
	return f.fakeLocalCores.List(ctx)
}
func TestConfiguredInstalledHomeRecoversStartupList(t *testing.T) {
	t.Setenv("FOGCAST_HOME_ROOM", "")
	h := newRoomHost(t)
	opts := Options{GFX: "menu-display", APIBase: h.server.URL, Home: "rooms", HomeSet: true, PrefsPath: filepath.Join(t.TempDir(), "tenfoot.json"), CatalogConfig: filepath.Join(t.TempDir(), "absent.toml")}.normalized()
	app, err := configuredApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	cores := &startupRoomCores{fakeLocalCores: fakeLocalCores{cores: []localcores.Core{{PackageID: tenfootPongID, CoreID: "fes.pong", Name: "FES Pong", Launchable: true}}}}
	app.SetKitLocal(cores, &fakePadFeed{})
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	snap := waitFor(t, app, "usable installed home", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "example.fes-cores" && s.Room.Destination.Kind == rooms.KindCore
	})
	if snap.Room.Destination.PackageID != tenfootPongID || !snap.Room.Destination.CoreLaunchable {
		t.Fatalf("destination %+v", snap.Room.Destination)
	}
	cores.listMu.Lock()
	defer cores.listMu.Unlock()
	if cores.listCalls < 3 {
		t.Fatalf("startup read was not retried: %d", cores.listCalls)
	}
}

// A kit that boots on Library has no resolved home room. Switching to Rooms
// in Settings must open Installed cores on confirm, before the process
// restarts, without storing that implicit id as home_room.
func TestSettingsSwitchToRoomsOpensInstalledCores(t *testing.T) {
	app, path := startUnconfiguredLibraryHome(t, "menu-display", "")
	if room := homeRoomID(app); room != "" {
		t.Fatalf("library start resolved %q", room)
	}
	toggleSettingsHome(t, app)
	if room := homeRoomID(app); room != "example.fes-cores" {
		t.Fatalf("settings switch home room = %q", room)
	}
	assertSavedHome(t, path, "rooms", "")
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "installed cores after settings", func(s Snapshot) bool {
		return !s.Settings.Open && s.Room.Open && s.Room.ID == "example.fes-cores" && s.Room.Err == "" && !s.RoomPicker.Open
	})
	assertSavedHome(t, path, "rooms", "")
}

func TestSettingsSwitchKeepsExplicitHomeRoom(t *testing.T) {
	app, path := startUnconfiguredLibraryHome(t, "menu-display", "chosen")
	if room := homeRoomID(app); room != "chosen" {
		t.Fatalf("explicit room = %q", room)
	}
	toggleSettingsHome(t, app)
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "explicit room after settings", func(s Snapshot) bool {
		return !s.Settings.Open && s.Room.Open && s.Room.ID == "chosen" && !s.RoomPicker.Open
	})
	assertSavedHome(t, path, "rooms", "chosen")
}

func TestSettingsSwitchOnSDLKeepsPicker(t *testing.T) {
	app, path := startUnconfiguredLibraryHome(t, "sdl", "")
	toggleSettingsHome(t, app)
	if room := homeRoomID(app); room != "" {
		t.Fatalf("sdl switch resolved %q", room)
	}
	app.HandleCommand(CmdSelect, time.Now())
	snap := app.Snapshot()
	if snap.Settings.Open || snap.Room.Open || !snap.RoomPicker.Open {
		t.Fatalf("sdl home settings=%v room=%v picker=%v id=%q", snap.Settings.Open, snap.Room.Open, snap.RoomPicker.Open, snap.Room.ID)
	}
	assertSavedHome(t, path, "rooms", "")
}

func TestSettingsLibraryConfirmLeavesInstalledCores(t *testing.T) {
	app, path := startUnconfiguredLibraryHome(t, "menu-display", "")
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "home", now)
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if snap.Settings.Open || snap.Room.ID == "example.fes-cores" || snap.Room.Open {
		t.Fatalf("library confirm opened installed cores: settings=%v room=%q open=%v", snap.Settings.Open, snap.Room.ID, snap.Room.Open)
	}
	if !snap.RoomPicker.Open {
		t.Fatal("library confirm must still open the home picker")
	}
	if room := homeRoomID(app); room != "" {
		t.Fatalf("library confirm resolved %q", room)
	}
	assertSavedHome(t, path, "library", "")
}

func startUnconfiguredLibraryHome(t *testing.T, gfxName, savedRoom string) (*App, string) {
	t.Helper()
	t.Setenv("FOGCAST_HOME_ROOM", "")
	t.Setenv("FOGCAST_THEME", "")
	t.Setenv("TENFOOT_GFX", "")
	h := newRoomHost(t)
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{Home: "library", HomeRoom: savedRoom}); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		GFX:       gfxName,
		APIBase:   h.server.URL,
		PrefsPath: path,
		Home:      "library",
		HomeSet:   true,
		Theme:     "default",
		Width:     1280,
		Height:    720,
	}.normalized()
	if opts.Home != "library" || opts.HomeRoom != savedRoom {
		t.Fatalf("startup home=%q room=%q", opts.Home, opts.HomeRoom)
	}
	app, err := configuredApp(opts)
	if err != nil {
		t.Fatal(err)
	}
	packs := rooms.Examples()
	if savedRoom != "" {
		packs = append(append([]rooms.Pack{}, packs...), testRoomPack(t, savedRoom, homeRoomScript))
	}
	app.SetRooms(rooms.NewIndex(packs), "")
	if gfxName == "menu-display" {
		app.SetKitLocal(&fakeLocalCores{cores: []localcores.Core{{
			PackageID: tenfootPongID, CoreID: "fes.pong", Name: "FES Pong", Launchable: true,
		}}}, &fakePadFeed{})
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app, path
}

func toggleSettingsHome(t *testing.T, app *App) {
	t.Helper()
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	if !app.Snapshot().Settings.Open {
		t.Fatal("settings did not open")
	}
	focusSettingsRow(t, app, "home", now)
	app.HandleCommand(CmdRight, now)
	row := settingsRowByID(t, app, "home")
	if !strings.HasPrefix(row.Value, "Rooms") {
		t.Fatalf("home row = %#v", row)
	}
}

func homeRoomID(app *App) string {
	app.mu.Lock()
	defer app.mu.Unlock()
	return app.homeRoom
}

func assertSavedHome(t *testing.T, path, home, room string) {
	t.Helper()
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Home != home || got.HomeRoom != room {
		t.Fatalf("saved home=%q room=%q, want home=%q room=%q", got.Home, got.HomeRoom, home, room)
	}
}
