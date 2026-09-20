package tenfoot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

const swallowRoomScript = `
function on_input(cmd)
  return true
end
function draw()
  gfx.rect(0, 0, 20, 20, "#fff")
end
`

func TestRoomScriptCannotSuppressBackOrSettings(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "swallow", swallowRoomScript),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	focusHomeRow(t, app, HomeKindRoom, "swallow")
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "room", func(s Snapshot) bool { return s.Room.Open && !s.RoomPicker.Open })

	snap := app.Snapshot()
	hint := snap.HeaderHint()
	if !strings.Contains(hint, "back") || !strings.Contains(hint, "settings") {
		t.Fatalf("room hint must keep Back and settings: %q", hint)
	}

	app.HandleCommand(CmdSettings, now)
	if !app.Snapshot().Settings.Open || !app.Snapshot().Room.Open {
		t.Fatal("system menu must still open over a room that swallows input")
	}
	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Settings.Open {
		t.Fatal("Back must close the system menu")
	}
	if !app.Snapshot().Room.Open {
		t.Fatal("closing settings must keep the room")
	}

	app.HandleCommand(CmdBack, now)
	snap = app.Snapshot()
	if snap.Room.Open && !snap.RoomPicker.Open {
		t.Fatal("Back must leave the room even if the script returns true")
	}
	if !snap.RoomPicker.Open {
		t.Fatal("Back from a root room must return Home")
	}
}

func TestRoomReducedMotionPrefAppliesToOpenRoom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "swallow", swallowRoomScript),
	})
	app := newRoomApp(t, h, index, true)
	app.SetPrefsPath(path)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	focusHomeRow(t, app, HomeKindRoom, "swallow")
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "room", func(s Snapshot) bool { return s.Room.Open && !s.RoomPicker.Open })
	if app.Snapshot().ReducedMotion || app.Snapshot().Room.ReducedMotion {
		t.Fatal("reduced motion defaults off")
	}

	app.HandleCommand(CmdSettings, now)
	waitFor(t, app, "settings", func(s Snapshot) bool { return s.Settings.Open && !s.Settings.Loading })
	focusSettingsRow(t, app, "reduced-motion", now)
	if app.Snapshot().Settings.Rows[settingsRowReducedMotion].Value != "Off" {
		t.Fatalf("row = %#v", app.Snapshot().Settings.Rows[settingsRowReducedMotion])
	}
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if !snap.ReducedMotion || snap.Settings.Rows[settingsRowReducedMotion].Value != "On" {
		t.Fatalf("toggle = %#v motion=%v", snap.Settings.Rows[settingsRowReducedMotion], snap.ReducedMotion)
	}
	if !app.Snapshot().Room.Open || !app.Snapshot().Room.ReducedMotion {
		t.Fatal("open room must receive the reduced-motion flag")
	}
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ReducedMotion {
		t.Fatalf("prefs = %#v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"reduced_motion": true`) {
		t.Fatalf("json = %s", raw)
	}

	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Settings.Open {
		t.Fatal("settings should close")
	}
	if !app.Snapshot().Room.Open {
		t.Fatal("room should stay open")
	}
}

func TestOptionsNormalizedLoadsReducedMotionPref(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, ReducedMotion: true}); err != nil {
		t.Fatal(err)
	}
	opts := Options{PrefsPath: path}.normalized()
	if !opts.ReducedMotion {
		t.Fatalf("pref on = %#v", opts)
	}
	t.Setenv("FOGCAST_TENFOOT_REDUCED_MOTION", "1")
	opts = Options{PrefsPath: filepath.Join(t.TempDir(), "missing.json")}.normalized()
	if !opts.ReducedMotion || !opts.ReducedMotionSet {
		t.Fatalf("env = %#v", opts)
	}
}
