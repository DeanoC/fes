package tenfoot

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
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
	return startHomeLoungeIn(t, t.TempDir(), cores, w, h, pairedLease...)
}

// startHomeLoungeIn is startHomeLounge with its prefs and room store in dir.
func startHomeLoungeIn(t *testing.T, dir string, cores rooms.LocalCores, w, h int, pairedLease ...string) *App {
	// Home follows the shipping v1-only default unless a fixture opts in below.
	return startHomeLoungeInWithAllCores(t, dir, cores, w, h, false, pairedLease...)
}

func startHomeLoungeAllCores(t *testing.T, cores rooms.LocalCores, w, h int, pairedLease ...string) *App {
	t.Helper()
	return startHomeLoungeInAllCores(t, t.TempDir(), cores, w, h, pairedLease...)
}

func startHomeLoungeInAllCores(t *testing.T, dir string, cores rooms.LocalCores, w, h int, pairedLease ...string) *App {
	t.Helper()
	return startHomeLoungeInWithAllCores(t, dir, cores, w, h, true, pairedLease...)
}

func startHomeLoungeInWithAllCores(t *testing.T, dir string, cores rooms.LocalCores, w, h int, allCores bool, pairedLease ...string) *App {
	t.Helper()
	return startHomeLoungeOn(t, newRoomHost(t), dir, cores, w, h, allCores, pairedLease...)
}

