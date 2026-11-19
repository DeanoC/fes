package rooms

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func loadCoreScreen(t *testing.T, svc Services, gameID, coreID string, opts Options) *Instance {
	t.Helper()
	if _, ok := svc.(HardwareServices); !ok {
		svc = c64ServicesWithEmptyHardware{Services: svc}
	}
	opts.Services, opts.InitialGameID, opts.InitialCoreID = svc, gameID, coreID
	if opts.Width == 0 {
		opts.Width, opts.Height = 1280, 720
	}
	r := newRoom(t, examplePack(t, "example.core"), opts)
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	return r
}

// A settled core screen has its games list, which the last paint stage adds.
func coreScreenSettled(f Frame) bool { return frameHas(f, "Games for the ") && frameHasHit(f, "media") }

func frameHasHit(f Frame, id string) bool {
	for _, h := range f.Hits {
		if h.ID == id {
			return true
		}
	}
	return false
}

func spectrumGames() []hostclient.Game {
	tape := preparedComputer("lunar", "Lunar Courier", "fes.spectrum")
	tape.CoreEntryKind, tape.PackageID, tape.MediaID, tape.MediaRole = "title", strings.Repeat("a", 64), strings.Repeat("1", 64), "cassette"
	second := preparedComputer("attic", "Attic Rally", "fes.spectrum")
	second.CoreEntryKind, second.PackageID, second.MediaID, second.MediaRole = "title", strings.Repeat("a", 64), strings.Repeat("2", 64), "cassette"
	disk := preparedComputer("harbour", "Harbour Mail", "fes.spectrum-plus3")
	disk.CoreEntryKind, disk.PackageID, disk.MediaID, disk.MediaRole = "title", strings.Repeat("c", 64), strings.Repeat("3", 64), "disk"
	return []hostclient.Game{second, disk, tape}
}

