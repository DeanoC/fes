package tenfoot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const liveHardwareTestScript = `
focus = 1
local live = {id="host-zx81",game_id="fpga-zx81",target="dev",package_id=string.rep("a",64),generation="9"}
function load() end
function on_resume() rooms.back() end
function on_input(cmd)
  if cmd == "right" then focus = focus + 1 return true end
  if cmd == "select" then session.launch("fpga-zx81") return true end
  if cmd == "filter-next" or cmd == "filter_next" then session.open_tape(live) return true end
  if cmd == "sort" then session.stop(live) return true end
  return false
end
function on_activate(id)
  if id == "tape" then session.open_tape(live) end
  if id == "home" then rooms.home() end
end
function draw()
  gfx.clear("#122738")
  gfx.text("Hardware room", 20, 20, {size=24})
  gfx.rect(20, 80, 200, 60, "#b8cad4")
  gfx.hit("tape", 20, 80, 200, 60)
  gfx.hit("home", 240, 80, 120, 60)
end
`

func newLiveHardwareApp(t *testing.T, h *tapeHost) *App {
	t.Helper()
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{testRoomPack(t, hardwareRoomID, liveHardwareTestScript)}), "")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.mu.Lock()
	app.openRoomLocked(hardwareRoomID)
	app.mu.Unlock()
	app.HandleCommand(CmdRight, time.Now())
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "active hardware", func(s Snapshot) bool { return s.GPUParked && s.Session.LoadTape && s.Launch.Phase == "ok" })
	return app
}

func assertLiveHardwareIdentity(t *testing.T, app *App, h *tapeHost) {
	t.Helper()
	app.mu.Lock()
	session := app.session
	app.mu.Unlock()
	h.mu.Lock()
	launches := h.launches
	h.mu.Unlock()
	if session.ID != h.sessionID || session.State != "active" || session.CorePackage == nil || session.CorePackage.Generation != 9 || launches != 1 {
		t.Fatalf("live media changed session: %+v launches=%d", session, launches)
	}
}

func TestHardwareRoomCanReturnDuringPlayWithoutRelaunch(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.mu.Lock()
	original := app.room
	app.mu.Unlock()
	if !strings.Contains(app.Snapshot().HeaderHint(), "hardware room") {
		t.Fatalf("missing visible route: %q", app.Snapshot().HeaderHint())
	}
	app.HandleCommand(CmdHome, time.Now())
	app.Tick(time.Now())
	snap := app.Snapshot()
	if snap.GPUParked || !snap.Room.DuringPlay || snap.Room.ID != hardwareRoomID || snap.RoomPicker.Open {
		t.Fatalf("room did not resume: %+v", snap.Room)
	}
	app.mu.Lock()
	same := app.room == original
	focus := luaGlobalNumber(app, "focus")
	app.mu.Unlock()
	if !same || focus != 2 {
		t.Fatalf("resume lost room/focus: same=%v focus=%v", same, focus)
	}
	app.HandleCommand(CmdSelect, time.Now())
	assertLiveHardwareIdentity(t, app, h)
	app.HandleCommand(CmdBack, time.Now())
	if !app.Snapshot().GPUParked {
		t.Fatal("Back must return to the running session")
	}
	button, ok := hardwareRoomButton(app.Snapshot())
	if !ok || HitTest(app.Snapshot(), button.X+2, button.Y+2).Kind != PointerHardwareRoom {
		t.Fatal("hardware room pointer route is missing")
	}
	app.PointerClick(button.X+2, button.Y+2, time.Now())
	if !app.Snapshot().Room.DuringPlay {
		t.Fatal("pointer did not return to hardware room")
	}
	app.HandleCommand(CmdLayoutCycle, time.Now())
	if !app.Snapshot().GPUParked {
		t.Fatal("Select/View must return to session")
	}
	app.HandleCommand(CommandFromButton(ButtonBack), time.Now())
	if !app.Snapshot().Room.DuringPlay {
		t.Fatal("single controller Select/View must return to hardware room")
	}
	assertLiveHardwareIdentity(t, app, h)
}

func TestHardwareRoomOpensFromLibraryDuringPlay(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.mu.Lock()
	app.closeAllRoomsLocked()
	app.mu.Unlock()
	app.HandleCommand(CmdHome, time.Now())
	if snap := app.Snapshot(); !snap.Room.DuringPlay || snap.Room.ID != hardwareRoomID || snap.GPUParked {
		t.Fatalf("library launch cannot open hardware room: %+v", snap.Room)
	}
	assertLiveHardwareIdentity(t, app, h)
}

