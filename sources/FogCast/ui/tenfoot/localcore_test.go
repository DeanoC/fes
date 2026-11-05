package tenfoot

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/testwait"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
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
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), "")
	app.SetHomeRooms(true)
	app.SetHomeRoom("example.fes-cores")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	// With no kit the host library alone decides; it has no machine
	// entries here, so every family is Unavailable and nothing launches.
	snap := waitFor(t, app, "families unavailable", homeFamilySettled)
	if d := snap.Room.Destination; d.Kind != rooms.KindFamily || d.Availability != rooms.AvailUnavailable {
		t.Fatalf("host Home %+v", d)
	}
	if app.localCores != nil {
		t.Fatal("home room on the host received a kit client")
	}
}

func TestLeaseReleaseKeepsUnrelatedStatus(t *testing.T) {
	app := &App{status: "Loading library", kitLeaseHave: true, kitLease: KitLeaseStatus{State: "held", Owner: "other"}}
	app.mu.Lock()
	wasForeign := app.foreignKitLeaseLocked()
	app.kitLease = KitLeaseStatus{State: "free"}
	app.clearLeaseRefusalAfterTransitionLocked(wasForeign)
	app.mu.Unlock()
	if got := app.status; got != "Loading library" {
		t.Fatalf("unrelated status cleared on lease release: %q", got)
	}
}

func TestUnpairedHostBusyDoesNotMarkInstalledCoreInUse(t *testing.T) {
	app := startCoreRoom(t, &fakeLocalCores{}, nil, coreRoomScript(tenfootPongID, "Pong", true, ""))
	app.mu.Lock()
	app.healthHave = true
	app.health.Connection.State = "busy"
	app.mu.Unlock()
	snap := app.Snapshot()
	d := snap.Room.Destination
	if d.Status != "Ready to play." || d.Action != "Play" || d.Confirm() != rooms.ConfirmLaunchCore {
		t.Fatalf("unpaired host busy changed core tile: %+v", d)
	}
}

func adoptTestLocalRun(app *App, fake *fakeLocalCores) {
	fake.mu.Lock()
	if !fake.statusSet {
		fake.status = localcores.RunStatus{Phase: "running", Running: true, CoreID: "fes.pong", PackageID: tenfootPongID, Generation: strings.Repeat("a", 64)}
		fake.statusSet = true
	}
	fake.mu.Unlock()
	app.mu.Lock()
	defer app.mu.Unlock()
	app.localStartupStatusChecked = true
	app.adoptStartupLocalRunLocked(localcores.RunStatus{Phase: "running", Running: true, CoreID: "fes.pong", PackageID: tenfootPongID, Generation: strings.Repeat("a", 64)})
}

func TestLocalStatusIdleWhileRunningResumes(t *testing.T) {
	fake := &fakeLocalCores{}
	app := startCoreRoom(t, fake, nil, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Label == "FES Pong"
	})
	waitFor(t, app, "startup status", func(Snapshot) bool { return fake.statusCount() >= 1 })
	if snap := app.Snapshot(); snap.LocalCorePhase != "" || snap.Status == localInUseCopy {
		t.Fatalf("idle startup adopted a run: %+v", snap)
	}
	polled := fake.statusCount()
	base := time.Now()
	for i := 0; i < 4; i++ {
		app.Tick(base.Add(time.Duration(i+1) * time.Second))
	}
	if fake.statusCount() != polled {
		t.Fatalf("kept polling after idle startup: %d -> %d", polled, fake.statusCount())
	}
	adoptTestLocalRun(app, fake)
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
	polled = fake.statusCount()
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
	adoptTestLocalRun(app, fake)
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning
	})
	deadline := time.Now().Add(testwait.Timeout)
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
	adoptTestLocalRun(app, fake)
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused
	})
	app.Tick(time.Now().Add(2 * time.Second))
	deadline := time.Now().Add(testwait.Timeout)
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
	case <-time.After(testwait.Timeout):
		t.Fatal("blocked status poll did not return")
	}
	deadline = time.Now().Add(testwait.Timeout)
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
	adoptTestLocalRun(app, fake)
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
	adoptTestLocalRun(app, fake)
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused
	})
	t1 := time.Unix(300, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t1)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t1.Add(10*time.Millisecond))
	app.Tick(t1.Add(10*time.Millisecond + time.Second))
	deadline := time.Now().Add(testwait.Timeout)
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
	adoptTestLocalRun(app, fake)
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