func TestCoreScreenShowsTheComputerItsDrivesAndItsGames(t *testing.T) {
	for _, size := range [][2]int{{1280, 720}, {1024, 600}} {
		games := spectrumGames()
		r := loadCoreScreen(t, &fakeServices{games: games}, "", "fes.spectrum", Options{Width: size[0], Height: size[1]})
		text := frameText(stepUntil(t, r, coreScreenSettled))
		for _, want := range []string{
			"ZX Spectrum\n", "Model\n", "48K\n", "128K/+2\n", "+2A/+3\n", "Pentagon\n",
			"ZX Spectrum 48K\n", "Memory\n", "Pad 1 is Kempston, pad 2 is\n",
			"Drives\n", "Cassette\n", "For next start\n", "Choose a tape\n",
			"Games for the 48K\n", "2 in your library\n", "Attic Rally\n", "Lunar Courier\n", "Start\n",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%dx%d core screen is missing %q:\n%s", size[0], size[1], want, text)
			}
		}
		for _, gone := range []string{"Harbour Mail", "Expansions", "Next system", "Next machine"} {
			if strings.Contains(text, gone) {
				t.Fatalf("%dx%d core screen shows %q:\n%s", size[0], size[1], gone, text)
			}
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoreScreenGameRowStartsItsExactEntry(t *testing.T) {
	games := spectrumGames()
	r := loadCoreScreen(t, &fakeServices{games: games}, "", "fes.spectrum", Options{})
	stepUntil(t, r, coreScreenSettled)
	// Focus starts on the first game; Confirm starts it.
	r.Input("select")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "attic" {
		t.Fatalf("first game launch %+v", acts)
	}
	r.Input("down")
	r.Input("select")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "lunar" {
		t.Fatalf("second game launch %+v", acts)
	}
	// The drive belongs to the game last chosen in the list.
	r.Activate("drive")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionHardwareMedia || acts[0].GameID != "lunar" || acts[0].CoreID != "fes.spectrum" ||
		acts[0].PackageID != games[2].PackageID || acts[0].ExpectedMediaID != games[2].MediaID {
		t.Fatalf("drive media binding %+v", acts)
	}
	r.Activate("game:1")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "attic" {
		t.Fatalf("pointer launch %+v", acts)
	}
}

func TestCoreScreenModelChangeFollowsWithoutActing(t *testing.T) {
	games := spectrumGames()
	r := loadCoreScreen(t, &fakeServices{games: games}, "", "fes.spectrum", Options{})
	stepUntil(t, r, coreScreenSettled)
	r.TakeFirmwareInfo()
	// Games, up to the disk button, the drive and the model.
	for _, cmd := range []string{"up", "up", "up", "right", "right"} {
		r.Input(cmd)
	}
	text := frameText(stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Games for the +2A/+3") }))
	for _, want := range []string{"ZX Spectrum +3\n", "Harbour Mail\n", "Drive A\n", "Choose a disk\n", "No drive B"} {
		if !strings.Contains(text, want) {
			t.Fatalf("+3 screen is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Lunar Courier") {
		t.Fatalf("+3 lists a 48K game:\n%s", text)
	}
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("changing the model acted: %+v", acts)
	}
	r.Activate("model:fes.pentagon")
	text = frameText(stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Games for the Pentagon") }))
	if !strings.Contains(text, "No Pentagon games yet") || !strings.Contains(text, "Set up the Pentagon") {
		t.Fatalf("empty model does not offer setup:\n%s", text)
	}
	r.Activate("start")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionHardwareSetup || acts[0].CoreID != "fes.pentagon" {
		t.Fatalf("empty model setup %+v", acts)
	}
}

func TestCoreScreenNavigationFollowsTheDesign(t *testing.T) {
	r := loadCoreScreen(t, &fakeServices{games: spectrumGames()}, "", "fes.spectrum", Options{})
	stepUntil(t, r, coreScreenSettled)
	focused := func(label string) {
		t.Helper()
		stepUntil(t, r, func(f Frame) bool {
			for _, op := range f.Ops {
				if op.Kind == OpText && op.Text == label {
					return coreScreenSettled(f)
				}
			}
			return false
		})
	}
	// Left from the games reaches Start, and Right returns to the games.
	r.Input("left")
	r.Input("select")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "attic" {
		t.Fatalf("Left then Confirm did not start the chosen game: %+v", acts)
	}
	r.Input("up")
	r.Input("select")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionFirmwareInfo || acts[0].FirstUse {
		t.Fatalf("Up from Start did not reach Firmware: %+v", acts)
	}
	r.Input("right")
	r.Input("down")
	r.Input("select")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "lunar" {
		t.Fatalf("Right did not return to the games: %+v", acts)
	}
	// Up from the first game: disk button, drive, model, header.
	for _, cmd := range []string{"up", "up", "up", "up", "up"} {
		r.Input(cmd)
	}
	r.Input("select")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionBack {
		t.Fatalf("Up did not reach Back: %+v", acts)
	}
	focused("Back")
}

func TestCoreScreenListsEachEditionAsItsOwnRow(t *testing.T) {
	base := preparedComputer("salt", "Salt Road", "fes.spectrum-plus3")
	base.CoreEntryKind, base.PackageID, base.MediaID, base.MediaRole, base.VariantCount = "title", strings.Repeat("a", 64), strings.Repeat("b", 64), "disk", 2
	edition := base
	edition.ID, edition.Region, edition.VariantCount = "salt-eu", "Europe", 0
	base.Variants = []hostclient.Game{edition}
	r := loadCoreScreen(t, &fakeServices{games: []hostclient.Game{base}}, edition.ID, base.CoreID, Options{})
	stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Salt Road (Europe)") })
	// Opened for the edition, so it is the chosen row.
	r.Input("select")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != edition.ID {
		t.Fatalf("edition launch %+v", acts)
	}
}

func TestCoreScreenRunningGameOffersReturnStopAndLiveMedia(t *testing.T) {
	game := preparedComputer("c64-game", "Copper Canyon", "fes.c64")
	game.CoreEntryKind, game.PackageID, game.MediaID, game.MediaRole = "title", strings.Repeat("ab", 32), strings.Repeat("c", 64), "disk"
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{game}}}
	host.snap.Session = c64DiskSession(game.ID)
	r := loadCoreScreen(t, host, game.ID, game.CoreID, Options{})
	r.SetSessionState("active")
	text := frameText(stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Return to play") }))
	for _, want := range []string{"Running\n", "Stop the C64 PAL\n", "In the drive now\n", "Change the disk\n", "Copper Canyon is running."} {
		if !strings.Contains(text, want) {
			t.Fatalf("running screen is missing %q:\n%s", want, text)
		}
	}
	r.Activate("media")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenTape || acts[0].SessionID != "c64-session" || acts[0].MediaBinding.Generation != 7 {
		t.Fatalf("live media binding %+v", acts)
	}
	r.Activate("start")
	if acts = r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionResumeSession {
		t.Fatalf("return to play %+v", acts)
	}
	r.Activate("game:1")
	if acts = r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionResumeSession {
		t.Fatalf("the running game's row did not return to it: %+v", acts)
	}
	r.Activate("stop")
	if acts = r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionStop {
		t.Fatalf("stop %+v", acts)
	}
}

// When a game ends, the Stop button can stay drawn for one more step after
// the session is gone; pressing it then must do nothing, not stop the room.
func TestCoreScreenStopAfterTheGameEndedDoesNothing(t *testing.T) {
	game := preparedComputer("c64-game", "Copper Canyon", "fes.c64")
	game.CoreEntryKind, game.PackageID, game.MediaID, game.MediaRole = "title", strings.Repeat("ab", 32), strings.Repeat("c", 64), "disk"
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{game}}}
	host.snap.Session = c64DiskSession(game.ID)
	r := loadCoreScreen(t, host, game.ID, game.CoreID, Options{})
	r.SetSessionState("active")
	r.SetOverlay(true)
	stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Return to play") })
	r.SetSessionState("idle")
	r.SetOverlay(false)
	r.Step(time.Now())
	r.Activate("stop")
	if err := r.Err(); err != nil {
		t.Fatalf("stale Stop stopped the room: %v", err)
	}
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("stale Stop sent %+v", acts)
	}
}

func TestCoreScreenDoesNotStartOverARunningGame(t *testing.T) {
	running := preparedComputer("c64-running", "Copper Canyon", "fes.c64")
	other := preparedComputer("c64-other", "Amber Vale", "fes.c64")
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{other, running}}}
	host.snap.Session = c64DiskSession(running.ID)
	r := loadCoreScreen(t, host, other.ID, other.CoreID, Options{})
	r.SetSessionState("active")
	stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Return to play") })
	r.Activate("game:1")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("a second game started over the running one: %+v", acts)
	}
	stepUntil(t, r, func(f Frame) bool {
		return frameHas(f, "Stop the running game before starting another.") || frameHas(f, "Copper Canyon is running.")
	})
	r.Activate("drive")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("next-start media changed while another game runs: %+v", acts)
	}
}

