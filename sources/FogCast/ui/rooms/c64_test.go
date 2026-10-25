package rooms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

func TestHomeDrawHardBudgetStays48ms(t *testing.T) {
	b := DefaultBudget()
	if b.Frame != 12*time.Millisecond {
		t.Fatalf("frame budget %s", b.Frame)
	}
	hard := b.Frame * callbackHardLimitFactor
	if hard != 48*time.Millisecond {
		t.Fatalf("hard budget %s", hard)
	}
}

type c64Host struct {
	fakeServices
	hardwareMu    sync.Mutex
	snap          hostclient.HardwareSnapshot
	hardwareErr   error
	hardwareReads int
}

type c64ServicesWithEmptyHardware struct {
	Services
}

func (c64ServicesWithEmptyHardware) Hardware(context.Context) (hostclient.HardwareSnapshot, error) {
	return hostclient.HardwareSnapshot{}, nil
}

func (c64ServicesWithEmptyHardware) SelectCoreEntryExpansion(context.Context, string, string, string, string) (hostclient.CoreEntryExpansion, error) {
	return hostclient.CoreEntryExpansion{}, nil
}

func (h *c64Host) Hardware(context.Context) (hostclient.HardwareSnapshot, error) {
	h.hardwareMu.Lock()
	defer h.hardwareMu.Unlock()
	h.hardwareReads++
	return h.snap, h.hardwareErr
}

func (h *c64Host) setHardwareSnapshot(snap hostclient.HardwareSnapshot) {
	h.hardwareMu.Lock()
	defer h.hardwareMu.Unlock()
	h.snap = snap
}

func (h *c64Host) hardwareReadCount() int {
	h.hardwareMu.Lock()
	defer h.hardwareMu.Unlock()
	return h.hardwareReads
}

func (h *c64Host) setHardwareError(err error) {
	h.hardwareMu.Lock()
	defer h.hardwareMu.Unlock()
	h.hardwareErr = err
}

func (h *c64Host) SelectCoreEntryExpansion(context.Context, string, string, string, string) (hostclient.CoreEntryExpansion, error) {
	return hostclient.CoreEntryExpansion{}, nil
}

func c64DiskSession(gameID string) *hostclient.SessionResult {
	disk := protocol.C64DiskInterface()
	return &hostclient.SessionResult{
		ID: "c64-session", Target: "kit", TargetID: "a", State: "active", GameID: gameID,
		CorePackage: &hostclient.SessionCorePackage{
			PackageID:  strings.Repeat("ab", 32),
			Generation: 7,
			ABI:        hostclient.SessionCoreABI{ID: protocol.ComputerABIID, Major: 1, Minor: 0},
			ActiveInterfaces: []hostclient.SessionCoreInterface{{
				ID: disk.ID, Major: disk.Major, Minor: disk.Minor,
			}},
			MediaUnits: []protocol.MediaUnitStatus{{
				Unit:       protocol.C64DiskUnit,
				Interface:  disk,
				MinBytes:   uint32(protocol.C64DiskBytes),
				MaxBytes:   uint32(protocol.C64DiskBytes),
				ChunkBytes: protocol.ComputerMediaChunkBytes,
				State:      protocol.MediaUnitEmpty,
			}},
		},
	}
}

func loadC64(t *testing.T, svc Services, initialGameID ...string) *Instance {
	return loadC64WithSessionState(t, svc, "idle", initialGameID...)
}

