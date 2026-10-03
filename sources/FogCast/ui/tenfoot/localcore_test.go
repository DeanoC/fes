package tenfoot

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const tenfootPongID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSelectStartDuringLocalLoadDefersStopUntilRunning(t *testing.T) {
	now := time.Unix(10, 0)
	app := &App{localPhase: localPhaseLaunching}
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	startPress, _ := remoteinput.NormalizeGamepad("start", true)
	if !app.HandleLocalPad(selectPress, now) || !app.HandleLocalPad(startPress, now.Add(10*time.Millisecond)) {
		t.Fatal("launching core did not consume the stop chord")
	}
	app.mu.Lock()
	app.tickLocalCoreLocked(now.Add(10*time.Millisecond + localChordHold))
	if !app.localStopAfterStart || app.localPhase != localPhaseLaunching {
		t.Fatalf("loading chord phase=%q deferred=%v", app.localPhase, app.localStopAfterStart)
	}
	app.mu.Unlock()
	selectRelease, _ := remoteinput.NormalizeGamepad("select", false)
	app.HandleLocalPad(selectRelease, now.Add(localChordHold+time.Millisecond))
	app.mu.Lock()
	deferred := app.localStopAfterStart
	app.mu.Unlock()
	if !deferred {
		t.Fatal("releasing the chord canceled the already-triggered stop")
	}
}