// startHomeLoungeOn starts Home against host, so a test can seed its library
// and recents first.
func startHomeLoungeOn(t *testing.T, host *roomHost, dir string, cores rooms.LocalCores, w, h int, allCores bool, pairedLease ...string) *App {
	t.Helper()
	client := NewClient(host.server.URL, host.server.Client())
	if len(pairedLease) > 0 {
		host.pairedLease = true
		host.pairedLeaseBody = pairedLease[0]
		client.paired = true
	}
	app := NewApp(client, w, h, 50)
	app.SetAllCores(allCores)
	app.SetPrefsPath(filepath.Join(dir, "tenfoot.json"))
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

// homeFamilySettled is Home with a family or recent title that is no
// longer Checking.
func homeFamilySettled(s Snapshot) bool {
	d := s.Room.Destination
	return s.Room.Open && s.Room.ID == installedCoresHomeID && (d.Kind == rooms.KindFamily || d.Kind == rooms.KindGame) &&
		d.Availability != rooms.AvailChecking && d.Availability != ""
}

// homeMachineEntry is a model's library row without media; homeTitleEntry
// adds media, so it counts as one of the family's games.
func homeMachineEntry(id, coreID string) hostclient.Game {
	return hostclient.Game{ID: id, Title: id, System: "fpga", CoreID: coreID, State: "available", RootOnline: true, Launchable: true}
}

func homeTitleEntry(id, title, coreID string) hostclient.Game {
	g := homeMachineEntry(id, coreID)
	g.Title = title
	g.MediaID = strings.Repeat("a", 64)
	g.MediaRole = "cassette"
	return g
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

func homeHeroKickerRendered(t *testing.T, snap Snapshot) bool {
	t.Helper()
	dev, err := gfx.NewSoftware(snap.Grid.Width, snap.Grid.Height)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]gpuTexture{}
	presentFrame(dev, snap, map[string]gpuTexture{}, labels, false)
	key := roomTextKey(rooms.Op{Kind: rooms.OpText, Text: "COMPUTER", Size: heroKickerPx, Bold: true})
	_, ok := labels[key]
	return ok
}

// TestHomeLoungeNativeRender runs the embedded Home room through the real
// display-list replay and software renderer. Set FES_ROOM_SCREENSHOT_DIR to
// keep the PNGs. Fixtures are host data, not hardware acceptance.
func TestHomeLoungeNativeRender(t *testing.T) {
	for _, size := range []struct {
		name string
		w, h int
	}{
		{"1280x720", 1280, 720},
		{"1280x720 safe", 1152, 648},
		{"1024x600", 1024, 600},
	} {
		t.Run("ready-"+size.name, func(t *testing.T) {
			app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, size.w, size.h)
			snap := waitFor(t, app, "first family", func(s Snapshot) bool {
				return homeFamilySettled(s) && s.Room.Destination.Label == "Sinclair ZX81" &&
					roomHit(s, "family:4").ID != ""
			})
			wantDetails := size.name != "1024x600"
			for _, reason := range []string{"No games yet", "Add your ROM files", "Not on this kit"} {
				if got := roomTextHas(snap, reason); got != wantDetails {
					t.Fatalf("%s reason %q present=%v, want %v", size.name, reason, got, wantDetails)
				}
			}
			if got := homeHeroKickerRendered(t, snap); got != wantDetails {
				c := heroDestinationCopy(snap)
				layout, _ := rooms.HeroPanel(snap.Room.Width, snap.Room.Height)
				avail := heroTextBudget(c, layout.Text.H)
				plan := planHero(c, layout.Text.W, avail)
				t.Fatalf("%s Computer kicker present=%v, want %v (avail=%d plan=%+v used=%d copy=%+v)", size.name, got, wantDetails, avail, plan, usedHeroPlan(c, plan), c)
			}
			writeHomeScreenshot(t, snap, "ready")
			app.HandleCommand(CmdUp, time.Now())
			app.HandleCommand(CmdRight, time.Now())
			snap = waitFor(t, app, "hero Details action", func(s Snapshot) bool {
				return s.BrowseActions.Focused && s.BrowseActions.Index == 1
			})
			writeHomeScreenshot(t, snap, "action-details")
		})
		t.Run("recent-"+size.name, func(t *testing.T) {
			host := newRoomHost(t)
			host.recents = []hostclient.Game{
				homeTitleEntry("lunar", "Lunar Courier", "fes.spectrum"),
				homeTitleEntry("bits", "Bit Garden", "fes.zx81"),
				homeTitleEntry("post", "Pixel Post", "fes.spectrum128"),
			}
			app := startHomeLoungeOn(t, host, t.TempDir(), &fakeLocalCores{cores: homeFixtureCores()}, size.w, size.h, false)
			snap := waitFor(t, app, "recent", func(s Snapshot) bool {
				return homeFamilySettled(s) && s.Room.Destination.GameID == "lunar" &&
					roomHit(s, "recent:3").ID != "" && roomHit(s, "family:4").ID != ""
			})
			if c := heroDestinationCopy(snap); c.Kicker != "Recently played" && size.h >= 720 {
				t.Fatalf("recent hero %+v", c)
			}
			writeHomeScreenshot(t, snap, "recent")
		})
		t.Run("needs-firmware-"+size.name, func(t *testing.T) {
			app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, size.w, size.h)
			waitFor(t, app, "first family", homeFamilySettled)
			app.HandleCommand(CmdRight, time.Now())
			app.HandleCommand(CmdRight, time.Now())
			snap := waitFor(t, app, "Atari ST", func(s Snapshot) bool { return s.Room.Destination.Label == "Atari ST" })
			if c := heroDestinationCopy(snap); c.Primary != "Add firmware" || c.State != heroStateUnavailable {
				t.Fatalf("firmware hero %+v", c)
			}
			writeHomeScreenshot(t, snap, "needs-firmware")
		})
		t.Run("no-games-"+size.name, func(t *testing.T) {
			host := newRoomHost(t)
			host.games = []hostclient.Game{homeMachineEntry("spectrum", "fes.spectrum"), homeMachineEntry("zx81", "fes.zx81"), homeTitleEntry("bits", "Bit Garden", "fes.zx81")}
			app := startHomeLoungeOn(t, host, t.TempDir(), &fakeLocalCores{cores: homeFixtureCores()}, size.w, size.h, false)
			waitFor(t, app, "first family", homeFamilySettled)
			app.HandleCommand(CmdRight, time.Now())
			snap := waitFor(t, app, "Spectrum", func(s Snapshot) bool { return s.Room.Destination.Label == "ZX Spectrum" })
			if d := snap.Room.Destination; d.Status != "No games yet" || d.Availability != rooms.AvailReady {
				t.Fatalf("no-games family %+v", d)
			}
			writeHomeScreenshot(t, snap, "no-games")
		})
	}
	t.Run("checking", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{listErr: localcores.ErrWarming}, 1280, 720)
		snap := waitFor(t, app, "room", func(s Snapshot) bool { return s.Room.Open && s.Room.Err == "" && len(s.Room.Frame.Ops) > 0 })
		writeHomeScreenshot(t, snap, "checking")
	})
	t.Run("settings", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first family", homeFamilySettled)
		app.HandleCommand(CmdUp, time.Now()) // Hero actions above the tiles.
		app.HandleCommand(CmdUp, time.Now()) // Settings, the only header route.
		snap := waitFor(t, app, "settings", func(s Snapshot) bool { return s.Room.Destination.Kind == rooms.KindAction })
		writeHomeScreenshot(t, snap, "settings")
	})
	t.Run("checking-confirm", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{listErr: localcores.ErrWarming}, 1280, 720)
		waitFor(t, app, "checking", func(s Snapshot) bool {
			return s.Room.Destination.Kind == rooms.KindFamily && s.Room.Destination.Confirm() == rooms.ConfirmWait
		})
		app.HandleCommand(CmdSelect, time.Now())
		snap := waitFor(t, app, "ack", func(s Snapshot) bool { return s.Room.WaitAck })
		writeHomeScreenshot(t, snap, "checking-confirm")
	})
	t.Run("details", func(t *testing.T) {
		app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first family", homeFamilySettled)
		app.HandleCommand(CmdDetails, time.Now())
		writeHomeScreenshot(t, app.Snapshot(), "details")
	})
	t.Run("coleco-arcade", func(t *testing.T) {
		app := startHomeLoungeAllCores(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
		waitFor(t, app, "first family", homeFamilySettled)
		for i := 0; i < 5; i++ {
			app.HandleCommand(CmdRight, time.Now())
		}
		waitFor(t, app, "coleco", func(s Snapshot) bool { return s.Room.Destination.Label == "ColecoVision" })
		app.HandleCommand(CmdSelect, time.Now())
		writeHomeScreenshot(t, app.Snapshot(), "coleco-arcade")
	})
	t.Run("no-kit", func(t *testing.T) {
		app := startHomeLounge(t, nil, 1280, 720)
		snap := waitFor(t, app, "no kit", homeFamilySettled)
		writeHomeScreenshot(t, snap, "no-kit")
	})
}

