package tenfoot

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
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
function draw()
  gfx.rect(0, 0, 10, 10, "#fff")
  gfx.hit("map", 0, 0, room.width, room.height)
end
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

func TestRoomDetailsLoadsCoverArtworkOffGrid(t *testing.T) {
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

	app.HandleCommand(CmdSortCycle, now)
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if !snap.Room.Choice.Open || len(snap.Room.Choice.Rows) == 0 {
		t.Fatalf("choice overlay %+v", snap.Room.Choice)
	}
	offGridID := snap.Room.Choice.Rows[0].ID
	for _, game := range snap.Games {
		if game.ID == offGridID {
			t.Fatalf("%s is already on the browse grid", offGridID)
		}
	}

	app.HandleCommand(CmdDetails, now)
	snap = app.Snapshot()
	if !snap.Detail.Open || snap.FocusDetail.Title == "" || !snap.Room.Open {
		t.Fatalf("details %+v room=%v title=%q", snap.Detail, snap.Room.Open, snap.FocusDetail.Title)
	}
	waitFor(t, app, "off-grid cover", func(s Snapshot) bool {
		return s.Detail.Open && s.Covers[offGridID] != nil
	})
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

const playHistoryRoomScript = `
focus = "played"
states = {
  played = { kind = "game", label = "Mario", game_id = "snes-mario", system = "snes", matches = {
    { id = "snes-mario", title = "Mario", system = "snes", launchable = true, state = "available", play_count = 3, last_played_at = 99 },
  }},
  unplayed = { kind = "game", label = "Yoshi", game_id = "snes-yoshi", system = "snes", matches = {
    { id = "snes-yoshi", title = "Yoshi", system = "snes", launchable = true, state = "available" },
  }},
  resume = { kind = "game", label = "Mario", game_id = "snes-mario", system = "snes", matches = {
    { id = "snes-mario", title = "Mario", system = "snes", launchable = true, state = "available", play_count = 3, last_played_at = 99 },
  }},
}
function publish()
  destination.set(states[focus])
end
function load() publish() end
function on_input(cmd)
  if cmd == "right" then focus = "unplayed" publish() return true end
  if cmd == "left" then focus = "played" publish() return true end
  return false
end
function on_resume()
  -- Same facts as after a launch return: Played stays Played, never Completed.
  focus = "resume"
  publish()
end
function draw() gfx.rect(0, 0, 10, 10, "#fff") end
`

func TestRoomDestinationPlayedIsNotCompleted(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{testRoomPack(t, "history", playHistoryRoomScript)})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	snap := waitFor(t, app, "played dest", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.History.Played
	})
	if snap.Room.Destination.History.Completed || snap.Room.Destination.History.Line() != "Played" {
		t.Fatalf("played dest claimed Completed: %+v", snap.Room.Destination.History)
	}
	if snap.Room.Destination.Status == "Completed" || strings.Contains(snap.Room.Destination.Action, "Completed") {
		t.Fatalf("availability copy used Completed: %+v", snap.Room.Destination)
	}

	app.HandleCommand(CmdDetails, now)
	snap = app.Snapshot()
	if !snap.Detail.Open || !snap.Room.Open {
		t.Fatalf("details %+v room=%v", snap.Detail, snap.Room.Open)
	}
	if snap.Room.Destination.History.Completed || snap.Room.Destination.History.Line() != "Played" {
		t.Fatalf("details claimed Completed: %+v", snap.Room.Destination.History)
	}
	app.HandleCommand(CmdBack, now)
	waitFor(t, app, "details closed", func(s Snapshot) bool { return s.Room.Open && !s.Detail.Open })

	app.HandleCommand(CmdRight, now)
	snap = app.Snapshot()
	if snap.Room.Destination.History.Played || snap.Room.Destination.History.Completed || snap.Room.Destination.History.Line() != "" {
		t.Fatalf("unplayed dest %+v", snap.Room.Destination.History)
	}

	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	app.HandleCommand(CmdStop, now)
	waitFor(t, app, "unpark", func(s Snapshot) bool { return !s.GPUParked })
	app.mu.Lock()
	if app.room != nil {
		app.room.Resume()
	}
	app.mu.Unlock()
	snap = waitFor(t, app, "resume dest", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Label == "Mario"
	})
	if snap.Room.Destination.History.Completed || snap.Room.Destination.History.Line() == "Completed" {
		t.Fatalf("launch return claimed Completed: %+v", snap.Room.Destination.History)
	}
	if !snap.Room.Destination.History.Played || snap.Room.Destination.History.Line() != "Played" {
		t.Fatalf("launch return should stay Played: %+v", snap.Room.Destination.History)
	}
}

