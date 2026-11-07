// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	stdimage "image"
	"maps"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// Actual retained packages, service, drivers and secure C publication decoder;
// only MMIO, secure calls and the SD lease are modeled. Endpoint changes follow
// the published descriptor and configuration completion, never a test command.
type programMenuCPU struct {
	*selectedMenuCPU
	activate func(programRole)
}

func (c *programMenuCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] == uint64(g.FesEmbeddedBridgeCall) && (a[1] == uint64(g.FesEmbeddedBridgePrepareConfiguration) || a[1] == uint64(g.FesEmbeddedBridgeRearmConfiguration)) {
		raw := c.configuration.memory[len(c.configuration.memory)-ProgramMetadataBytes:]
		role := programRole(binary.LittleEndian.Uint32(raw[programRoleOffset:]))
		c.configuration.role = role
		c.configuration.activate = func() { c.activate(role) }
	}
	return c.selectedMenuCPU.Invoke(a)
}

type programMenuFixture struct {
	r        *romProgramFixture
	menu     *menuFixture
	cpu      *programMenuCPU
	s        *ProgramService
	request  packagestore.Request
	reserves int
	fault    string
}

func newProgramMenuFixture(t *testing.T, startupFault string) *programMenuFixture {
	return newProgramMenuFixtureContext(t, context.Background(), startupFault)
}

func newProgramMenuFixtureContext(t *testing.T, ctx context.Context, startupFault string) *programMenuFixture {
	t.Helper()
	r := romProgramFixtureFor(t, NativeComputerProfile, false)
	l, err := addCompiledMenu(t, r.f.loader, compiledMenuFor(t, r.f.loader))
	if err != nil {
		t.Fatal(err)
	}
	r.f.loader = l
	f := newMenuFixture(t)
	f.cpu.computerCPU = *r.f.cpu.computerCPU
	r.f.cpu.computerCPU = &f.cpu.computerCPU
	port := &selectedRoleCPU{romProgramCPU: r.cpu}
	cpu := &programMenuCPU{selectedMenuCPU: &selectedMenuCPU{menuCPU: f.cpu, configuration: port}}
	l.boot.cpu = cpu
	fixture := &programMenuFixture{r: r, menu: f, cpu: cpu, fault: startupFault}
	registers := maps.Clone(f.cpu.registers)
	cpu.activate = func(role programRole) {
		cpu.kind = role
		if role == programRoleMenu {
			f.cpu.registers = maps.Clone(registers)
			f.cpu.accepted, f.cpu.displayed, f.cpu.slot, f.cpu.lastDrainCookie = 0, 0, 0, 0
			// The endpoint exposes the completed publication's selected build.
			raw := port.memory[len(port.memory)-ProgramMetadataBytes:]
			for i := range 4 {
				f.cpu.registers[g.FesEmbeddedRegBuild0+uint32(i)*4] = binary.BigEndian.Uint32(raw[116-i*4:])
			}
			if fixture.fault == "menu-identity" {
				f.cpu.registers[g.FesEmbeddedRegAbi] ^= 1
			}
			if fixture.fault == "frame" {
				f.cpu.fault = "frame-command"
			}
		} else {
			for i := range 8 {
				word := uint16(r.f.cpu.wire.source[0]) * 0x11
				r.f.cpu.words[8+i] = word | word<<8
			}
			if fixture.fault == "computer-identity" {
				r.f.cpu.words[8] ^= 1
			}
		}
	}
	request, _, _ := addMenuSelection(t, r, 0)
	fixture.request = request
	reserve := func(data []byte) (*Region, error) {
		region, err := r.f.arena.Reserve(data)
		if err == nil {
			r.f.cpu.wire = &transferWire{t: t, source: bytes.Clone(data), region: region}
			if fixture.fault == "arena" {
				l.boot.readerMemoryRegion.Bytes[(8<<20)-1] ^= 1
			}
			if fixture.fault == "write" {
				r.f.cpu.wire.fault = "write"
			}
		}
		return region, err
	}
	opts := ProgramMenuOptions{Request: request, Limits: scanout.MenuLimits{ReaderLimits: windowReadLimits(), PublicationTimeout: 4 * time.Second},
		Reserve: func(data []byte) (*Region, error) {
			fixture.reserves++
			return &Region{Address: 0x95000000, Bytes: bytes.Clone(data), Release: func() error { f.cpu.releases++; return nil }, OrderFrameStores: func() error { return nil }}, nil
		}}
	fixture.s, err = NewProgramServiceWithMenu(ctx, l, r.f.store, reserve, computerLimits(), opts)
	if startupFault == "" && err != nil {
		t.Fatal(err)
	}
	if startupFault != "" && err == nil {
		t.Fatal("startup fault not exercised")
	}
	return fixture
}

