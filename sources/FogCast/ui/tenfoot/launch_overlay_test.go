package tenfoot

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestLaunchOverlayCopyUsesRealPhasesWithoutPercent(t *testing.T) {
	t.Parallel()
	grid := testDrawGrid()
	base := Snapshot{
		Grid:  grid,
		Room:  RoomSnapshot{Open: true, Destination: rooms.Destination{Label: "Donkey Kong"}},
		Launch: LaunchSnapshot{GameID: "coleco-dk", Phase: "launching", Message: "launching Donkey Kong"},
	}
	got := launchOverlayCopy(base)
	if !got.Visible || got.Failed || got.Heading != "Launching" || got.Title != "Donkey Kong" {
		t.Fatalf("busy overlay %+v", got)
	}
	if got.Phase != "launching Donkey Kong" || strings.Contains(got.Phase, "%") {
		t.Fatalf("invented progress %q", got.Phase)
	}
	if !strings.Contains(got.Hint, "launch in progress") {
		t.Fatalf("busy hint %q", got.Hint)
	}

	base.Session.Progress = "transferring cartridge"
	if phase := launchOverlayCopy(base).Phase; phase != "transferring cartridge" {
		t.Fatalf("session progress %q", phase)
	}

	base.Session.Progress = ""
	base.Launch = LaunchSnapshot{
		GameID:       "coleco-dk",
		Phase:        "host",
		Message:      "host launch 500 TRANSFER_FAILED: content transfer failed",
		HTTPStatus:   500,
		ErrorCode:    "TRANSFER_FAILED",
		ErrorMessage: "content transfer failed",
	}
	fail := launchOverlayCopy(base)
	if !fail.Visible || !fail.Failed || fail.Heading != "Launch failed" {
		t.Fatalf("fail overlay %+v", fail)
	}
	if !strings.Contains(fail.Reason, "TRANSFER_FAILED") || !strings.Contains(fail.Reason, "content transfer failed") {
		t.Fatalf("reason %q", fail.Reason)
	}
	if strings.Contains(fail.Reason, "%") || !strings.Contains(fail.Hint, "retry") || !strings.Contains(fail.Hint, "back to room") {
		t.Fatalf("fail copy %+v", fail)
	}

	parked := base
	parked.GPUParked = true
	if launchOverlayCopy(parked).Visible {
		t.Fatal("now-playing must own the screen once the session is parked")
	}
}

func TestDrawLaunchOverlayCoversRoomWithoutPercent(t *testing.T) {
	rec := gfx.NewRecorder()
	grid := testDrawGrid()
	snap := Snapshot{
		Grid: grid,
		Room: RoomSnapshot{
			Open:    true,
			OffsetX: grid.contentLeft(), OffsetY: grid.contentTop(),
			Width: grid.contentWidth(), Height: grid.contentHeight(),
			Destination: rooms.Destination{Label: "Mario", Action: "Play"},
		},
		Launch: LaunchSnapshot{GameID: "snes-mario", Phase: "launching", Message: "launching Mario"},
	}
	if _, ok := launchOverlayPanel(snap); !ok {
		t.Fatal("launch overlay panel missing")
	}
	if _, ok := roomDestGeom(snap); ok {
		t.Fatal("destination strip must hide under the launch overlay")
	}
	presentFrame(rec, snap, map[string]gpuTexture{}, map[string]gpuTexture{}, false)
	copy := launchOverlayCopy(snap)
	if strings.Contains(copy.Phase, "%") || strings.Contains(copy.Hint, "%") {
		t.Fatalf("percent in overlay copy %+v", copy)
	}
}

func openOverworldRoom(t *testing.T, app *App) Snapshot {
	t.Helper()
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	app.HandleCommand(CmdDown, now)
	app.HandleCommand(CmdSelect, now)
	return waitFor(t, app, "ready dest", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Availability == rooms.AvailReady
	})
}

func TestRoomConfirmShowsLaunchOverlayAndDropsDuplicatePost(t *testing.T) {
	h := newRoomHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	snap := openOverworldRoom(t, app)
	focus := snap.Room.Destination.Label

	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(gate)
			h.mu.Lock()
			h.launchGate = nil
			h.mu.Unlock()
		})
	}
	t.Cleanup(release)
	h.mu.Lock()
	h.launchGate = gate
	h.mu.Unlock()

	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	copy := launchOverlayCopy(snap)
	if snap.Launch.Phase != "launching" || !copy.Visible || copy.Failed {
		t.Fatalf("immediate overlay: phase=%q copy=%+v", snap.Launch.Phase, copy)
	}
	if copy.Phase == "" || strings.Contains(copy.Phase, "%") {
		t.Fatalf("honest phase %q", copy.Phase)
	}
	if !strings.Contains(snap.HeaderHint(), "launch in progress") {
		t.Fatalf("hint %q", snap.HeaderHint())
	}
	if h.launchCount() != 0 {
		t.Fatalf("POST completed before overlay: %v", h.launches)
	}

	app.HandleCommand(CmdSelect, now)
	app.HandleCommand(CmdRight, now)
	app.HandleCommand(CmdDetails, now)
	snap = app.Snapshot()
	if h.launchCount() != 0 {
		t.Fatalf("duplicate Confirm posted: %v", h.launches)
	}
	if snap.Room.Destination.Label != focus {
		t.Fatalf("in-flight input moved selection %q → %q", focus, snap.Room.Destination.Label)
	}
	if snap.Launch.Phase != "launching" || !snap.Room.Open {
		t.Fatalf("duplicate Confirm left overlay: phase=%q open=%v", snap.Launch.Phase, snap.Room.Open)
	}

	app.HandleCommand(CmdSettings, now)
	if !app.Snapshot().Settings.Open || !app.Snapshot().Room.Open {
		t.Fatal("system menu must stay reachable during launch")
	}
	app.HandleCommand(CmdBack, now)
	if app.Snapshot().Settings.Open {
		t.Fatal("Back closes settings")
	}

	release()
	waitFor(t, app, "launch ok", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != 1 || !strings.Contains(h.launches[0], "snes-mario") {
		t.Fatalf("launches %v", h.launches)
	}
}