func TestRoomDestinationStripPointerOpensDetails(t *testing.T) {
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
	snap := waitFor(t, app, "ready dest", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Availability == rooms.AvailReady
	})
	pane, ok := roomDestGeom(snap)
	if !ok {
		t.Fatal("compact destination strip missing")
	}
	cx, cy := pane.X+pane.W/2, pane.Y+pane.H/2
	if got := HitTest(snap, cx, cy); got.Kind != PointerRoomDestination {
		t.Fatalf("strip hit %+v", got)
	}

	before := h.launchCount()
	app.PointerClick(cx, cy, now)
	snap = app.Snapshot()
	if !snap.Detail.Open || !snap.Room.Open || snap.FocusDetail.Title != "Mario" {
		t.Fatalf("strip details %+v room=%v title=%q", snap.Detail, snap.Room.Open, snap.FocusDetail.Title)
	}
	if h.launchCount() != before || snap.Launch.Phase == "ok" {
		t.Fatalf("strip click must not Confirm: launches=%v phase=%s", h.launches, snap.Launch.Phase)
	}
	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Detail.Open || !app.Snapshot().Room.Open {
		t.Fatal("Back must close Details and keep the room")
	}

	app.HandleCommand(CmdDetails, now)
	if !app.Snapshot().Detail.Open || !app.Snapshot().Room.Open {
		t.Fatal("keyboard Details must still open the shared panel")
	}
	app.HandleCommand(CmdBack, now)

	app.HandleCommand(CmdRight, now)
	snap = app.Snapshot()
	if snap.Room.Destination.Kind != rooms.KindRoom {
		t.Fatalf("island dest %+v", snap.Room.Destination)
	}
	pane, ok = roomDestGeom(snap)
	if !ok {
		t.Fatal("room dest strip missing")
	}
	app.PointerClick(pane.X+pane.W/2, pane.Y+pane.H/2, now)
	snap = app.Snapshot()
	if snap.Room.ID != "overworld" || snap.RoomPicker.Open {
		t.Fatalf("strip click must not enter a room dest: %+v", snap.Room)
	}

	app.HandleCommand(CmdLeft, now)
	waitFor(t, app, "ready again", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Availability == rooms.AvailReady
	})
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "confirm launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != before+1 {
		t.Fatalf("Confirm must still launch, launches=%v", h.launches)
	}
}

func enterOverworldChoice(t *testing.T, app *App, now time.Time) {
	t.Helper()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "room dest", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "overworld"
	})
	app.HandleCommand(CmdSortCycle, now)
}

func TestRoomEditionPreferencePersistsAndSkipsReask(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	enterOverworldChoice(t, app, now)

	snap := app.Snapshot()
	if snap.Room.Destination.Availability != rooms.AvailNeedsChoice || snap.Room.Destination.Confirm() != rooms.ConfirmChoose {
		t.Fatalf("unsaved dest %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if !snap.Room.Choice.Open || len(snap.Room.Choice.Rows) != 2 {
		t.Fatalf("must force a choice %+v", snap.Room.Choice)
	}
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "choice launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if !strings.Contains(strings.Join(h.launches, " "), "nes-smb-usa") {
		t.Fatalf("launches %v", h.launches)
	}
	waitForPrefs(t, h, 1)
	app.HandleCommand(CmdStop, now)
	waitFor(t, app, "unpark", func(s Snapshot) bool { return !s.GPUParked })
	app.Stop()

	revisit := newRoomApp(t, h, index, true)
	enterOverworldChoice(t, revisit, now)
	snap = waitFor(t, revisit, "preferred dest", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Availability == rooms.AvailReady && s.Room.Destination.GameID == "nes-smb-usa"
	})
	if snap.Room.Choice.Open || snap.Room.Destination.Confirm() != rooms.ConfirmLaunch || snap.Room.Destination.Action != "Play" {
		t.Fatalf("revisit must skip re-ask %+v choice=%+v", snap.Room.Destination, snap.Room.Choice)
	}

	revisit.HandleCommand(CmdDetails, now)
	snap = revisit.Snapshot()
	if !snap.Detail.Open || snap.Room.Choice.Open || snap.FocusDetail.Title == "" || !snap.Room.Open {
		t.Fatalf("details with preference %+v choice=%+v room=%v title=%q", snap.Detail, snap.Room.Choice, snap.Room.Open, snap.FocusDetail.Title)
	}
	revisit.HandleCommand(CmdBack, now)
	if revisit.Snapshot().Detail.Open || !revisit.Snapshot().Room.Open {
		t.Fatal("Back must close Details and keep the room")
	}
	revisit.HandleCommand(CmdSettings, now)
	if !revisit.Snapshot().Settings.Open || !revisit.Snapshot().Room.Open {
		t.Fatal("system menu must still open over a room")
	}
	revisit.HandleCommand(CmdBack, now)

	before := h.launchCount()
	revisit.HandleCommand(CmdSelect, now)
	waitFor(t, revisit, "preferred launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != before+1 {
		t.Fatalf("confirm launches=%v", h.launches)
	}
	if !strings.Contains(h.launches[len(h.launches)-1], "nes-smb-usa") {
		t.Fatalf("revisit launch %v", h.launches)
	}
}

