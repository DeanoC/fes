package tenfoot

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/shared"
)

// libraryFixture is a household library with every readiness family the
// Library shows. It is host data, not a catalogue observation.
func libraryFixture() ([]hostclient.Game, map[string]shared.FocusDetail) {
	ready := func(id, title, system string) hostclient.Game { return availableGame(id, title, system) }
	coleco := ready("coleco-dk", "Donkey Kong", "coleco")
	coleco.FirmwareRequired = true
	browse := ready("snes-mario", "Super Mario World", "snes")
	browse.Launchable = false
	offline := ready("sms-wonder", "Wonder Boy in Monster Land", "sms")
	offline.RootOnline = false
	cart := ready("sg1000-flicky", "Flicky", "sg1000")
	cart.ROMRequired = true
	games := []hostclient.Game{
		ready("sms-data-storm", "Data Storm", "sms"),
		coleco,
		ready("zx81-mazogs", "Mazogs", "zx81"),
		browse,
		offline,
		cart,
		ready("spectrum-manic", "Manic Miner", "spectrum"),
		ready("pong", "Pong", "pong"),
	}
	details := map[string]shared.FocusDetail{
		"sms-data-storm": {Title: "Data Storm", Platform: "Master System", Year: "2021", Genre: "Shooter", Studio: "Homebrew Collective", Players: "1", Summary: "A fast twin-stick style shooter written for the Master System."},
		"coleco-dk":      {Title: "Donkey Kong", Platform: "ColecoVision", Year: "1982", Genre: "Platform", Studio: "Coleco", Players: "1-2"},
		"zx81-mazogs":    {Title: "Mazogs", Platform: "ZX81", Year: "1982", Genre: "Maze", Studio: "Bud Systems", Summary: "Find the treasure in a maze full of Mazogs. Needs the 16K RAM pack."},
	}
	for _, g := range games {
		if _, ok := details[g.ID]; !ok {
			details[g.ID] = shared.FocusDetail{Title: g.Title, Platform: g.System}
		}
	}
	return games, details
}

