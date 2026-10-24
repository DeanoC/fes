package rooms

import (
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

func preparedComputer(id, title, coreID string) hostclient.Game {
	return hostclient.Game{
		ID: id, Title: title, System: "fpga", CoreID: coreID,
		State: "available", RootOnline: true, Launchable: true,
	}
}

func TestComputersRoomListsPreparedZX81AndC64(t *testing.T) {
	pack := examplePack(t, "example.computers")
	if !pack.Valid() || pack.Title != "Computers" {
		t.Fatalf("pack %+v", pack.Manifest)
	}
	expansion := strings.Repeat("ab", 32)
	slotID := strings.Repeat("cd", 32)
	zx := preparedComputer("fpga-zx81-maze", "3D Monster Maze", "fes.zx81")
	zx.ExpansionID = expansion
	zx.ExpansionReady = true
	zx.VariantCount = 2
	japan := preparedComputer("fpga-zx81-jp", "3D Monster Maze", "fes.zx81")
	japan.Region = "Japan"
	zx.Variants = []hostclient.Game{japan}
	bare := preparedComputer("fpga-zx81-bare", "Bare ZX81", "fes.zx81")
	bare.FirmwareRequired = true
	bare.FirmwareReady = false
	c64 := preparedComputer("fpga-c64-game", "Impossible Mission", "fes.c64")
	c64.SlotExpansions = []hostclient.SlotExpansion{{Slot: 1, ExpansionID: slotID, Ready: true}}
	pong := preparedComputer("fpga-pong", "FES Pong", "fes.pong")
	apple := preparedComputer("fpga-apple", "Apple II", "fes.apple2")
	coleco := preparedComputer("fpga-coleco", "Donkey Kong", "fes.coleco")

	svc := &fakeServices{games: []hostclient.Game{zx, bare, c64, pong, apple, coleco}}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		text := frameText(f)
		return strings.Contains(text, "3D Monster Maze") && strings.Contains(text, "Commodore 64") && strings.Contains(text, "Japan")
	})
	text := frameText(frame)
	for _, want := range []string{"ZX81", "3D Monster Maze", "Japan", "Commodore 64", "Expansion " + expansion[:8], "Slot 1 " + slotID[:8]} {
		if !strings.Contains(text, want) {
			t.Fatalf("frame missing %q\n%s", want, text)
		}
	}
	for _, absent := range []string{expansion, slotID, "Bare ZX81", "FES Pong", "Apple II", "Donkey Kong", "16K", "REU", "Zon X"} {
		if strings.Contains(text, absent) {
			t.Fatalf("frame includes %q\n%s", absent, text)
		}
	}
	if len(frame.Ops) >= 80 {
		t.Fatalf("computers draw used %d ops", len(frame.Ops))
	}
	queries := svc.recorded()
	if len(queries) != 1 || queries[0].Platform != "fpga" || queries[0].Sort != "title" {
		t.Fatalf("queries %+v", queries)
	}
	if got := r.Destination(); got.Kind != "" || got.GameID != "" {
		t.Fatalf("computers published %+v", got)
	}

	wantIDs := []string{
		"fpga-zx81-maze",
		"fpga-zx81-maze",
		"fpga-zx81-jp",
		"fpga-c64-game",
		"fpga-c64-game",
	}
	for i, id := range wantIDs {
		if i > 0 {
			if !r.Input("down") {
				t.Fatalf("down %d", i)
			}
			if acts := r.TakeActions(); len(acts) != 0 {
				t.Fatalf("down launched %+v", acts)
			}
		}
		if !r.Input("select") {
			t.Fatalf("select %d", i)
		}
		acts := r.TakeActions()
		if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != id {
			t.Fatalf("row %d launch %+v, want %s", i, acts, id)
		}
	}
	r.Activate("computers:1")
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-zx81-maze" {
		t.Fatalf("pointer launch %+v", acts)
	}
	r.Step(time.Unix(3, 0))
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}

func TestComputersRoomLoadsEditionsWhenTheListOmitsThem(t *testing.T) {
	pack := examplePack(t, "example.computers")
	list := preparedComputer("fpga-zx81-maze", "3D Monster Maze", "fes.zx81")
	list.VariantCount = 2
	detail := list
	japan := preparedComputer("fpga-zx81-jp", "3D Monster Maze", "fes.zx81")
	japan.Region = "Japan"
	detail.Variants = []hostclient.Game{japan}
	svc := &fakeServices{
		games:   []hostclient.Game{list},
		details: map[string]hostclient.Game{list.ID: detail},
	}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "Japan")
	})
	text := frameText(frame)
	if !strings.Contains(text, "ZX81") || !strings.Contains(text, "3D Monster Maze") {
		t.Fatalf("frame\n%s", text)
	}
	if !r.Input("down") || !r.Input("select") {
		t.Fatal("variant select")
	}
	acts := r.TakeActions()
	if len(acts) != 1 || acts[0].Kind != ActionLaunch || acts[0].GameID != "fpga-zx81-jp" {
		t.Fatalf("edition launch %+v", acts)
	}
}

func TestComputersRoomEmptyLibraryDoesNotLaunch(t *testing.T) {
	pack := examplePack(t, "example.computers")
	r := newRoom(t, pack, Options{Services: &fakeServices{}, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	frame := stepUntil(t, r, func(f Frame) bool {
		return strings.Contains(frameText(f), "No prepared ZX81 or Commodore 64 entries yet.")
	})
	if len(frame.Ops) >= 80 {
		t.Fatalf("empty computers draw used %d ops", len(frame.Ops))
	}
	r.Input("select")
	if acts := r.TakeActions(); len(acts) != 0 {
		t.Fatalf("empty select launched %+v", acts)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
}
