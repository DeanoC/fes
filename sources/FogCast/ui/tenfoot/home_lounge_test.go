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

// startHomeLounge starts the lounge Home. A pairedLease body pairs the
// client before Start and makes the host serve that kit lease, so the
// session poller observes it instead of racing an injected one.
func startHomeLounge(t *testing.T, cores rooms.LocalCores, w, h int, pairedLease ...string) *App {
	t.Helper()
	host := newRoomHost(t)
	client := NewClient(host.server.URL, host.server.Client())
	if len(pairedLease) > 0 {
		host.pairedLease = true
		host.pairedLeaseBody = pairedLease[0]
		client.paired = true
	}
	app := NewApp(client, w, h, 50)
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
				return s.Room.Open && s.Room.Destination.Kind == rooms.KindCore && s.Room.Destination.Label == "Pong"
			})
			writeHomeScreenshot(t, snap, "ready")
			app.HandleCommand(CmdUp, time.Now())
			app.HandleCommand(CmdRight, time.Now())
			snap = waitFor(t, app, "hero Details action", func(s Snapshot) bool {
				return s.BrowseActions.Focused && s.BrowseActions.Index == 1
			})
			writeHomeScreenshot(t, snap, "action-details")
		})
	}
	t.Run("blocked", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
		for i := 0; i < 3; i++ {
			app.HandleCommand(CmdRight, time.Now())
		}
		snap := waitFor(t, app, "blocked core", func(s Snapshot) bool { return s.Room.Destination.Label == "ColecoVision" })
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
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
		app.HandleCommand(CmdUp, time.Now()) // Hero actions above the shelf.
		app.HandleCommand(CmdUp, time.Now()) // Header routes above the actions.
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
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
		app.HandleCommand(CmdSelect, time.Now())
		snap := waitFor(t, app, "launching", func(s Snapshot) bool { return s.LocalCorePhase == localPhaseLaunching })
		snap.LocalCorePresentsPaused = false
		writeHomeScreenshot(t, snap, "launching")
	})
	t.Run("launch-failed", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores(), err: localcores.ErrBlocked}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
		app.HandleCommand(CmdSelect, time.Now())
		snap := waitFor(t, app, "failed", func(s Snapshot) bool { return s.LocalCorePhase == "" && s.LocalCoreStatus != "" })
		if snap.Room.Destination.Label != "Pong" {
			t.Fatalf("failure moved focus: %+v", snap.Room.Destination)
		}
		writeHomeScreenshot(t, snap, "launch-failed")
	})
	t.Run("in-use", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720,
			`{"state":"held","owner":"another-shell","purpose":"playing"}`)
		snap := waitFor(t, app, "in use", func(s Snapshot) bool {
			return s.Room.Destination.Label == "Pong" && heroDestinationCopy(s).Headline == rooms.InUseStatus
		})
		writeHomeScreenshot(t, snap, "in-use")
	})
	for _, size := range []struct{ w, h int }{{1280, 720}, {1024, 600}} {
		t.Run(fmt.Sprintf("long-%dx%d", size.w, size.h), func(t *testing.T) {
			cores := homeFixtureCores()
			cores[3].Name = "Sinclair ZX Spectrum 128K +2A Expanded Edition With A Long Name"
			cores[3].CoreID = "custom.long-system" // Unknown machine names remain verbatim.
			cores[3].Inputs = []string{"keyboard"}
			app := startHomeLounge(t, &fakeLocalCores{cores: cores}, size.w, size.h)
			waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
			for i := 0; i < 3; i++ {
				app.HandleCommand(CmdRight, time.Now())
			}
			snap := waitFor(t, app, "long", func(s Snapshot) bool { return s.Room.Destination.Label == cores[3].Name })
			writeHomeScreenshot(t, snap, "long")
		})
	}
	t.Run("details", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
		for i := 0; i < 4; i++ {
			app.HandleCommand(CmdRight, time.Now())
		}
		waitFor(t, app, "Sega Master System", func(s Snapshot) bool { return s.Room.Destination.Label == "Sega Master System" })
		app.HandleCommand(CmdSelect, time.Now())
		writeHomeScreenshot(t, app.Snapshot(), "details")
	})
	t.Run("coleco-arcade", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first core", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
		for i := 0; i < 3; i++ {
			app.HandleCommand(CmdRight, time.Now())
		}
		waitFor(t, app, "coleco", func(s Snapshot) bool { return s.Room.Destination.Label == "ColecoVision" })
		app.HandleCommand(CmdSelect, time.Now())
		writeHomeScreenshot(t, app.Snapshot(), "coleco-arcade")
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
		return s.Room.Destination.Kind == rooms.KindCore && s.Room.Destination.Label == "Pong"
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
	app := startHomeLounge(t, fake, 1280, 720)
	// Startup reads status once and adopts a running generation. This launch
	// is a new selection, so the probe has to see idle first. The explicit
	// running answer is only for the poll after Play, which must name this
	// package rather than the fake's default.
	waitFor(t, app, "startup idle", func(s Snapshot) bool {
		app.mu.Lock()
		checked, phase := app.localStartupStatusChecked, app.localPhase
		app.mu.Unlock()
		return checked && phase == "" && s.Room.Open && s.Room.Destination.Label == "Pong"
	})
	fake.setStatus(localcores.RunStatus{Generation: strings.Repeat("a", 64), Phase: "running", Running: true, PackageID: zx81.PackageID})
	app.HandleCommand(CmdRight, time.Now())
	snap := waitFor(t, app, "zx81", func(s Snapshot) bool { return s.Room.Destination.Label == "Sinclair ZX81" })
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
	if snap.Room.ID != installedCoresHomeID || snap.Room.Destination.Label != "Sinclair ZX81" || snap.Room.Destination.PackageID != zx81.PackageID {
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
	snap := waitFor(t, app, "ready", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
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

func TestHomeColecoTileOpensArcadeLibraryRoom(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: []localcores.Core{
		{PackageID: strings.Repeat("c", 64), CoreID: colecoCoreID, Name: "FES ColecoVision", Needs: "parts", Block: "Needs a video part"},
	}}, 1280, 720)
	snap := waitFor(t, app, "Coleco home tile", func(s Snapshot) bool {
		return s.Room.Destination.Label == "ColecoVision" && s.Room.Destination.Kind == rooms.KindRoom
	})
	if !roomTextHas(snap, "Browse Coleco titles") || roomTextHas(snap, "Display setup incomplete") {
		t.Fatalf("Coleco tile still advertises incomplete display setup: %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdSelect, time.Now())
	snap = waitFor(t, app, "Coleco Arcade room", func(s Snapshot) bool {
		return s.Room.ID == colecoArcadeRoomID && s.Room.Open
	})
	if snap.Room.Destination.Kind == rooms.KindCore && snap.Room.Destination.CoreNeeds == "parts" {
		t.Fatalf("Coleco tile retained its blocked local-core destination: %+v", snap.Room.Destination)
	}
}

func TestHomeSkipsAnOmittedCoreAndHidesItsPackage(t *testing.T) {
	id := func(n byte) string { return strings.Repeat(string("0123456789abcdef"[n]), 64) }
	st := localcores.Core{PackageID: id(8), CoreID: "fes.atari-st", Name: "Atari ST", Needs: "unavailable", Launchable: false, Block: "Hardware unavailable"}
	pong := localcores.Core{PackageID: id(1), CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true}
	zx := localcores.Core{PackageID: id(2), CoreID: "fes.zx81", Name: "ZX81", Needs: "firmware", Launchable: false, Block: "Needs firmware"}
	app := startHomeLounge(t, &fakeLocalCores{cores: []localcores.Core{st, pong, zx}}, 1280, 720)
	snap := waitFor(t, app, "first installed", func(s Snapshot) bool {
		return s.Room.Destination.Label == "Pong" && roomTextHas(s, "2 installed")
	})
	if snap.Room.Destination.CoreID == "fes.atari-st" || roomTextHas(snap, "3 installed") {
		t.Fatalf("Home opened on the omitted core: %+v", snap.Room.Destination)
	}
	app.HandleCommand(CmdLeft, time.Now())
	snap = waitFor(t, app, "omitted core", func(s Snapshot) bool { return s.Room.Destination.Label == "Atari ST" })
	app.HandleCommand(CmdSelect, time.Now())
	snap = app.Snapshot()
	joined := strings.Join(heroDestinationCopy(snap).Details, "\n")
	if !snap.Room.CoreDetail || strings.Contains(joined, "Package:") || strings.Contains(joined, st.PackageID[:12]) {
		t.Fatalf("omitted Details showed a package id: detail=%v %q", snap.Room.CoreDetail, joined)
	}
	if !strings.Contains(joined, "Core: fes.atari-st") || !strings.Contains(joined, "Kit says: Hardware unavailable") {
		t.Fatalf("omitted Details %q", joined)
	}
	app.HandleCommand(CmdBack, time.Now())
	app.HandleCommand(CmdRight, time.Now())
	app.HandleCommand(CmdRight, time.Now())
	snap = waitFor(t, app, "firmware core", func(s Snapshot) bool { return s.Room.Destination.Label == "Sinclair ZX81" })
	app.HandleCommand(CmdSelect, time.Now())
	snap = app.Snapshot()
	joined = strings.Join(heroDestinationCopy(snap).Details, "\n")
	if !snap.Room.CoreDetail || !strings.Contains(joined, "Package: "+zx.PackageID[:12]+"…") || !strings.Contains(joined, "Kit says: Needs firmware") {
		t.Fatalf("installed Details %q", joined)
	}
}

func TestHomeCoreDetailsOpenInTheHeroAndBackClosesThem(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "pong", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
	for i := 0; i < 4; i++ {
		app.HandleCommand(CmdRight, time.Now())
	}
	waitFor(t, app, "master system", func(s Snapshot) bool { return s.Room.Destination.Label == "Sega Master System" })
	// A on a blocked core opens Details; it never does nothing.
	app.HandleCommand(CmdSelect, time.Now())
	snap := app.Snapshot()
	c := heroDestinationCopy(snap)
	if !snap.Room.CoreDetail || len(c.Details) == 0 || !strings.Contains(strings.Join(c.Details, "\n"), "Kit says: Needs a cartridge") {
		t.Fatalf("blocked core Details %+v", c)
	}
	app.HandleCommand(CmdBack, time.Now())
	snap = app.Snapshot()
	if snap.Room.CoreDetail || !snap.Room.Open || snap.Room.ID != installedCoresHomeID || snap.RoomPicker.Open {
		t.Fatalf("Back did not just close Details: detail=%v room=%s picker=%v", snap.Room.CoreDetail, snap.Room.ID, snap.RoomPicker.Open)
	}
	// Moving away closes Details too.
	app.HandleCommand(CmdDetails, time.Now())
	if !app.Snapshot().Room.CoreDetail {
		t.Fatal("Y did not open Details")
	}
	app.HandleCommand(CmdLeft, time.Now())
	snap = waitFor(t, app, "moved", func(s Snapshot) bool { return s.Room.Destination.Label != "Sega Master System" })
	if snap.Room.CoreDetail {
		t.Fatal("Details followed focus to another core")
	}
}

func TestHomeHeroButtonsDoWhatTheySayWithAPointer(t *testing.T) {
	fake := &fakeLocalCores{cores: homeFixtureCores()}
	app := startHomeLounge(t, fake, 1280, 720)
	snap := waitFor(t, app, "pong", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
	pane, _ := roomDestGeom(snap)
	primary, secondary := heroButtonRects(snap, pane)
	if primary.W == 0 || secondary.W == 0 || primary.contains(secondary.X+1, secondary.Y+1) {
		t.Fatalf("buttons %+v %+v", primary, secondary)
	}
	app.PointerClick(secondary.X+secondary.W/2, secondary.Y+secondary.H/2, time.Now())
	if !app.Snapshot().Room.CoreDetail || fake.launchCount() != 0 {
		t.Fatalf("Details button: detail=%v launches=%d", app.Snapshot().Room.CoreDetail, fake.launchCount())
	}
	app.PointerClick(primary.X+primary.W/2, primary.Y+primary.H/2, time.Now())
	waitFor(t, app, "launch", func(s Snapshot) bool { return fake.launchCount() == 1 })
}

func TestHomeSaysCheckingWhileAFailedLaunchReconciles(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "pong", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
	// The launcher's startup status poll answers idle, which rightly clears
	// a reconcile. Let it settle, and hold later polls, so the flag set here
	// is the state under test.
	waitFor(t, app, "startup status", func(Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return app.localStartupStatusChecked && !app.localStatusBusy
	})
	app.mu.Lock()
	app.localReconcileAfterFailure = true
	app.localStatusNext = time.Now().Add(time.Hour)
	app.mu.Unlock()
	snap := app.Snapshot()
	if c := heroDestinationCopy(snap); c.State != heroStateChecking || c.Primary == "Play" || snap.Room.Destination.Confirm() != rooms.ConfirmWait {
		t.Fatalf("reconciling core offered Play: %+v confirm=%v", c, snap.Room.Destination.Confirm())
	}
	app.mu.Lock()
	app.localReconcileAfterFailure = false
	app.mu.Unlock()
	if c := heroDestinationCopy(app.Snapshot()); c.State != heroStateReady {
		t.Fatalf("did not return to Ready: %+v", c)
	}
}

// TestHomeStopHintFitsTheHeroColumn checks the ready-core stop line at the
// two Home sizes. 1280×720 on the kit is the room inside the default 5% safe
// area, which is where a longer sentence lost the one-second hold. 1024×600
// is the compact hero.
func TestHomeStopHintFitsTheHeroColumn(t *testing.T) {
	safe := insetsFromPct(1280, 720, DefaultSafeAreaPct)
	sizes := []struct {
		name string
		w, h int
	}{
		{"1280x720", 1280, 720},
		{"1280x720 safe", 1280 - safe.Left - safe.Right, 720 - safe.Top - safe.Bottom},
		{"1024x600", 1024, 600},
	}
	cores := []rooms.Destination{
		{Kind: rooms.KindCore, Label: "FES Pong", CoreLaunchable: true, Availability: rooms.AvailReady},
		{Kind: rooms.KindCore, Label: "Zx81", CoreLaunchable: true, Availability: rooms.AvailReady, Inputs: []string{"keyboard"}},
		{Kind: rooms.KindCore, Label: "FES ZX Spectrum 48K", CoreLaunchable: true, Availability: rooms.AvailReady, Inputs: []string{"keyboard"}},
	}
	if !strings.Contains(heroStopCopy, "one second") {
		t.Fatalf("stop hint lost the hold: %q", heroStopCopy)
	}
	for _, size := range sizes {
		layout, ok := rooms.HeroPanel(size.w, size.h)
		if !ok {
			t.Fatalf("%s has no hero", size.name)
		}
		for _, d := range cores {
			snap := Snapshot{Room: RoomSnapshot{Open: true, Hero: true, Destination: d}}
			c := heroDestinationCopy(snap)
			avail := heroTextBudget(c, layout.Text.H)
			plan := planHero(c, layout.Text.W, avail)
			got := strings.Join(plan.explainLines, " ")
			if got != heroStopCopy || strings.Contains(got, "...") {
				t.Fatalf("%s %s stop hint %q, want %q", size.name, d.Label, got, heroStopCopy)
			}
			for _, line := range plan.explainLines {
				if w := gfx.MeasureText(line, heroExplainPx); w > layout.Text.W {
					t.Fatalf("%s %s line %q is %d px in a %d px column", size.name, d.Label, line, w, layout.Text.W)
				}
			}
			if used := usedHeroPlan(c, plan); used > avail {
				t.Fatalf("%s %s uses %d over %d", size.name, d.Label, used, avail)
			}
		}
	}
}

func TestHomeHeroFitsLongTitlesAboveTheButtons(t *testing.T) {
	long := "Sinclair ZX Spectrum 128K +2A Expanded Edition With A Long Name"
	for _, size := range []struct{ w, h int }{{1280, 720}, {1024, 600}} {
		for _, d := range []rooms.Destination{
			{Kind: rooms.KindCore, Label: long, PackageID: strings.Repeat("a", 64), CoreLaunchable: true, Availability: rooms.AvailReady, Inputs: []string{"keyboard", "mouse"}},
			{Kind: rooms.KindCore, Label: long, PackageID: strings.Repeat("a", 64), CoreNeeds: "parts", CoreBlock: "Needs a video part", Availability: rooms.AvailUnavailable, Inputs: []string{"keyboard"}},
		} {
			layout, _ := rooms.HeroPanel(size.w, size.h)
			snap := Snapshot{Room: RoomSnapshot{Open: true, Hero: true, WaitAck: true, Destination: d}}
			c := heroDestinationCopy(snap)
			c.Ack = waitAckCopy(InputGamepad)
			avail := layout.Text.H - heroButtonH - 8
			plan := planHero(c, layout.Text.W, avail)
			fixed := gfx.TextHeight(heroMetaPx) + 12 +
				max(gfx.TextHeight(heroLinePx), gfx.TextHeightWeight(heroStatePx, gfx.WeightBold)+10) + 8
			used := fixed + len(plan.titleLines)*lineStep(heroTitlePx, gfx.WeightBold) +
				(len(plan.explainLines)+len(plan.details))*lineStep(heroExplainPx, gfx.WeightRegular)
			if plan.ack {
				used += lineStep(heroExplainPx, gfx.WeightBold) + 4
			}
			if plan.kicker {
				used += gfx.TextHeightWeight(heroKickerPx, gfx.WeightBold) + 6
			}
			if !plan.ack || used > avail || len(plan.titleLines) == 0 {
				t.Fatalf("%dx%d %s: text %d px over %d px available", size.w, size.h, d.CoreNeeds, used, avail)
			}
		}
	}
}

func TestHomeHeroDetailsOmitAPackageForAnOmittedCore(t *testing.T) {
	omitted := rooms.Destination{
		Kind: rooms.KindCore, Label: "Atari ST",
		PackageID: strings.Repeat("b", 64), CoreID: "fes.atari-st",
		CoreNeeds: "unavailable", CoreBlock: "Hardware unavailable",
		Availability: rooms.AvailUnavailable,
	}
	lines := strings.Join(heroCoreDetails(omitted), "\n")
	if strings.Contains(lines, "Package:") || !strings.Contains(lines, "Core: fes.atari-st") || !strings.Contains(lines, "Kit says: Hardware unavailable") {
		t.Fatalf("omitted details %q", lines)
	}
	installed := omitted
	installed.CoreNeeds = "media"
	installed.CoreBlock = "Needs a cartridge"
	if !strings.Contains(strings.Join(heroCoreDetails(installed), "\n"), "Package: ") {
		t.Fatal("an installed block lost its package line")
	}
}

func TestHomeHeroDetailsKeepTheBlockWhenLinesAreDropped(t *testing.T) {
	block := "Kit says: Needs a video part"
	c := heroCopy{
		Title:    "ColecoVision",
		State:    heroStateUnavailable,
		Headline: "Display setup incomplete",
		Details:  []string{"Core: fes.coleco", "Package: abcdefghijkl…", block},
	}
	fixed := 12 + max(gfx.TextHeight(heroLinePx), gfx.TextHeightWeight(heroStatePx, gfx.WeightBold)+10) + 8
	titleH := lineStep(heroTitlePx, gfx.WeightBold)
	detailH := lineStep(heroExplainPx, gfx.WeightRegular)
	plan := planHero(c, 480, fixed+titleH+detailH)
	if len(plan.details) != 1 || plan.details[0] != block {
		t.Fatalf("truncated details %q", plan.details)
	}
	if used := usedHeroPlan(c, plan); used > fixed+titleH+detailH {
		t.Fatalf("truncated plan uses %d", used)
	}
	plan = planHero(c, 480, fixed+titleH+3*detailH)
	if len(plan.details) != 3 || plan.details[2] != block {
		t.Fatalf("untruncated details %q", plan.details)
	}
}

func TestHomeHeroDetailsKeepTheBlockOnACompactScreen(t *testing.T) {
	long := "Sinclair ZX Spectrum 128K +2A Expanded Edition With A Long Name"
	block := "Needs a video part"
	for _, tc := range []struct {
		label  string
		inputs []string
	}{
		{label: "FES ColecoVision"},
		{label: long, inputs: []string{"keyboard"}},
	} {
		layout, ok := rooms.HeroPanel(1024, 600)
		if !ok {
			t.Fatal("1024x600 has no hero")
		}
		d := rooms.Destination{
			Kind: rooms.KindCore, Label: tc.label,
			PackageID: strings.Repeat("a", 64), CoreID: "fes.coleco",
			CoreNeeds: "parts", CoreBlock: block,
			Availability: rooms.AvailUnavailable, Inputs: tc.inputs,
		}
		snap := Snapshot{Room: RoomSnapshot{Open: true, Hero: true, CoreDetail: true, Destination: d}}
		c := heroDestinationCopy(snap)
		avail := heroTextBudget(c, layout.Text.H)
		plan := planHero(c, layout.Text.W, avail)
		if !strings.Contains(strings.Join(plan.details, "\n"), kitSaysPrefix+block) {
			t.Fatalf("%q details %q", tc.label, plan.details)
		}
		if used := usedHeroPlan(c, plan); used > avail || len(plan.titleLines) == 0 {
			t.Fatalf("%q uses %d over %d", tc.label, used, avail)
		}
	}
}

func usedHeroPlan(c heroCopy, plan heroPlan) int {
	h := 12
	if c.Meta != "" {
		h += gfx.TextHeight(heroMetaPx)
	}
	if c.State != "" || c.Headline != "" {
		h += max(gfx.TextHeight(heroLinePx), gfx.TextHeightWeight(heroStatePx, gfx.WeightBold)+10) + 8
	}
	if plan.kicker {
		h += gfx.TextHeightWeight(heroKickerPx, gfx.WeightBold) + 6
	}
	h += len(plan.titleLines) * lineStep(heroTitlePx, gfx.WeightBold)
	h += (len(plan.explainLines) + len(plan.details)) * lineStep(heroExplainPx, gfx.WeightRegular)
	if plan.ack {
		h += lineStep(heroExplainPx, gfx.WeightBold) + 4
	}
	return h
}

func TestHomeSmallLayoutPointerReachesCoresAndRoutes(t *testing.T) {
	fake := &fakeLocalCores{cores: homeFixtureCores()}
	app := startHomeLounge(t, fake, 470, 350)
	snap := waitFor(t, app, "compact hits", func(s Snapshot) bool {
		return s.Room.Open && !s.Room.Hero && s.Room.Destination.Label == "Pong" &&
			roomHit(s, "core:2").ID != "" && roomHit(s, "menu:1").ID != "" && roomHit(s, "menu:3").ID != ""
	})
	pane, ok := roomDestGeom(snap)
	if !ok {
		t.Fatal("compact destination strip missing")
	}
	core := roomHitCenter(snap, "core:2")
	library := roomHitCenter(snap, "menu:1")
	settings := roomHitCenter(snap, "menu:3")
	for _, pt := range [][2]int{core, library, settings} {
		if pane.contains(pt[0], pt[1]) {
			t.Fatalf("hit %v sits on the strip %+v", pt, pane)
		}
	}
	if got := HitTest(snap, core[0], core[1]); got.Kind != PointerRoom || got.KeyID != "core:2" {
		t.Fatalf("core pointer %+v", got)
	}
	now := time.Now()
	app.PointerMove(core[0], core[1], now)
	if got := app.Snapshot().Room.Destination; got.Label != "Sinclair ZX81" || fake.launchCount() != 0 {
		t.Fatalf("pointer focus %+v launches=%d", got, fake.launchCount())
	}
	app.PointerMove(library[0], library[1], now)
	if got := app.Snapshot().Room.Destination; got.Kind != rooms.KindLibrary {
		t.Fatalf("library pointer %+v", got)
	}
	if got := HitTest(snap, settings[0], settings[1]); got.Kind != PointerRoom || got.KeyID != "menu:3" {
		t.Fatalf("settings pointer %+v", got)
	}
	app.PointerClick(settings[0], settings[1], now)
	if snap = app.Snapshot(); !snap.Settings.Open || fake.launchCount() != 0 {
		t.Fatalf("settings click open=%v launches=%d dest=%+v", snap.Settings.Open, fake.launchCount(), snap.Room.Destination)
	}
}

func roomHit(s Snapshot, id string) rooms.Hit {
	for _, h := range s.Room.Frame.Hits {
		if h.ID == id {
			return h
		}
	}
	return rooms.Hit{}
}

func roomHitCenter(s Snapshot, id string) [2]int {
	h := roomHit(s, id)
	return [2]int{s.Room.OffsetX + int(h.X+h.W/2), s.Room.OffsetY + int(h.Y+h.H/2)}
}