func libraryRenderApp(t *testing.T, w, h int, withArt bool) *App {
	t.Helper()
	app := NewApp(nil, w, h, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.attractDisabled = true
	games, details := libraryFixture()
	app.games = games
	for i, g := range games {
		app.details[g.ID] = details[g.ID]
		if withArt && i%2 == 0 {
			img := image.NewRGBA(image.Rect(0, 0, 160, 224))
			for p := 0; p < len(img.Pix); p += 4 {
				img.Pix[p], img.Pix[p+1], img.Pix[p+2], img.Pix[p+3] = uint8(40+i*20), 90, 140, 255
			}
			app.covers[g.ID] = &coverSlot{phase: coverReady, image: img}
		}
	}
	app.grid.SetCount(len(games))
	return app
}

func writeLibraryScreenshot(t *testing.T, app *App, name string) Snapshot {
	t.Helper()
	snap := app.Snapshot()
	dev, err := gfx.NewSoftware(snap.Grid.Width, snap.Grid.Height)
	if err != nil {
		t.Fatal(err)
	}
	presentFrame(dev, snap, map[string]gpuTexture{}, map[string]gpuTexture{}, false)
	output := strings.TrimSpace(os.Getenv("FES_ROOM_SCREENSHOT_DIR"))
	if output == "" {
		return snap
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(output, fmt.Sprintf("library-%s-%dx%d.png", name, snap.Grid.Width, snap.Grid.Height))
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
	return snap
}

// TestLibraryNativeRender captures the Library and Details through the
// production renderer. Set FES_ROOM_SCREENSHOT_DIR to keep the PNGs.
func TestLibraryNativeRender(t *testing.T) {
	now := time.Now()
	t.Run("grid", func(t *testing.T) {
		writeLibraryScreenshot(t, libraryRenderApp(t, 1280, 720, true), "grid")
	})
	t.Run("blocked", func(t *testing.T) {
		app := libraryRenderApp(t, 1280, 720, true)
		app.HandleCommand(CmdRight, now)
		writeLibraryScreenshot(t, app, "blocked")
	})
	t.Run("blocked-confirm", func(t *testing.T) {
		app := libraryRenderApp(t, 1280, 720, true)
		app.HandleCommand(CmdRight, now)
		app.HandleCommand(CmdRight, now)
		app.HandleCommand(CmdRight, now)
		app.HandleCommand(CmdSelect, now)
		writeLibraryScreenshot(t, app, "blocked-confirm")
	})
	t.Run("details", func(t *testing.T) {
		app := libraryRenderApp(t, 1280, 720, true)
		app.HandleCommand(CmdDetails, now)
		writeLibraryScreenshot(t, app, "details")
	})
	t.Run("details-blocked-1024", func(t *testing.T) {
		app := libraryRenderApp(t, 1024, 600, true)
		app.HandleCommand(CmdRight, now)
		app.HandleCommand(CmdDetails, now)
		writeLibraryScreenshot(t, app, "details-blocked")
	})
	t.Run("details-blocked", func(t *testing.T) {
		app := libraryRenderApp(t, 1280, 720, true)
		app.HandleCommand(CmdRight, now)
		app.HandleCommand(CmdDetails, now)
		writeLibraryScreenshot(t, app, "details-blocked")
	})
}

func TestLibraryBackReturnsToTheSameHomeAndSelection(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "pong", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
	app.HandleCommand(CmdRight, time.Now())
	waitFor(t, app, "zx81", func(s Snapshot) bool { return s.Room.Destination.Label == "Zx81" })
	app.mu.Lock()
	home := app.room
	app.mu.Unlock()
	app.HandleCommand(CmdUp, time.Now())
	waitFor(t, app, "library route", func(s Snapshot) bool { return s.Room.Destination.Label == "Library" })
	app.HandleCommand(CmdSelect, time.Now())
	snap := app.Snapshot()
	if snap.Room.Open || snap.LibraryBackTo != "Home" || !strings.Contains(snap.HeaderHint(), "back to Home") {
		t.Fatalf("library from Home: room=%v backTo=%q hint=%q", snap.Room.Open, snap.LibraryBackTo, snap.HeaderHint())
	}
	app.HandleCommand(CmdBack, time.Now())
	snap = waitFor(t, app, "home again", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == installedCoresHomeID })
	app.mu.Lock()
	same := app.room == home
	app.mu.Unlock()
	if !same || snap.LibraryBackTo != "" {
		t.Fatalf("Back did not resume the same Home instance (same=%v backTo=%q)", same, snap.LibraryBackTo)
	}
	app.HandleCommand(CmdDown, time.Now())
	if got := waitFor(t, app, "shelf", func(s Snapshot) bool { return s.Room.Destination.Kind == "core" }); got.Room.Destination.Label != "Zx81" {
		t.Fatalf("Home lost its selection: %+v", got.Room.Destination)
	}
}

func TestLibraryHomeKeyReclaimsTheSuspendedHome(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "pong", func(s Snapshot) bool { return s.Room.Destination.Label == "FES Pong" })
	app.mu.Lock()
	home := app.room
	app.mu.Unlock()
	app.HandleCommand(CmdUp, time.Now())
	app.HandleCommand(CmdSelect, time.Now())
	if app.Snapshot().Room.Open {
		t.Fatal("Library did not open")
	}
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "home", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == installedCoresHomeID })
	app.mu.Lock()
	same, origin := app.room == home, app.libraryReturn
	app.mu.Unlock()
	if !same || origin != nil {
		t.Fatalf("Home key loaded a new Home (same=%v) or kept an origin (%v)", same, origin != nil)
	}
}