func TestCoreScreenZX81KeepsItsWorkshopBehindExpansions(t *testing.T) {
	game := preparedComputer("zx81-setup", "ZX81", "fes.zx81")
	game.CoreEntryKind = "machine"
	r := loadCoreScreen(t, &fakeServices{games: []hostclient.Game{game}}, game.ID, game.CoreID, Options{})
	text := frameText(stepUntil(t, r, coreScreenSettled))
	for _, want := range []string{"Sinclair ZX81\n", "Expansions\n", "TS1500\n", "The computer on its own\n", "SAVE keeps your program as\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ZX81 screen is missing %q:\n%s", want, text)
		}
	}
	r.Activate("expansions")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.hardware" || acts[0].InitialCoreID != "fes.zx81" || acts[0].InitialGameID != game.ID {
		t.Fatalf("expansions %+v", acts)
	}
}

func TestCoreScreenAsksForItsFamilysFirmwareCard(t *testing.T) {
	games := spectrumGames()
	r := loadCoreScreen(t, &fakeServices{games: games}, "harbour", "fes.spectrum", Options{})
	stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Harbour Mail") })
	req, ok := r.TakeFirmwareInfo()
	if !ok || !req.FirstUse || req.FamilyID != "spectrum" || req.CoreID != "fes.spectrum-plus3" ||
		strings.Join(req.CoreIDs, ",") != "fes.spectrum,fes.spectrum128,fes.spectrum-plus3,fes.pentagon" {
		t.Fatalf("first-use request %+v %v", req, ok)
	}
	r.Activate("firmware")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionFirmwareInfo || acts[0].FirstUse || acts[0].FamilyID != "spectrum" {
		t.Fatalf("firmware button %+v", acts)
	}
}

