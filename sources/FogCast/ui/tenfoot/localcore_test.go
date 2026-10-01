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

func TestPairedForeignLeaseBlocksInstalledCoreVisiblyAndClearsLive(t *testing.T) {
	fake := &fakeLocalCores{}
	app := startCoreRoom(t, fake, nil, coreRoomScript(tenfootPongID, "Pong", true, ""))
	waitFor(t, app, "ready Pong", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Kind == rooms.KindCore && s.Room.Destination.Status == "Ready to play."
	})

	app.mu.Lock()
	app.client.paired = true
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "held", Owner: "hil-355", Purpose: "hil-355-in-use"}
	app.mu.Unlock()
	snap := app.Snapshot()
	if snap.Room.Destination.Status != localInUseCopy || snap.Room.Destination.Action != localInUseCopy {
		t.Fatalf("foreign Pong copy: status=%q action=%q", snap.Room.Destination.Status, snap.Room.Destination.Action)
	}
	if strings.Contains(strings.ToLower(snap.HeaderHint()), "play") {
		t.Fatalf("foreign Pong hint offers play: %q", snap.HeaderHint())
	}
	app.HandleCommand(CmdSelect, time.Now())
	snap = app.Snapshot()
	if snap.Status != localInUseCopy {
		t.Fatalf("A did not surface in-use copy: %q", snap.Status)
	}
	if fake.launchCount() != 0 || snap.LocalCorePhase != "" {
		t.Fatalf("foreign A launched core: launches=%d phase=%q", fake.launchCount(), snap.LocalCorePhase)
	}

	app.mu.Lock()
	app.kitLease = KitLeaseStatus{State: "free"}
	app.mu.Unlock()
	snap = app.Snapshot()
	if snap.Room.Destination.Status != "Ready to play." || snap.Room.Destination.Action != "Play" || snap.Room.Destination.Confirm() != rooms.ConfirmLaunchCore {
		t.Fatalf("Pong did not become ready after lease free: %+v", snap.Room.Destination)
	}

	app.mu.Lock()
	app.kitLease = KitLeaseStatus{Unavailable: true}
	app.mu.Unlock()
	snap = app.Snapshot()
	if snap.Room.Destination.Status != "kit status unavailable" || snap.Room.Destination.Action != "kit status unavailable" || snap.Room.Destination.Confirm() != rooms.ConfirmExplain {
		t.Fatalf("unavailable lease did not gate Pong visibly: %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, time.Now())
	if fake.launchCount() != 0 || app.Snapshot().Status != "kit status unavailable" {
		t.Fatalf("unavailable lease A was not refused visibly: launches=%d status=%q", fake.launchCount(), app.Snapshot().Status)
	}
}

func TestLocalStatusIdleWhileRunningResumes(t *testing.T) {
	hold := make(chan struct{})
	fake := &fakeLocalCores{hold: hold}
	app := startCoreRoom(t, fake, nil, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Label == "FES Pong"
	})
	base := time.Now()
	for i := 0; i < 4; i++ {
		app.Tick(base.Add(time.Duration(i) * time.Second))
	}
	if fake.statusCount() != 0 {
		t.Fatalf("polled while idle: %d", fake.statusCount())
	}
	app.HandleCommand(CmdSelect, base)
	for i := 0; i < 4; i++ {
		app.Tick(base.Add(time.Duration(i) * time.Second))
	}
	if fake.statusCount() != 0 {
		t.Fatalf("polled while launching: %d", fake.statusCount())
	}
	close(hold)
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused
	})
	fake.setStatus(localcores.RunStatus{Phase: "idle", Running: false})
	snap := waitFor(t, app, "status resume", func(s Snapshot) bool {
		return !s.LocalCorePresentsPaused && s.LocalCorePhase == "" && s.LocalCoreRedraw >= 1 && roomTextHas(s, "resumed-1")
	})
	if snap.LocalCoreRedraw < 1 {
		t.Fatalf("redraw %d", snap.LocalCoreRedraw)
	}
	if fake.statusCount() < 1 {
		t.Fatal("running phase never polled status")
	}
	polled := fake.statusCount()
	after := time.Now()
	for i := 0; i < 4; i++ {
		app.Tick(after.Add(time.Duration(i) * time.Second))
	}
	if fake.statusCount() != polled {
		t.Fatalf("polled after idle %d -> %d", polled, fake.statusCount())
	}
}