func TestLibraryBlockedConfirmOpensDetailsWithTheReason(t *testing.T) {
	app := libraryRenderApp(t, 1280, 720, false)
	for i := 0; i < 3; i++ {
		app.HandleCommand(CmdRight, time.Now())
	}
	app.HandleCommand(CmdSelect, time.Now())
	snap := app.Snapshot()
	c := detailReadiness(snap.DetailPlay)
	if !snap.Detail.Open || c.State != heroStateUnavailable || c.Headline != "Browse only" || c.Explain == "" {
		t.Fatalf("blocked Confirm: open=%v readiness=%+v", snap.Detail.Open, c)
	}
	if strings.Contains(snap.HeaderHint(), "play") || strings.Contains(snap.HeaderHint(), "launch") {
		t.Fatalf("Details offers Play on a browse-only title: %q", snap.HeaderHint())
	}
	app.HandleCommand(CmdBack, time.Now())
	if app.Snapshot().Detail.Open || app.Snapshot().Grid.Focus != 3 {
		t.Fatal("Back from Details lost the title")
	}
}

func TestLibraryBIOSCancelReturnsToDetails(t *testing.T) {
	app := libraryRenderApp(t, 1280, 720, false)
	app.HandleCommand(CmdRight, time.Now())
	app.HandleCommand(CmdDetails, time.Now())
	snap := app.Snapshot()
	if c := detailReadiness(snap.DetailPlay); c.Headline != "Needs BIOS" || !strings.Contains(snap.HeaderHint(), "import BIOS") {
		t.Fatalf("BIOS Details %+v hint %q", c, snap.HeaderHint())
	}
	app.HandleCommand(CmdSelect, time.Now())
	if !app.Snapshot().FirmwarePicker.Open {
		t.Fatal("A did not open the BIOS picker")
	}
	app.HandleCommand(CmdBack, time.Now())
	snap = app.Snapshot()
	if snap.FirmwarePicker.Open || !snap.Detail.Open || snap.Grid.Focus != 1 {
		t.Fatalf("cancel: picker=%v detail=%v focus=%d", snap.FirmwarePicker.Open, snap.Detail.Open, snap.Grid.Focus)
	}
}

func TestLibraryCellTagsNameEachBlock(t *testing.T) {
	games, _ := libraryFixture()
	want := map[string]string{"sms-data-storm": "", "coleco-dk": "Needs BIOS", "snes-mario": "Browse only", "sms-wonder": "Offline", "sg1000-flicky": "Needs a cartridge"}
	for _, g := range games {
		if w, ok := want[g.ID]; ok && libraryBlockTag(g.LaunchBlock()) != w {
			t.Fatalf("%s tag %q, want %q", g.ID, libraryBlockTag(g.LaunchBlock()), w)
		}
	}
}

// libraryHomeApp runs a home room that counts on_resume and offers the
// Library, over a fixture host.
func libraryHomeApp(t *testing.T) *App {
	t.Helper()
	h := newRoomHost(t)
	pack := testRoomPack(t, "lib-home", `
local resumed = 0
function on_resume() resumed = resumed + 1 end
function draw()
  gfx.clear("#000000")
  gfx.text("resumed-" .. resumed, 10, 10)
  destination.set{ kind = "library", label = "Library" }
end
`)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom("lib-home")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "home", func(s Snapshot) bool { return s.Room.Open && roomTextHas(s, "resumed-0") })
	return app
}

func TestLibraryHomeKeyClosesLibraryOverlaysAndResumesHome(t *testing.T) {
	app := libraryHomeApp(t)
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "library", func(s Snapshot) bool { return !s.Room.Open && len(s.Games) > 0 })
	app.HandleCommand(CmdDetails, time.Now())
	if !app.Snapshot().Detail.Open {
		t.Fatal("Library Details did not open")
	}
	app.HandleCommand(CmdHome, time.Now())
	snap := waitFor(t, app, "home resumed", func(s Snapshot) bool { return s.Room.Open && roomTextHas(s, "resumed-1") })
	if snap.Detail.Open || snap.Filters.Open || snap.ViewPicker {
		t.Fatalf("Library overlays followed Home: detail=%v filters=%v view=%v", snap.Detail.Open, snap.Filters.Open, snap.ViewPicker)
	}
}

