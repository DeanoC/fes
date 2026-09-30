package tenfoot

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const tenfootPongID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestHostModeLeavesKitUnset(t *testing.T) {
	prefs := filepath.Join(t.TempDir(), "prefs.json")
	host, err := configuredApp(Options{GFX: "sdl", APIBase: "http://127.0.0.1:9", PrefsPath: prefs})
	if err != nil {
		t.Fatal(err)
	}
	if host.localCores != nil || host.localFeed != nil {
		t.Fatal("host mode constructed a local-control client")
	}
	kit, err := configuredApp(Options{GFX: "menu-display", APIBase: "http://127.0.0.1:9", PrefsPath: filepath.Join(t.TempDir(), "kit.json")})
	if err != nil {
		t.Fatal(err)
	}
	if kit.localCores == nil || kit.localFeed == nil {
		t.Fatal("menu-display left the kit client unset")
	}

	h := newRoomHost(t)
	var pack rooms.Pack
	for _, candidate := range rooms.Examples() {
		if candidate.ID == "example.fes-cores" {
			pack = candidate
		}
	}
	if !pack.Valid() {
		t.Fatal("example.fes-cores missing")
	}
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "home.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom("example.fes-cores")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	snap := waitFor(t, app, "cores unavailable", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Status == "Installed cores are not available here." && roomTextHas(s, "Installed cores are not available here.")
	})
	if snap.Room.Destination.Action != snap.Room.Destination.Status {
		t.Fatalf("strip action %+v", snap.Room.Destination)
	}
	if app.localCores != nil {
		t.Fatal("home room on the host received a kit client")
	}
}

func TestKitCoreLaunchChordStopAndResume(t *testing.T) {
	hold := make(chan struct{})
	fake := &fakeLocalCores{hold: hold}
	feed := &fakePadFeed{}
	app := startCoreRoom(t, fake, feed, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Kind == rooms.KindCore && s.Room.Destination.Label == "FES Pong"
	})
	app.HandleCommand(CmdSelect, time.Unix(100, 0))
	snap := app.Snapshot()
	if !snap.LocalCorePresentsPaused || snap.LocalCorePhase != localPhaseLaunching || !strings.Contains(snap.Status, "Starting FES Pong…") {
		t.Fatalf("launch snapshot phase=%s paused=%v status=%q", snap.LocalCorePhase, snap.LocalCorePresentsPaused, snap.Status)
	}
	close(hold)
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused
	})
	if got := fake.launchCount(); got != 1 {
		t.Fatalf("launches %d", got)
	}

	t0 := time.Unix(200, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t0)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t0)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, false), t0.Add(400*time.Millisecond))
	app.Tick(t0.Add(2 * time.Second))
	if fake.stopCount() != 0 {
		t.Fatal("short chord stopped the core")
	}
	if feed.saw(remoteinput.ButtonStart, remoteinput.ActionPress) {
		t.Fatal("short chord forwarded Start")
	}
	if !feed.saw(remoteinput.ButtonSelect, remoteinput.ActionPress) || !feed.saw(remoteinput.ButtonSelect, remoteinput.ActionRelease) {
		t.Fatalf("select was not pressed and released: %+v", feed.snapshot())
	}

	t1 := time.Unix(300, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t1)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t1.Add(10*time.Millisecond))
	app.Tick(t1.Add(10*time.Millisecond + time.Second))
	snap = waitFor(t, app, "resume", func(s Snapshot) bool {
		return !s.LocalCorePresentsPaused && s.LocalCorePhase == "" && s.LocalCoreRedraw >= 1 && roomTextHas(s, "resumed-1")
	})
	if fake.stopCount() != 1 || fake.launchCount() != 1 {
		t.Fatalf("stops %d launches %d", fake.stopCount(), fake.launchCount())
	}
	if snap.LocalCoreRedraw != 1 {
		t.Fatalf("redraw %d", snap.LocalCoreRedraw)
	}
}