func (f *programMenuFixture) frame(t *testing.T, red byte) {
	t.Helper()
	if err := f.s.PresentMenu(context.Background(), menuFrame(red)); err != nil {
		t.Fatal(err)
	}
}

func (f *programMenuFixture) assertFrame(t *testing.T, red byte) {
	t.Helper()
	region := f.r.f.loader.boot.readerMemoryRegion
	start := int(f.menu.cpu.slot) * (4 << 20)
	if region == nil || !bytes.Equal(region.Bytes[start:start+4], []byte{0xa7, 0x3a, red, 0xff}) {
		t.Fatal("actual scanout lost the launcher frame")
	}
}

func TestMenuProgramServiceOwnsRepeatedDisplayLaunchAndReturn(t *testing.T) {
	f := newProgramMenuFixture(t, "")
	boot := f.r.f.loader.boot
	id, digest := boot.Identity(), boot.digest
	if f.s.Status().State != ProgramReady || f.menu.cpu.displayed != 1 || f.r.f.lease.r.Active {
		t.Fatal("Ready did not own an initial displayed menu")
	}
	f.frame(t, 0x42)
	f.assertFrame(t, 0x42)
	var prior ProgramSession
	for i := range 3 {
		request, _, _ := addROMProgram(t, f.r, i, 8192)
		session, err := f.s.Launch(context.Background(), f.s.Status().Generation, request)
		if err != nil {
			t.Fatal(i, err)
		}
		if f.s.Status().State != ProgramRunning || f.r.f.lease.r.Active || f.cpu.kind != programRoleComputer {
			t.Fatal("core did not take exclusive ownership")
		}
		calls, opens := f.r.cpu.calls, f.r.f.files.opens
		if f.s.Execution(context.Background(), prior, true) == nil {
			t.Fatal("stale session controlled core")
		}
		if _, err := f.s.Launch(context.Background(), 0, request); err == nil {
			t.Fatal("concurrent launch admitted")
		}
		if err := f.s.PresentMenu(context.Background(), menuFrame(0x99)); err != nil {
			t.Fatal(err)
		}
		if calls != f.r.cpu.calls || opens != f.r.f.files.opens {
			t.Fatal("stale request or running UI performed I/O")
		}
		if _, err = f.s.Return(context.Background(), session); err != nil {
			t.Fatal(i, err)
		}
		if f.s.Status().State != ProgramReady || f.s.Status().Generation != uint64(2*i+2) || f.r.f.lease.r.Active || f.cpu.kind != programRoleMenu || f.menu.cpu.displayed != 1 {
			t.Fatal("return published before menu restoration", f.s.Status())
		}
		f.assertFrame(t, 0x42)
		f.frame(t, 0x42) // The finished load request must not expire the new session.
		prior = session
	}
	if f.reserves != 1 || f.menu.cpu.releases != 0 || boot.Identity() != id || boot.digest != digest || f.r.f.cpu.configurations != 7 {
		t.Fatal("cycle changed immutable boot, lost arena or omitted a load")
	}
	if err := f.s.ParkMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := f.r.cpu.calls
	if f.s.Status().State != ProgramParked || f.r.f.lease.r.Active || f.s.PresentMenu(context.Background(), menuFrame(1)) == nil || f.s.ParkMenu(context.Background()) == nil || calls != f.r.cpu.calls {
		t.Fatal("park reopened the menu or storage")
	}
}

func TestMenuProgramServiceRejectedContentRestoresDisplay(t *testing.T) {
	f := newProgramMenuFixture(t, "")
	f.frame(t, 0x33)
	for _, request := range []packagestore.Request{{Path: "missing"}, f.request} {
		before := f.s.Status()
		if _, err := f.s.Launch(context.Background(), before.Generation, request); err == nil {
			t.Fatal("invalid playable selection admitted")
		}
		if f.s.Status().State != ProgramReady || f.s.Status().Generation != before.Generation || f.r.f.lease.r.Active {
			t.Fatal("rejection did not restore owned display")
		}
		f.assertFrame(t, 0x33)
	}
	if f.r.f.cpu.configurations != 3 || f.reserves != 1 {
		t.Fatal("rejection did not freshly load the menu")
	}
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	if _, err := f.s.Launch(context.Background(), f.s.Status().Generation, request); err != nil {
		t.Fatal(err)
	}
}

