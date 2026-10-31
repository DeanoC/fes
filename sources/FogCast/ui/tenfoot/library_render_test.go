package tenfoot

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	app.mu.Lock()
	app.setSafeAreaPctLocked(DefaultSafeAreaPct, false)
	app.mu.Unlock()
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
	t.Run("grid-1024", func(t *testing.T) {
		writeLibraryScreenshot(t, libraryRenderApp(t, 1024, 600, true), "grid")
	})
	for _, size := range [][2]int{{1280, 720}, {1024, 600}} {
		t.Run(fmt.Sprintf("action-details-%d", size[0]), func(t *testing.T) {
			app := libraryRenderApp(t, size[0], size[1], true)
			app.mu.Lock()
			if !app.enterBrowseActionsLocked(1) {
				t.Fatal("Details action is unavailable")
			}
			app.mu.Unlock()
			writeLibraryScreenshot(t, app, "action-details")
		})
	}
	for _, mode := range []LayoutKind{LayoutShelf, LayoutList} {
		t.Run(mode.String(), func(t *testing.T) {
			app := libraryRenderApp(t, 1280, 720, true)
			app.grid.Mode = mode
			app.grid.Layout(1280, 720)
			writeLibraryScreenshot(t, app, mode.String())
		})
	}
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

func TestRoomDetailsKeepsLongSummaryAndCuratorNoteInsidePane(t *testing.T) {
	for _, size := range [][2]int{{1024, 600}, {1280, 720}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			snap := libraryRenderApp(t, size[0], size[1], false).Snapshot()
			snap.Grid.Safe = insetsFromPct(size[0], size[1], DefaultSafeAreaPct)
			snap.Grid.Layout(size[0], size[1])
			snap.Detail.Open, snap.Detail.Count = true, 1
			snap.FocusDetail.Title = "A long selected title with a special anniversary edition and additional adventures"
			snap.FocusDetail.Summary = strings.Repeat("A detailed history of this game and its many different adventures. ", 18)
			snap.DetailPlay = rooms.Destination{Kind: rooms.KindGame, Availability: rooms.AvailUnavailable, Status: "Choose the required firmware before starting this title on the selected machine."}
			snap.Room = RoomSnapshot{Open: true, Width: snap.Grid.contentWidth(), Height: snap.Grid.contentHeight(),
				OffsetX: snap.Grid.contentLeft(), OffsetY: snap.Grid.contentTop(), Frame: rooms.Frame{HasClear: true, Clear: heroBackground},
				Destination: rooms.Destination{History: rooms.History{Played: true, Completed: true}, NoteBy: "The room curator", Note: strings.Repeat("Look for the hidden route and explore the bonus stages. ", 8)}}
			geom, ok := detailGeomOf(snap)
			if !ok || geom.Carousel.W == 0 {
				t.Fatal("fixture needs the narrow Details text column")
			}
			rec := gfx.NewRecorder()
			labels := map[string]gpuTexture{}
			drawDetail(rec, snap, labels, map[string]struct{}{}, map[string]gpuTexture{})
			attribution, note := false, false
			for key := range labels {
				attribution = attribution || strings.HasPrefix(key, "d-note-attr\x1f")
				note = note || strings.HasPrefix(key, "d-note-0\x1f")
			}
			if !attribution || !note {
				t.Fatal("long general summary displaced the curator attribution or note")
			}
			for _, call := range rec.Calls {
				if call.Op != "DrawMask" {
					continue
				}
				if call.X < geom.Pane.X || call.Y < geom.Pane.Y || call.X+call.MaskSrc.Dx() > geom.Pane.X+geom.Pane.W || call.Y+call.MaskSrc.Dy() > geom.Pane.Y+geom.Pane.H {
					t.Fatalf("Details text escapes its pane/safe area: x=%d y=%d mask=%v pane=%+v", call.X, call.Y, call.MaskSrc, geom.Pane)
				}
			}
			writeHomeScreenshot(t, snap, "curator-notes")
		})
	}
}

