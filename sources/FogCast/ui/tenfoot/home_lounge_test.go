package tenfoot

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

// homeFixtureCores mirrors the factory kit set's names and local blocks.
// They are host fixtures, not an installed-kit observation.
func homeFixtureCores() []localcores.Core {
	id := func(n byte) string { return strings.Repeat(string("0123456789abcdef"[n]), 64) }
	return []localcores.Core{
		{PackageID: id(1), CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true, Inputs: []string{"gamepad"}},
		{PackageID: id(2), CoreID: "fes.zx81", Name: "Zx81", Needs: "none", Launchable: true, Inputs: []string{"keyboard", "gamepad"}},
		{PackageID: id(3), CoreID: "fes.spectrum", Name: "FES ZX Spectrum 48K", Needs: "none", Launchable: true, Inputs: []string{"keyboard", "gamepad"}},
		{PackageID: id(4), CoreID: "fes.coleco", Name: "FES ColecoVision", Needs: "parts", Block: "Needs a video part"},
		{PackageID: id(5), CoreID: "fes.sms", Name: "FES Master System", Needs: "media", Block: "Needs a cartridge"},
		{PackageID: id(6), CoreID: "fes.sg1000", Name: "FES SG-1000", Needs: "media", Block: "Needs a cartridge"},
		{PackageID: id(7), CoreID: "fes.ramtest", Name: "FES RAM Tester", Needs: "none", Launchable: true},
		{PackageID: id(8), CoreID: "fes.atari-st", Name: "FES Atari 520ST", Needs: "firmware", Block: "Needs firmware", Inputs: []string{"keyboard", "mouse"}},
	}
}

func startHomeLounge(t *testing.T, cores rooms.LocalCores, w, h int) *App {
	t.Helper()
	host := newRoomHost(t)
	app := NewApp(NewClient(host.server.URL, host.server.Client()), w, h, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex(rooms.Examples()), t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom(installedCoresHomeID)
	if cores != nil {
		app.SetKitLocal(cores, &fakePadFeed{})
	}
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app
}

func writeHomeScreenshot(t *testing.T, snap Snapshot, name string) {
	t.Helper()
	dev, err := gfx.NewSoftware(snap.Grid.Width, snap.Grid.Height)
	if err != nil {
		t.Fatal(err)
	}
	presentFrame(dev, snap, map[string]gpuTexture{}, map[string]gpuTexture{}, false)
	output := strings.TrimSpace(os.Getenv("FES_ROOM_SCREENSHOT_DIR"))
	if output == "" {
		return
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(output, fmt.Sprintf("home-%s-%dx%d.png", name, snap.Grid.Width, snap.Grid.Height))
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, dev.Snapshot())
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Log(path)
}

// TestHomeLoungeNativeRender runs the embedded Home room through the real
// display-list replay and software renderer. Set FES_ROOM_SCREENSHOT_DIR to
// keep the PNGs. Fixtures are host data, not hardware acceptance.
func TestHomeLoungeNativeRender(t *testing.T) {
	for _, size := range []struct{ w, h int }{{1280, 720}, {1024, 600}} {
		t.Run(fmt.Sprintf("ready-%dx%d", size.w, size.h), func(t *testing.T) {
			app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, size.w, size.h)
			snap := waitFor(t, app, "first core", func(s Snapshot) bool {
				return s.Room.Open && s.Room.Destination.Kind == rooms.KindCore && s.Room.Destination.Label == "FES Pong"
			})
			writeHomeScreenshot(t, snap, "ready")
		})
	}
	t.Run("blocked", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
		for i := 0; i < 3; i++ {
			app.HandleCommand(CmdRight, time.Now())
		}
		snap := waitFor(t, app, "blocked core", func(s Snapshot) bool { return s.Room.Destination.Label == "FES ColecoVision" })
		writeHomeScreenshot(t, snap, "blocked")
	})
	t.Run("checking", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{listErr: localcores.ErrWarming}, 1280, 720)
		snap := waitFor(t, app, "room", func(s Snapshot) bool { return s.Room.Open && s.Room.Err == "" && len(s.Room.Frame.Ops) > 0 })
		writeHomeScreenshot(t, snap, "checking")
	})
	t.Run("empty", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: []localcores.Core{}}, 1280, 720)
		snap := waitFor(t, app, "empty", func(s Snapshot) bool { return s.Room.Open && roomTextHasPrefix(s, "Nothing is installed") })
		writeHomeScreenshot(t, snap, "empty")
	})
	t.Run("settings", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
		app.HandleCommand(CmdUp, time.Now())
		app.HandleCommand(CmdRight, time.Now())
		app.HandleCommand(CmdRight, time.Now())
		snap := waitFor(t, app, "settings", func(s Snapshot) bool { return s.Room.Destination.Kind == rooms.KindAction })
		writeHomeScreenshot(t, snap, "settings")
	})
	t.Run("checking-confirm", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{listErr: localcores.ErrWarming}, 1280, 720)
		waitFor(t, app, "checking", func(s Snapshot) bool { return s.Room.Destination.Confirm() == rooms.ConfirmWait })
		app.HandleCommand(CmdSelect, time.Now())
		snap := waitFor(t, app, "ack", func(s Snapshot) bool { return s.Room.WaitAck })
		writeHomeScreenshot(t, snap, "checking-confirm")
	})
	t.Run("launching", func(t *testing.T) {
		hold := make(chan struct{})
		t.Cleanup(func() { close(hold) })
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores(), hold: hold}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
		app.HandleCommand(CmdSelect, time.Now())
		snap := waitFor(t, app, "launching", func(s Snapshot) bool { return s.LocalCorePhase == localPhaseLaunching })
		snap.LocalCorePresentsPaused = false
		writeHomeScreenshot(t, snap, "launching")
	})
	t.Run("launch-failed", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores(), err: localcores.ErrBlocked}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
		app.HandleCommand(CmdSelect, time.Now())
		snap := waitFor(t, app, "failed", func(s Snapshot) bool { return s.LocalCorePhase == "" && s.LocalCoreStatus != "" })
		if snap.Room.Destination.Label != "FES Pong" {
			t.Fatalf("failure moved focus: %+v", snap.Room.Destination)
		}
		writeHomeScreenshot(t, snap, "launch-failed")
	})
	t.Run("in-use", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
		app.mu.Lock()
		app.client.paired = true
		app.kitLeaseHave = true
		app.kitLease = KitLeaseStatus{State: "held", Owner: "another-shell", Purpose: "playing"}
		app.mu.Unlock()
		snap := app.Snapshot()
		if c := heroDestinationCopy(snap); c.Headline != rooms.InUseStatus {
			t.Fatalf("in-use hero %+v", c)
		}
		writeHomeScreenshot(t, snap, "in-use")
	})
	t.Run("no-kit", func(t *testing.T) {
		app := startHomeLounge(t, nil, 1280, 720)
		snap := waitFor(t, app, "no kit", func(s Snapshot) bool { return s.Room.Open && s.Room.Destination.Set() })
		writeHomeScreenshot(t, snap, "no-kit")
	})
}