func TestPresentHoldPausesAfterTheStartingFrame(t *testing.T) {
	rec := &recordingMenu{}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	var hold presentHold
	snap := Snapshot{Room: RoomSnapshot{Open: true}, LocalCorePhase: localPhaseLaunching,
		LocalCoreTitle: "FES Pong", LocalCoreStartedAt: time.Now().Add(-time.Second),
		LocalCorePresentsPaused: true, Status: "Starting FES Pong…"}
	copy := launchOverlayCopy(snap)
	if !copy.Visible || copy.Phase != "Starting core" || !strings.Contains(copy.Elapsed, "0:01") || !strings.Contains(copy.Hint, "Select+Start") {
		t.Fatalf("armed loading frame lacks overlay: %+v", copy)
	}
	if skip, err := hold.skip(context.Background(), dev, snap); skip || err != nil {
		t.Fatal("the starting frame should still be presented")
	}
	if skip, err := hold.skip(context.Background(), dev, snap); !skip || err != nil {
		t.Fatal("later frames should skip")
	}
	dev.Present()
	if calls, _, _ := rec.snapshot(); calls != 0 {
		t.Fatalf("paused present submitted %d frames", calls)
	}
	snap.LocalCorePresentsPaused = false
	if skip, err := hold.skip(context.Background(), dev, snap); skip || err != nil {
		t.Fatal("resume should present")
	}
}

func TestPresentHoldLateAdoptionSkipsArmedFrame(t *testing.T) {
	rec := &recordingMenu{}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	var hold presentHold
	snap := Snapshot{LocalCorePresentsPaused: true, LocalCoreLateAdopt: true}
	if skip, err := hold.skip(context.Background(), dev, snap); !skip || err != nil {
		t.Fatalf("late adoption should drain and skip immediately: skip=%v err=%v", skip, err)
	}
	dev.Present()
	if calls, _, _ := rec.snapshot(); calls != 0 {
		t.Fatalf("late adoption submitted %d menu frames", calls)
	}
}

type staleFailMenu struct {
	mu    sync.Mutex
	fail  error
	calls int
}

func (c *staleFailMenu) Status(context.Context) (menudisplay.Status, error) {
	return menudisplay.Status{Available: true, Generation: 4, Width: 1280, Height: 720, Stride: 5120, ByteCount: menudisplay.FrameBytes}, nil
}

func (c *staleFailMenu) Present(context.Context, uint64, []byte) (menudisplay.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.fail != nil {
		return menudisplay.Result{}, c.fail
	}
	return menudisplay.Result{Generation: 4, DisplayedSequence: uint64(c.calls)}, nil
}

func (c *staleFailMenu) snapshot() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func TestPresentHoldLateAdoptionPausesAfterStaleFailure(t *testing.T) {
	rec := &staleFailMenu{fail: errors.New("stale menu")}
	dev, err := gfx.NewMenuDisplayWithPresenter(rec)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	dev.SetChangeDriven(true)
	dev.Present()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := dev.WaitIdle(ctx); err == nil || !strings.Contains(err.Error(), "stale menu") {
		t.Fatalf("failed frame WaitIdle = %v", err)
	}
	dev.Resume()
	var hold presentHold
	snap := Snapshot{LocalCorePresentsPaused: true, LocalCoreLateAdopt: true}
	if skip, err := hold.skip(context.Background(), dev, snap); !skip || err != nil {
		t.Fatalf("late adoption after a stale failure: skip=%v err=%v", skip, err)
	}
	before := rec.snapshot()
	dev.Present()
	if dev.FramePending() || rec.snapshot() != before {
		t.Fatalf("late adoption left the menu presenting: pending=%v calls=%d", dev.FramePending(), rec.snapshot())
	}
}