func TestRoomLaunchFailureKeepsFocusAndOffersRetry(t *testing.T) {
	h := newRoomHost(t)
	h.mu.Lock()
	h.launchStatus = 500
	h.launchBody = `{"error":{"code":"TRANSFER_FAILED","message":"content transfer failed"}}`
	h.mu.Unlock()
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	snap := openOverworldRoom(t, app)
	focus := snap.Room.Destination.Label

	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "launch failed", func(s Snapshot) bool {
		copy := launchOverlayCopy(s)
		return copy.Visible && copy.Failed
	})
	copy := launchOverlayCopy(snap)
	if snap.Launch.Phase != "host" || snap.Launch.ErrorCode != "TRANSFER_FAILED" {
		t.Fatalf("launch %+v", snap.Launch)
	}
	if !strings.Contains(copy.Reason, "TRANSFER_FAILED") {
		t.Fatalf("reason %q", copy.Reason)
	}
	if snap.Room.Destination.Label != focus || !snap.Room.Open {
		t.Fatalf("failure cleared selection: dest=%q open=%v", snap.Room.Destination.Label, snap.Room.Open)
	}
	if !strings.Contains(snap.HeaderHint(), "retry") || !strings.Contains(snap.HeaderHint(), "back to room") {
		t.Fatalf("fail hint %q", snap.HeaderHint())
	}
	if _, ok := launchOverlayPanel(snap); !ok {
		t.Fatal("failure overlay missing")
	}
	if h.launchCount() != 1 {
		t.Fatalf("first POST count %d", h.launchCount())
	}

	app.HandleCommand(CmdRight, now)
	if app.Snapshot().Room.Destination.Label != focus {
		t.Fatal("direction during failure overlay moved the room location")
	}

	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "retry posted", func(s Snapshot) bool { return h.launchCount() >= 2 && launchOverlayCopy(s).Failed })
	if h.launchCount() != 2 {
		t.Fatalf("retry launches %v", h.launches)
	}
	if app.Snapshot().Room.Destination.Label != focus {
		t.Fatal("retry moved the selected location")
	}

	app.HandleCommand(CmdBack, now)
	snap = app.Snapshot()
	if launchOverlayCopy(snap).Visible || snap.Launch.Phase != "idle" {
		t.Fatalf("Back to room left overlay: phase=%q copy=%+v", snap.Launch.Phase, launchOverlayCopy(snap))
	}
	if !snap.Room.Open || snap.Room.Destination.Label != focus {
		t.Fatalf("Back to room lost focus: open=%v dest=%q", snap.Room.Open, snap.Room.Destination.Label)
	}

	h.mu.Lock()
	h.launchStatus = 0
	h.launchBody = ""
	h.mu.Unlock()
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "play after recovery", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != 3 {
		t.Fatalf("recovery launches %v", h.launches)
	}
}

func TestLaunchOverlayPointerRetryAndBackdropDismiss(t *testing.T) {
	h := newRoomHost(t)
	h.mu.Lock()
	h.launchStatus = 500
	h.launchBody = `{"error":{"code":"TRANSFER_FAILED","message":"content transfer failed"}}`
	h.mu.Unlock()
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "overworld", destRoomScript),
		testRoomPack(t, "nested", "function draw() gfx.rect(0,0,10,10,'#fff') end"),
	})
	app := newRoomApp(t, h, index, true)
	now := time.Now()
	snap := openOverworldRoom(t, app)
	focus := snap.Room.Destination.Label
	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "failed overlay", func(s Snapshot) bool { return launchOverlayCopy(s).Failed })
	panel, ok := launchOverlayPanel(snap)
	if !ok {
		t.Fatal("overlay panel")
	}
	if got := HitTest(snap, panel.X+panel.W/2, panel.Y+panel.H/2); got.Kind != PointerLaunchOverlay {
		t.Fatalf("overlay hit %+v", got)
	}
	if got := HitTest(snap, snap.Room.OffsetX+8, snap.Room.OffsetY+8); got.Kind != PointerBackdrop {
		t.Fatalf("map under overlay %+v", got)
	}

	app.PointerClick(panel.X+panel.W/2, panel.Y+panel.H/2, now)
	waitFor(t, app, "pointer retry", func(s Snapshot) bool {
		return h.launchCount() >= 2 && launchOverlayCopy(s).Failed
	})
	if app.Snapshot().Room.Destination.Label != focus {
		t.Fatal("pointer retry moved selection")
	}

	snap = app.Snapshot()
	app.PointerClick(snap.Room.OffsetX+8, snap.Room.OffsetY+8, now)
	snap = app.Snapshot()
	if launchOverlayCopy(snap).Visible || snap.Room.Destination.Label != focus || !snap.Room.Open {
		t.Fatalf("backdrop dismiss %+v dest=%q", launchOverlayCopy(snap), snap.Room.Destination.Label)
	}
}
