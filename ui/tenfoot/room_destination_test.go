package tenfoot

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

const destRoomScript = `
focus = "ready"
states = {
  checking = { kind = "game", label = "Donkey Kong", query = "Donkey Kong", platform = "arcade", resolving = true },
  missing = { kind = "game", label = "Picross", query = "Picross", platform = "gb", missing = true },
  choice = { kind = "game", label = "Super Mario Bros.", query = "Super Mario Bros.", platform = "nes", matches = {
    { id = "nes-smb-usa", title = "Super Mario Bros.", system = "nes", launchable = true, state = "available" },
    { id = "nes-smb-jp", title = "Super Mario Bros.", system = "nes", launchable = true, state = "available" },
  }},
  unavailable = { kind = "game", label = "Zelda", game_id = "snes-zelda", matches = { { id = "snes-zelda", title = "Zelda", system = "snes", launchable = false, state = "available" } } },
  ready = { kind = "game", label = "Mario", game_id = "snes-mario", system = "snes", matches = { { id = "snes-mario", title = "Mario", system = "snes", launchable = true, state = "available" } }, note = "Keep Yoshi." },
  island = { kind = "room", label = "Sports Island", room_id = "nested" },
}
function publish()
  destination.set(states[focus])
end
function load() publish() end
function on_input(cmd)
  if cmd == "right" then focus = "island" publish() return true end
  if cmd == "left" then focus = "ready" publish() return true end
  if cmd == "up" then focus = "checking" publish() return true end
  if cmd == "down" then focus = "missing" publish() return true end
  if cmd == "sort" then focus = "choice" publish() return true end
  if cmd == "filter_next" then focus = "unavailable" publish() return true end
  return false
end
function draw() gfx.rect(0, 0, 10, 10, "#fff") end
`

func TestRoomDestinationPanelAndConfirmStates(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	snap := waitFor(t, app, "room dest", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Availability == rooms.AvailReady
	})
	if snap.Room.Destination.Action != "Play" || snap.Room.Destination.Status != "Ready to play." {
		t.Fatalf("ready panel %+v", snap.Room.Destination)
	}
	if !strings.Contains(snap.HeaderHint(), "details") {
		t.Fatalf("hint %q", snap.HeaderHint())
	}

	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "ready launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != 1 || !strings.Contains(h.launches[0], "snes-mario") {
		t.Fatalf("launches %v", h.launches)
	}
	app.HandleCommand(CmdStop, now)
	waitFor(t, app, "unpark", func(s Snapshot) bool { return !s.GPUParked })

	app.HandleCommand(CmdUp, now)
	snap = app.Snapshot()
	focusBefore := snap.Room.Destination.Label
	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if snap.Room.Destination.Label != focusBefore {
		t.Fatalf("checking confirm moved selection %q → %q", focusBefore, snap.Room.Destination.Label)
	}
	if snap.Launch.Phase == "ok" && h.launchCount() > 1 {
		t.Fatal("checking confirm launched")
	}
	if !strings.Contains(snap.Status, "Matching") && snap.Room.Destination.Confirm() != rooms.ConfirmWait {
		t.Fatalf("checking confirm silent: status=%q dest=%+v", snap.Status, snap.Room.Destination)
	}

	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "missing opens library", func(s Snapshot) bool {
		return !s.Room.Open && strings.Contains(s.Query, "Picross")
	})
	if snap.Room.Open {
		t.Fatal("missing confirm must offer a library resolution")
	}

	app.HandleCommand(CmdHome, now)
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "room again", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == "overworld" })

	app.HandleCommand(CmdSortCycle, now)
	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if !snap.Room.Choice.Open || len(snap.Room.Choice.Rows) != 2 {
		t.Fatalf("needs choice overlay %+v", snap.Room.Choice)
	}
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "choice launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	app.HandleCommand(CmdStop, now)
	waitFor(t, app, "unpark2", func(s Snapshot) bool { return !s.GPUParked })

	app.HandleCommand(CmdFilterNext, now)
	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if !snap.Detail.Open {
		t.Fatalf("unavailable confirm must open details, got status=%q", snap.Status)
	}
	if snap.Launch.Phase == "ok" && strings.Contains(strings.Join(h.launches, " "), "zelda") {
		t.Fatal("unavailable launched")
	}
	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Detail.Open || !app.Snapshot().Room.Open {
		t.Fatal("Esc/B must close details and keep the room")
	}

	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "nested from dest", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == "nested" })
	app.HandleCommand(CmdBack, now)
	snap = waitFor(t, app, "parent", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == "overworld" })
	if snap.RoomPicker.Open {
		t.Fatal("back from nested must restore the parent room, not the picker")
	}
}

func TestRoomDetailsOpensSharedPanelAndKeepsContext(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "ready dest", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Availability == rooms.AvailReady
	})

	app.HandleCommand(CmdDetails, now)
	snap := app.Snapshot()
	if !snap.Detail.Open || snap.FocusDetail.Title != "Mario" || !snap.Room.Open {
		t.Fatalf("details %+v room=%v title=%q", snap.Detail, snap.Room.Open, snap.FocusDetail.Title)
	}
	if snap.Room.Destination.Note != "Keep Yoshi." || snap.Room.Destination.CuratorAttribution() == "" {
		t.Fatalf("curator note %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "play from details", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	app.HandleCommand(CmdStop, now)
	waitFor(t, app, "unpark", func(s Snapshot) bool { return !s.GPUParked })

	app.HandleCommand(CmdSearch, now)
	if !app.Snapshot().Detail.Open || !app.Snapshot().Room.Open {
		t.Fatal("Y / search in a room must open Details, not leave")
	}
	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Detail.Open || !app.Snapshot().Room.Open {
		t.Fatal("B closes Details and keeps the room")
	}

	app.HandleCommand(CmdSettings, now)
	if !app.Snapshot().Settings.Open || !app.Snapshot().Room.Open {
		t.Fatal("system menu must still open over a room")
	}
	app.HandleCommand(CmdBack, now)
}

func TestRoomAttractStaysOffWhileFocused(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{testRoomPack(t, "overworld", destRoomScript)})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "room", func(s Snapshot) bool { return s.Room.Open })
	app.mu.Lock()
	blocked := app.attractBlockedLocked()
	app.mu.Unlock()
	if !blocked {
		t.Fatal("attract must stay off while a room is the focused surface")
	}
}
