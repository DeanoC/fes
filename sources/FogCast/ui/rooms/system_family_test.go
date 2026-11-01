package rooms

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

func spectrumFamilyEntries() []hostclient.Game {
	ids := []string{"fes.spectrum", "fes.spectrum128", "fes.spectrum-plus3", "fes.pentagon"}
	names := []string{"My 48K setup", "My 128K setup", "My +3 setup", "My Pentagon setup"}
	out := make([]hostclient.Game, 0, len(ids))
	for i, id := range ids {
		game := preparedComputer("spectrum-setup-"+string(rune('a'+i)), names[i], id)
		game.PackageID = strings.Repeat(string(rune('a'+i)), 64)
		game.MediaID = strings.Repeat(string(rune('1'+i)), 64)
		game.MediaRole = "cassette"
		game.SlotExpansions = []hostclient.SlotExpansion{{Slot: 1, ExpansionID: strings.Repeat(string(rune('5'+i)), 64), Ready: true}}
		if i > 1 {
			game.MediaRole = "disk"
		}
		out = append(out, game)
	}
	return out
}

func TestSpectrumFamilyVariantKeepsExactSavedSetup(t *testing.T) {
	games := spectrumFamilyEntries()
	r := loadSystemWorkbench(t, &fakeServices{games: games}, games[0].ID, games[0].CoreID)
	for i, game := range games {
		frame := stepUntil(t, r, func(f Frame) bool {
			return strings.Contains(frameText(f), game.Title) && strings.Contains(frameText(f), "Set up system")
		})
		text := frameText(frame)
		for _, want := range []string{"ZX SPECTRUM SETUP", "Variant:", game.MediaID[:8], game.SlotExpansions[0].ExpansionID[:8]} {
			if !strings.Contains(text, want) {
				t.Fatalf("variant %s missing %q:\n%s", game.CoreID, want, text)
			}
		}
		if i < 2 && (strings.Contains(text, "Disk .dsk") || strings.Contains(text, "Disk .trd")) {
			t.Fatalf("tape variant inherited disk guidance:\n%s", text)
		}
		if i == 2 && (!strings.Contains(text, "Disk .dsk") || strings.Contains(text, "Disk .trd")) {
			t.Fatalf("+3 inherited wrong disk format:\n%s", text)
		}
		if i == 3 && (!strings.Contains(text, "Disk .trd") || strings.Contains(text, "Disk .dsk")) {
			t.Fatalf("Pentagon inherited wrong disk format:\n%s", text)
		}
		for _, button := range []string{"setup", "media", "expansions", "start"} {
			r.Activate(button)
			acts := r.TakeActions()
			if len(acts) != 1 {
				t.Fatalf("%s on %s: %+v", button, game.CoreID, acts)
			}
			act := acts[0]
			switch button {
			case "setup":
				if act.Kind != ActionHardwareSetup || act.CoreID != game.CoreID {
					t.Fatalf("wrong variant setup: %+v", act)
				}
			case "media":
				if act.Kind != ActionHardwareMedia || act.GameID != game.ID || act.CoreID != game.CoreID || act.PackageID != game.PackageID || act.ExpectedMediaID != game.MediaID {
					t.Fatalf("cross-variant media: %+v", act)
				}
			case "expansions":
				if act.Kind != ActionHardwareExpansions || act.GameID != game.ID || act.CoreID != game.CoreID || act.PackageID != game.PackageID {
					t.Fatalf("cross-variant sockets: %+v", act)
				}
			case "start":
				if act.Kind != ActionLaunch || act.GameID != game.ID {
					t.Fatalf("wrong variant launch: %+v", act)
				}
			}
		}
		// Keyboard/controller confirmation uses the same focused selector as pointer input.
		r.Hover("variant")
		r.Input("select")
		if acts := r.TakeActions(); len(acts) != 0 {
			t.Fatalf("variant navigation mutated a setup or session: %+v", acts)
		}
	}
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), games[0].Title) && strings.Contains(frameText(f), "Set up system")
	})
}

