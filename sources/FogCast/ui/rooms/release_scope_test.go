package rooms

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

func TestReleaseOnlyRoomsListOnlyV1Cores(t *testing.T) {
	fake := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
		{PackageID: "zx", CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true},
	}}
	src := `
systems = require "systems"
function load()
  kit.cores(function(rows) got = rows end)
end
function draw() end`
	for _, releaseOnly := range []bool{true, false} {
		r := newRoom(t, memPack(t, "release-scope", src, nil), Options{Local: fake, ReleaseOnly: releaseOnly})
		if err := r.Load(); err != nil {
			t.Fatal(err)
		}
		stepUntil(t, r, func(Frame) bool { return r.CheckGlobal(`got ~= nil`) == nil })
		check := `#got == 1 and got[1].core_id == "fes.zx81"
			and systems.by_id["fes.c64"] and systems.by_id["fes.atari-st"] and not systems.by_id["fes.coleco"]
			and not room.core_shown("fes.pong") and not room.core_shown("fes.pc386") and room.core_shown("fes.pentagon")`
		if !releaseOnly {
			check = `#got == 2 and systems.by_id["fes.coleco"] and room.core_shown("fes.pong")`
		}
		if err := r.CheckGlobal(check); err != nil {
			t.Fatalf("release only %v: %v", releaseOnly, err)
		}
	}
}

func TestReleaseOnlyHardwareLeavesOutHeldBackCores(t *testing.T) {
	r := newRoom(t, memPack(t, "release-hardware", "function load() end function draw() end", nil), Options{ReleaseOnly: true})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	snap := hostclient.HardwareSnapshot{
		Machines:  []hostclient.HardwareMachine{{CoreID: "fes.zx81"}, {CoreID: "fes.coleco"}},
		Computers: []hostclient.ShelfComputer{{CoreID: "fes.spectrum"}, {CoreID: "fes.pcxt"}},
	}
	table := r.hardwareTable(snap)
	machines := table.RawGetString("machines").(interface{ Len() int })
	computers := table.RawGetString("computers").(interface{ Len() int })
	if machines.Len() != 1 || computers.Len() != 1 {
		t.Fatalf("machines=%d computers=%d", machines.Len(), computers.Len())
	}
}

func TestReleaseOnlyRoomKeepsTheCoreItWasOpenedFor(t *testing.T) {
	src := `systems = require "systems"
function load() end
function draw() end`
	r := newRoom(t, memPack(t, "release-initial", src, nil), Options{ReleaseOnly: true, InitialCoreID: "fes.apple2"})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckGlobal(`room.core_shown("fes.apple2") and systems.by_id["fes.apple2"] and not systems.by_id["fes.coleco"] and not room.core_shown("")`); err != nil {
		t.Fatal(err)
	}
	machines := r.hardwareTable(hostclient.HardwareSnapshot{Machines: []hostclient.HardwareMachine{{CoreID: "fes.apple2"}, {CoreID: "fes.coleco"}}}).RawGetString("machines").(interface{ Len() int })
	if machines.Len() != 1 {
		t.Fatalf("machines = %d", machines.Len())
	}
}

func TestHomeDropsRememberedHeldBackCore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "home.state.json")
	if err := os.WriteFile(storePath, []byte(`{"package_id":"`+corePongID+`","package_name":"Pong","package_core_id":"fes.pong"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: corePongID, CoreID: "fes.pong", Name: "Pong", Needs: "none", Launchable: true},
		{PackageID: coreZXID, CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true},
	}}
	r := newRoom(t, examplePack(t, "example.fes-cores"), Options{Local: fake, Width: 1280, Height: 720, ReleaseOnly: true, StorePath: storePath})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	stepUntil(t, r, func(Frame) bool { return r.Destination().PackageID == coreZXID })
	if got := r.Destination(); got.CoreID != "fes.zx81" || got.PackageID == corePongID {
		t.Fatalf("remembered held-back card remained selected: %+v", got)
	}
}
