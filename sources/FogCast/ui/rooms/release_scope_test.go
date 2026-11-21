package rooms

import (
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

func TestReleaseOnlyRoomsIncludeThePCLane(t *testing.T) {
	fake := &fakeLocalCores{cores: []localcores.Core{
		{PackageID: corePongID, CoreID: "fes.pong", Name: "FES Pong", Needs: "none", Launchable: true},
		{PackageID: "zx", CoreID: "fes.zx81", Name: "ZX81", Needs: "none", Launchable: true},
		{PackageID: "xt", CoreID: "fes.pcxt-dos", Name: "PC/XT", Needs: "none", Launchable: true},
		{PackageID: "386", CoreID: "fes.pc386", Name: "PC 386", Needs: "none", Launchable: true},
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
		check := `#got == 3
			and systems.by_id["fes.c64"] and systems.by_id["fes.atari-st"] and not systems.by_id["fes.coleco"]
			and systems.by_id["fes.pcxt-dos"] and systems.by_id["fes.pc386"]
			and systems.by_id["fes.pc386"].hint == "Choose a machine setup and hard disk image"
			and not room.core_shown("fes.pong") and room.core_shown("fes.pc386") and room.core_shown("fes.pentagon")`
		if !releaseOnly {
			check = `#got == 4 and systems.by_id["fes.coleco"] and room.core_shown("fes.pong")`
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
		Computers: []hostclient.ShelfComputer{{CoreID: "fes.spectrum"}, {CoreID: "fes.pcxt-dos"}, {CoreID: "fes.pc386"}, {CoreID: "fes.coleco"}},
	}
	table := r.hardwareTable(snap)
	machines := table.RawGetString("machines").(interface{ Len() int })
	computers := table.RawGetString("computers").(interface{ Len() int })
	if machines.Len() != 1 || computers.Len() != 3 {
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