func startCoreRoom(t *testing.T, cores rooms.LocalCores, feed localPadSender, script string, paired ...bool) *App {
	t.Helper()
	app, _ := startCoreRoomWithHost(t, cores, feed, script, paired...)
	return app
}

func startCoreRoomWithHost(t *testing.T, cores rooms.LocalCores, feed localPadSender, script string, paired ...bool) (*App, *roomHost) {
	t.Helper()
	h := newRoomHost(t)
	if len(paired) > 0 && paired[0] {
		h.pairedLease = true
	}
	pack := testRoomPack(t, "cores", script)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	if len(paired) > 0 && paired[0] {
		app.client.paired = true
	}
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), "")
	app.SetHomeRooms(true)
	app.SetHomeRoom("cores")
	app.SetKitLocal(cores, feed)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app, h
}

func TestKitSessionPadStaysLocalWhenTheFeedFails(t *testing.T) {
	h := newRoomHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	feed := &errPadFeed{}
	app.SetKitLocal(&fakeLocalCores{}, feed)
	app.mu.Lock()
	app.session.State = "active"
	app.mu.Unlock()
	t.Cleanup(app.Stop)

	now := time.Unix(50, 0)
	if !app.HandleLocalPad(padButton(remoteinput.ButtonA, true), now) {
		t.Fatal("active kit session let the menu take the pad")
	}
	if feed.count(remoteinput.ButtonA, remoteinput.ActionPress) != 1 {
		t.Fatal("failed local write was not attempted")
	}
	if h.launchCount() != 0 || h.inputCount() != 0 {
		t.Fatalf("failed local write fell back to the host launches=%d inputs=%d", h.launchCount(), h.inputCount())
	}

	app.SetKitLocal(&fakeLocalCores{}, nil)
	if !app.HandleLocalPad(padButton(remoteinput.ButtonB, true), now.Add(time.Second)) {
		t.Fatal("missing local socket fell through to the menu")
	}
	if h.launchCount() != 0 || h.inputCount() != 0 {
		t.Fatalf("missing local socket fell back to the host launches=%d inputs=%d", h.launchCount(), h.inputCount())
	}
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
	mu              sync.Mutex
	cores           []localcores.Core
	listErr         error
	launches        []string
	stops           int
	stopGenerations []string
	err             error
	stopErr         error
	hold            chan struct{}
	statuses        int
	status          localcores.RunStatus
	statusSet       bool
	statusErr       error
	blockStatus     chan struct{}
	statusDone      chan struct{}
	statusQueue     []localcores.RunStatus
}

func (f *fakeLocalCores) List(context.Context) ([]localcores.Core, error) {
	if f == nil {
		return []localcores.Core{}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]localcores.Core, len(f.cores))
	copy(out, f.cores)
	return out, nil
}

func (f *fakeLocalCores) Launch(_ context.Context, id string) (localcores.LaunchResult, error) {
	f.mu.Lock()
	f.launches = append(f.launches, id)
	err := f.err
	hold := f.hold
	f.mu.Unlock()
	if hold != nil {
		<-hold
	}
	return localcores.LaunchResult{Core: localcores.Core{PackageID: id}, Generation: strings.Repeat("a", 64)}, err
}

func (f *fakeLocalCores) LaunchROM(ctx context.Context, id, _, _ string) (localcores.LaunchResult, error) {
	return f.Launch(ctx, id)
}

