package tenfoot

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

const settingsActionScript = `
function load()
  destination.set{ kind = "action", action = "settings", label = "Settings" }
end
function draw()
  gfx.rect(0, 0, 10, 10, "#fff")
end
`

func openSettingsActionRoom(t *testing.T, h *roomHost) *App {
	t.Helper()
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "utils", settingsActionScript),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	focusHomeRow(t, app, HomeKindRoom, "utils")
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "settings action", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "utils" && s.Room.Destination.Kind == rooms.KindAction
	})
	return app
}

func TestLauncherActionConfirmOpensSettings(t *testing.T) {
	h := newRoomHost(t)
	app := openSettingsActionRoom(t, h)
	now := time.Now()
	snap := app.Snapshot()
	if snap.Room.Destination.LauncherAction != "settings" || snap.Room.Destination.Confirm() != rooms.ConfirmLauncherAction || snap.Room.Destination.Availability != rooms.AvailReady {
		t.Fatalf("dest %+v", snap.Room.Destination)
	}
	if snap.Room.Destination.Status != "Open settings." || snap.Room.Destination.Action != "Open settings." {
		t.Fatalf("copy %+v", snap.Room.Destination)
	}

	app.HandleCommand(CmdDetails, now)
	snap = app.Snapshot()
	if snap.Settings.Open || snap.Detail.Open {
		t.Fatalf("details must not run the action: settings=%v detail=%v", snap.Settings.Open, snap.Detail.Open)
	}
	if snap.Status != "Open settings." {
		t.Fatalf("details status %q", snap.Status)
	}

	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if !snap.Settings.Open || !snap.Room.Open {
		t.Fatalf("confirm should open settings over the room: settings=%v room=%v", snap.Settings.Open, snap.Room.Open)
	}
	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Settings.Open || !app.Snapshot().Room.Open {
		t.Fatal("back should close settings and keep the room")
	}
}

func TestLauncherActionRefusesWhileSessionActive(t *testing.T) {
	h := newRoomHost(t)
	app := openSettingsActionRoom(t, h)
	now := time.Now()
	h.mu.Lock()
	h.state = "active"
	h.mu.Unlock()
	waitFor(t, app, "session active", func(s Snapshot) bool {
		return s.Session.State == "active" || s.GPUParked
	})
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if snap.Settings.Open {
		t.Fatal("settings must stay closed while the session is active")
	}
	if !strings.Contains(snap.Status, "Settings are unavailable during play.") {
		t.Fatalf("status %q", snap.Status)
	}
	if snap.Room.ID != "utils" {
		t.Fatalf("room changed %+v", snap.Room)
	}
}

func TestLauncherActionUnknownIDNoops(t *testing.T) {
	h := newRoomHost(t)
	app := newRoomApp(t, h, rooms.NewIndex(nil), false)
	app.mu.Lock()
	app.runLauncherActionLocked("shell")
	status := app.status
	open := app.settingsOpen
	app.mu.Unlock()
	if open {
		t.Fatal("unknown action opened settings")
	}
	if status != "That action is not available." || strings.Contains(status, "shell") {
		t.Fatalf("status %q", status)
	}
	ev := waitForUIPost(t, h, "ui.launcher_action")
	if ev.Detail["action"] != "shell" || ev.Detail["result"] != "rejected" {
		t.Fatalf("event %+v", ev)
	}
	if ev.Detail["action"] != "" && strings.Contains(app.Snapshot().Status, ev.Detail["action"]) {
		t.Fatalf("tv status leaked the id %q", app.Snapshot().Status)
	}
}