func TestKitCoreInUseAndBlockedCopy(t *testing.T) {
	blocked := &fakeLocalCores{}
	app := startCoreRoom(t, blocked, nil, coreRoomScript(tenfootPongID, "ColecoVision", false, "Needs a cartridge"))
	waitFor(t, app, "blocked tile", func(s Snapshot) bool {
		return s.Room.Destination.Kind == rooms.KindCore && !s.Room.Destination.CoreLaunchable
	})
	app.HandleCommand(CmdSelect, time.Unix(10, 0))
	snap := app.Snapshot()
	if snap.LocalCorePresentsPaused || snap.LocalCorePhase != "" || !strings.Contains(snap.Status, "Needs a cartridge") {
		t.Fatalf("blocked snapshot phase=%s paused=%v status=%q", snap.LocalCorePhase, snap.LocalCorePresentsPaused, snap.Status)
	}
	if blocked.launchCount() != 0 || blocked.stopCount() != 0 {
		t.Fatalf("blocked called the runtime launches=%d stops=%d", blocked.launchCount(), blocked.stopCount())
	}

	busy := &fakeLocalCores{err: localcores.ErrInUse}
	app = startCoreRoom(t, busy, nil, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "busy tile", func(s Snapshot) bool {
		return s.Room.Destination.CoreLaunchable && s.Room.Destination.Label == "FES Pong"
	})
	app.HandleCommand(CmdSelect, time.Unix(20, 0))
	snap = waitFor(t, app, "in use", func(s Snapshot) bool {
		return strings.Contains(s.Status, "In use") && strings.Contains(s.Status, "Someone else is playing on this machine")
	})
	if snap.LocalCorePresentsPaused || snap.LocalCorePhase != "" {
		t.Fatalf("in use left the core up phase=%s paused=%v", snap.LocalCorePhase, snap.LocalCorePresentsPaused)
	}
	if busy.launchCount() != 1 {
		t.Fatalf("launches %d", busy.launchCount())
	}
}

func TestPresentHoldPausesAfterTheStartingFrame(t *testing.T) {
	rec := &recordingMenu{}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	var hold presentHold
	snap := Snapshot{LocalCorePresentsPaused: true, Status: "Starting FES Pong…"}
	if hold.skip(context.Background(), dev, snap) {
		t.Fatal("the starting frame should still be presented")
	}
	if !hold.skip(context.Background(), dev, snap) {
		t.Fatal("later frames should skip")
	}
	dev.Present()
	if calls, _, _ := rec.snapshot(); calls != 0 {
		t.Fatalf("paused present submitted %d frames", calls)
	}
	snap.LocalCorePresentsPaused = false
	if hold.skip(context.Background(), dev, snap) {
		t.Fatal("resume should present")
	}
}

func startCoreRoom(t *testing.T, cores rooms.LocalCores, feed localPadSender, script string) *App {
	t.Helper()
	h := newRoomHost(t)
	pack := testRoomPack(t, "cores", script)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom("cores")
	app.SetKitLocal(cores, feed)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app
}

func coreRoomScript(id, label string, launchable bool, block string) string {
	flag := "false"
	if launchable {
		flag = "true"
	}
	return `
resumed = 0
function load()
  destination.set{
    kind = "core",
    package_id = "` + id + `",
    core_id = "fes.pong",
    label = "` + label + `",
    launchable = ` + flag + `,
    block = "` + block + `",
  }
end
function on_resume()
  resumed = resumed + 1
end
function draw()
  gfx.clear("#102030")
  gfx.text("resumed-" .. tostring(resumed), 8, 8, { size = 16 })
end
`
}

func roomTextHas(s Snapshot, text string) bool {
	for _, op := range s.Room.Frame.Ops {
		if op.Kind == rooms.OpText && strings.Contains(op.Text, text) {
			return true
		}
	}
	return false
}

func padButton(code remoteinput.Code, down bool) remoteinput.Event {
	action := remoteinput.ActionRelease
	if down {
		action = remoteinput.ActionPress
	}
	return remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: action, Code: code}
}

type fakeLocalCores struct {
	mu       sync.Mutex
	launches []string
	stops    int
	err      error
	hold     chan struct{}
}

func (f *fakeLocalCores) List(context.Context) ([]localcores.Core, error) {
	return []localcores.Core{}, nil
}

func (f *fakeLocalCores) Launch(_ context.Context, id string) error {
	f.mu.Lock()
	f.launches = append(f.launches, id)
	err := f.err
	hold := f.hold
	f.mu.Unlock()
	if hold != nil {
		<-hold
	}
	return err
}

func (f *fakeLocalCores) Stop(context.Context) error {
	f.mu.Lock()
	f.stops++
	f.mu.Unlock()
	return nil
}

func (f *fakeLocalCores) launchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.launches)
}

func (f *fakeLocalCores) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops
}

type fakePadFeed struct {
	mu     sync.Mutex
	events []remoteinput.Event
}

func (f *fakePadFeed) Send(e remoteinput.Event, _ time.Time) error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	f.events = append(f.events, e)
	f.mu.Unlock()
	return nil
}

func (f *fakePadFeed) Close() {}

func (f *fakePadFeed) saw(code remoteinput.Code, action remoteinput.Action) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if e.Code == code && e.Action == action {
			return true
		}
	}
	return false
}

func (f *fakePadFeed) snapshot() []remoteinput.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]remoteinput.Event(nil), f.events...)
}
