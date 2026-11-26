package rooms

import (
	"errors"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

func TestWelcomePublishesContinueAndSkip(t *testing.T) {
	r := newRoom(t, examplePack(t, "example.welcome"), Options{Setup: Setup{Step: 1, Steps: 2}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Welcome to FES") })
	text := frameText(f)
	for _, want := range []string{"Step 1 of 2\n", "Sinclair ZX81\n", "Commodore 64\n", "Next: your controller"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Welcome is missing %q:\n%s", want, text)
		}
	}
	if d := r.Destination(); d.Kind != KindAction || d.LauncherAction != "setup_continue" {
		t.Fatalf("Welcome opens on %+v", d)
	}
	r.Input("right")
	if d := r.Destination(); d.LauncherAction != "setup_skip" {
		t.Fatalf("right %+v", d)
	}
	f = stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Skip goes straight to Home.") })
	r.Input("left")
	if d := r.Destination(); d.LauncherAction != "setup_continue" {
		t.Fatalf("left %+v", d)
	}
}

func TestWelcomeOutsideSetupHasNoTracker(t *testing.T) {
	r := newRoom(t, examplePack(t, "example.welcome"), Options{})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Welcome to FES") })
	if strings.Contains(frameText(f), "Step ") {
		t.Fatalf("tracker drawn outside setup:\n%s", frameText(f))
	}
}

func TestWelcomeDrawsAllSixFamiliesWithoutTileKeyboards(t *testing.T) {
	r := newRoom(t, examplePack(t, "example.welcome"), Options{Setup: Setup{Step: 1, Steps: 2}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Welcome to FES") })
	text := frameText(f)
	for _, want := range []string{"Sinclair ZX81\n", "ZX Spectrum\n", "Atari ST\n", "Commodore 64\n", "PC/XT\n", "PC 386\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Welcome is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "In development") {
		t.Fatalf("Welcome retains the obsolete PC 386 label:\n%s", text)
	}
	if got := len(f.Ops); got > 64 {
		t.Fatalf("six-family Welcome draw has %d ops; simplified intro marks should stay under 64", got)
	}
	t.Logf("six-family Welcome draw: %d ops", len(f.Ops))
}

func TestReadyListsFamiliesAndPublishesFinishAndBack(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{
		machineEntry("zx81", "fes.zx81", ""), titleEntry("zx81-a", "fes.zx81"),
		machineEntry("c64", "fes.c64", "rom"),
	}}}
	svc.computers = []hostclient.ShelfComputer{computerEntry("zx81", "fes.zx81", "")}
	r := newRoom(t, examplePack(t, "example.setup-ready"), Options{Services: svc, Local: &fakeLocalCores{cores: []localcores.Core{{CoreID: "fes.c64", Needs: "firmware"}}}, ReleaseOnly: true, Setup: Setup{Step: 2, Steps: 2, Skipped: []string{"folders"}}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "1 game\n") })
	text := frameText(f)
	for _, want := range []string{"You can play now\n", "Step 2 of 2\n", "No games yet  ·  needs your firmware files\n", "Not on this kit\n", "Change any of this later in Settings.\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Ready is missing %q:\n%s", want, text)
		}
	}
	if d := r.Destination(); d.LauncherAction != "setup_finish" {
		t.Fatalf("Ready opens on %+v", d)
	}
	r.Input("right")
	if d := r.Destination(); d.LauncherAction != "setup_back" {
		t.Fatalf("right %+v", d)
	}
}

func TestReadyFailedRefreshClearsStaleFamilyGames(t *testing.T) {
	svc := &homeServices{fakeServices: fakeServices{games: []hostclient.Game{
		machineEntry("zx81", "fes.zx81", ""),
		titleEntry("old-title-a", "fes.zx81"),
		titleEntry("old-title-b", "fes.zx81"),
	}}}
	local := &fakeLocalCores{cores: []localcores.Core{{
		PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true,
	}}}
	r := newRoom(t, examplePack(t, "example.setup-ready"), Options{
		Services: svc, Local: local, ReleaseOnly: true,
		Setup: Setup{Step: 2, Steps: 2},
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "2 games\n") })
	if !strings.Contains(frameText(f), "2 games\n") {
		t.Fatalf("Ready did not show the initial library count:\n%s", frameText(f))
	}

	svc.setLibraryError(errors.New("host unreachable"))
	r.Resume()
	f = stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "Ready\n") && !strings.Contains(text, "2 games\n")
	})
	if strings.Contains(frameText(f), "2 games\n") {
		t.Fatalf("Ready kept the stale library count after refresh failed:\n%s", frameText(f))
	}
}