func TestSpectrumFamilyInitialEntryOverridesSavedVariant(t *testing.T) {
	games := spectrumFamilyEntries()
	path := filepath.Join(t.TempDir(), "room.json")
	if err := os.WriteFile(path, []byte(`{"navigation":{"core_id":"fes.spectrum","game_id":"spectrum-setup-a","edition_id":"spectrum-setup-a"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := newRoom(t, examplePack(t, "example.system"), Options{Services: c64ServicesWithEmptyHardware{Services: &fakeServices{games: games}}, Width: 1280, Height: 720, StorePath: path, InitialGameID: games[2].ID, InitialCoreID: games[2].CoreID})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), games[2].Title) && strings.Contains(frameText(f), "Set up system")
	})
	if strings.Contains(frameText(frame), games[0].Title) {
		t.Fatal("saved 48K entry replaced selected +3")
	}
	r.Activate("setup")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].CoreID != games[2].CoreID {
		t.Fatalf("selected entry lost its variant: %+v", acts)
	}
}

func TestSpectrumFamilyEmptyVariantOffersOnlySetup(t *testing.T) {
	game := spectrumFamilyEntries()[0]
	r := loadSystemWorkbench(t, &fakeServices{games: []hostclient.Game{game}}, game.ID, game.CoreID)
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), game.Title) && strings.Contains(frameText(f), "Set up system")
	})
	r.Activate("variant")
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "ZX Spectrum 128K") && strings.Contains(frameText(f), "Set up this system to begin") && strings.Contains(frameText(f), "Set up system")
	})
	if strings.Contains(frameText(frame), game.MediaID[:8]) {
		t.Fatal("empty variant inherited the other model's medium")
	}
	for _, button := range []string{"start", "media", "expansions"} {
		r.Activate(button)
	}
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("empty variant acted on another model: %+v", acts)
	}
	r.Activate("setup")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionHardwareSetup || acts[0].CoreID != "fes.spectrum128" {
		t.Fatalf("empty variant setup: %+v", acts)
	}
}

func TestFamilyVariantSelectionSurvivesRoomReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "room.json")
	open := func(core string) *Instance {
		r := newRoom(t, examplePack(t, "example.system"), Options{Services: c64ServicesWithEmptyHardware{Services: &fakeServices{}}, Width: 1280, Height: 720, StorePath: path, InitialCoreID: core})
		r.SetSessionState("idle")
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Set up system") })
		return r
	}
	r := open("fes.spectrum")
	r.Activate("variant") // 128K
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "ZX Spectrum 128K") && strings.Contains(frameText(f), "Set up this system to begin") && strings.Contains(frameText(f), "Set up system")
	})
	r.Activate("system") // Apple II
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Apple II") && strings.Contains(frameText(f), "Set up this system to begin")
	})
	r.Close()
	reopened := open("")
	for i := 0; i < 12; i++ {
		reopened.Activate("setup")
		acts := reopened.TakeActions()
		if len(acts) != 1 || acts[0].Kind != ActionHardwareSetup {
			t.Fatalf("family setup action: %+v", acts)
		}
		if strings.HasPrefix(acts[0].CoreID, "fes.spectrum") || acts[0].CoreID == "fes.pentagon" {
			if acts[0].CoreID != "fes.spectrum128" {
				t.Fatalf("Spectrum family forgot 128K: %+v", acts)
			}
			return
		}
		reopened.Activate("system")
		stepUntil(t, reopened, func(f Frame) bool { return strings.Contains(frameText(f), "Set up system") })
	}
	t.Fatal("Spectrum family was not reachable after reopen")
}

func TestSingletonFamilyHasNoVariantSelector(t *testing.T) {
	game := preparedComputer("apple-setup", "My Apple II", "fes.apple2")
	r := loadSystemWorkbench(t, &fakeServices{games: []hostclient.Game{game}}, game.ID, game.CoreID)
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), game.Title) && strings.Contains(frameText(f), "Set up system")
	})
	for _, hit := range frame.Hits {
		if hit.ID == "variant" {
			t.Fatal("single-model family has a variant control")
		}
	}
	r.Activate("variant")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("single-model variant action: %+v", acts)
	}
}

func TestBrowsingAnotherVariantPreservesRunningSession(t *testing.T) {
	games := spectrumFamilyEntries()
	host := &c64Host{fakeServices: fakeServices{games: games}}
	live := c64DiskSession(games[1].ID)
	live.ID = "live-spectrum-128"
	live.CorePackage.PackageID = games[1].PackageID
	tape := protocol.SpectrumTapeInterface()
	live.CorePackage.ActiveInterfaces = []hostclient.SessionCoreInterface{{ID: tape.ID, Major: tape.Major, Minor: tape.Minor}}
	live.CorePackage.MediaUnits = []protocol.MediaUnitStatus{{Unit: 0, Interface: tape, MinBytes: uint32(protocol.SpectrumTapeMinBytes), MaxBytes: uint32(protocol.SpectrumTapeMaxBytes), ChunkBytes: protocol.ComputerMediaChunkBytes, State: protocol.MediaUnitEmpty}}
	host.snap.Session = live
	r := loadSystemWorkbench(t, host, games[1].ID, games[1].CoreID)
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Stop machine") })
	r.Activate("variant")
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), games[2].Title) && strings.Contains(frameText(f), "Set up system") && strings.Contains(frameText(f), "Machine in use")
	})
	for _, button := range []string{"start", "disk", "setup", "media", "expansions"} {
		r.Activate(button)
	}
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("browsing +3 replaced or mutated active 128K: %+v", acts)
	}
	r.Activate("resume")
	if acts := r.TakeActions(); len(acts) != 1 || acts[0].Kind != ActionResumeSession {
		t.Fatalf("return to active variant: %+v", acts)
	}
	r.Activate("variant") // Pentagon
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), games[3].Title) && strings.Contains(frameText(f), "Set up system")
	})
	r.Activate("variant") // 48K
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), games[0].Title) && strings.Contains(frameText(f), "Set up system")
	})
	r.Activate("variant") // 128K
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), games[1].Title) && strings.Contains(frameText(f), "Stop machine")
	})
	r.Activate("disk")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenTape || acts[0].SessionID != live.ID || acts[0].GameID != games[1].ID || acts[0].MediaBinding.PackageID != games[1].PackageID || acts[0].MediaBinding.Generation != live.CorePackage.Generation {
		t.Fatalf("active variant binding changed: %+v", acts)
	}
}

func TestGameOnlySingletonEntryClearsHiddenVariantFocus(t *testing.T) {
	game := preparedComputer("coleco-game", "My Coleco setup", "fes.coleco")
	path := filepath.Join(t.TempDir(), "room.json")
	if err := os.WriteFile(path, []byte(`{"navigation":{"core_id":"fes.spectrum","focus":"variant"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Active-session routing supplies a game ID without an initial core ID.
	// Resolve that entry before validating focus inherited from another family.
	r := newRoom(t, examplePack(t, "example.system"), Options{
		Services: c64ServicesWithEmptyHardware{Services: &fakeServices{games: []hostclient.Game{game}}},
		Width:    1280, Height: 720, StorePath: path, InitialGameID: game.ID,
	})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), game.Title) && strings.Contains(frameText(f), "Set up system")
	})
	for _, hit := range frame.Hits {
		if hit.ID == "variant" {
			t.Fatal("singleton retained a variant control")
		}
	}
	r.Input("select")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != game.ID {
		t.Fatalf("Confirm remained on the hidden variant selector: %+v", acts)
	}
}