func TestForeignLeaseShowsInUseAndDoesNotLaunch(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "ready", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Availability == rooms.AvailReady
	})

	app.mu.Lock()
	app.healthHave = true
	app.health.Connection = hostclient.TargetConnection{State: "busy", Owner: "other-shell", Message: "The kit is held by another session."}
	app.mu.Unlock()
	snap := app.Snapshot()
	if snap.Room.Destination.Availability != rooms.AvailUnavailable || snap.Room.Destination.Confirm() != rooms.ConfirmExplain {
		t.Fatalf("foreign destination %+v confirm=%v", snap.Room.Destination, snap.Room.Destination.Confirm())
	}
	if !strings.Contains(snap.Room.Destination.Status, "in use") || snap.Room.Destination.Action != "Do not take the lease." {
		t.Fatalf("in-use copy %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, now)
	if h.launchCount() != 0 || app.Snapshot().Launch.Phase == "ok" || app.Snapshot().Launch.Phase == "launching" {
		t.Fatalf("foreign confirm launched %+v count=%d", app.Snapshot().Launch, h.launchCount())
	}

	app.mu.Lock()
	app.games = []hostclient.Game{availableGame("snes-mario", "Mario", "snes")}
	app.startLaunchGameLocked(app.games[0])
	phase := app.launch.Phase
	app.mu.Unlock()
	if phase == "launching" || phase == "ok" || h.launchCount() != 0 {
		t.Fatalf("library play stole the kit phase=%s launches=%d", phase, h.launchCount())
	}

	app.mu.Lock()
	app.health.Connection = hostclient.TargetConnection{State: "ready", Address: "192.0.2.10:8182", TargetID: "01234567-89ab-cdef-0123-456789abcdef"}
	app.launch = LaunchSnapshot{}
	app.mu.Unlock()
	snap = app.Snapshot()
	if snap.Room.Destination.Availability != rooms.AvailReady || snap.Room.Destination.Confirm() != rooms.ConfirmLaunch || snap.Room.Destination.Status != "Ready to play." {
		t.Fatalf("same-shell retained lease %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "owned launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != 1 {
		t.Fatalf("owned confirm launches=%v", h.launches)
	}
}

func TestForeignLeaseHostOnlyStillLaunches(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "ready", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Availability == rooms.AvailReady
	})

	app.mu.Lock()
	app.healthHave = true
	app.health.Connection = hostclient.TargetConnection{State: "busy", Owner: "other-shell"}
	hostGame := availableGame("snes-mario", "Mario", "snes")
	hostGame.Execution = hostclient.ExecutionHostOnly
	app.room.RefreshCachedGames([]hostclient.Game{hostGame})
	app.mu.Unlock()
	snap := app.Snapshot()
	if snap.Room.Destination.Availability != rooms.AvailReady || snap.Room.Destination.Confirm() != rooms.ConfirmLaunch {
		t.Fatalf("host-only destination %+v confirm=%v", snap.Room.Destination, snap.Room.Destination.Confirm())
	}
	if strings.Contains(snap.Room.Destination.Status, "in use") {
		t.Fatalf("host-only shown in use %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "host launch", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != 1 || !strings.Contains(h.launches[0], "snes-mario") {
		t.Fatalf("host-only confirm launches=%v", h.launches)
	}

	app.mu.Lock()
	app.launch = LaunchSnapshot{}
	fpga := availableGame("snes-zelda", "Zelda", "snes")
	app.games = []hostclient.Game{fpga}
	app.startLaunchGameLocked(app.games[0])
	phase := app.launch.Phase
	app.mu.Unlock()
	if phase == "launching" || phase == "ok" || h.launchCount() != 1 {
		t.Fatalf("busy FPGA library play phase=%s launches=%d", phase, h.launchCount())
	}
}