func (f *fakeLocalCores) Stop(_ context.Context, generation string) error {
	f.mu.Lock()
	f.stops++
	f.stopGenerations = append(f.stopGenerations, generation)
	err := f.stopErr
	f.mu.Unlock()
	return err
}

func (f *fakeLocalCores) Status(context.Context) (localcores.RunStatus, error) {
	f.mu.Lock()
	f.statuses++
	if len(f.statusQueue) > 0 {
		status := f.statusQueue[0]
		f.statusQueue = f.statusQueue[1:]
		f.mu.Unlock()
		return status, nil
	}
	block := f.blockStatus
	set := f.statusSet
	status := f.status
	err := f.statusErr
	launched := f.err == nil && len(f.launches) > 0
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
		// An explicit status is the agent's answer. Otherwise a launch this
		// fake accepted is still running, and a fresh launcher sees idle.
		if launched {
			return localcores.RunStatus{Generation: strings.Repeat("a", 64), Phase: "running", Running: true, PackageID: tenfootPongID}, nil
		}
		return localcores.RunStatus{Phase: "idle"}, nil
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
	return f.count(code, action) > 0
}

func (f *fakePadFeed) count(code remoteinput.Code, action remoteinput.Action) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.events {
		if e.Code == code && e.Action == action {
			n++
		}
	}
	return n
}

// errPadFeed records the frame and reports that the local socket write failed.
type errPadFeed struct {
	fakePadFeed
}

func (f *errPadFeed) Send(e remoteinput.Event, now time.Time) error {
	_ = f.fakePadFeed.Send(e, now)
	return errors.New("local socket is down")
}

func (f *fakePadFeed) snapshot() []remoteinput.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]remoteinput.Event(nil), f.events...)
}

func TestFreshLauncherAdoptsSurvivingLocalRunAndChordStopsIt(t *testing.T) {
	generation := strings.Repeat("b", 64)
	fake := &fakeLocalCores{statusSet: true, status: localcores.RunStatus{
		Phase: "running", Running: true, PackageID: tenfootPongID, CoreID: "fes.pong", Generation: generation,
	}}
	feed := &fakePadFeed{}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	app.localFeed = feed
	t.Cleanup(app.Stop)

	base := time.Unix(4000, 0)
	app.Tick(base)
	waitFor(t, app, "surviving run adopted", func(s Snapshot) bool {
		return adoptedInUseScreen(s) && s.LocalCoreTitle == "Pong" && s.LocalCorePresentsPaused && s.LocalCoreLateAdopt
	})
	app.mu.Lock()
	gotGen := app.localLeaseGeneration
	app.mu.Unlock()
	if gotGen != generation {
		t.Fatalf("generation = %q", gotGen)
	}
	if !app.HandleLocalPad(padButton(remoteinput.ButtonA, true), base.Add(time.Second)) {
		t.Fatal("menu took the surviving core's pad")
	}
	if feed.count(remoteinput.ButtonA, remoteinput.ActionPress) != 1 {
		t.Fatal("surviving core did not receive play input")
	}

	t1 := base.Add(2 * time.Second)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t1)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t1.Add(10*time.Millisecond))
	app.Tick(t1.Add(10*time.Millisecond + localChordHold))
	waitFor(t, app, "chord stopped surviving run", func(s Snapshot) bool {
		return s.LocalCorePhase == "" && !s.LocalCorePresentsPaused
	})
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.stopGenerations) != 1 || fake.stopGenerations[0] != generation {
		t.Fatalf("Stop generations = %v", fake.stopGenerations)
	}
	if feed.saw(remoteinput.ButtonStart, remoteinput.ActionPress) {
		t.Fatal("stop chord forwarded Start")
	}
}