func loadC64WithSessionState(t *testing.T, svc Services, state string, initialGameID ...string) *Instance {
	t.Helper()
	pack := examplePack(t, "example.c64")
	if !pack.Valid() || pack.Title != "Commodore 64" {
		t.Fatalf("pack %+v", pack.Manifest)
	}
	if _, ok := svc.(HardwareServices); !ok {
		svc = c64ServicesWithEmptyHardware{Services: svc}
	}
	opts := Options{Services: svc, Width: 1280, Height: 720}
	if len(initialGameID) > 0 {
		opts.InitialGameID = initialGameID[0]
	}
	r := newRoom(t, pack, opts)
	r.SetSessionState(state)
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestC64RoomColdOpenStaysOnTheOpeningFrame(t *testing.T) {
	r := loadC64(t, &fakeServices{})
	first := r.Step(time.Unix(1, 0))
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	text := frameText(first)
	if !strings.Contains(text, "Opening the Commodore 64") || strings.Contains(text, "Start machine") {
		t.Fatalf("first frame\n%s", text)
	}
	if len(first.Ops) >= 40 {
		t.Fatalf("opening frame used %d ops", len(first.Ops))
	}
}

func TestC64RoomShowsSavedSocketsAndDiskThenLaunches(t *testing.T) {
	slot := strings.Repeat("cd", 32)
	disk := strings.Repeat("ef", 32)
	pkg := strings.Repeat("12", 32)
	base := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	base.PackageID = pkg
	base.MediaID = disk
	base.MediaRole = "disk"
	base.SlotExpansions = []hostclient.SlotExpansion{{Slot: 1, ExpansionID: slot, Ready: true}}
	base.VariantCount = 2
	japan := preparedComputer("fpga-c64-jp", "Impossible Mission", "fes.c64")
	japan.Region = "Japan"
	base.Variants = []hostclient.Game{japan}
	pong := preparedComputer("fpga-pong", "FES Pong", "fes.pong")
	svc := &fakeServices{games: []hostclient.Game{base, pong}}
	r := loadC64(t, svc)
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Start machine") && strings.Contains(frameText(f), "Impossible Mission")
	})
	text := frameText(frame)
	for _, want := range []string{
		"Socket 1", "Cartridge", shortID(slot), "ready",
		"Socket 2", "empty",
		"Disk .d64", shortID(disk),
		"1541", "unit 0",
		"Tape .tap and .t64: not on this core",
		"Cartridge file .crt: not on this core",
		"Package", shortID(pkg),
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame missing %q\n%s", want, text)
		}
	}
	for _, absent := range []string{slot, disk, pkg, "FES Pong", "REU", ".crt "} {
		if strings.Contains(text, absent) {
			t.Fatalf("frame includes %q\n%s", absent, text)
		}
	}
	assertRowContrast(t, frame)
	queries := svc.recorded()
	if len(queries) != 1 || queries[0].Platform != "fpga" || queries[0].Sort != "title" {
		t.Fatalf("queries %+v", queries)
	}
	r.Activate("start")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-c64-game" {
		t.Fatalf("launch %+v", acts)
	}
	r.Activate("edition")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("edition launched %+v", acts)
	}
	next := stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Japan") })
	assertRowContrast(t, next)
	r.Activate("start")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-c64-jp" {
		t.Fatalf("edition launch %+v", acts)
	}
}

func TestC64RoomChooseDiskUsesTheRunningSession(t *testing.T) {
	game := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{game}}}
	host.snap.Session = c64DiskSession(game.ID)
	r := loadC64(t, host)
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Stop machine") && strings.Contains(frameText(f), "Running: this Commodore 64")
	})
	r.Activate("disk")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionOpenTape || acts[0].SessionID != "c64-session" || acts[0].MediaBinding.Generation != 7 {
		t.Fatalf("disk %+v", acts)
	}
	r.Activate("start")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionStop || acts[0].SessionID != "c64-session" {
		t.Fatalf("stop %+v", acts)
	}
}