func TestLibraryBackReturnsToTheSameHomeAndSelection(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "pong", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
	app.HandleCommand(CmdRight, time.Now())
	waitFor(t, app, "zx81", func(s Snapshot) bool { return s.Room.Destination.Label == "Sinclair ZX81" })
	app.mu.Lock()
	home := app.room
	app.mu.Unlock()
	app.HandleCommand(CmdUp, time.Now()) // Hero actions above the shelf.
	app.HandleCommand(CmdUp, time.Now()) // Header routes above the actions.
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
	if got := waitFor(t, app, "shelf", func(s Snapshot) bool { return s.Room.Destination.Kind == "core" }); got.Room.Destination.Label != "Sinclair ZX81" {
		t.Fatalf("Home lost its selection: %+v", got.Room.Destination)
	}
}

func TestLibraryHomeKeyReclaimsTheSuspendedHome(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "pong", func(s Snapshot) bool { return s.Room.Destination.Label == "Pong" })
	app.mu.Lock()
	home := app.room
	app.mu.Unlock()
	app.HandleCommand(CmdUp, time.Now()) // Hero actions above the shelf.
	app.HandleCommand(CmdUp, time.Now()) // Header routes above the actions.
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
// Library, over a fixture host. paired pairs the client before Start: the
// health poller reads it without the app lock, so a test must not change it
// while running. The host serves no paired lease, so the poller reports the
// kit lease as unavailable.
func libraryHomeApp(t *testing.T, paired ...bool) *App {
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
	client := NewClient(h.server.URL, h.server.Client())
	client.paired = len(paired) > 0 && paired[0]
	app := NewApp(client, 1280, 720, 50)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.SetRooms(rooms.NewIndex([]rooms.Pack{pack}), "")
	app.SetHomeRooms(true)
	app.SetHomeRoom("lib-home")
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "home", func(s Snapshot) bool { return s.Room.Open && roomTextHas(s, "resumed-0") })
	return app
}