func TestLocalStatusReplacementRevokesStopOwnership(t *testing.T) {
	fake := &fakeLocalCores{statusSet: true, status: localcores.RunStatus{Phase: "running", Running: true, PackageID: tenfootPongID, Generation: strings.Repeat("b", 64)}}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	app.mu.Lock()
	app.localPhase = localPhaseRunning
	app.localLeaseGeneration = strings.Repeat("a", 64)
	app.localPackageID = tenfootPongID
	app.localStopAfterStart = true
	app.pollLocalStatusLocked(time.Now())
	app.mu.Unlock()
	waitFor(t, app, "replacement fenced", func(s Snapshot) bool { return s.LocalCorePhase != localPhaseRunning })
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.localLeaseGeneration != "" || app.localStopAfterStart {
		t.Fatal("replacement retained old ownership or deferred Stop")
	}
	app.beginLocalStopLocked()
	if fake.stopCount() != 0 {
		t.Fatal("replacement was stopped")
	}
}

func startupRunningStatus(generation string) localcores.RunStatus {
	return localcores.RunStatus{
		Phase: "running", Running: true, CoreID: "fes.pong", PackageID: tenfootPongID, Generation: generation,
	}
}

// A new launcher process, with the agent already running a core, is the
// restart the supervisor performs after the old launcher is killed.
func TestLauncherStartupAdoptsActiveRunShowsInUseAndStops(t *testing.T) {
	generation := strings.Repeat("c", 64)
	fake := &fakeLocalCores{statusSet: true, status: startupRunningStatus(generation)}
	feed := &fakePadFeed{}
	app := startCoreRoom(t, fake, feed, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	snap := waitFor(t, app, "in use screen", func(s Snapshot) bool {
		return s.Room.Open && adoptedInUseScreen(s) && launchOverlayCopy(s).Visible && s.LocalCorePresentsPaused && s.LocalCoreLateAdopt
	})
	copy := launchOverlayCopy(snap)
	if copy.Heading != rooms.InUseStatus || copy.Phase != rooms.InUseDetail || !strings.Contains(copy.Hint, "Select+Start") || copy.Title != "Pong" {
		t.Fatalf("in use screen %+v", copy)
	}
	if fake.launchCount() != 0 {
		t.Fatal("adoption launched a core")
	}
	now := time.Unix(50, 0)
	if !app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), now) ||
		!app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), now.Add(time.Millisecond)) {
		t.Fatal("menu received the stop chord")
	}
	app.Tick(now.Add(time.Millisecond + localChordHold))
	waitFor(t, app, "stopped", func(s Snapshot) bool {
		return fake.stopCount() == 1 && s.LocalCorePhase == "" && !adoptedInUseScreen(s)
	})
	fake.mu.Lock()
	got := append([]string(nil), fake.stopGenerations...)
	fake.mu.Unlock()
	if len(got) != 1 || got[0] != generation {
		t.Fatalf("stop generations %v", got)
	}
	if app.HandleLocalPad(padButton(remoteinput.ButtonA, true), now.Add(5*time.Second)) {
		t.Fatal("menu did not regain the pad")
	}
}