func roomTextHasPrefix(s Snapshot, prefix string) bool {
	for _, op := range s.Room.Frame.Ops {
		if op.Kind == rooms.OpText && strings.HasPrefix(op.Text, prefix) {
			return true
		}
	}
	return false
}

// warmingLocalCores answers ErrWarming until warm, like an agent whose
// catalog read is still filling after health reports ready (#764).
type warmingLocalCores struct {
	*fakeLocalCores
	warm atomic.Bool
}

func (w *warmingLocalCores) List(ctx context.Context) ([]localcores.Core, error) {
	if !w.warm.Load() {
		return nil, localcores.ErrWarming
	}
	return w.fakeLocalCores.List(ctx)
}

func TestHomeConfirmDuringCheckingIsAcknowledgedAndNeverQueued(t *testing.T) {
	fake := &warmingLocalCores{fakeLocalCores: &fakeLocalCores{cores: homeFixtureCores()}}
	app := startHomeLounge(t, fake, 1280, 720)
	snap := waitFor(t, app, "checking", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Hero && s.Room.Destination.Confirm() == rooms.ConfirmWait
	})
	if snap.Room.WaitAck {
		t.Fatal("acknowledged a Confirm that was never pressed")
	}
	if c := heroDestinationCopy(snap); c.State != heroStateChecking || c.Primary != "" {
		t.Fatalf("checking hero %+v", c)
	}
	app.HandleCommand(CmdSelect, time.Now())
	snap = app.Snapshot()
	if !snap.Room.WaitAck || !strings.Contains(heroDestinationCopy(snap).Ack, "again") {
		t.Fatalf("Confirm during Checking was not acknowledged: %+v", snap.Room)
	}
	fake.warm.Store(true)
	snap = waitFor(t, app, "ready", func(s Snapshot) bool {
		return s.Room.Destination.Kind == rooms.KindCore && s.Room.Destination.Label == "FES Pong"
	})
	if snap.Room.WaitAck {
		t.Fatal("acknowledgement outlived Checking")
	}
	if fake.launchCount() != 0 || snap.LocalCorePhase != "" {
		t.Fatalf("a Confirm from Checking launched the first item: launches=%d phase=%q", fake.launchCount(), snap.LocalCorePhase)
	}
	if c := heroDestinationCopy(snap); c.State != heroStateReady || c.Headline != heroPlaysOnThisTV || c.Primary != "Play" || !c.Filled {
		t.Fatalf("ready hero %+v", c)
	}
}