func TestMenuProgramServiceStartupAndTransitionFailuresDoNotReplay(t *testing.T) {
	for _, fault := range []string{"menu-identity", "frame", "write"} {
		t.Run("startup-"+fault, func(t *testing.T) {
			f := newProgramMenuFixture(t, fault)
			assertMenuRetired(t, f)
		})
	}
	for _, fault := range []string{"menu-stop", "computer-identity", "write", "arena", "core-close", "menu-identity", "frame", "frontend"} {
		t.Run(fault, func(t *testing.T) {
			f := newProgramMenuFixture(t, "")
			request, _, _ := addROMProgram(t, f.r, 0, 8192)
			if fault == "frontend" {
				if err := f.s.Retire(errors.New("input disappeared")); err == nil {
					t.Fatal("frontend fault hidden")
				}
			} else {
				if fault == "menu-stop" {
					f.menu.cpu.fault = "master-stop"
				}
				if fault == "computer-identity" || fault == "write" || fault == "arena" {
					f.fault = fault
				}
				session, err := f.s.Launch(context.Background(), 0, request)
				if fault == "menu-stop" || fault == "computer-identity" || fault == "write" || fault == "arena" {
					if err == nil {
						t.Fatal("launch fault hidden")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if fault == "core-close" {
						f.r.f.cpu.loseClose = true
					} else {
						f.fault = fault
					}
					if _, err = f.s.Return(context.Background(), session); err == nil {
						t.Fatal("return fault hidden")
					}
				}
			}
			assertMenuRetired(t, f)
		})
	}
}

func assertMenuRetired(t *testing.T, f *programMenuFixture) {
	t.Helper()
	if f.s == nil || f.s.Status().State != ProgramRebootRequired || f.r.f.lease.r.Active || f.menu.cpu.releases != 0 {
		t.Fatal("uncertain transition lost containment", f.s.Status())
	}
	calls := f.r.cpu.calls
	_, _ = f.s.Launch(context.Background(), f.s.Status().Generation, f.request)
	_ = f.s.PresentMenu(context.Background(), menuFrame(1))
	_ = f.s.ParkMenu(context.Background())
	_ = f.s.Retire(errors.New("second frontend fault"))
	if calls != f.r.cpu.calls {
		t.Fatal("terminal transition replayed hardware")
	}
}

func TestMenuProgramFrameValidationAndBusyPresentation(t *testing.T) {
	f := newProgramMenuFixture(t, "")
	calls := f.r.cpu.calls
	for _, frame := range []*stdimage.RGBA{nil, stdimage.NewRGBA(stdimage.Rect(0, 0, 1, 1)), {Rect: stdimage.Rect(0, 0, 1280, 720), Stride: 1 << 30}} {
		if err := f.s.PresentMenu(context.Background(), frame); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(f.s.PresentMenu(ctx, menuFrame(1)), context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	f.s.state.operation.Lock()
	err := f.s.PresentMenu(context.Background(), menuFrame(1))
	f.s.state.operation.Unlock()
	if err != nil || calls != f.r.cpu.calls || f.s.Status().State != ProgramReady {
		t.Fatal("invalid or busy frame touched hardware")
	}
}

func TestMenuProgramRequestCancellationDoesNotExpireRestoredDisplay(t *testing.T) {
	load, cancelLoad := context.WithTimeout(context.Background(), time.Minute)
	f := newProgramMenuFixtureContext(t, load, "")
	cancelLoad()
	f.frame(t, 0x15)
	request, _, _ := addROMProgram(t, f.r, 0, 8192)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.menu.cpu.after = func(string) {
		if f.menu.cpu.quiesces > 0 {
			f.menu.cpu.after = nil
			cancel()
		}
	}
	if _, err := f.s.Launch(ctx, 0, request); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled launch was not rejected", err)
	}
	if f.s.Status().State != ProgramReady || f.s.Status().Generation != 0 || f.r.f.lease.r.Active {
		t.Fatal("cancelled request lost the restored menu")
	}
	f.assertFrame(t, 0x15)
	f.frame(t, 0x21)
	if _, err := f.s.Launch(context.Background(), 0, request); err != nil {
		t.Fatal(err)
	}
}