func TestLibraryBackResumesHomeOnce(t *testing.T) {
	app := libraryHomeApp(t)
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "library", func(s Snapshot) bool { return !s.Room.Open })
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "home resumed", func(s Snapshot) bool { return s.Room.Open && roomTextHas(s, "resumed-1") })
	for i := 0; i < 5; i++ {
		app.Tick(time.Now())
	}
	if !roomTextHas(app.Snapshot(), "resumed-1") {
		t.Fatal("Back resumed Home more than once")
	}
}

func TestDetailsClassifyTheChosenEditionAndPairedLease(t *testing.T) {
	app := libraryHomeApp(t)
	blocked := availableGame("snes-mario-jp", "Super Mario World (JP)", "snes")
	blocked.Launchable = false
	app.mu.Lock()
	app.roomDetail = blocked
	d := app.detailPlayLocked()
	app.roomDetail = hostclient.Game{}
	host := availableGame("emu-pong", "Pong", "pong")
	host.Execution = hostclient.ExecutionHostOnly
	app.client.paired = true
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "held", Owner: "another-shell", Purpose: "playing"}
	leased := app.gamePlayDestinationLocked(host)
	app.mu.Unlock()
	if c := detailReadiness(d); c.Headline != "Browse only" {
		t.Fatalf("edition Details described the room destination: %+v", c)
	}
	if leased.Availability != rooms.AvailUnavailable || !leased.LeaseHeld || libraryDetailHint(InputGamepad, 0, leased) != "B back  GUIDE settings" {
		t.Fatalf("paired foreign lease left a host-only title Ready: %+v", leased)
	}
}

func TestBlockedRecentOpensItsOwnDetails(t *testing.T) {
	browse := availableGame("snes-mario", "Super Mario World", "snes")
	browse.Launchable = false
	ready := availableGame("recent-pong", "Recent Pong", "pong")
	busy := availableGame("recent-busy", "Recent Busy", "sms")

	t.Run("catalog block", func(t *testing.T) {
		app := libraryRenderApp(t, 1280, 720, false)
		app.homeRecents = []hostclient.Game{browse}
		app.launchRecentFromHomeLocked(browse.ID)
		snap := app.Snapshot()
		c := detailReadiness(snap.DetailPlay)
		if snap.Grid.Focus != 0 || snap.FocusDetail.Title != "Super Mario World" || !snap.Detail.Open || c.Headline != "Browse only" {
			t.Fatalf("recent details focus=%d title=%q open=%v readiness=%+v", snap.Grid.Focus, snap.FocusDetail.Title, snap.Detail.Open, c)
		}
		if strings.Contains(snap.HeaderHint(), "play") {
			t.Fatalf("blocked recent offers Play: %q", snap.HeaderHint())
		}
	})

	t.Run("empty library", func(t *testing.T) {
		app := libraryRenderApp(t, 1280, 720, false)
		app.games = nil
		app.grid.SetCount(0)
		app.grid.Focus = -1
		app.homeRecents = []hostclient.Game{browse}
		app.launchRecentFromHomeLocked(browse.ID)
		snap := app.Snapshot()
		if !snap.Detail.Open || snap.FocusDetail.Title != "Super Mario World" || detailReadiness(snap.DetailPlay).Headline != "Browse only" {
			t.Fatalf("empty library recent: open=%v title=%q play=%+v", snap.Detail.Open, snap.FocusDetail.Title, snap.DetailPlay)
		}
	})

	t.Run("unknown kit status", func(t *testing.T) {
		app := libraryRenderApp(t, 1280, 720, false)
		app.client = &Client{paired: true}
		app.homeRecents = []hostclient.Game{ready}
		app.launchRecentFromHomeLocked(ready.ID)
		snap := app.Snapshot()
		c := detailReadiness(snap.DetailPlay)
		if snap.Grid.Focus != 0 || snap.FocusDetail.Title != "Recent Pong" || c.Headline != machineStatusUnknown || !strings.Contains(c.Explain, "Wait for the kit") {
			t.Fatalf("unknown status details focus=%d title=%q readiness=%+v", snap.Grid.Focus, snap.FocusDetail.Title, c)
		}
		if strings.Contains(snap.HeaderHint(), "play") || snap.Launch.GameID != ready.ID {
			t.Fatalf("unknown recent hint %q launch %+v", snap.HeaderHint(), snap.Launch)
		}
	})

	t.Run("foreign lease", func(t *testing.T) {
		app := libraryRenderApp(t, 1280, 720, false)
		app.healthHave = true
		app.health.Connection = hostclient.TargetConnection{State: "busy", Owner: "other-shell"}
		app.homeRecents = []hostclient.Game{busy}
		app.launchRecentFromHomeLocked(busy.ID)
		snap := app.Snapshot()
		c := detailReadiness(snap.DetailPlay)
		if snap.Grid.Focus != 0 || snap.FocusDetail.Title != "Recent Busy" || c.Headline != rooms.InUseStatus || snap.Launch.GameID != busy.ID {
			t.Fatalf("lease recent focus=%d title=%q readiness=%+v launch=%+v", snap.Grid.Focus, snap.FocusDetail.Title, c, snap.Launch)
		}
		app.HandleCommand(CmdSelect, time.Now())
		snap = app.Snapshot()
		if snap.Launch.Phase == "launching" || snap.Launch.GameID != busy.ID || snap.FocusDetail.Title != "Recent Busy" {
			t.Fatalf("A launched the focused row: %+v title=%q", snap.Launch, snap.FocusDetail.Title)
		}
	})
}

