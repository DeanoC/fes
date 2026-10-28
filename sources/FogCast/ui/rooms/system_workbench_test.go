package rooms

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func loadSystemWorkbench(t *testing.T, svc Services, gameID, coreID string) *Instance {
	t.Helper()
	if _, ok := svc.(HardwareServices); !ok {
		svc = c64ServicesWithEmptyHardware{Services: svc}
	}
	r := newRoom(t, examplePack(t, "example.system"), Options{
		Services: svc, Width: 1280, Height: 720, InitialGameID: gameID, InitialCoreID: coreID,
	})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSystemWorkbenchEmptyLibrarySetsUpEveryPlayableModel(t *testing.T) {
	r := loadSystemWorkbench(t, &fakeServices{}, "", "")
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Set up system") })
	ids := []string{
		"fes.zx81", "fes.zx80", "fes.ts1000", "fes.ts1500", "fes.c64", "fes.spectrum",
		"fes.spectrum128", "fes.spectrum-plus3", "fes.pentagon", "fes.apple2", "fes.atari-st",
		"fes.coleco", "fes.sms", "fes.sg1000", "fes.pong", "fes.catch", "fes.ramtest", "fes.riscv",
	}
	for _, id := range ids {
		r.Activate("setup")
		acts := r.TakeActions()
		if len(acts) != 1 || acts[0].Kind != ActionHardwareSetup || acts[0].CoreID != id {
			t.Fatalf("setup %s: %+v", id, acts)
		}
		r.Activate("start")
		if acts := r.TakeActions(); len(acts) != 0 {
			t.Fatalf("empty system %s launched %+v", id, acts)
		}
		r.Activate("system")
		stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Set up system") })
	}
}

func TestSystemWorkbenchSelectedModelMediaAndEditionKeepExactEntry(t *testing.T) {
	pkg, media := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	base := preparedComputer("spectrum-title", "Spectrum title", "fes.spectrum-plus3")
	base.PackageID, base.MediaID, base.MediaRole, base.VariantCount = pkg, media, "disk", 2
	edition := base
	edition.ID, edition.Region, edition.VariantCount = "spectrum-edition", "Europe", 0
	base.Variants = []hostclient.Game{edition}
	first := preparedComputer("apple-title", "Apple title", "fes.apple2")
	r := loadSystemWorkbench(t, &fakeServices{games: []hostclient.Game{first, base}}, edition.ID, base.CoreID)
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Europe") && strings.Contains(frameText(f), "Media for next start")
	})
	text := frameText(frame)
	for _, want := range []string{"ZX Spectrum +3", "Disk .dsk", media[:8], "Europe"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	if strings.Contains(text, ".trd") || strings.Contains(text, "Apple title") {
		t.Fatalf("wrong model/media guidance\n%s", text)
	}
	r.Activate("media")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionHardwareMedia || acts[0].GameID != edition.ID || acts[0].CoreID != base.CoreID || acts[0].PackageID != pkg || acts[0].ExpectedMediaID != media {
		t.Fatalf("media binding %+v", acts)
	}
	r.Activate("expansions")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionHardwareExpansions || acts[0].GameID != edition.ID || acts[0].PackageID != pkg {
		t.Fatalf("expansion binding %+v", acts)
	}
	r.Activate("start")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != edition.ID {
		t.Fatalf("edition launch %+v", acts)
	}
}

func TestSystemWorkbenchKeepsRunningMachineForLiveMediaAndResume(t *testing.T) {
	game := preparedComputer("c64-game", "C64 game", "fes.c64")
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{game}}}
	host.snap.Session = c64DiskSession(game.ID)
	r := loadSystemWorkbench(t, host, game.ID, game.CoreID)
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Stop machine") })
	r.Activate("disk")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenTape || acts[0].SessionID != "c64-session" || acts[0].MediaBinding.Generation != 7 {
		t.Fatalf("live picker binding %+v", acts)
	}
	r.Activate("resume")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionResumeSession {
		t.Fatalf("resume %+v", acts)
	}
	r.Activate("setup")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("active machine offered next setup %+v", acts)
	}
}

func TestSystemWorkbenchResumeRefreshesAddedLibraryEntries(t *testing.T) {
	game := preparedComputer("new-title", "New title", "fes.apple2")
	svc := &fakeServices{details: map[string]hostclient.Game{}}
	r := loadSystemWorkbench(t, svc, "", game.CoreID)
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "No Apple II entries yet.") })
	// A setup operation adds an entry while the workbench is covered by its
	// native picker. Resuming must reread library entries as well as hardware.
	svc.mu.Lock()
	svc.games = []hostclient.Game{game}
	svc.mu.Unlock()
	r.Resume()
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), game.Title) })
	r.Activate("start")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != game.ID {
		t.Fatalf("new entry launch %+v", acts)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	r.Step(time.Unix(3, 0))
}

func TestSystemsRoomRoutesOtherFamiliesToExactModelWorkbench(t *testing.T) {
	game := preparedComputer("st-title", "Atari ST title", "fes.atari-st")
	r := newRoom(t, examplePack(t, "example.computers"), Options{
		Services: &fakeServices{games: []hostclient.Game{game}}, Index: NewIndex(Examples()), Width: 1280, Height: 720,
	})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), game.Title) })
	r.Activate("setup")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenRoom || acts[0].RoomID != "example.system" || acts[0].InitialGameID != game.ID || acts[0].InitialCoreID != game.CoreID {
		t.Fatalf("setup route %+v", acts)
	}
}

func TestSystemWorkbenchRefreshRetainsChangedMachineAndEdition(t *testing.T) {
	for _, action := range []string{"machine", "edition"} {
		t.Run(action, func(t *testing.T) {
			first := preparedComputer("first-title", "First title", "fes.c64")
			second := preparedComputer("second-title", "Second title", "fes.c64")
			if action == "edition" {
				first.VariantCount = 2
				first.Variants = []hostclient.Game{second}
			}
			r := loadSystemWorkbench(t, &fakeServices{games: []hostclient.Game{first, second}}, first.ID, first.CoreID)
			stepUntil(t, r, func(f Frame) bool {
				return strings.Contains(frameText(f), first.Title) && strings.Contains(frameText(f), "Start machine")
			})
			r.Activate(action)
			stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), second.Title) })
			r.Resume()
			// Wait for the refreshed library reply before drawing: the old
			// frame still contains the second title while that reply is in flight.
			deadline := time.Now().Add(3 * time.Second)
			for {
				r.pendingMu.Lock()
				ready := false
				for _, result := range r.pending {
					ready = ready || result.games != nil
				}
				r.pendingMu.Unlock()
				if ready {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("library refresh did not complete")
				}
				time.Sleep(time.Millisecond)
			}
			frame := stepUntil(t, r, func(f Frame) bool {
				return strings.Contains(frameText(f), second.Title) && strings.Contains(frameText(f), "Start machine")
			})
			if strings.Contains(frameText(frame), "no longer available") {
				t.Fatalf("changed selection was treated as missing: %s", frameText(frame))
			}
			r.Activate("start")
			acts := r.TakeActions()
			if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != second.ID {
				t.Fatalf("refreshed selection %+v", acts)
			}
		})
	}
}