func TestHomeWorkbenchReturnsToTheSameFamily(t *testing.T) {
	fake := &fakeLocalCores{cores: homeFixtureCores()}
	app := startHomeLounge(t, fake, 1280, 720)
	waitFor(t, app, "first family", homeFamilySettled)
	app.HandleCommand(CmdRight, time.Now())
	waitFor(t, app, "Spectrum", func(s Snapshot) bool { return s.Room.Destination.Label == "ZX Spectrum" })
	app.HandleCommand(CmdSelect, time.Now())
	snap := waitFor(t, app, "workbench", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == systemRoomID })
	if fake.launchCount() != 0 {
		t.Fatalf("Open launched %d cores", fake.launchCount())
	}
	app.HandleCommand(CmdBack, time.Now())
	snap = waitFor(t, app, "home", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == installedCoresHomeID })
	if snap.Room.Destination.Label != "ZX Spectrum" {
		t.Fatalf("return lost the family: %+v", snap.Room.Destination)
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
	snap := waitFor(t, app, "ready", homeFamilySettled)
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
	if c := heroDestinationCopy(snap); c.Kicker != "Computer" || c.Meta != "4 models: ZX81, ZX80, TS1000, TS1500" || c.Primary != "Open" || c.Secondary != "Details" {
		t.Fatalf("family hero %+v", c)
	}
}

func TestHomeColecoTileOpensArcadeLibraryRoom(t *testing.T) {
	app := startHomeLoungeAllCores(t, &fakeLocalCores{cores: []localcores.Core{
		{PackageID: strings.Repeat("c", 64), CoreID: colecoCoreID, Name: "FES ColecoVision", Needs: "parts", Block: "Needs a video part"},
	}}, 1280, 720)
	waitFor(t, app, "first family", homeFamilySettled)
	for i := 0; i < 5; i++ {
		app.HandleCommand(CmdRight, time.Now())
	}
	snap := waitFor(t, app, "Coleco family", func(s Snapshot) bool {
		return s.Room.Destination.Label == "ColecoVision" && s.Room.Destination.Kind == rooms.KindFamily
	})
	if d := snap.Room.Destination; d.RoomID != colecoArcadeRoomID || d.Confirm() != rooms.ConfirmEnterRoom {
		t.Fatalf("Coleco family %+v", d)
	}
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "Coleco Arcade room", func(s Snapshot) bool {
		return s.Room.ID == colecoArcadeRoomID && s.Room.Open
	})
}

func TestHomeFamilyDetailsOpenInTheHeroAndBackClosesThem(t *testing.T) {
	app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, 1280, 720)
	waitFor(t, app, "first family", homeFamilySettled)
	app.HandleCommand(CmdDetails, time.Now())
	snap := app.Snapshot()
	c := heroDestinationCopy(snap)
	if !snap.Room.HeroDetail || c.Kicker != "Details" || len(c.Details) == 0 || c.Details[0] != "Ready: ZX81" {
		t.Fatalf("family Details %+v", c)
	}
	if hint := heroFooterHint(snap); !strings.Contains(hint, "open") || !strings.Contains(hint, "close details") {
		t.Fatalf("details hint %q", hint)
	}
	app.HandleCommand(CmdBack, time.Now())
	snap = app.Snapshot()
	if snap.Room.HeroDetail || !snap.Room.Open || snap.Room.ID != installedCoresHomeID || snap.RoomPicker.Open {
		t.Fatalf("Back did not just close Details: detail=%v room=%s picker=%v", snap.Room.HeroDetail, snap.Room.ID, snap.RoomPicker.Open)
	}
	// Moving away closes Details too.
	app.HandleCommand(CmdDetails, time.Now())
	if !app.Snapshot().Room.HeroDetail {
		t.Fatal("Y did not open Details")
	}
	app.HandleCommand(CmdRight, time.Now())
	snap = waitFor(t, app, "moved", func(s Snapshot) bool { return s.Room.Destination.Label != "Sinclair ZX81" })
	if snap.Room.HeroDetail {
		t.Fatal("Details followed focus to another family")
	}
}