func TestHomeBannerWhileSetupIsUnfinished(t *testing.T) {
	svc := &homeServices{
		fakeServices: fakeServices{games: []hostclient.Game{machineEntry("zx81", "fes.zx81", "")}},
		recents:      []hostclient.Game{titleEntry("lunar", "fes.spectrum")},
	}
	r := loadHome(t, Options{Services: svc, ReleaseOnly: true, Setup: Setup{Unfinished: true, Skipped: []string{"ready"}}})
	f := stepUntil(t, r, func(Frame) bool { return homeSettled(r) })
	text := frameText(f)
	// The banner holds the Recently played slot, and Home opens on a family.
	if !strings.Contains(text, "Get started\n") || !strings.Contains(text, "Setup isn't finished\n") || strings.Contains(text, "Recently played") {
		t.Fatalf("banner slot:\n%s", text)
	}
	if d := r.Destination(); d.Kind != KindFamily {
		t.Fatalf("opening %+v", d)
	}
	if !r.Input("up") {
		t.Fatal("Up from the tiles did not reach the banner")
	}
	if d := r.Destination(); d.Kind != KindAction || d.LauncherAction != "setup_resume" {
		t.Fatalf("banner %+v", d)
	}
	if r.Input("up") {
		t.Fatal("Home consumed Up from the banner, its top row")
	}
	r.Input("down")
	if d := r.Destination(); d.Kind != KindFamily {
		t.Fatalf("down from the banner %+v", d)
	}
}

func setupControllersRoom(t *testing.T) *Instance {
	t.Helper()
	r := newRoom(t, examplePack(t, "example.setup-controllers"), Options{Setup: Setup{Step: 2, Steps: 3}})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func xboxPad(player int) Pad {
	return Pad{
		Player: player, Name: "Xbox 360 Controller", Family: "xbox", Recognised: true,
		Glyphs: map[string]string{"south": "Ⓐ", "east": "Ⓑ", "west": "Ⓧ", "north": "Ⓨ", "l": "LB", "r": "RB", "select": "VIEW", "start": "MENU", "dpad": "✚"},
		Sends:  map[string]string{"south": "A", "east": "B", "west": "X", "north": "Y", "l": "L", "r": "R", "select": "Select", "start": "Start"},
	}
}

func TestSetupControllersWaitsForAPad(t *testing.T) {
	r := setupControllersRoom(t)
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Pick up a controller") })
	text := frameText(f)
	for _, want := range []string{"Step 2 of 3\n", "Listening on USB and Bluetooth…\n", "Continue with keyboard\n", "Hold its pairing button"} {
		if !strings.Contains(text, want) {
			t.Fatalf("waiting step is missing %q:\n%s", want, text)
		}
	}
	if d := r.Destination(); d.Kind != KindAction || d.LauncherAction != "setup_continue" {
		t.Fatalf("waiting step opens on %+v", d)
	}
}

func TestSetupControllersShowsPlayersAndPresses(t *testing.T) {
	r := setupControllersRoom(t)
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Pick up a controller") })
	r.SetPads(1, []Pad{xboxPad(1)})
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Player 1 is ready") })
	text := frameText(f)
	for _, want := range []string{"Xbox 360 Controller\n", "Wired  ·  Xbox layout  ·  buttons as printed\n", "Add Player 2\n", "Keyboard\n", "Press any button\n", "Swap A / B\n", "Controller settings\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("found step is missing %q:\n%s", want, text)
		}
	}

	// A held button lights up and is named; with Swap A / B on, the chip
	// says what FES reads instead.
	pad := xboxPad(1)
	pad.Pressed = []string{"south"}
	pad.SwapAB = true
	pad.Sends["south"], pad.Sends["east"] = "B", "A"
	r.SetPads(2, []Pad{pad})
	f = stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Ⓐ pressed") })
	text = frameText(f)
	for _, want := range []string{"Ⓐ pressed  ·  FES reads B\n", "A and B swapped\n", "Swap A / B: on\n", "Ⓑ choose  ·  Ⓐ back"} {
		if !strings.Contains(text, want) {
			t.Fatalf("pressed step is missing %q:\n%s", want, text)
		}
	}

	if d := r.Destination(); d.LauncherAction != "setup_continue" {
		t.Fatalf("found step opens on %+v", d)
	}
	r.Input("right")
	if d := r.Destination(); d.LauncherAction != "setup_swap_ab" {
		t.Fatalf("right %+v", d)
	}
	r.Input("right")
	if d := r.Destination(); d.LauncherAction != "controllers" {
		t.Fatalf("right twice %+v", d)
	}
	r.Input("right")
	if d := r.Destination(); d.LauncherAction != "controllers" {
		t.Fatalf("right past the end %+v", d)
	}
}