func TestSavedFamilyReopenUsesSelectedVariantHint(t *testing.T) {
	games := spectrumFamilyEntries()
	path := filepath.Join(t.TempDir(), "room.json")
	if err := os.WriteFile(path, []byte(`{"navigation":{"core_id":"fes.spectrum","game_id":"spectrum-setup-a","edition_id":"spectrum-setup-a"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	// Direct room reopen supplies no initial selection; all visible copy must
	// follow the persisted model rather than the workbench's initial default.
	r := newRoom(t, examplePack(t, "example.system"), Options{
		Services: c64ServicesWithEmptyHardware{Services: &fakeServices{games: games}},
		Width:    1280, Height: 720, StorePath: path,
	})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), games[0].Title) && strings.Contains(frameText(f), "Set up system")
	})
	text := frameText(frame)
	if !strings.Contains(text, "Choose a ZX Spectrum 48K entry.") || strings.Contains(text, "Choose a Sinclair ZX81 entry.") {
		t.Fatalf("saved Spectrum reopened with another model's hint:\n%s", text)
	}
}

func TestWorkbenchAsksForFirmwareCardOfTheOpenedEntrysFamily(t *testing.T) {
	games := spectrumFamilyEntries()
	// Opened by game alone: the workbench starts on its default ZX81 core and
	// only learns the entry's Spectrum +3 core from the library.
	r := newRoom(t, examplePack(t, "example.system"), Options{Services: c64ServicesWithEmptyHardware{Services: &fakeServices{games: games}}, Width: 1280, Height: 720, StorePath: filepath.Join(t.TempDir(), "room.json"), InitialGameID: games[2].ID})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if req, ok := r.TakeFirmwareInfo(); ok {
		t.Fatalf("asked for a firmware card before the core was known: %+v", req)
	}
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), games[2].Title) })
	req, ok := r.TakeFirmwareInfo()
	if !ok || !req.FirstUse || req.Kind != ActionFirmwareInfo || req.FamilyID != "spectrum" || req.FamilyLabel != "ZX Spectrum" || req.CoreID != games[2].CoreID ||
		strings.Join(req.CoreIDs, ",") != "fes.spectrum,fes.spectrum128,fes.spectrum-plus3,fes.pentagon" {
		t.Fatalf("first-use request %+v %v", req, ok)
	}
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("first-use request entered the action queue: %+v", acts)
	}
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Firmware") && strings.Contains(frameText(f), "Set up system") })
	r.Activate("firmware")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionFirmwareInfo || acts[0].FirstUse || acts[0].FamilyID != "spectrum" {
		t.Fatalf("Firmware button: %+v", acts)
	}
}