func TestC64RoomRefreshesControlsWhenSessionStateChanges(t *testing.T) {
	game := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{game}}}
	pack := examplePack(t, "example.c64")
	r := newRoom(t, pack, Options{Services: host, Width: 1280, Height: 720})
	r.SetSessionState("idle")
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Start machine") && strings.Contains(frameText(f), "Running: not running")
	})

	r.Activate("start")
	if actions := r.TakeActions(); len(actions) != 1 || actions[0].Kind != ActionLaunch || actions[0].GameID != game.ID {
		t.Fatalf("launch actions %+v", actions)
	}
	host.setHardwareSnapshot(hostclient.HardwareSnapshot{Session: c64DiskSession(game.ID)})
	r.SetSessionState("active")
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Stop machine") && strings.Contains(frameText(f), "Running: this Commodore 64")
	})

	r.Activate("start")
	if actions := r.TakeActions(); len(actions) != 1 || actions[0].Kind != ActionStop || actions[0].SessionID != "c64-session" {
		t.Fatalf("stop actions %+v", actions)
	}
	host.setHardwareSnapshot(hostclient.HardwareSnapshot{})
	r.SetSessionState("idle")
	stepUntil(t, r, func(f Frame) bool {
		return host.hardwareReadCount() >= 3 && strings.Contains(frameText(f), "Start machine") && strings.Contains(frameText(f), "Running: not running")
	})
}

func TestC64RoomBlocksStartWhileAnotherTitleRuns(t *testing.T) {
	game := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{game}}}
	host.snap.Session = c64DiskSession("fpga-other")
	r := loadC64(t, host)
	frame := stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "Machine in use") && strings.Contains(text, "Running: another title")
	})
	assertRowContrast(t, frame)
	r.Activate("start")
	r.Activate("disk")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("busy machine acted %+v", acts)
	}
}

func TestC64RoomDisablesStartForLocalAndTransitioningSessions(t *testing.T) {
	game := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	for _, state := range []string{"local", "launching", "stopping"} {
		t.Run(state, func(t *testing.T) {
			r := loadC64WithSessionState(t, &fakeServices{games: []hostclient.Game{game}}, state)
			frame := stepUntil(t, r, func(f Frame) bool {
				text := frameText(f)
				return strings.Contains(text, game.Title) && strings.Contains(text, "Start machine")
			})
			assertRowContrast(t, frame)

			r.Activate("start")
			if actions := r.TakeActions(); len(actions) != 0 {
				t.Fatalf("start acted while session state is %q: %+v", state, actions)
			}
		})
	}
}

func TestC64RoomUsesInitialGameIDWithoutChangingDirectOpen(t *testing.T) {
	first := preparedComputer("fpga-c64-first", "First C64 title", "fes.c64")
	second := preparedComputer("fpga-c64-second", "Second C64 title", "fes.c64")
	svc := &fakeServices{games: []hostclient.Game{first, second}}

	direct := loadC64(t, svc)
	directFrame := stepUntil(t, direct, func(f Frame) bool {
		return strings.Contains(frameText(f), "First C64 title")
	})
	if strings.Contains(frameText(directFrame), "Second C64 title") {
		t.Fatalf("direct open changed its default selection\n%s", frameText(directFrame))
	}

	selected := loadC64(t, svc, second.ID)
	selectedFrame := stepUntil(t, selected, func(f Frame) bool {
		return strings.Contains(frameText(f), "Second C64 title")
	})
	if strings.Contains(frameText(selectedFrame), "First C64 title") {
		t.Fatalf("initial selection opened a different entry\n%s", frameText(selectedFrame))
	}
}

func TestC64RoomFiltersLargeLibraryAndFetchesSelectedGameBeyondLimit(t *testing.T) {
	games := make([]hostclient.Game, 0, 2601)
	for i := 0; i < 600; i++ {
		games = append(games, preparedComputer(fmt.Sprintf("fpga-other-%04d", i), fmt.Sprintf("Other core title %04d", i), "fes.pong"))
	}
	for i := 0; i < 2000; i++ {
		games = append(games, preparedComputer(fmt.Sprintf("fpga-c64-%04d", i), fmt.Sprintf("C64 title %04d", i), "fes.c64"))
	}
	selected := preparedComputer("fpga-c64-beyond-limit", "C64 title beyond the query limit", "fes.c64")
	games = append(games, selected)
	svc := &fakeServices{games: games}

	r := loadC64(t, svc, selected.ID)
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), selected.Title)
	})
	queries := svc.recorded()
	if len(queries) != 1 || queries[0].Platform != "fpga" || len(queries[0].CoreIDs) != 1 || queries[0].CoreIDs[0] != "fes.c64" {
		t.Fatalf("C64 query did not filter prepared core rows: %+v", queries)
	}
	if limits := svc.recordedQueryLimits(); len(limits) != 1 || limits[0] != 2000 {
		t.Fatalf("C64 query limit = %v, want [2000]", limits)
	}
}

