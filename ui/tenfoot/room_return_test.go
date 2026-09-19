package tenfoot

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

const nestedPlayRoomScript = `
resumed = 0
function load()
  destination.set{ kind = "game", label = "Kart", game_id = "snes-mario", system = "snes", matches = {
    { id = "snes-mario", title = "Kart", system = "snes", launchable = true, state = "available" },
  }}
end
function on_resume()
  resumed = resumed + 1
  -- Returning from play must ignore this: stay in the nested room (§6).
  rooms.back()
end
function draw() gfx.rect(0, 0, 10, 10, '#fff') end
`

func TestRoomStopRestoresSameLocation(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", nestedPlayRoomScript),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	snap := openOverworldRoom(t, app)
	focus := snap.Room.Destination.Label
	if focus == "" || snap.Room.ID != "overworld" || len(snap.Room.Parents) != 0 {
		t.Fatalf("start %+v", snap.Room)
	}

	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "launch overlay then park", func(s Snapshot) bool {
		return s.GPUParked && s.Launch.Phase == "ok"
	})
	if launchOverlayCopy(app.Snapshot()).Visible {
		t.Fatal("now-playing must own the screen while parked")
	}

	app.HandleCommand(CmdStop, now)
	snap = waitFor(t, app, "return to room", func(s Snapshot) bool {
		return !s.GPUParked && s.Room.Open && s.Room.ID == "overworld"
	})
	if snap.Room.Destination.Label != focus {
		t.Fatalf("return moved location %q → %q", focus, snap.Room.Destination.Label)
	}
	if len(snap.Room.Parents) != 0 || snap.RoomPicker.Open {
		t.Fatalf("return left the root room: parents=%v picker=%v", snap.Room.Parents, snap.RoomPicker.Open)
	}
	if snap.Room.Destination.History.Completed || snap.Room.Destination.History.Line() == "Completed" {
		t.Fatalf("return claimed Completed: %+v", snap.Room.Destination.History)
	}
	if launchOverlayCopy(snap).Visible || snap.Launch.Phase == "error" || snap.Launch.Phase == "host" {
		t.Fatalf("return left launch overlay: phase=%q copy=%+v", snap.Launch.Phase, launchOverlayCopy(snap))
	}
	if strings.Contains(snap.NowPlayingLine(), "Completed") {
		t.Fatalf("return chrome %q", snap.NowPlayingLine())
	}
}

func TestNestedRoomPlayReturnKeepsParentStack(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", nestedPlayRoomScript),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	openOverworldRoom(t, app)
	app.HandleCommand(CmdRight, now)
	snap := app.Snapshot()
	if snap.Room.Destination.Kind != rooms.KindRoom || snap.Room.Destination.Label != "Sports Island" {
		t.Fatalf("island dest %+v", snap.Room.Destination)
	}
	parentFocus := snap.Room.Destination.Label

	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "nested room", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "nested" && s.Room.Destination.Availability == rooms.AvailReady
	})
	if len(snap.Room.Parents) != 1 || snap.Room.Parents[0] != "overworld" {
		t.Fatalf("nested parents %+v", snap.Room.Parents)
	}
	nestedFocus := snap.Room.Destination.Label

	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "nested launch", func(s Snapshot) bool {
		return s.GPUParked && s.Launch.Phase == "ok"
	})
	app.HandleCommand(CmdStop, now)
	snap = waitFor(t, app, "nested return", func(s Snapshot) bool {
		return !s.GPUParked && s.Room.Open
	})
	if snap.Room.ID != "nested" {
		t.Fatalf("play return left nested room: id=%q", snap.Room.ID)
	}
	if len(snap.Room.Parents) != 1 || snap.Room.Parents[0] != "overworld" {
		t.Fatalf("play return popped parent stack: %+v", snap.Room.Parents)
	}
	if snap.Room.Destination.Label != nestedFocus {
		t.Fatalf("nested return moved location %q → %q", nestedFocus, snap.Room.Destination.Label)
	}
	app.mu.Lock()
	resumed := luaGlobalNumber(app, "resumed")
	app.mu.Unlock()
	if resumed < 1 {
		t.Fatalf("nested on_resume did not run: %v", resumed)
	}

	app.HandleCommand(CmdBack, now)
	snap = waitFor(t, app, "parent after nested back", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "overworld"
	})
	if snap.RoomPicker.Open || len(snap.Room.Parents) != 0 {
		t.Fatalf("Back from nested must restore the parent room: picker=%v parents=%v", snap.RoomPicker.Open, snap.Room.Parents)
	}
	if snap.Room.Destination.Label != parentFocus {
		t.Fatalf("Back from nested moved parent location %q → %q", parentFocus, snap.Room.Destination.Label)
	}
}