func TestHardwareRoomHomeKeepsActiveRoomForIdleOnlyChoices(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.HandleCommand(CmdHome, time.Now())
	app.mu.Lock()
	original := app.room
	app.homeRecents = []hostclient.Game{h.zx81()}
	app.mu.Unlock()
	for _, kind := range []string{HomeKindLibrary, HomeKindRecents, HomeKindRecent} {
		app.HandleCommand(CmdHome, time.Now())
		app.mu.Lock()
		rows := app.roomPickerRowsLocked()
		found := false
		for _, row := range rows {
			if row.Kind == kind {
				found = true
				if !row.Invalid {
					t.Errorf("%s must explain that Stop is needed", kind)
				}
				app.activateHomeRowLocked(row)
				break
			}
		}
		same := app.room == original
		app.mu.Unlock()
		if !found || !same || !app.Snapshot().Room.DuringPlay {
			t.Fatalf("%s discarded live room", kind)
		}
		app.HandleCommand(CmdBack, time.Now())
		if app.Snapshot().RoomPicker.Open || !app.Snapshot().Room.DuringPlay {
			t.Fatal("Home Back should return to room")
		}
		assertLiveHardwareIdentity(t, app, h)
	}
	app.mu.Lock()
	app.openLibraryFromRoomLocked(rooms.Action{Kind: rooms.ActionOpenLibrary})
	same := app.room == original
	app.mu.Unlock()
	if !same || !app.Snapshot().Room.DuringPlay {
		t.Fatal("script Library discarded live room")
	}
	app.HandleCommand(CmdBack, time.Now())
	assertLiveHardwareIdentity(t, app, h)
	if !app.Snapshot().GPUParked {
		t.Fatal("Back should resume the same session")
	}
}

func TestHardwareRoomTapeUsesExistingPickerAndKeepsFocus(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.HandleCommand(CmdHome, time.Now())
	app.Tick(time.Now())
	app.HandleCommand(CmdFilterNext, time.Now())
	snap := app.Snapshot()
	if !snap.TapePicker.Open || !snap.Room.DuringPlay {
		t.Fatalf("room did not open tape picker: %+v", snap.TapePicker)
	}
	panel, ok := tapePickerPanel(snap)
	if !ok {
		t.Fatal("missing tape picker")
	}
	x, y := panel.X+20, panel.rowY(0)+panel.RowH/2
	if hit := HitTest(snap, x, y); hit.Kind != PointerTapePicker {
		t.Fatalf("tape picker did not receive pointer: %+v", hit)
	}
	app.PointerClick(x, y, time.Now())
	waitFor(t, app, "ejected in room", func(s Snapshot) bool { return !s.TapePicker.Open && strings.Contains(s.Status, "Tape ejected") })
	app.mu.Lock()
	focus := luaGlobalNumber(app, "focus")
	app.mu.Unlock()
	if !app.Snapshot().Room.DuringPlay || focus != 2 || h.clearCount() != 1 {
		t.Fatalf("tape return lost room: focus=%v clear=%d room=%+v", focus, h.clearCount(), app.Snapshot().Room)
	}
	assertLiveHardwareIdentity(t, app, h)
}

func TestHardwareRoomTapeFailuresKeepSessionAndNavigation(t *testing.T) {
	for _, tc := range []struct {
		name        string
		busy        int
		unavailable bool
	}{
		{name: "busy", busy: 4},
		{name: "unavailable", unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTapeHost(t)
			h.clearBusyLeft, h.clearUnavailable = tc.busy, tc.unavailable
			app := newLiveHardwareApp(t, h)
			app.HandleCommand(CmdHome, time.Now())
			app.HandleCommand(CmdFilterNext, time.Now())
			app.HandleCommand(CmdSelect, time.Now())
			snap := waitFor(t, app, "live tape rejection", func(s Snapshot) bool { return s.TapePicker.Open && !s.TapePicker.Busy && h.clearCount() == 3 })
			if !strings.Contains(strings.ToLower(snap.TapePicker.Status), tc.name) {
				t.Fatalf("missing failure: %q", snap.TapePicker.Status)
			}
			app.HandleCommand(CmdBack, time.Now())
			if snap := app.Snapshot(); snap.TapePicker.Open || !snap.Room.DuringPlay || snap.GPUParked {
				t.Fatalf("failure did not return to room: %+v", snap.Room)
			}
			assertLiveHardwareIdentity(t, app, h)
		})
	}
}