func TestC64RoomKeepsOtherEntriesWhenInitialSelectionIsGone(t *testing.T) {
	first := preparedComputer("fpga-c64-first", "First remaining C64 title", "fes.c64")
	second := preparedComputer("fpga-c64-second", "Second remaining C64 title", "fes.c64")
	r := loadC64(t, &fakeServices{games: []hostclient.Game{first, second}}, "fpga-c64-gone")
	stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "The selected Commodore 64 entry is no longer available.") && strings.Contains(text, first.Title) && strings.Contains(text, "Start machine")
	})
	r.Activate("start")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != first.ID {
		t.Fatalf("remaining first entry launch %+v", acts)
	}
	r.Activate("machine")
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), second.Title) })
	r.Activate("start")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != second.ID {
		t.Fatalf("remaining second entry launch %+v", acts)
	}
}

func TestC64RoomDisablesLiveControlsAfterRefreshFailure(t *testing.T) {
	game := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	host := &c64Host{fakeServices: fakeServices{games: []hostclient.Game{game}}}
	host.snap.Session = c64DiskSession(game.ID)
	r := loadC64(t, host)
	r.SetSessionState("active")
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Stop machine") && strings.Contains(frameText(f), "Running: this Commodore 64")
	})

	host.setHardwareError(errors.New("temporary read failure"))
	r.Resume()
	failed := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Running: status unavailable") && strings.Contains(frameText(f), "Status unavailable")
	})
	if strings.Contains(frameText(failed), "Stop machine") {
		t.Fatalf("failed refresh left Stop visible\n%s", frameText(failed))
	}
	r.Activate("start")
	r.Activate("disk")
	r.Activate("resume")
	if actions := r.TakeActions(); len(actions) != 0 {
		t.Fatalf("failed refresh left session controls active: %+v", actions)
	}

	host.setHardwareError(nil)
	r.Resume()
	stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Stop machine") && strings.Contains(frameText(f), "Running: this Commodore 64")
	})
}

func TestC64RoomEmptyLibraryDoesNotLaunch(t *testing.T) {
	r := loadC64(t, &fakeServices{})
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "No Commodore 64 entries yet.") && strings.Contains(frameText(f), "Start machine")
	})
	assertRowContrast(t, frame)
	text := frameText(frame)
	if !strings.Contains(text, "not on this core") {
		t.Fatalf("frame\n%s", text)
	}
	r.Activate("start")
	r.Activate("disk")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("empty launch %+v", acts)
	}
}

func TestC64RoomLoadsEditionsWhenTheListOmitsThem(t *testing.T) {
	list := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	list.VariantCount = 2
	detail := list
	japan := preparedComputer("fpga-c64-jp", "Impossible Mission", "fes.c64")
	japan.Region = "Japan"
	detail.Variants = []hostclient.Game{japan}
	svc := &fakeServices{
		games:   []hostclient.Game{list},
		details: map[string]hostclient.Game{list.ID: detail},
	}
	r := loadC64(t, svc)
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Start machine") && strings.Contains(frameText(f), "Impossible Mission")
	})
	assertRowContrast(t, frame)
	if strings.Contains(frameText(frame), "Japan") {
		t.Fatal("Japan was shown before the detail edition was chosen")
	}
	r.Activate("start")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-c64-game" {
		t.Fatalf("base launch %+v", acts)
	}
	r.Activate("edition")
	stepUntil(t, r, func(f Frame) bool { return strings.Contains(frameText(f), "Japan") })
	r.Activate("start")
	acts = r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-c64-jp" {
		t.Fatalf("detail edition %+v", acts)
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
