package rooms

import (
	"context"
	"strings"
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
	snap hostclient.HardwareSnapshot
}

func (h *c64Host) Hardware(context.Context) (hostclient.HardwareSnapshot, error) {
	return h.snap, nil
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

func loadC64(t *testing.T, svc Services) *Instance {
	t.Helper()
	pack := examplePack(t, "example.c64")
	if !pack.Valid() || pack.Title != "Commodore 64" {
		t.Fatalf("pack %+v", pack.Manifest)
	}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
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