func TestRoomSaveFailedStaysVisibleUntilRetryStop(t *testing.T) {
	h := newRoomHost(t)
	h.mu.Lock()
	h.stopStatus = 500
	h.stopBody = `{"error":{"code":"SAVE_FAILED","message":"cartridge save could not be written"}}`
	h.mu.Unlock()
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", nestedPlayRoomScript),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	snap := openOverworldRoom(t, app)
	focus := snap.Room.Destination.Label

	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "parked play", func(s Snapshot) bool {
		return s.GPUParked && s.Session.State == "active"
	})
	app.HandleCommand(CmdStop, now)
	snap = waitFor(t, app, "save failed chrome", func(s Snapshot) bool {
		return s.Session.RetryStop && s.GPUParked
	})
	line := snap.NowPlayingLine()
	if nowPlayingHeading(snap) != nowPlayingHeadingSave {
		t.Fatalf("heading %q line %q", nowPlayingHeading(snap), line)
	}
	if !strings.Contains(line, "Save failed") {
		t.Fatalf("failed save chrome %q", line)
	}
	if strings.Contains(line, "Now playing") || strings.Contains(line, "Completed") || strings.Contains(line, "Played") {
		t.Fatalf("failed save looked like success %q", line)
	}
	if !snap.Room.Open || snap.Room.Destination.Label != focus {
		t.Fatalf("failed save dropped room: open=%v dest=%q", snap.Room.Open, snap.Room.Destination.Label)
	}
	if snap.Room.Destination.History.Completed || snap.Room.Destination.History.Line() == "Completed" {
		t.Fatalf("failed save claimed Completed: %+v", snap.Room.Destination.History)
	}
	if launchOverlayCopy(snap).Visible {
		t.Fatal("launch overlay must not replace failed-save chrome")
	}
	if snap.Session.Chrome != sessionChromeFailed {
		t.Fatalf("chrome = %q", snap.Session.Chrome)
	}

	app.HandleCommand(CmdSelect, now)
	if h.launchCount() != 1 {
		t.Fatalf("Confirm during save failure launched again: %v", h.launches)
	}
	app.HandleCommand(CmdRight, now)
	if app.Snapshot().Room.Destination.Label != focus {
		t.Fatal("direction during failed save moved the selected location")
	}

	h.mu.Lock()
	h.stopStatus = 0
	h.stopBody = ""
	h.mu.Unlock()
	app.HandleCommand(CmdStop, now)
	snap = waitFor(t, app, "retry stop restores room", func(s Snapshot) bool {
		return !s.GPUParked && !s.Session.RetryStop && s.Room.Open
	})
	if snap.Room.ID != "overworld" || snap.Room.Destination.Label != focus {
		t.Fatalf("retry Stop lost location: id=%q dest=%q", snap.Room.ID, snap.Room.Destination.Label)
	}
	if snap.Room.Destination.History.Completed || snap.Room.Destination.History.Line() == "Completed" {
		t.Fatalf("retry Stop claimed Completed: %+v", snap.Room.Destination.History)
	}
	if launchOverlayCopy(snap).Visible {
		t.Fatal("retry Stop must not reopen the launch overlay")
	}
}

func TestNowPlayingLineSaveFailedIsNotSuccess(t *testing.T) {
	t.Parallel()
	play := Snapshot{
		GPUParked: true,
		Session:   SessionSnapshot{State: "active", Chrome: "active", Title: "Mario", GameID: "snes-mario"},
	}
	if got := play.NowPlayingLine(); !strings.Contains(got, "Now playing") || strings.Contains(got, "Save failed") {
		t.Fatalf("active %q", got)
	}

	save := Snapshot{
		GPUParked: true,
		Status:    "host stop 500 SAVE_FAILED: cartridge save could not be written",
		Session: SessionSnapshot{
			State:     "active",
			Chrome:    sessionChromeFailed,
			Title:     "Mario",
			GameID:    "snes-mario",
			RetryStop: true,
			RetryHint: retryStopHint,
			RetryCode: "SAVE_FAILED",
		},
	}
	got := save.NowPlayingLine()
	if nowPlayingHeading(save) != nowPlayingHeadingSave || !strings.Contains(got, "Save failed") {
		t.Fatalf("save heading %q line %q", nowPlayingHeading(save), got)
	}
	if strings.Contains(got, "Now playing") || strings.Contains(got, "Completed") || strings.Contains(got, "Played") {
		t.Fatalf("save looked like success %q", got)
	}

	busy := Snapshot{
		GPUParked: true,
		Status:    "host stop 500 TARGET_BUSY: target is busy",
		Session: SessionSnapshot{
			State:     "active",
			Chrome:    sessionChromeFailed,
			Title:     "Mario",
			RetryStop: true,
			RetryHint: "retry Stop  ·  target is busy",
			RetryCode: "TARGET_BUSY",
		},
	}
	got = busy.NowPlayingLine()
	if nowPlayingHeading(busy) != nowPlayingHeadingStop || !strings.Contains(got, "Stop failed") {
		t.Fatalf("busy heading %q line %q", nowPlayingHeading(busy), got)
	}
	if strings.Contains(got, "Now playing") || strings.Contains(got, "Save failed") || strings.Contains(got, "Completed") {
		t.Fatalf("busy chrome %q", got)
	}
}