func TestHomeHeroButtonsDoWhatTheySayWithAPointer(t *testing.T) {
	fake := &fakeLocalCores{cores: homeFixtureCores()}
	app := startHomeLounge(t, fake, 1280, 720)
	snap := waitFor(t, app, "first family", homeFamilySettled)
	pane, _ := roomDestGeom(snap)
	primary, secondary := heroButtonRects(snap, pane)
	if primary.W == 0 || secondary.W == 0 || primary.contains(secondary.X+1, secondary.Y+1) {
		t.Fatalf("buttons %+v %+v", primary, secondary)
	}
	app.PointerClick(secondary.X+secondary.W/2, secondary.Y+secondary.H/2, time.Now())
	if !app.Snapshot().Room.HeroDetail || fake.launchCount() != 0 {
		t.Fatalf("Details button: detail=%v launches=%d", app.Snapshot().Room.HeroDetail, fake.launchCount())
	}
	app.PointerClick(primary.X+primary.W/2, primary.Y+primary.H/2, time.Now())
	waitFor(t, app, "workbench", func(s Snapshot) bool { return s.Room.ID == hardwareRoomID })
	if fake.launchCount() != 0 {
		t.Fatalf("Open launched %d cores", fake.launchCount())
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

func TestHomeFamilyHeroFitsAtSafe720pSize(t *testing.T) {
	layout, ok := rooms.HeroPanel(1152, 648)
	if !ok {
		t.Fatal("1152x648 safe area has no hero")
	}
	d := rooms.Destination{
		Kind: rooms.KindFamily, Label: "Sinclair ZX Spectrum 128K +2A Expanded Edition With A Long Name",
		RoomID: "example.system", CoreID: "fes.spectrum", GameID: "spectrum",
		Availability: rooms.AvailReady, Status: "4 games", Models: "48K, 128K, +3, Pentagon 128",
		Kicker: "Computers", Action: "Open the selected model's workbench.",
	}
	c := heroDestinationCopy(Snapshot{Room: RoomSnapshot{Open: true, Hero: true, Destination: d}})
	avail := heroTextBudget(c, layout.Text.H)
	plan := planHero(c, layout.Text.W, avail)
	if c.Kicker != "Computers" || plan.statusGap != 6 {
		t.Fatalf("family kicker/gap = %q/%d, want Computers/6", c.Kicker, plan.statusGap)
	}
	if used := usedHeroPlan(c, plan); used > avail {
		t.Fatalf("family hero uses %d px over its %d px budget: plan=%+v", used, avail, plan)
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
		snap := Snapshot{Room: RoomSnapshot{Open: true, Hero: true, HeroDetail: true, Destination: d}}
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
	h := plan.statusGap
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

func TestHomeSmallLayoutPointerReachesFamiliesAndRoutes(t *testing.T) {
	fake := &fakeLocalCores{cores: homeFixtureCores()}
	app := startHomeLoungeAllCores(t, fake, 470, 350)
	snap := waitFor(t, app, "compact hits", func(s Snapshot) bool {
		return homeFamilySettled(s) && !s.Room.Hero &&
			roomHit(s, "family:2").ID != "" && roomHit(s, "menu:1").ID != "" && roomHit(s, "menu:3").ID != ""
	})
	pane, ok := roomDestGeom(snap)
	if !ok {
		t.Fatal("compact destination strip missing")
	}
	family := roomHitCenter(snap, "family:2")
	library := roomHitCenter(snap, "menu:1")
	settings := roomHitCenter(snap, "menu:3")
	for _, pt := range [][2]int{family, library, settings} {
		if pane.contains(pt[0], pt[1]) {
			t.Fatalf("hit %v sits on the strip %+v", pt, pane)
		}
	}
	if got := HitTest(snap, family[0], family[1]); got.Kind != PointerRoom || got.KeyID != "family:2" {
		t.Fatalf("family pointer %+v", got)
	}
	now := time.Now()
	app.PointerMove(family[0], family[1], now)
	if got := app.Snapshot().Room.Destination; got.Label != "ZX Spectrum" || fake.launchCount() != 0 {
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