func TestCoreScreenLibraryErrorStaysVisible(t *testing.T) {
	svc := &fakeServices{games: spectrumGames()}
	r := loadCoreScreen(t, svc, "", "fes.spectrum", Options{})
	stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Lunar Courier") })
	svc.mu.Lock()
	svc.err = context.DeadlineExceeded
	svc.mu.Unlock()
	r.Resume()
	text := frameText(stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, context.DeadlineExceeded.Error()) }))
	if strings.Contains(text, "Lunar Courier") {
		t.Fatalf("a failed read listed games:\n%s", text)
	}
}

func TestCoreScreenRemembersModelAndGameOnUnload(t *testing.T) {
	games := spectrumGames()
	path := filepath.Join(t.TempDir(), "room.json")
	r := loadCoreScreen(t, &fakeServices{games: games}, "harbour", "fes.spectrum", Options{StorePath: path})
	stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Harbour Mail") })
	r.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Navigation struct {
			CoreID         string            `json:"core_id"`
			FamilyVariants map[string]string `json:"family_variants"`
			CoreChoices    map[string]struct {
				GameID string `json:"game_id"`
			} `json:"core_choices"`
		} `json:"navigation"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Navigation.CoreID != "fes.spectrum-plus3" || state.Navigation.FamilyVariants["spectrum"] != "fes.spectrum-plus3" ||
		state.Navigation.CoreChoices["fes.spectrum-plus3"].GameID != "harbour" {
		t.Fatalf("unload did not save the place: %s", data)
	}
	// The family tile names the family's first model; the screen returns to
	// the model last browsed.
	again := loadCoreScreen(t, &fakeServices{games: games}, "", "fes.spectrum", Options{StorePath: path})
	stepUntil(t, again, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Games for the +2A/+3") })
}

func TestCoreScreenDrawsOverARunningGame(t *testing.T) {
	r := loadCoreScreen(t, &fakeServices{games: spectrumGames()}, "", "fes.spectrum", Options{})
	stepUntil(t, r, coreScreenSettled)
	r.SetOverlay(true)
	// The light frame stays up while the parts are rebuilt, one per draw.
	f := stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && f.Clear.A != 255 })
	if f.Clear.A == 255 || f.Clear.A == 0 {
		t.Fatalf("overlay clear is not translucent: %+v", f.Clear)
	}
}

// Home opens a family with its machine entry's ID, not a game's.
func TestCoreScreenOpenedFromHomeStartsOnTheFirstGame(t *testing.T) {
	machine := preparedComputer("spectrum", "ZX Spectrum 48K", "fes.spectrum")
	machine.CoreEntryKind = "machine"
	games := []hostclient.Game{}
	for i := 0; i < 30; i++ {
		g := preparedComputer(fmt.Sprintf("title-%02d", i), fmt.Sprintf("Game %02d", i), "fes.spectrum")
		g.CoreEntryKind = "title"
		games = append(games, g)
	}
	// The machine entry sorts after the games in the library's title order.
	games = append(games, machine)
	path := filepath.Join(t.TempDir(), "room.json")
	for _, size := range [][2]int{{1280, 720}, {1024, 600}} {
		r := loadCoreScreen(t, &fakeServices{games: games}, machine.ID, machine.CoreID, Options{Width: size[0], Height: size[1]})
		text := frameText(stepUntil(t, r, coreScreenSettled))
		for _, want := range []string{"ZX Spectrum 48K\n", "The computer on its own\n", "Game 00\n", "Game 03\n", "30 in your library\n"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%dx%d screen from Home is missing %q:\n%s", size[0], size[1], want, text)
			}
		}
		r.Input("select")
		if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "title-00" {
			t.Fatalf("focus did not start on the first game: %+v", acts)
		}
		// The computer on its own is the row above the first game.
		r.Input("up")
		r.Input("select")
		if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != machine.ID {
			t.Fatalf("the computer's own row did not start it: %+v", acts)
		}
		// Held Down reaches the last game with the list still full.
		for i := 0; i < 40; i++ {
			r.Input("down")
		}
		f := stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Game 29") })
		rows := 0
		for _, h := range f.Hits {
			if strings.HasPrefix(h.ID, "game:") {
				rows++
			}
		}
		if rows < 4 || !frameHas(f, "Game 26") {
			t.Fatalf("%dx%d list shows %d rows at its end:\n%s", size[0], size[1], rows, frameText(f))
		}
		r.Input("select")
		if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "title-29" {
			t.Fatalf("last game launch %+v", acts)
		}
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
	}
	// Home names the family's first model; the screen returns to the model
	// and game last browsed.
	plus3 := preparedComputer("harbour", "Harbour Mail", "fes.spectrum-plus3")
	plus3.CoreEntryKind = "title"
	games = append(games, plus3)
	r := loadCoreScreen(t, &fakeServices{games: games}, plus3.ID, plus3.CoreID, Options{StorePath: path})
	stepUntil(t, r, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Harbour Mail") })
	r.Close()
	again := loadCoreScreen(t, &fakeServices{games: games}, machine.ID, machine.CoreID, Options{StorePath: path})
	stepUntil(t, again, func(f Frame) bool { return coreScreenSettled(f) && frameHas(f, "Games for the +2A/+3") })
	again.Input("select")
	if acts := again.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != plus3.ID {
		t.Fatalf("reopening from Home lost the last game: %+v", acts)
	}
}

// TestCoreScreenDrawWorkCeiling holds one draw callback under a fixed count
// of Lua instructions (#1770). On Kit B a 12,800-instruction repaint at
// session start missed the hard deadline while the core loaded and retired
// the room, so the picture is built in two parts on separate draws. The
// ceiling is about 1.25x the worst draw measured (7,046).
func TestCoreScreenDrawWorkCeiling(t *testing.T) {
	const ceiling = 8800
	games := []hostclient.Game{}
	for i := 0; i < 30; i++ {
		g := preparedComputer(fmt.Sprintf("title-%02d", i), fmt.Sprintf("Game %02d", i), "fes.spectrum")
		g.CoreEntryKind = "title"
		games = append(games, g)
	}
	w := &workMeter{}
	r := newRoom(t, examplePack(t, "example.core"), Options{
		Services: c64ServicesWithEmptyHardware{Services: &fakeServices{games: games}},
		Width:    1280, Height: 720, InitialCoreID: "fes.spectrum",
	})
	r.work = w
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	check := func(what string) {
		t.Helper()
		stepUntil(t, r, coreScreenSettled)
		for i := 0; i < 6; i++ {
			r.Step(time.Now())
		}
		for _, c := range w.calls {
			if c.kind == "draw" && c.instructions > ceiling {
				t.Fatalf("%s: a draw ran %d instructions, over %d", what, c.instructions, ceiling)
			}
		}
		w.calls = w.calls[:0]
	}
	check("first picture")
	r.Input("down")
	check("a move in the list")
	r.Input("left")
	check("a move to the computer panel")
	// A session start changes both parts at once.
	r.SetSessionState("active")
	check("session start")
	r.Activate("model:fes.spectrum-plus3")
	check("a model change")
}