func TestBlockedLibraryConfirmDoesNotFailTheResumedHome(t *testing.T) {
	block := func(app *App) {
		t.Helper()
		app.HandleCommand(CmdSelect, time.Now())
		waitFor(t, app, "library", func(s Snapshot) bool { return !s.Room.Open && !s.Loading })
		blocked := availableGame("sms-datastorm", "Data Storm", "sms")
		blocked.Launchable = false
		app.mu.Lock()
		app.games = []hostclient.Game{blocked}
		app.grid.SetCount(1)
		app.grid.Focus = 0
		app.mu.Unlock()
		app.HandleCommand(CmdSelect, time.Now())
		snap := app.Snapshot()
		if !snap.Detail.Open || snap.Launch.Phase != "error" || snap.Launch.Message == "" {
			t.Fatalf("blocked confirm: detail=%v launch=%+v", snap.Detail.Open, snap.Launch)
		}
	}
	resumed := func(snap Snapshot) {
		t.Helper()
		copy := launchOverlayCopy(snap)
		if !snap.Room.Open || copy.Visible || snap.Launch.Phase == "error" || snap.Launch.Phase == "host" {
			t.Fatalf("resumed home showed a launch failure: open=%v phase=%q copy=%+v", snap.Room.Open, snap.Launch.Phase, copy)
		}
	}

	t.Run("back", func(t *testing.T) {
		app := libraryHomeApp(t)
		block(app)
		app.HandleCommand(CmdBack, time.Now())
		if app.Snapshot().Detail.Open {
			t.Fatal("Details stayed open")
		}
		app.HandleCommand(CmdBack, time.Now())
		resumed(waitFor(t, app, "home", func(s Snapshot) bool { return s.Room.Open }))
	})

	t.Run("home key", func(t *testing.T) {
		app := libraryHomeApp(t, true)
		block(app)
		app.mu.Lock()
		ready := availableGame("pong", "Pong", "pong")
		app.games = []hostclient.Game{ready}
		app.grid.SetCount(1)
		app.grid.Focus = 0
		app.launch = LaunchSnapshot{}
		app.detailOpen = false
		app.mu.Unlock()
		app.HandleCommand(CmdSelect, time.Now())
		snap := app.Snapshot()
		if !snap.Detail.Open || snap.Launch.Message != machineStatusUnknown {
			t.Fatalf("unknown confirm: detail=%v launch=%+v", snap.Detail.Open, snap.Launch)
		}
		app.HandleCommand(CmdHome, time.Now())
		resumed(waitFor(t, app, "home", func(s Snapshot) bool { return s.Room.Open && !s.Detail.Open }))
		if snap := app.Snapshot(); snap.Status == machineStatusUnknown {
			t.Fatalf("home kept the library refusal: %q", snap.Status)
		}
	})
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
	app := libraryHomeApp(t, true)
	blocked := availableGame("snes-mario-jp", "Super Mario World (JP)", "snes")
	blocked.Launchable = false
	app.mu.Lock()
	app.roomDetail = blocked
	d := app.detailPlayLocked()
	app.roomDetail = hostclient.Game{}
	host := availableGame("emu-pong", "Pong", "pong")
	host.Execution = hostclient.ExecutionHostOnly
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{State: "held", Owner: "another-shell", Purpose: "playing"}
	leased := app.gamePlayDestinationLocked(host)
	app.mu.Unlock()
	if c := detailReadiness(d); c.Headline != "Browse only" {
		t.Fatalf("edition Details described the room destination: %+v", c)
	}
	if leased.Availability != rooms.AvailUnavailable || !leased.LeaseHeld || libraryDetailHint(InputGamepad, 0, leased) != "B back  Y search  GUIDE settings" {
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

func TestRecentDetailFavoriteAndClickStayOnThatTitle(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/api/v1/library/favorites/") {
			_, _ = io.WriteString(w, `{"id":"snes-mario","favorite":true}`)
			return
		}
		if r.URL.Path == "/api/v1/session/launch" {
			http.Error(w, `{"error":{"code":"BAD_REQUEST","message":"no"}}`, http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	app := libraryRenderApp(t, 1280, 720, false)
	app.client = NewClient(server.URL, server.Client())
	app.ctx = context.Background()
	focused := app.games[0]
	recent := availableGame("snes-mario", "Super Mario World", "snes")
	recent.Launchable = false
	app.homeRecents = []hostclient.Game{recent}
	app.libraryDetail = recent
	app.detailOpen = true

	app.HandleCommand(CmdFavorite, time.Now())
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := app.Snapshot()
		app.mu.Lock()
		busy := app.favoriteBusy
		app.mu.Unlock()
		if !busy && snap.FocusDetail.Favorite {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("favorite did not settle: busy=%v detail=%+v status=%q", busy, snap.FocusDetail, snap.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
	snap := app.Snapshot()
	if snap.FocusDetail.Title != "Super Mario World" || snap.Games[0].ID != focused.ID || snap.Games[0].Favorite {
		t.Fatalf("favorite moved to the focused row: title=%q games=%+v", snap.FocusDetail.Title, snap.Games[0])
	}
	app.mu.Lock()
	recentFav := len(app.homeRecents) == 1 && app.homeRecents[0].Favorite
	app.mu.Unlock()
	if !recentFav {
		t.Fatal("recent favorite flag was not stored")
	}
	mu.Lock()
	got := append([]string(nil), paths...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "PUT /api/v1/library/favorites/snes-mario" {
		t.Fatalf("favorite request = %#v", got)
	}

	app.mu.Lock()
	app.activatePointerHitLocked(PointerHit{Kind: PointerDetail}, time.Now())
	id, phase, title, open := app.launch.GameID, app.launch.Phase, app.focusDetailLocked().Title, app.detailOpen
	app.mu.Unlock()
	if id != recent.ID || phase == "launching" || title != "Super Mario World" || !open {
		t.Fatalf("Details click left the recent: id=%s phase=%s title=%q open=%v", id, phase, title, open)
	}

	app.mu.Lock()
	app.launch = LaunchSnapshot{}
	hit := PointerHit{Kind: PointerCatalog, Index: 0}
	app.applyPointerFocusLocked(hit)
	app.activatePointerHitLocked(hit, time.Now())
	id, open = app.launch.GameID, app.detailOpen
	app.mu.Unlock()
	if open || id != focused.ID {
		t.Fatalf("catalog click stayed on the recent: open=%v id=%s", open, id)
	}
}

func TestRecentDetailQueuesItsOwnPresentation(t *testing.T) {
	app := libraryRenderApp(t, 1280, 720, false)
	recent := availableGame("recent-mario", "Mario", "snes")
	app.games = nil
	app.grid.SetCount(0)
	app.grid.Focus = -1
	app.libraryDetail = recent
	app.detailOpen = true
	app.shotIDs[recent.ID] = []string{"recent-shot"}
	app.mu.Lock()
	queue := app.queueLibraryDetailWorkLocked(time.Now(), nil)
	if _, ok := app.covers[recent.ID]; !ok {
		t.Fatal("recent cover slot was not created")
	}
	app.evictCoversLocked()
	_, kept := app.covers[recent.ID]
	queue = app.queueFocusedScreenshotsLocked(queue)
	app.mu.Unlock()
	if !kept {
		t.Fatal("empty Library evicted the recent cover")
	}
	var sawPresentation, sawShot bool
	for _, item := range queue {
		if item.item.gameID != recent.ID {
			t.Fatalf("queued work for %s", item.item.gameID)
		}
		switch item.item.kind {
		case workPresentation:
			sawPresentation = true
		case workScreenshot:
			sawShot = item.item.handle == "recent-shot"
		}
	}
	if !sawPresentation || !sawShot {
		t.Fatalf("recent work presentation=%v shot=%v queue=%+v", sawPresentation, sawShot, queue)
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
	if libraryDetailHint(InputGamepad, 0, d) != "B back  Y search  GUIDE settings" {
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

func TestLibraryHeaderListAndDetailsShareOneClassifier(t *testing.T) {
	app := libraryHomeApp(t, true)
	app.HandleCommand(CmdSelect, time.Now())
	snap := waitFor(t, app, "library", func(s Snapshot) bool { return !s.Room.Open && len(s.Games) > 0 })
	focus := snap.Games[snap.Grid.Focus]
	app.mu.Lock()
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{Unavailable: true}
	app.mu.Unlock()
	snap = app.Snapshot()
	header := snap.ChromeLine()
	row := classifyLibraryGame(focus, snap.KitPlay)
	if !snap.KitPlay.Unknown || row.Availability != snap.DetailPlay.Availability || strings.Contains(header, "Ready") {
		t.Fatalf("unknown kit: header %q, row %v, details %v", header, row.Availability, snap.DetailPlay.Availability)
	}
}

func TestReadyDetailsSaysWhereAPairedLaunchPlays(t *testing.T) {
	kit := availableGame("pong", "Pong", "pong")
	host := availableGame("emu-pong", "Pong", "pong")
	host.Execution = hostclient.ExecutionHostOnly
	paired := kitPlayState{Paired: true}
	if c := detailReadiness(classifyLibraryGame(kit, paired)); c.State != heroStateReady || c.Headline != heroPlaysOnThisTV {
		t.Fatalf("paired kit title %+v", c)
	}
	// A host-only title plays on the host; no picture claim.
	if c := detailReadiness(classifyLibraryGame(host, paired)); c.State != heroStateReady || c.Headline != "" {
		t.Fatalf("host-only title %+v", c)
	}
	// An unpaired launcher does not know where the host will send it.
	if c := detailReadiness(classifyLibraryGame(kit, kitPlayState{})); c.Headline != "" {
		t.Fatalf("unpaired title %+v", c)
	}
}

// One Back from the Library resumes Home even while the kit's status is
// unknown; only a second Back at the Home root opens the Home picker.
func TestOneBackFromTheLibraryNeverOpensTheHomePicker(t *testing.T) {
	app := libraryHomeApp(t, true)
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "library", func(s Snapshot) bool { return !s.Room.Open && len(s.Games) > 0 })
	app.mu.Lock()
	app.kitLeaseHave = true
	app.kitLease = KitLeaseStatus{Unavailable: true}
	app.mu.Unlock()
	app.HandleCommand(CmdDetails, time.Now())
	if !app.Snapshot().Detail.Open {
		t.Fatal("Details did not open while the kit status is unknown")
	}
	app.HandleCommand(CmdBack, time.Now()) // closes Details
	app.HandleCommand(CmdBack, time.Now()) // leaves the Library
	snap := app.Snapshot()
	if !snap.Room.Open || snap.RoomPicker.Open {
		t.Fatalf("Back from the Library: room=%v picker=%v", snap.Room.Open, snap.RoomPicker.Open)
	}
	app.HandleCommand(CmdBack, time.Now())
	if !app.Snapshot().RoomPicker.Open {
		t.Fatal("Back at the Home root no longer opens the Home picker")
	}
}

// coverReadinessApp is a cover grid with one playable title and one that
// needs a BIOS. Kit state is applied by the caller.
func coverReadinessApp(t *testing.T, games ...hostclient.Game) *App {
	t.Helper()
	app := NewApp(nil, 1280, 720, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.attractDisabled = true
	app.games = append([]hostclient.Game(nil), games...)
	app.grid.SetCount(len(games))
	app.grid.Focus = 0
	app.SetLayout(LayoutGrid)
	return app
}

func renderLibraryFrame(t *testing.T, snap Snapshot) (map[string]gpuTexture, *image.RGBA) {
	t.Helper()
	dev, err := gfx.NewSoftware(snap.Grid.Width, snap.Grid.Height)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]gpuTexture{}
	presentFrame(dev, snap, map[string]gpuTexture{}, labels, false)
	return labels, dev.Snapshot()
}

// paintedCellTags are the 14px bold plates drawCellBlockTag rasterizes.
func paintedCellTags(labels map[string]gpuTexture) []string {
	var out []string
	for key := range labels {
		rest, ok := strings.CutPrefix(key, "room-text|")
		if !ok {
			continue
		}
		text, rest, ok := strings.Cut(rest, "|")
		if !ok {
			continue
		}
		size, rest, ok := strings.Cut(rest, "|")
		if !ok || size != "14" {
			continue
		}
		weight, _, ok := strings.Cut(rest, "|")
		if ok && weight == "b" {
			out = append(out, text)
		}
	}
	return out
}

func cellTagPainted(labels map[string]gpuTexture, want string) bool {
	for _, got := range paintedCellTags(labels) {
		if got == want {
			return true
		}
		if trimmed, ok := strings.CutSuffix(got, "..."); ok && len(trimmed) > 0 && strings.HasPrefix(want, trimmed) {
			return true
		}
	}
	return false
}

func coverPlatePixels(img *image.RGBA, grid Grid, index int, tag string) int {
	x, y, ok := grid.CellOrigin(index)
	if !ok || tag == "" || img == nil {
		return 0
	}
	const px = 14
	maxW := grid.CellW - 16
	w := min(maxW, gfx.MeasureTextWeight(tag, px, gfx.WeightBold)+22)
	h := gfx.TextHeightWeight(px, gfx.WeightBold) + 8
	left := x + 8
	bottom := y + grid.CellH - 44
	plate := detailPill.unavailable
	n := 0
	for py := bottom - h; py < bottom; py++ {
		for px0 := left; px0 < left+w; px0++ {
			c := img.RGBAAt(px0, py)
			if c.R == plate.R && c.G == plate.G && c.B == plate.B && c.A == plate.A {
				n++
			}
		}
	}
	return n
}

func listMetaSource(labels map[string]gpuTexture, id string) string {
	prefix := "lm:" + id + "\x1f"
	for key, tex := range labels {
		if strings.HasPrefix(key, prefix) {
			return tex.labelSource
		}
	}
	return ""
}

// TestCoverLayoutShowsKitReadiness paints the cover grid for an unknown kit
// status and a foreign lease. The playable title's plate matches the Details
// headline, Needs BIOS stays a catalog tag, and the list and header use the
// same Unavailable classification.
func TestCoverLayoutShowsKitReadiness(t *testing.T) {
	play := availableGame("pong", "Pong", "pong")
	bios := availableGame("coleco-dk", "Donkey Kong", "coleco")
	bios.FirmwareRequired = true
	cases := []struct {
		name  string
		apply func(*App)
		want  string
	}{
		{
			name: "unknown status",
			apply: func(app *App) {
				app.client = &Client{paired: true}
				app.mu.Lock()
				app.kitLeaseHave = true
				app.kitLease = KitLeaseStatus{Unavailable: true}
				app.mu.Unlock()
			},
			want: machineStatusUnknown,
		},
		{
			name: "foreign lease",
			apply: func(app *App) {
				app.client = &Client{paired: true}
				app.mu.Lock()
				app.kitLeaseHave = true
				app.kitLease = KitLeaseStatus{State: "held", Owner: "another-shell", Purpose: "playing"}
				app.mu.Unlock()
			},
			want: rooms.InUseStatus,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := coverReadinessApp(t, play, bios)
			tc.apply(app)
			snap := app.Snapshot()
			if snap.Grid.Mode != LayoutGrid {
				t.Fatalf("layout %s", snap.Grid.Mode)
			}
			row := classifyLibraryGame(play, snap.KitPlay)
			detail := detailReadiness(snap.DetailPlay)
			header := snap.ChromeLine()
			tag := libraryCellTag(play, snap.KitPlay)
			biosClass := classifyLibraryGame(bios, snap.KitPlay)
			biosTag := libraryCellTag(bios, snap.KitPlay)
			if row.Availability != rooms.AvailUnavailable || snap.DetailPlay.Availability != row.Availability || tag != tc.want || tag != detail.Headline || detail.State != heroStateUnavailable {
				t.Fatalf("playable title header %q row %+v detail %+v tag %q", header, row, detail, tag)
			}
			if !strings.Contains(header, row.Availability.Label()) || strings.Contains(header, "Ready") {
				t.Fatalf("header %q", header)
			}
			if biosTag != "Needs BIOS" || detailReadiness(biosClass).Headline != "Needs BIOS" || biosClass.Availability != rooms.AvailUnavailable {
				t.Fatalf("catalog tag under kit state: tag %q class %+v", biosTag, biosClass)
			}
			labels, img := renderLibraryFrame(t, snap)
			if !cellTagPainted(labels, tag) || !cellTagPainted(labels, "Needs BIOS") {
				t.Fatalf("cover tags %q, want %q and Needs BIOS", paintedCellTags(labels), tag)
			}
			if n := coverPlatePixels(img, snap.Grid, 0, tag); n < 20 {
				t.Fatalf("playable cover plate pixels %d", n)
			}
			if n := coverPlatePixels(img, snap.Grid, 1, "Needs BIOS"); n < 20 {
				t.Fatalf("BIOS cover plate pixels %d", n)
			}

			app.mu.Lock()
			app.grid.Focus = 1
			app.mu.Unlock()
			biosSnap := app.Snapshot()
			biosDetail := detailReadiness(biosSnap.DetailPlay)
			if biosDetail.Headline != "Needs BIOS" || biosSnap.DetailPlay.Availability != biosClass.Availability || !strings.Contains(biosSnap.ChromeLine(), "Unavailable") {
				t.Fatalf("BIOS header %q detail %+v", biosSnap.ChromeLine(), biosDetail)
			}

			app.SetLayout(LayoutList)
			listSnap := app.Snapshot()
			listLabels, _ := renderLibraryFrame(t, listSnap)
			meta := listMetaSource(listLabels, play.ID)
			if !strings.Contains(meta, row.Availability.Label()) {
				t.Fatalf("list meta %q, want %q", meta, row.Availability.Label())
			}
		})
	}

	t.Run("ready stays untagged", func(t *testing.T) {
		app := coverReadinessApp(t, play, bios)
		app.client = &Client{paired: true}
		app.mu.Lock()
		app.kitLeaseHave = true
		app.kitLease = KitLeaseStatus{State: "free"}
		app.mu.Unlock()
		snap := app.Snapshot()
		if tag := libraryCellTag(play, snap.KitPlay); tag != "" || snap.DetailPlay.Availability != rooms.AvailReady || !strings.Contains(snap.ChromeLine(), "Ready") {
			t.Fatalf("ready title tagged %q header %q play %+v", tag, snap.ChromeLine(), snap.DetailPlay)
		}
		if libraryCellTag(bios, snap.KitPlay) != "Needs BIOS" {
			t.Fatal("ready kit dropped Needs BIOS")
		}
		labels, img := renderLibraryFrame(t, snap)
		if cellTagPainted(labels, machineStatusUnknown) || cellTagPainted(labels, rooms.InUseStatus) || !cellTagPainted(labels, "Needs BIOS") {
			t.Fatalf("ready cover tags %q", paintedCellTags(labels))
		}
		if n := coverPlatePixels(img, snap.Grid, 0, "Unavailable"); n != 0 {
			t.Fatalf("ready cover plate pixels %d", n)
		}
		if n := coverPlatePixels(img, snap.Grid, 1, "Needs BIOS"); n < 20 {
			t.Fatalf("BIOS cover plate pixels %d", n)
		}
	})
}