func TestParkedTapePickerIsPaintedAndPointerAccessible(t *testing.T) {
	h := newTapeHost(t)
	app := newLiveHardwareApp(t, h)
	app.HandleCommand(CmdSearch, time.Now())
	snap := app.Snapshot()
	panel, ok := tapePickerPanel(snap)
	if !snap.GPUParked || !ok || HitTest(snap, panel.X+10, panel.rowY(0)+10).Kind != PointerTapePicker {
		t.Fatal("parked tape picker is not pointer accessible")
	}
	rec := gfx.NewRecorder()
	labels := map[string]gpuTexture{}
	presentFrame(rec, snap, map[string]gpuTexture{}, labels, false)
	painted := false
	for key := range labels {
		painted = painted || strings.HasPrefix(key, "tape-title\x1f")
	}
	if !painted {
		t.Fatal("parked tape controls not painted")
	}
}

func TestHardwareRoomEntryReleasesHeldKeysBeforeMenu(t *testing.T) {
	var mu sync.Mutex
	var events []remoteinput.Event
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/input/event" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Event remoteinput.Event `json:"event"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Event.Action == remoteinput.ActionPress {
			<-gate
		}
		mu.Lock()
		events = append(events, body.Event)
		mu.Unlock()
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{testRoomPack(t, hardwareRoomID, liveHardwareTestScript)}), "")
	app.mu.Lock()
	app.session = hostclient.SessionResult{State: "active", CoreKeyboardHID: true, Input: &hostclient.SessionInput{State: "attached", Ready: true}}
	app.syncGPUParkLocked()
	app.mu.Unlock()
	if !app.HandlePlayHIDScancode("a", 0x04, true, time.Now()) {
		t.Fatal("core did not accept key")
	}
	if !app.HandlePlayHIDScancode("home", 0x4a, true, time.Now()) || !app.Snapshot().Room.DuringPlay {
		t.Fatal("Home did not enter live room")
	}
	if app.ForwardsPlayHID() || app.HandlePlayHIDScancode("d", 0x07, true, time.Now()) {
		t.Fatal("menu key forwarded to running machine")
	}
	release()
	waitFor(t, app, "held key release", func(Snapshot) bool { mu.Lock(); defer mu.Unlock(); return len(events) == 2 })
	mu.Lock()
	defer mu.Unlock()
	if events[0].Action != remoteinput.ActionPress || events[1].Action != remoteinput.ActionRelease || events[0].Code != events[1].Code {
		t.Fatalf("held key was not released in order: %+v", events)
	}
	app.mu.Lock()
	app.closeAllRoomsLocked()
	app.mu.Unlock()
}

func TestHardwareRoomQueuedInputDoesNotReachReplacementSession(t *testing.T) {
	first := make(chan struct{}, 1)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	var mu sync.Mutex
	var events []remoteinput.Event
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/input/event" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Event remoteinput.Event `json:"event"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		events = append(events, body.Event)
		mu.Unlock()
		if body.Event.Code == remoteinput.KeyA {
			first <- struct{}{}
			select {
			case <-gate:
			case <-r.Context().Done():
			}
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(release)
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 20)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	app.session = hostclient.SessionResult{ID: "same-host", State: "active", GameID: "first", Input: &hostclient.SessionInput{Ready: true}, CorePackage: &hostclient.SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 1}}
	app.mu.Unlock()
	app.SendPlayHID(remoteinput.Event{Code: remoteinput.KeyA, Action: remoteinput.ActionPress})
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("first key not sent")
	}
	app.SendPlayHID(remoteinput.Event{Code: remoteinput.KeyLeft, Action: remoteinput.ActionPress})
	app.mu.Lock()
	app.releasePlayHIDLocked()
	app.applySessionLocked(hostclient.SessionResult{ID: "same-host", State: "active", GameID: "second", Input: &hostclient.SessionInput{Ready: true}, CorePackage: &hostclient.SessionCorePackage{PackageID: strings.Repeat("a", 64), Generation: 2}})
	app.mu.Unlock()
	app.SendPlayHID(remoteinput.Event{Code: remoteinput.KeyRight, Action: remoteinput.ActionPress})
	waitFor(t, app, "replacement key", func(Snapshot) bool { mu.Lock(); defer mu.Unlock(); return len(events) >= 2 })
	release()
	app.mu.Lock()
	tail := app.playHIDTail
	app.mu.Unlock()
	select {
	case <-tail:
	case <-time.After(time.Second):
		t.Fatal("input queue did not drain")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0].Code != remoteinput.KeyA || events[1].Code != remoteinput.KeyRight {
		t.Fatalf("old queued input reached replacement session: %+v", events)
	}
}
