package tenfoot

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestHomeListsPinnedRecentRoomsAndLibrary(t *testing.T) {
	h := newRoomHost(t)
	h.mu.Lock()
	h.recents = []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}
	h.mu.Unlock()
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", testRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)

	snap := waitFor(t, app, "home recents", func(s Snapshot) bool {
		return s.RoomPicker.Open && homeHasRow(s, HomeKindRecent, "snes-mario") &&
			homeHasRow(s, HomeKindRecents, "recents") &&
			homeHasRow(s, HomeKindRoom, "arcade") &&
			homeHasRow(s, HomeKindLibrary, "")
	})
	if homeHasRow(snap, HomeKindPinned, "arcade") {
		t.Fatalf("unpinned room listed as pinned: %+v", snap.RoomPicker.Rows)
	}
	if !strings.Contains(snap.HeaderHint(), "pin") || !strings.Contains(snap.HeaderHint(), "settings") {
		t.Fatalf("home hint %q", snap.HeaderHint())
	}
}

func TestHomePinPersistsAndStaysFirst(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", testRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "home", func(s Snapshot) bool { return s.RoomPicker.Open })
	focusHomeRow(t, app, HomeKindRoom, "nested")
	app.HandleCommand(CmdDetails, now)
	snap := waitFor(t, app, "pinned", func(s Snapshot) bool {
		return homeHasRow(s, HomeKindPinned, "nested")
	})
	if snap.RoomPicker.Rows[0].Kind != HomeKindPinned || snap.RoomPicker.Rows[0].ID != "nested" {
		t.Fatalf("pinned should lead home: %+v", snap.RoomPicker.Rows)
	}
	if snap.RoomPicker.Index != 0 {
		t.Fatalf("pin should keep focus on the pin: index=%d rows=%+v", snap.RoomPicker.Index, snap.RoomPicker.Rows)
	}

	prefs, err := loadTenfootPrefs(app.prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs.PinnedRooms) != 1 || prefs.PinnedRooms[0] != "nested" {
		t.Fatalf("pinned prefs %#v", prefs.PinnedRooms)
	}

	app.HandleCommand(CmdDetails, now)
	snap = waitFor(t, app, "unpinned", func(s Snapshot) bool {
		return !homeHasRow(s, HomeKindPinned, "nested") && homeHasRow(s, HomeKindRoom, "nested")
	})
	row := snap.RoomPicker.Rows[snap.RoomPicker.Index]
	if row.Kind != HomeKindRoom || row.ID != "nested" {
		t.Fatalf("unpin should land on the rooms copy: %+v", row)
	}
}

func TestHomeRecentsLoadDoesNotMoveSelection(t *testing.T) {
	h := newRoomHost(t)
	gate := make(chan struct{})
	h.mu.Lock()
	h.recents = []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}
	h.recentsGate = gate
	h.mu.Unlock()
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", testRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	waitFor(t, app, "home before recents", func(s Snapshot) bool {
		return s.RoomPicker.Open && homeHasRow(s, HomeKindRoom, "arcade") && !homeHasRow(s, HomeKindRecent, "snes-mario")
	})
	focusHomeRow(t, app, HomeKindRoom, "arcade")
	close(gate)
	snap := waitFor(t, app, "recents arrived", func(s Snapshot) bool {
		return homeHasRow(s, HomeKindRecent, "snes-mario")
	})
	row := snap.RoomPicker.Rows[snap.RoomPicker.Index]
	if row.Kind != HomeKindRoom || row.ID != "arcade" {
		t.Fatalf("recents load moved selection to %+v", row)
	}
}