func TestSMSPlayRequiresInstalledMasterSystemCore(t *testing.T) {
	game := hostclient.Game{
		ID: "sms-data-storm", Title: "Data Storm 1.00", System: "sms",
		State: "available", RootOnline: true,
	}
	dest := rooms.Destination{Kind: rooms.KindGame, GameID: game.ID, Matches: []hostclient.Game{game}}
	app := NewApp(nil, 1280, 720, 50)
	t.Cleanup(app.Stop)

	app.SetKitLocal(&fakeLocalCores{}, &fakePadFeed{})
	missing := app.applyKitDirectLocked(dest)
	if missing.Availability == rooms.AvailReady || missing.Confirm() == rooms.ConfirmLaunchKit || missing.Action == "Play" || missing.KitDirect {
		t.Fatalf("playable without fes.sms: %+v", missing)
	}
	if missing.Status != localCoreMissingCopy || missing.Action != localCoreMissingAction || missing.Confirm() != rooms.ConfirmExplain {
		t.Fatalf("missing-core copy %+v", missing)
	}

	app.SetKitLocal(&fakeLocalCores{listErr: errors.New("socket is not ready")}, &fakePadFeed{})
	checking := app.applyKitDirectLocked(dest)
	if checking.Availability != rooms.AvailChecking || checking.KitDirect || checking.Action == "Play" || checking.Confirm() == rooms.ConfirmLaunchKit {
		t.Fatalf("unknown install showed play: %+v", checking)
	}
	if checking.Status != localCoreCheckingCopy || checking.Action != localCoreCheckingAction {
		t.Fatalf("checking copy %+v", checking)
	}

	app.SetKitLocal(&fakeLocalCores{cores: []localcores.Core{{CoreID: "fes.sms", Name: "Master System"}}}, &fakePadFeed{})
	ready := app.applyKitDirectLocked(dest)
	if ready.Availability != rooms.AvailReady || ready.Confirm() != rooms.ConfirmLaunchKit || !ready.KitDirect || ready.Action != "Play" {
		t.Fatalf("installed core did not enable play: %+v", ready)
	}
}

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
	app := startCoreRoom(t, fake, nil, coreRoomScript(tenfootPongID, "Pong", true, ""), true)
	waitFor(t, app, "ready Pong", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Kind == rooms.KindCore && s.Room.Destination.Status == "Ready to play."
	})

	app.mu.Lock()
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "held", Owner: "hil-355", Purpose: "hil-355-in-use"}
	app.mu.Unlock()
	snap := app.Snapshot()
	if snap.Room.Destination.Status != rooms.InUseStatus || snap.Room.Destination.Action != "" {
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
	app.mu.Lock()
	wasForeign := app.foreignKitLeaseLocked()
	app.kitLease = KitLeaseStatus{State: "free"}
	app.clearLeaseRefusalAfterTransitionLocked(wasForeign)
	app.mu.Unlock()
	if got := app.Snapshot().Status; got == localInUseCopy {
		t.Fatalf("lease refusal stayed in status after lease release: %q", got)
	}
	if fake.launchCount() != 0 || snap.LocalCorePhase != "" {
		t.Fatalf("foreign A launched core: launches=%d phase=%q", fake.launchCount(), snap.LocalCorePhase)
	}

	snap = app.Snapshot()
	if snap.Room.Destination.Status != "Ready to play." || snap.Room.Destination.Action != "Play" || snap.Room.Destination.Confirm() != rooms.ConfirmLaunchCore {
		t.Fatalf("Pong did not become ready after lease free: %+v", snap.Room.Destination)
	}

	app.mu.Lock()
	app.kitLease = KitLeaseStatus{Unavailable: true}
	app.mu.Unlock()
	snap = app.Snapshot()
	if snap.Room.Destination.Status != machineStatusUnknown || snap.Room.Destination.Action != machineStatusUnknown || snap.Room.Destination.Confirm() != rooms.ConfirmExplain {
		t.Fatalf("unavailable lease did not gate Pong visibly: %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, time.Now())
	if fake.launchCount() != 0 || app.Snapshot().Status != machineStatusUnknown {
		t.Fatalf("unavailable lease A was not refused visibly: launches=%d status=%q", fake.launchCount(), app.Snapshot().Status)
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
	app := startCoreRoom(t, blocked, nil, coreRoomScript(tenfootPongID, "ColecoVision", false, "Needs a cartridge"), true)
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
	app.mu.Lock()
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "held", Owner: "other", Purpose: "in-use"}
	app.mu.Unlock()
	snap = app.Snapshot()
	if snap.Room.Destination.Status != "Needs a cartridge" || snap.Room.Destination.Action != "Needs a cartridge" {
		t.Fatalf("lease overrode blocked-core copy: %+v", snap.Room.Destination)
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
	snap := Snapshot{Room: RoomSnapshot{Open: true}, LocalCorePhase: localPhaseLaunching,
		LocalCoreTitle: "FES Pong", LocalCoreStartedAt: time.Now().Add(-time.Second),
		LocalCorePresentsPaused: true, Status: "Starting FES Pong…"}
	copy := launchOverlayCopy(snap)
	if !copy.Visible || copy.Phase != "Starting core" || copy.Elapsed != "" || !strings.Contains(copy.Hint, "Select+Start") {
		t.Fatalf("armed loading frame lacks overlay: %+v", copy)
	}
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

func TestDeferredLocalStopDispatchesAfterRunning(t *testing.T) {
	hold := make(chan struct{})
	fake := &fakeLocalCores{hold: hold}
	app := startCoreRoom(t, fake, &fakePadFeed{}, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	waitFor(t, app, "pong tile", func(s Snapshot) bool { return s.Room.Open && s.Room.Destination.Label == "FES Pong" })
	app.HandleCommand(CmdSelect, time.Unix(100, 0))
	waitFor(t, app, "armed frame", func(s Snapshot) bool { return s.LocalCorePhase == localPhaseLaunching && launchOverlayCopy(s).Visible })
	now := time.Now()
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), now)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), now.Add(time.Millisecond))
	app.Tick(now.Add(localChordHold + time.Second))
	app.mu.Lock()
	deferred := app.localStopAfterStart
	app.mu.Unlock()
	if !deferred || fake.stopCount() != 0 {
		t.Fatalf("stop was not deferred: deferred=%v stops=%d", deferred, fake.stopCount())
	}
	close(hold)
	waitFor(t, app, "deferred stop", func(s Snapshot) bool { return fake.stopCount() == 1 && s.LocalCorePhase != localPhaseLaunching })
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
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom("cores")
	app.SetKitLocal(cores, feed)
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app, h
}

func TestKitDirectPadLaunchInputStopAndRelaunch(t *testing.T) {
	hold := make(chan struct{})
	fake := &fakeLocalCores{hold: hold}
	feed := &fakePadFeed{}
	app, host := startCoreRoomWithHost(t, fake, feed, coreRoomScript(tenfootPongID, "FES Pong", true, ""))
	snap := waitFor(t, app, "pong tile", func(s Snapshot) bool {
		return s.Room.Open && s.Room.ID == "cores" && s.Room.Destination.Kind == rooms.KindCore && s.Room.Destination.Label == "FES Pong"
	})
	focus := snap.Room.Destination.Label
	packageID := snap.Room.Destination.PackageID

	app.HandleCommand(CmdSelect, time.Unix(100, 0))
	snap = app.Snapshot()
	if !snap.LocalCorePresentsPaused || snap.LocalCorePhase != localPhaseLaunching {
		t.Fatalf("launch did not pause the menu phase=%s paused=%v", snap.LocalCorePhase, snap.LocalCorePresentsPaused)
	}
	app.HandleLocalPad(padButton(remoteinput.ButtonA, true), time.Unix(110, 0))
	if feed.count(remoteinput.ButtonA, remoteinput.ActionPress) != 0 {
		t.Fatal("launch forwarded A to the core")
	}
	if host.launchCount() != 0 || host.inputCount() != 0 {
		t.Fatalf("launch touched the host launches=%d inputs=%d", host.launchCount(), host.inputCount())
	}

	close(hold)
	waitFor(t, app, "running", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused && s.Room.Destination.Label == focus
	})
	if fake.launchCount() != 1 {
		t.Fatalf("local launches %d", fake.launchCount())
	}
	tPlay := time.Unix(200, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonA, true), tPlay)
	app.HandleLocalPad(padButton(remoteinput.ButtonA, false), tPlay.Add(20*time.Millisecond))
	if feed.count(remoteinput.ButtonA, remoteinput.ActionPress) != 1 || feed.count(remoteinput.ButtonA, remoteinput.ActionRelease) != 1 {
		t.Fatalf("running core did not see A: %+v", feed.snapshot())
	}

	t0 := time.Unix(210, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t0)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t0)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, false), t0.Add(400*time.Millisecond))
	app.Tick(t0.Add(2 * time.Second))
	if fake.stopCount() != 0 || !app.Snapshot().LocalCorePresentsPaused {
		t.Fatal("short chord stopped the core or resumed the menu")
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
	assertRoomFocus(t, snap, focus, packageID)

	app.HandleCommand(CmdSelect, time.Unix(400, 0))
	waitFor(t, app, "relaunch", func(s Snapshot) bool {
		return s.LocalCorePhase == localPhaseRunning && s.LocalCorePresentsPaused && s.Room.Destination.Label == focus
	})
	if fake.launchCount() != 2 {
		t.Fatalf("relaunch count %d", fake.launchCount())
	}
	t2 := time.Unix(500, 0)
	app.HandleLocalPad(padButton(remoteinput.ButtonA, true), t2)
	if feed.count(remoteinput.ButtonA, remoteinput.ActionPress) != 2 {
		t.Fatalf("relaunch did not take A locally: %+v", feed.snapshot())
	}
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t2.Add(time.Second))
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t2.Add(time.Second+10*time.Millisecond))
	app.Tick(t2.Add(2*time.Second + 10*time.Millisecond))
	snap = waitFor(t, app, "second resume", func(s Snapshot) bool {
		return !s.LocalCorePresentsPaused && s.LocalCorePhase == "" && roomTextHas(s, "resumed-2")
	})
	assertRoomFocus(t, snap, focus, packageID)
	if fake.stopCount() != 2 {
		t.Fatalf("stops %d", fake.stopCount())
	}
	if host.launchCount() != 0 || host.inputCount() != 0 {
		t.Fatalf("pad path used the host launches=%d inputs=%d", host.launchCount(), host.inputCount())
	}
}

func assertRoomFocus(t *testing.T, snap Snapshot, label, packageID string) {
	t.Helper()
	if !snap.Room.Open || snap.Room.ID != "cores" || snap.RoomPicker.Open || len(snap.Room.Parents) != 0 {
		t.Fatalf("room was not restored: open=%v id=%q picker=%v parents=%v", snap.Room.Open, snap.Room.ID, snap.RoomPicker.Open, snap.Room.Parents)
	}
	if snap.Room.Destination.Label != label || snap.Room.Destination.PackageID != packageID || snap.Room.Destination.Kind != rooms.KindCore {
		t.Fatalf("focus moved: %+v", snap.Room.Destination)
	}
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
	mu          sync.Mutex
	cores       []localcores.Core
	listErr     error
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