func TestLocalStatusPollIsSingleFlight(t *testing.T) {
	block := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(block) }) })
	fake := &fakeLocalCores{blockStatus: block}
	app := startCoreRoom(t, fake, nil, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Destination.CoreLaunchable && s.Room.Destination.Label == "FES Pong"
	})
	app.HandleCommand(CmdSelect, time.Unix(10, 0))
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning
	})
	deadline := time.Now().Add(2 * time.Second)
	for fake.statusCount() < 1 {
		app.Tick(time.Now())
		if time.Now().After(deadline) {
			t.Fatal("status poll did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		app.Tick(time.Now().Add(time.Duration(i+1) * time.Second))
	}
	if got := fake.statusCount(); got != 1 {
		t.Fatalf("status polls in flight %d", got)
	}
	once.Do(func() { close(block) })
}

func TestStaleLocalStatusPollDoesNotResumeAfterFailedStop(t *testing.T) {
	statusRelease := make(chan struct{})
	fake := &fakeLocalCores{
		stopErr:     localcores.ErrUnavailable,
		blockStatus: statusRelease,
		statusDone:  make(chan struct{}, 1),
		status:      localcores.RunStatus{Phase: localPhaseStopping, Running: false},
		statusSet:   true,
	}
	app := startCoreRoom(t, fake, &fakePadFeed{}, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Destination.CoreLaunchable
	})
	app.HandleCommand(CmdSelect, time.Unix(20, 0))
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused
	})
	app.Tick(time.Now().Add(2 * time.Second))
	deadline := time.Now().Add(2 * time.Second)
	for fake.statusCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fake.statusCount() == 0 {
		t.Fatal("status poll did not start")
	}
	t1 := time.Unix(300, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t1)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t1.Add(10*time.Millisecond))
	app.Tick(t1.Add(10*time.Millisecond + time.Second))
	waitFor(t, app, "failed stop returned to running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused && fake.stopCount() == 1
	})
	close(statusRelease)
	select {
	case <-fake.statusDone:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked status poll did not return")
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		app.mu.Lock()
		busy := app.localStatusBusy
		app.mu.Unlock()
		if !busy || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	snap := app.Snapshot()
	if snap.LocalCorePhase != localPhaseRunning || !snap.LocalCorePresentsPaused {
		t.Fatalf("stale poll changed running core phase=%s paused=%v", snap.LocalCorePhase, snap.LocalCorePresentsPaused)
	}
}

func TestLocalStopInUseResumes(t *testing.T) {
	fake := &fakeLocalCores{stopErr: localcores.ErrInUse}
	app := startCoreRoom(t, fake, &fakePadFeed{}, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Destination.Label == "FES Pong" && s.Room.Destination.CoreLaunchable
	})
	app.HandleCommand(CmdSelect, time.Unix(20, 0))
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused
	})
	t1 := time.Unix(300, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t1)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t1.Add(10*time.Millisecond))
	app.Tick(t1.Add(10*time.Millisecond + time.Second))
	snap := waitFor(t, app, "in_use resume", func(s Snapshot) bool {
		return !s.LocalCorePresentsPaused && s.LocalCorePhase == "" && s.LocalCoreRedraw >= 1 && roomTextHas(s, "resumed-1")
	})
	if fake.stopCount() != 1 {
		t.Fatalf("stops %d", fake.stopCount())
	}
	if snap.LocalCorePresentsPaused || snap.LocalCorePhase != "" {
		t.Fatalf("phase=%s paused=%v", snap.LocalCorePhase, snap.LocalCorePresentsPaused)
	}
}

func TestLocalStopUnavailableStaysRunning(t *testing.T) {
	fake := &fakeLocalCores{stopErr: localcores.ErrUnavailable}
	app := startCoreRoom(t, fake, &fakePadFeed{}, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Destination.CoreLaunchable
	})
	app.HandleCommand(CmdSelect, time.Unix(20, 0))
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused
	})
	t1 := time.Unix(300, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t1)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t1.Add(10*time.Millisecond))
	app.Tick(t1.Add(10*time.Millisecond + time.Second))
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := app.Snapshot()
		if fake.stopCount() >= 1 && snap.LocalCorePhase == localPhaseRunning && snap.LocalCorePresentsPaused {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("unavailable resumed phase=%s paused=%v stops=%d", snap.LocalCorePhase, snap.LocalCorePresentsPaused, fake.stopCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestChordCompletingEventIsNotForwarded(t *testing.T) {
	fake := &fakeLocalCores{}
	feed := &fakePadFeed{}
	app := startCoreRoom(t, fake, feed, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Destination.Label == "FES Pong"
	})
	app.HandleCommand(CmdSelect, time.Unix(20, 0))
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning
	})
	t1 := time.Unix(400, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t1)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t1.Add(10*time.Millisecond))
	app.HandleLocalPad(padButton(remoteinput.ButtonA, true), t1.Add(10*time.Millisecond+time.Second))
	if feed.saw(remoteinput.ButtonA, remoteinput.ActionPress) {
		t.Fatal("chord-completing press was forwarded")
	}
	waitFor(t, app, "stopped", func(s Snapshot) bool {
		return s.LocalCorePhase == "" && !s.LocalCorePresentsPaused
	})
	if feed.saw(remoteinput.ButtonA, remoteinput.ActionPress) {
		t.Fatal("chord-completing press was forwarded after stop")
	}
	if fake.stopCount() != 1 {
		t.Fatalf("stops %d", fake.stopCount())
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
	mu          sync.Mutex
	launches    []string
	stops       int
	err         error
	stopErr     error
	hold        chan struct{}
	statuses    int
	status      localcores.RunStatus
	statusSet   bool
	statusErr   error
	blockStatus chan struct{}
	statusDone  chan struct{}
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
	err := f.stopErr
	f.mu.Unlock()
	return err
}

func (f *fakeLocalCores) Status(context.Context) (localcores.RunStatus, error) {
	f.mu.Lock()
	f.statuses++
	block := f.blockStatus
	set := f.statusSet
	status := f.status
	err := f.statusErr
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	if f.statusDone != nil {
		defer func() { f.statusDone <- struct{}{} }()
	}
	if err != nil {
		return localcores.RunStatus{}, err
	}
	if !set {
		return localcores.RunStatus{Phase: "running", Running: true, PackageID: tenfootPongID}, nil
	}
	return status, nil
}

func (f *fakeLocalCores) setStatus(status localcores.RunStatus) {
	f.mu.Lock()
	f.status = status
	f.statusSet = true
	f.mu.Unlock()
}

func (f *fakeLocalCores) statusCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses
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