func TestHomeOpensRoomLibraryRecentsAndLaunchesRecent(t *testing.T) {
	h := newRoomHost(t)
	h.mu.Lock()
	h.recents = []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}
	h.mu.Unlock()
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", testRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "home recents", func(s Snapshot) bool {
		return s.RoomPicker.Open && homeHasRow(s, HomeKindRecent, "snes-mario")
	})

	focusHomeRow(t, app, HomeKindRoom, "arcade")
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "room", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == "arcade" })

	app.HandleCommand(CmdHome, now)
	waitFor(t, app, "home from room", func(s Snapshot) bool { return s.RoomPicker.Open })
	focusHomeRow(t, app, HomeKindLibrary, "")
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if snap.RoomPicker.Open || snap.Room.Open {
		t.Fatalf("library should close home: picker=%v room=%v", snap.RoomPicker.Open, snap.Room.Open)
	}

	app.HandleCommand(CmdHome, now)
	waitFor(t, app, "home again", func(s Snapshot) bool { return s.RoomPicker.Open })
	focusHomeRow(t, app, HomeKindRecents, "recents")
	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "recents collection", func(s Snapshot) bool {
		return !s.RoomPicker.Open && s.Collection == "recents"
	})
	if snap.Collection != "recents" {
		t.Fatalf("recents collection = %+v", snap)
	}

	app.HandleCommand(CmdHome, now)
	waitFor(t, app, "home for launch", func(s Snapshot) bool {
		return s.RoomPicker.Open && homeHasRow(s, HomeKindRecent, "snes-mario")
	})
	focusHomeRow(t, app, HomeKindRecent, "snes-mario")
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "recent launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() < 1 || !strings.Contains(strings.Join(h.launches, " "), "snes-mario") {
		t.Fatalf("recent launches %v", h.launches)
	}
}

func TestHomeOpensWithoutInstalledRooms(t *testing.T) {
	h := newRoomHost(t)
	app := newRoomApp(t, h, rooms.NewIndex(nil), false)
	now := time.Now()
	waitFor(t, app, "library start", func(s Snapshot) bool { return !s.RoomPicker.Open })
	app.HandleCommand(CmdHome, now)
	snap := waitFor(t, app, "home without rooms", func(s Snapshot) bool { return s.RoomPicker.Open })
	if !homeHasRow(snap, HomeKindRecents, "recents") || !homeHasRow(snap, HomeKindLibrary, "") {
		t.Fatalf("home rows %+v", snap.RoomPicker.Rows)
	}
	for _, row := range snap.RoomPicker.Rows {
		if row.isRoom() {
			t.Fatalf("unexpected room row %+v", row)
		}
	}
}

func TestSettingsConfirmGoesHomeAndKeepsSystemMenu(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", testRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "home", func(s Snapshot) bool { return s.RoomPicker.Open })
	focusHomeRow(t, app, HomeKindRoom, "arcade")
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "room", func(s Snapshot) bool { return s.Room.Open && !s.RoomPicker.Open })

	app.HandleCommand(CmdSettings, now)
	if !app.Snapshot().Settings.Open {
		t.Fatal("system menu")
	}
	for i := 0; i < settingsRowHome; i++ {
		app.HandleCommand(CmdDown, now)
	}
	settings := app.Snapshot().Settings
	if settings.Index < 0 || settings.Index >= len(settings.Rows) || settings.Rows[settings.Index].ID != "home" {
		t.Fatalf("settings row %#v", settings)
	}
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if snap.Settings.Open {
		t.Fatal("confirm Home must close settings")
	}
	if !snap.RoomPicker.Open {
		t.Fatal("confirm Home must open the Home overlay")
	}
	if !snap.Room.Open {
		t.Fatal("go Home should keep the room under the overlay")
	}

	app.HandleCommand(CmdSettings, now)
	if !app.Snapshot().Settings.Open || !app.Snapshot().RoomPicker.Open {
		t.Fatal("system menu must still open over Home")
	}
	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Settings.Open {
		t.Fatal("Back must still close the system menu")
	}
	if !app.Snapshot().RoomPicker.Open {
		t.Fatal("Back must not dismiss Home while closing settings")
	}
}

func TestHomeLoadsPinnedRoomsFromPrefs(t *testing.T) {
	h := newRoomHost(t)
	path := t.TempDir() + "/tenfoot.json"
	if err := saveTenfootPrefs(path, tenfootPrefs{
		SafeAreaPct: DefaultSafeAreaPct,
		Home:        "rooms",
		PinnedRooms: []string{"mushroom", "arcade", "arcade"},
	}); err != nil {
		t.Fatal(err)
	}
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "arcade", testRoomScript),
		testRoomPack(t, "mushroom", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(path)
	app.SetRooms(index, "/tmp/rooms")
	app.SetHomeRooms(true)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	snap := waitFor(t, app, "loaded pins", func(s Snapshot) bool {
		return s.RoomPicker.Open && homeHasRow(s, HomeKindPinned, "mushroom") && homeHasRow(s, HomeKindPinned, "arcade")
	})
	if snap.RoomPicker.Rows[0].ID != "mushroom" || snap.RoomPicker.Rows[1].ID != "arcade" {
		t.Fatalf("pin order %+v", snap.RoomPicker.Rows)
	}
}