func TestLauncherStartupReturnsToMenuWhenAdoptedRunEnds(t *testing.T) {
	generation := strings.Repeat("d", 64)
	fake := &fakeLocalCores{statusSet: true, status: startupRunningStatus(generation)}
	app := startCoreRoom(t, fake, &fakePadFeed{}, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "in use screen", func(s Snapshot) bool {
		return adoptedInUseScreen(s) && s.LocalCorePresentsPaused && s.LocalCoreLateAdopt
	})
	deadline := time.Now().Add(testwait.Timeout)
	for {
		app.mu.Lock()
		busy := app.localStatusBusy
		app.mu.Unlock()
		if !busy || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	fake.setStatus(localcores.RunStatus{Phase: "idle"})
	app.mu.Lock()
	app.localStatusNext = time.Time{}
	app.mu.Unlock()
	app.Tick(time.Now().Add(2 * time.Second))
	waitFor(t, app, "menu after idle", func(s Snapshot) bool {
		return s.LocalCorePhase == "" && !s.LocalCorePresentsPaused && !adoptedInUseScreen(s)
	})
	if fake.stopCount() != 0 || fake.launchCount() != 0 {
		t.Fatalf("idle adopted run launched=%d stopped=%d", fake.launchCount(), fake.stopCount())
	}
}

func TestLauncherStartupRetriesStatusThenAdopts(t *testing.T) {
	generation := strings.Repeat("e", 64)
	fake := &fakeLocalCores{statusErr: errors.New("status unavailable")}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	app.Tick(time.Unix(10, 0))
	waitFor(t, app, "failed startup status", func(Snapshot) bool { return fake.statusCount() >= 1 })
	if snap := app.Snapshot(); snap.LocalCorePhase != "" || snap.Status == localInUseCopy {
		t.Fatalf("failed status adopted a run: %+v", snap)
	}
	fake.mu.Lock()
	fake.statusErr = nil
	fake.status = startupRunningStatus(generation)
	fake.statusSet = true
	fake.mu.Unlock()
	app.mu.Lock()
	app.localStatusNext = time.Time{}
	app.mu.Unlock()
	app.Tick(time.Unix(12, 0))
	waitFor(t, app, "adopted after retry", func(s Snapshot) bool {
		return adoptedInUseScreen(s) && s.LocalCorePresentsPaused && s.LocalCoreLateAdopt
	})
}

func TestLauncherStartupIgnoresIdleAndUnnamedRuns(t *testing.T) {
	for _, status := range []localcores.RunStatus{
		{Phase: "idle"},
		{Phase: "running", Running: true, PackageID: tenfootPongID, CoreID: "fes.pong"},
		{Phase: "launching", PackageID: tenfootPongID, CoreID: "fes.pong"},
		{Phase: "stopping", PackageID: tenfootPongID, CoreID: "fes.pong"},
	} {
		t.Run(status.Phase, func(t *testing.T) {
			fake := &fakeLocalCores{statusSet: true, status: status}
			app := NewApp(nil, 1280, 720, 50)
			app.localCores = fake
			app.Tick(time.Unix(10, 0))
			deadline := time.Now().Add(testwait.Timeout)
			var phase, generation, gotStatus string
			var checked bool
			for {
				app.mu.Lock()
				phase, generation, gotStatus = app.localPhase, app.localLeaseGeneration, app.localStatus
				checked = app.localStartupStatusChecked
				app.mu.Unlock()
				if checked || time.Now().After(deadline) {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if !checked || phase != "" || generation != "" || gotStatus == localInUseCopy {
				t.Fatalf("startup adopted %+v phase=%q generation=%q status=%q checked=%v", status, phase, generation, gotStatus, checked)
			}
		})
	}
}

func TestLauncherStartupDefersStopUntilRunning(t *testing.T) {
	generation := strings.Repeat("c", 64)
	fake := &fakeLocalCores{statusSet: true, status: localcores.RunStatus{
		Phase: "launching", CoreID: "fes.pong", PackageID: tenfootPongID, Generation: generation,
	}}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	t.Cleanup(app.Stop)
	app.Tick(time.Unix(10, 0))
	waitFor(t, app, "launching generation watched", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseLaunching && s.LocalCorePresentsPaused && s.LocalCoreLateAdopt && strings.Contains(s.Status, "Starting Pong")
	})
	if fake.stopCount() != 0 {
		t.Fatal("watched launch was stopped")
	}
	now := time.Unix(20, 0)
	if !app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), now) ||
		!app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), now.Add(time.Millisecond)) {
		t.Fatal("menu received the stop chord")
	}
	app.Tick(now.Add(time.Millisecond + localChordHold))
	app.mu.Lock()
	deferred := app.localStopAfterStart && app.localPhase == localPhaseLaunching
	app.mu.Unlock()
	if !deferred || fake.stopCount() != 0 {
		t.Fatalf("stop was posted while launching: deferred=%v stops=%d", deferred, fake.stopCount())
	}
	fake.setStatus(startupRunningStatus(generation))
	app.mu.Lock()
	app.localStatusNext = time.Time{}
	app.mu.Unlock()
	app.Tick(now.Add(2 * time.Second))
	waitFor(t, app, "deferred stop", func(s Snapshot) bool {
		return fake.stopCount() == 1 && s.LocalCorePhase == ""
	})
	fake.mu.Lock()
	got := append([]string(nil), fake.stopGenerations...)
	fake.mu.Unlock()
	if len(got) != 1 || got[0] != generation {
		t.Fatalf("stop generations %v", got)
	}
}