func TestBIOSCancelReturnsToTheRefusedTitle(t *testing.T) {
	app := libraryRenderApp(t, 1280, 720, false)
	coleco := app.games[1]
	app.openLibraryDetailLocked(coleco)
	snap := app.Snapshot()
	if snap.Grid.Focus != 0 || snap.FocusDetail.Title != "Donkey Kong" || detailReadiness(snap.DetailPlay).Headline != "Needs BIOS" {
		t.Fatalf("BIOS details focus=%d title=%q play=%+v", snap.Grid.Focus, snap.FocusDetail.Title, snap.DetailPlay)
	}
	app.HandleCommand(CmdSelect, time.Now())
	if !app.Snapshot().FirmwarePicker.Open {
		t.Fatal("A did not open the BIOS picker for the refused title")
	}
	app.HandleCommand(CmdBack, time.Now())
	snap = app.Snapshot()
	if snap.FirmwarePicker.Open || !snap.Detail.Open || snap.FocusDetail.Title != "Donkey Kong" || snap.Grid.Focus != 0 {
		t.Fatalf("cancel returned to the focused row: picker=%v detail=%v title=%q focus=%d", snap.FirmwarePicker.Open, snap.Detail.Open, snap.FocusDetail.Title, snap.Grid.Focus)
	}
}

func TestUnknownKitStatusNamesTheHeadline(t *testing.T) {
	app := libraryRenderApp(t, 1280, 720, false)
	app.client = &Client{paired: true}
	app.mu.Lock()
	d := app.gamePlayDestinationLocked(app.games[0])
	app.mu.Unlock()
	c := detailReadiness(d)
	if d.Status != machineStatusUnknown || c.Headline != machineStatusUnknown || c.Headline == "Can't play here" || !strings.Contains(c.Explain, "Wait for the kit") {
		t.Fatalf("unknown headline dest=%+v readiness=%+v", d, c)
	}
	if libraryDetailHint(InputGamepad, 0, d) != "B back  GUIDE settings" {
		t.Fatalf("unknown status offers an action: %q", libraryDetailHint(InputGamepad, 0, d))
	}
}

func TestLibraryBackHintFitsAt1024(t *testing.T) {
	snap := Snapshot{Affinity: InputGamepad, LibraryBackTo: "Home"}
	hint := snap.HeaderHint()
	if !strings.Contains(hint, "B back to Home") || 24+8*len(hint) > 1024 {
		t.Fatalf("hint %q (%d px)", hint, 24+8*len(hint))
	}
}