func TestSetupControllersTwoPlayersAndAnUnknownPad(t *testing.T) {
	r := setupControllersRoom(t)
	unknown := xboxPad(2)
	unknown.Name, unknown.Family, unknown.Recognised, unknown.Bluetooth = "Wireless pad", "generic", false, true
	unknown.Pressed = []string{"up"}
	r.SetPads(1, []Pad{xboxPad(1), unknown})
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Players 1 and 2 are ready") })
	text := frameText(f)
	for _, want := range []string{"Bluetooth  ·  Standard layout  ·  not in FES's pad list\n", "Try your buttons  ·  P2\n", "✚ up\n", "FES doesn't know this pad yet"} {
		if !strings.Contains(text, want) {
			t.Fatalf("two-player step is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Add Player 2") {
		t.Fatalf("Add Player 2 shown with two players:\n%s", text)
	}
}

// A pad that stays Player 2 after Player 1's pad leaves offers Player 1.
func TestSetupControllersOffersTheMissingPlayer(t *testing.T) {
	r := setupControllersRoom(t)
	r.SetPads(1, []Pad{xboxPad(2)})
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Player 2 is ready") })
	text := frameText(f)
	if !strings.Contains(text, "Add Player 1\n") || strings.Contains(text, "Add Player 2") {
		t.Fatalf("lone Player 2 step:\n%s", text)
	}
}

func TestSetupControllersShowsTestedBadge(t *testing.T) {
	r := setupControllersRoom(t)
	r.SetPads(1, []Pad{xboxPad(1)})
	f := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Player 1 is ready") })
	if strings.Contains(frameText(f), "Tested") {
		t.Fatalf("untested pad showed Tested badge:\n%s", frameText(f))
	}

	tested := xboxPad(1)
	tested.Name, tested.Family, tested.Tested = "Super Famicom Controller", "generic", true
	untested := xboxPad(2)
	r.SetPads(2, []Pad{tested, untested})
	f = stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Players 1 and 2 are ready") })
	text := frameText(f)
	if !strings.Contains(text, "Super Famicom Controller\n") {
		t.Fatalf("missing tested pad name:\n%s", text)
	}
	if got := strings.Count(text, "Tested\n"); got != 1 {
		t.Fatalf("Tested badge count = %d, want 1:\n%s", got, text)
	}

	// A long controller name must truncate before pushing the Tested badge out of the left column.
	longPad := tested
	longPad.Name = "Super Famicom USB Gamepad With An Extraordinarily Long Name"
	r.SetPads(3, []Pad{longPad})
	f = stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Player 1 is ready") })
	margin := float32(36)
	leftColRight := margin + float32(int(float64(1280-int(margin)*3)*0.48))
	var badgeX float32 = -1
	for _, op := range f.Ops {
		if op.Kind == OpText && op.Text == "Tested" {
			badgeX = op.X
		}
	}
	if badgeX < 0 || badgeX >= leftColRight {
		t.Fatalf("Tested badge X = %v, want inside left column (< %v)", badgeX, leftColRight)
	}
}