func TestLauncherStartupChordRestartsWhenLaunchBecomesRunning(t *testing.T) {
	generation := strings.Repeat("a", 64)
	fake := &fakeLocalCores{statusSet: true, status: localcores.RunStatus{
		Phase: "launching", CoreID: "fes.pong", PackageID: tenfootPongID, Generation: generation,
	}}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	t.Cleanup(app.Stop)
	app.Tick(time.Unix(10, 0))
	waitFor(t, app, "launching generation watched", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseLaunching && s.LocalCorePresentsPaused
	})
	press := time.Now()
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), press)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), press)
	time.Sleep(200 * time.Millisecond)
	partial := time.Now()
	app.Tick(partial)
	if fake.stopCount() != 0 || partial.Sub(press) >= localChordHold {
		t.Fatalf("partial hold stopped the launch or ran too long: stops=%d held=%s", fake.stopCount(), partial.Sub(press))
	}
	fake.setStatus(startupRunningStatus(generation))
	app.mu.Lock()
	app.localStatusNext = time.Time{}
	app.mu.Unlock()
	app.Tick(time.Now())
	waitFor(t, app, "running after partial hold", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && fake.stopCount() == 0
	})
	app.mu.Lock()
	since := app.localChordSince
	app.mu.Unlock()
	if !since.After(press.Add(100 * time.Millisecond)) {
		t.Fatalf("chord clock kept the loading press: since=%s press=%s", since, press)
	}
	app.Tick(since.Add(localChordHold - time.Millisecond))
	if fake.stopCount() != 0 {
		t.Fatal("running chord fired before a full second of play")
	}
	app.Tick(since.Add(localChordHold))
	waitFor(t, app, "full play hold stopped", func(Snapshot) bool { return fake.stopCount() == 1 })
}

func TestLauncherStartupLaunchingIdleDoesNotStop(t *testing.T) {
	generation := strings.Repeat("d", 64)
	fake := &fakeLocalCores{statusSet: true, status: localcores.RunStatus{
		Phase: "launching", CoreID: "fes.pong", PackageID: tenfootPongID, Generation: generation,
	}}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	t.Cleanup(app.Stop)
	app.Tick(time.Unix(10, 0))
	waitFor(t, app, "launching watched", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseLaunching && s.LocalCorePresentsPaused
	})
	now := time.Unix(20, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), now)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), now.Add(time.Millisecond))
	app.Tick(now.Add(time.Millisecond + localChordHold))
	fake.setStatus(localcores.RunStatus{Phase: "idle", Generation: generation})
	app.mu.Lock()
	app.localStatusNext = time.Time{}
	app.mu.Unlock()
	app.Tick(now.Add(2 * time.Second))
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		snap := app.Snapshot()
		if snap.LocalCorePhase == "" || !snap.LocalCorePresentsPaused || fake.stopCount() != 0 {
			t.Fatalf("generation-bearing idle resumed phase=%q paused=%v stops=%d", snap.LocalCorePhase, snap.LocalCorePresentsPaused, fake.stopCount())
		}
		time.Sleep(5 * time.Millisecond)
		app.Tick(time.Now())
	}
	fake.setStatus(localcores.RunStatus{Phase: "idle"})
	app.mu.Lock()
	app.localStatusNext = time.Time{}
	app.mu.Unlock()
	app.Tick(now.Add(4 * time.Second))
	waitFor(t, app, "menu after launch idle", func(s Snapshot) bool {
		return s.LocalCorePhase == "" && !s.LocalCorePresentsPaused
	})
	if fake.stopCount() != 0 {
		t.Fatal("generation-free idle posted Stop")
	}
}