func TestHomeLaunchReturnsToTheSameSelection(t *testing.T) {
	fake := &fakeLocalCores{cores: homeFixtureCores()}
	zx81 := homeFixtureCores()[1]
	fake.setStatus(localcores.RunStatus{Generation: strings.Repeat("a", 64), Phase: "running", Running: true, PackageID: zx81.PackageID})
	app := startHomeLounge(t, fake, 1280, 720)
	waitFor(t, app, "pong", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
	app.HandleCommand(CmdRight, time.Now())
	snap := waitFor(t, app, "zx81", func(s Snapshot) bool { return s.Room.Destination.Label == zx81.Name })
	if c := heroDestinationCopy(snap); c.Meta != "Needs a keyboard" {
		t.Fatalf("keyboard requirement not shown before Play: %+v", c)
	}
	app.HandleCommand(CmdSelect, time.Now())
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "running", func(s Snapshot) bool { return s.LocalCorePhase == localPhaseRunning })
	fake.mu.Lock()
	launched := append([]string(nil), fake.launches...)
	fake.mu.Unlock()
	if len(launched) != 1 || launched[0] != zx81.PackageID {
		t.Fatalf("launches %v", launched)
	}
	t0 := time.Now()
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, true), t0)
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, true), t0)
	fake.setStatus(localcores.RunStatus{Phase: "idle"})
	app.Tick(t0.Add(2 * time.Second))
	app.HandleLocalPad(padButton(remoteinput.ButtonSelect, false), t0.Add(2*time.Second))
	app.HandleLocalPad(padButton(remoteinput.ButtonStart, false), t0.Add(2*time.Second))
	snap = waitFor(t, app, "returned", func(s Snapshot) bool {
		return s.LocalCorePhase == "" && !s.LocalCorePresentsPaused && s.Room.Open
	})
	if fake.stopCount() != 1 {
		t.Fatalf("stops %d", fake.stopCount())
	}
	if snap.Room.ID != installedCoresHomeID || snap.Room.Destination.Label != zx81.Name || snap.Room.Destination.PackageID != zx81.PackageID {
		t.Fatalf("return lost the selection: room=%s dest=%+v", snap.Room.ID, snap.Room.Destination)
	}
}

func TestHomeHeroCopyExplainsBlocksAndLeases(t *testing.T) {
	base := Snapshot{Room: RoomSnapshot{Open: true, Hero: true}}
	for _, tc := range []struct {
		needs, block, headline string
	}{
		{"parts", "Needs a video part", "Display setup incomplete"},
		{"firmware", "Needs firmware", "Needs its system firmware"},
		{"media", "Needs a cartridge", "Needs a cartridge"},
		{"unsupported", "Needs host media delivery", "Needs host media delivery"},
	} {
		snap := base
		snap.Room.Destination = rooms.Destination{Kind: rooms.KindCore, Label: "X", PackageID: strings.Repeat("a", 64), CoreNeeds: tc.needs, CoreBlock: tc.block, Availability: rooms.AvailUnavailable}
		c := heroDestinationCopy(snap)
		if c.State != heroStateUnavailable || c.Headline != tc.headline || c.Explain == "" || c.Filled || c.Primary != "Details" {
			t.Fatalf("%s: %+v", tc.needs, c)
		}
	}
	snap := base
	snap.Room.Destination = rooms.Destination{Kind: rooms.KindCore, Label: "Pong", PackageID: strings.Repeat("a", 64), CoreLaunchable: true, LeaseHeld: true, Availability: rooms.AvailUnavailable, Status: rooms.InUseStatus}
	if c := heroDestinationCopy(snap); c.State != heroStateUnavailable || c.Headline != rooms.InUseStatus || c.Primary == "Play" {
		t.Fatalf("leased core offered Play: %+v", c)
	}
	if got := heroInputsLine([]string{"mouse", "keyboard"}); got != "Needs a keyboard and mouse" {
		t.Fatalf("inputs %q", got)
	}
}

func TestHomeHeroIsTheTapTargetAndFitsTheRoom(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	snap := waitFor(t, app, "ready", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
	pane, ok := roomDestGeom(snap)
	layout, _ := rooms.HeroPanel(snap.Room.Width, snap.Room.Height)
	if !ok || pane.X != snap.Room.OffsetX+layout.Text.X || pane.Y != snap.Room.OffsetY+layout.Text.Y {
		t.Fatalf("hero pane %+v ok=%v layout %+v", pane, ok, layout.Text)
	}
	for _, hit := range snap.Room.Frame.Hits {
		if hit.X < 0 || hit.Y < 0 || hit.X+hit.W > float32(snap.Room.Width)+1 || hit.Y+hit.H > float32(snap.Room.Height)+1 {
			t.Fatalf("control %q outside the room: %+v", hit.ID, hit)
		}
	}
	if hint := heroFooterHint(snap); !strings.Contains(hint, "details") || !strings.Contains(hint, "settings") {
		t.Fatalf("footer hint %q", hint)
	}
}