func TestLauncherStartupStoppingResumesThenChordStops(t *testing.T) {
	generation := strings.Repeat("e", 64)
	fake := &fakeLocalCores{statusSet: true, status: localcores.RunStatus{
		Phase: "stopping", Running: false, CoreID: "fes.pong", PackageID: tenfootPongID, Generation: generation,
	}}
	feed := &fakePadFeed{}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	app.localFeed = feed
	t.Cleanup(app.Stop)
	app.Tick(time.Unix(10, 0))
	waitFor(t, app, "stopping generation watched", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseStopping && s.LocalCorePresentsPaused && s.LocalCoreLateAdopt && strings.Contains(s.Status, "Stopping Pong")
	})
	if !app.HandleLocalPad(padButton(remoteinput.ButtonA, true), time.Unix(11, 0)) {
		t.Fatal("menu took the pad while stop was in progress")
	}
	if feed.count(remoteinput.ButtonA, remoteinput.ActionPress) != 0 {
		t.Fatal("stopping forwarded play input")
	}
	fake.setStatus(startupRunningStatus(generation))
	app.mu.Lock()
	app.localStatusNext = time.Time{}
	app.mu.Unlock()
	app.Tick(time.Unix(12, 0))
	waitFor(t, app, "resumed run adopted", func(s Snapshot) bool {
		return adoptedInUseScreen(s) && s.LocalCorePresentsPaused && s.LocalCoreLateAdopt
	})
	if fake.stopCount() != 0 {
		t.Fatal("resume posted Stop on its own")
	}
	now := time.Unix(13, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), now)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), now.Add(time.Millisecond))
	app.Tick(now.Add(time.Millisecond + localChordHold))
	waitFor(t, app, "chord stopped resumed run", func(s Snapshot) bool {
		return fake.stopCount() == 1 && s.LocalCorePhase == "" && !s.LocalCorePresentsPaused
	})
	fake.mu.Lock()
	got := append([]string(nil), fake.stopGenerations...)
	fake.mu.Unlock()
	if len(got) != 1 || got[0] != generation {
		t.Fatalf("stop generations %v", got)
	}
}

func TestLauncherStartupStoppingIdleReturnsToMenu(t *testing.T) {
	generation := strings.Repeat("f", 64)
	fake := &fakeLocalCores{statusSet: true, status: localcores.RunStatus{
		Phase: "stopping", CoreID: "fes.pong", PackageID: tenfootPongID, Generation: generation,
	}}
	app := NewApp(nil, 1280, 720, 50)
	app.localCores = fake
	t.Cleanup(app.Stop)
	app.Tick(time.Unix(10, 0))
	waitFor(t, app, "stopping watched", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseStopping && s.LocalCorePresentsPaused
	})
	app.mu.Lock()
	checked := app.localStartupStatusChecked
	watching := app.localWatchStartup
	app.localStatusNext = time.Time{}
	app.mu.Unlock()
	if !checked || !watching {
		t.Fatalf("stopping probe checked=%v watching=%v", checked, watching)
	}
	fake.setStatus(localcores.RunStatus{Phase: "idle"})
	app.Tick(time.Unix(12, 0))
	waitFor(t, app, "menu after stop settled", func(s Snapshot) bool {
		return s.LocalCorePhase == "" && !s.LocalCorePresentsPaused
	})
	if fake.stopCount() != 0 || fake.launchCount() != 0 {
		t.Fatalf("settled stop launched=%d stopped=%d", fake.launchCount(), fake.stopCount())
	}
}
