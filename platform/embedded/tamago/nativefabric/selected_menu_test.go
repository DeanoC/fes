// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

func compiledMenuFor(t *testing.T, l *ProgramLoader) CompiledMenuProgram {
	t.Helper()
	source, control, memory, access := menuMaps(t)
	var c HPSBootContext
	if err := json.Unmarshal(source, &c); err != nil {
		t.Fatal(err)
	}
	c.Platform, c.Device, c.HPSIOHash, c.PFGIdentity = l.platform.Platform, l.platform.Device, l.platform.HPSIOHash, l.platform.PFGIdentity
	source = programJSON(t, c)
	p := l.platform
	p.ProgrammingProfile, p.ABI, p.Major, p.Minor, p.ControlABI = c.ProgrammingProfile, c.ABI, c.Major, c.Minor, c.ControlABI
	p.MasterInventorySHA256 = strings.Repeat("4", 64)
	h := sha256.Sum256(source)
	platform := programJSON(t, programPlatformSource{1, "nano-program-platform-source-v1", hex.EncodeToString(h[:]), p})
	return CompiledMenuProgram{source, platform, control, memory, access}
}

func addCompiledMenu(t *testing.T, base *ProgramLoader, m CompiledMenuProgram) (*ProgramLoader, error) {
	t.Helper()
	p := programJSON(t, programPlatformSource{1, "nano-program-platform-source-v1", hex.EncodeToString(base.boot.digest[:]), base.platform})
	return NewProgramLoaderWithMenu(base.boot, p, programJSON(t, base.control), programJSON(t, base.configuration), programJSON(t, base.computer), m)
}

func mixedMenuTest(t *testing.T) (*romProgramFixture, CompiledMenuProgram) {
	t.Helper()
	r := romProgramTestFor(t, NativeComputerProfile)
	m := compiledMenuFor(t, r.f.loader)
	l, err := addCompiledMenu(t, r.f.loader, m)
	if err != nil {
		t.Fatal(err)
	}
	r.f.loader = l
	return r, m
}

func addMenuSelection(t *testing.T, r *romProgramFixture, index int) (packagestore.Request, selectedMenuProgramRecord, []byte) {
	t.Helper()
	m := r.f.loader.menu
	data := bytes.Repeat([]byte{byte(0xa0 + index)}, 128)
	sum := sha256.Sum256(data)
	build := strings.Repeat(fmt.Sprintf("%x", index+5), 32)
	manifest := fmt.Sprintf(`format = 2
[[interfaces]]
id = "fes.embedded.control"
major = 1
minor = 0
required = true
[core]
id = "fes.menu-fixture-%d"
name = "Selected menu fixture"
description = "Synthetic software fixture; never deploy."
version = "0.1.0"
[target]
platform = %q
device = %q
programming_profile = %q
[payload]
file = "core.rbf"
size = %d
sha256 = "%x"
[abi]
id = %q
major = %d
minor = %d
[build]
id = %q
repository = "https://example.invalid/menu-role"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "synthetic test"
`, index, m.platform.Platform, m.platform.Device, m.platform.ProgrammingProfile, len(data), sum, m.platform.ABI, m.platform.Major, m.platform.Minor, build)
	dir := t.TempDir()
	for name, b := range map[string][]byte{"manifest.toml": []byte(manifest), "core.rbf": data} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	body, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	i, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	control := m.control
	control.PackageID, control.BuildID = i.PackageID, build
	control.HandoffSHA256 = strings.Repeat("9", 64)
	memory := m.memory
	memory.PackageID, memory.BuildID = i.PackageID, build
	ch, mh := sha256.Sum256(programJSON(t, control)), sha256.Sum256(programJSON(t, memory))
	access := m.access
	access.PackageID, access.BuildID = i.PackageID, build
	access.ControlMapSHA256, access.ReaderMemorySHA256 = hex.EncodeToString(ch[:]), hex.EncodeToString(mh[:])
	record := selectedMenuProgramRecord{1, "nano-selected-menu-program-v1", i.PackageID, build, m.platform, control, memory, access}
	meta := programJSON(t, record)
	ms := sha256.Sum256(meta)
	req := packagestore.Request{Path: fmt.Sprintf("FES/M%d.FCP", index), PackageID: i.PackageID, MetadataPath: fmt.Sprintf("FES/M%d.JSN", index), MetadataSHA256: hex.EncodeToString(ms[:])}
	r.f.files.files[req.Path] = &fstest.MapFile{Data: body}
	r.f.files.files[req.MetadataPath] = &fstest.MapFile{Data: meta}
	return req, record, data
}

func retainMenu(t *testing.T, r *romProgramFixture, req packagestore.Request, record selectedMenuProgramRecord) *packagestore.Selection {
	t.Helper()
	b := programJSON(t, record)
	sum := sha256.Sum256(b)
	r.f.files.files[req.MetadataPath] = &fstest.MapFile{Data: b}
	req.MetadataSHA256 = hex.EncodeToString(sum[:])
	s, err := r.f.store.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSelectedMenuAndComputerKeepOneImmutableBootAnchor(t *testing.T) {
	r, m := mixedMenuTest(t)
	boot := r.f.loader.boot
	id, digest := boot.Identity(), boot.digest
	calls := r.cpu.calls
	for index := 0; index < 2; index++ {
		req, record, _ := addMenuSelection(t, r, index)
		s := retainMenu(t, r, req, record)
		p, err := r.f.loader.Preflight(s)
		if err != nil || p.state.v != boot || p.state.program.role != programRoleMenu || len(p.state.program.rom) != 0 || p.Identity().PackageID != req.PackageID {
			t.Fatal(p, err)
		}
	}
	req, _, _ := addROMProgram(t, r, 0, 8192)
	s, err := r.f.store.Retain(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.f.loader.Preflight(s)
	if err != nil || p.state.v != boot || p.state.program.role != programRoleComputer || len(p.state.program.rom) != 8192 {
		t.Fatal(p, err)
	}
	if calls != r.cpu.calls || id != boot.Identity() || digest != boot.digest || boot.menuMemory != nil {
		t.Fatal("preflight altered boot, reserved memory or called firmware")
	}
	clear(m.Context)
	clear(m.Control)
	clear(m.Memory)
	clear(m.Access)
	clear(m.Platform)
	if r.f.loader.menu.platform.ControlABI != programRoleMenu.controlABI() || r.f.loader.menu.memory.ArenaBytes != 8<<20 {
		t.Fatal("compiled driver borrowed caller bytes")
	}
}

func TestSelectedMenuRejectsChangedBoardAndResourcesBeforeContainment(t *testing.T) {
	r, _ := mixedMenuTest(t)
	for _, fault := range []string{"platform", "masters", "control", "stop", "memory", "memory-id", "access", "adapter", "package", "build", "scope"} {
		t.Run(fault, func(t *testing.T) {
			req, record, _ := addMenuSelection(t, r, 0)
			switch fault {
			case "platform":
				record.Platform.HPSConfigurationSHA256 = strings.Repeat("5", 64)
			case "masters":
				record.Platform.MasterInventorySHA256 = r.f.loader.platform.MasterInventorySHA256
			case "control":
				record.Control.CPUBase += 256
			case "stop":
				record.Control.FabricStop = &hpsFabricStopMap{"sealed-ddr-master-stop-v1", 0x20010100, 128, "held-reset-from-configuration"}
			case "memory":
				record.Memory.SlotBytes >>= 1
			case "memory-id":
				record.Memory.PackageID = strings.Repeat("f", 64)
			case "access":
				record.Access.ControlMapSHA256 = strings.Repeat("5", 64)
			case "adapter":
				record.Access.AdapterGeneratedSHA256 = strings.Repeat("5", 64)
			case "package":
				record.PackageID = strings.Repeat("f", 64)
			case "build":
				record.BuildID = strings.Repeat("f", 32)
			case "scope":
				record.Scope = "nano-selected-program-v1"
			}
			s := retainMenu(t, r, req, record)
			calls := r.cpu.calls
			if p, err := r.f.loader.Preflight(s); err == nil || p != nil || calls != r.cpu.calls || !r.f.lease.r.Active {
				t.Fatal("bad menu reached containment or hardware", p, err)
			}
		})
	}
}

func TestCompiledMenuRejectsChangedSharedFingerprintsWithoutIO(t *testing.T) {
	r := romProgramTestFor(t, NativeComputerProfile)
	for _, fault := range []string{"parameters", "configuration", "masters-empty", "context", "control", "memory", "access"} {
		t.Run(fault, func(t *testing.T) {
			m := compiledMenuFor(t, r.f.loader)
			var p programPlatformSource
			json.Unmarshal(m.Platform, &p)
			switch fault {
			case "parameters":
				p.Platform.HPSParametersSHA256 = strings.Repeat("5", 64)
			case "configuration":
				p.Platform.HPSConfigurationSHA256 = strings.Repeat("5", 64)
			case "masters-empty":
				p.Platform.MasterInventorySHA256 = strings.Repeat("0", 64)
			case "context":
				p.BootContextSHA256 = strings.Repeat("5", 64)
			case "control":
				m.Control = []byte("{}\n")
			case "memory":
				m.Memory = []byte("{}\n")
			case "access":
				m.Access = []byte("{}\n")
			}
			m.Platform = programJSON(t, p)
			calls := r.cpu.calls
			if l, err := addCompiledMenu(t, r.f.loader, m); err == nil || l != nil || calls != r.cpu.calls {
				t.Fatal("invalid role driver installed or called hardware", l, err)
			}
		})
	}
}

type selectedRoleCPU struct {
	*romProgramCPU
	role         programRole
	publications int
	activate     func()
}

func (c *selectedRoleCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] == uint64(g.FesEmbeddedBridgeCall) && (a[1] == uint64(g.FesEmbeddedBridgePrepareConfiguration) || a[1] == uint64(g.FesEmbeddedBridgeRearmConfiguration)) {
		raw := c.memory[len(c.memory)-ProgramMetadataBytes:][:768]
		source := HPSBootObservation{Digest: c.digest, Bridges: c.state}
		out, err := decodeRolePublication(c.t, c.decoder, raw, source, c.chunkBytes)
		if err != nil || !strings.HasSuffix(string(out), fmt.Sprintf(" role=%d abi=%08x\n", c.role, c.role.controlABI())) {
			c.t.Fatalf("actual secure role decoder rejected publication: %v %s", err, out)
		}
		c.publications++
		return c.serviceLeaseCPU.Invoke(a)
	}
	r, err := c.romProgramCPU.Invoke(a)
	if a[0] == isdone && err == nil && r == ([4]uint64{}) && c.activate != nil {
		c.activate()
	}
	return r, err
}

// Secure calls, register responses and SD ownership are injected. The private
// store/loader/transfer/menu/computer implementations and C decoder are real.
// Activation changes the modeled endpoint only after configuration completes.
type selectedMenuCPU struct {
	*menuCPU
	configuration *selectedRoleCPU
	kind          programRole
}

func (c *selectedMenuCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] != uint64(g.FesEmbeddedBridgeCall) || a[1] == uint64(g.FesEmbeddedBridgePrepareConfiguration) || a[1] == uint64(g.FesEmbeddedBridgeRearmConfiguration) || c.kind == programRoleComputer {
		return c.configuration.Invoke(a)
	}
	return c.menuCPU.Invoke(a)
}
func (c *selectedMenuCPU) ReadPhysical32(a uint64) (uint32, error) {
	if c.kind == programRoleComputer {
		return c.configuration.ReadPhysical32(a)
	}
	return c.menuCPU.ReadPhysical32(a)
}
func (c *selectedMenuCPU) WritePhysical32(a uint64, v uint32) error {
	if c.kind == programRoleComputer {
		return c.configuration.WritePhysical32(a, v)
	}
	return c.menuCPU.WritePhysical32(a, v)
}

func TestSelectedMenuComputerMenuCycleKeepsBootAndPinnedArena(t *testing.T) {
	runSelectedMenuComputerMenu(t, "")
}

func TestSelectedMenuComputerMenuFailuresRetainOwnershipWithoutReplay(t *testing.T) {
	for _, fault := range []string{"computer-open", "computer-identity", "menu-identity", "menu-close", "before-start-arena"} {
		t.Run(fault, func(t *testing.T) { runSelectedMenuComputerMenu(t, fault) })
	}
}

func runSelectedMenuComputerMenu(t *testing.T, fault string) {
	t.Helper()
	r, _ := mixedMenuTest(t)
	l := r.f.loader
	bootID, bootDigest := l.boot.Identity(), l.boot.digest
	f := newMenuFixture(t)
	f.cpu.computerCPU = *r.f.cpu.computerCPU
	r.f.cpu.computerCPU = &f.cpu.computerCPU
	port := &selectedRoleCPU{romProgramCPU: r.cpu}
	joined := &selectedMenuCPU{menuCPU: f.cpu, configuration: port}
	l.boot.cpu = joined
	initialRegisters := maps.Clone(f.cpu.registers)
	var previousMenu *MenuSession
	for cycle, role := range []programRole{programRoleMenu, programRoleComputer, programRoleMenu} {
		port.role = role
		var s *packagestore.Selection
		if role == programRoleMenu {
			req, record, _ := addMenuSelection(t, r, cycle)
			s = retainMenu(t, r, req, record)
		} else {
			req, _, _ := addROMProgram(t, r, 0, 8192)
			var err error
			s, err = r.f.store.Retain(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
		}
		plan, err := l.Preflight(s)
		if err != nil {
			t.Fatal(err)
		}
		h, err := r.f.store.Contain(s)
		if err != nil {
			t.Fatal(err)
		}
		if cycle == 0 {
			_, err = l.ReserveMenuMemory(func(b []byte) (*Region, error) {
				return &Region{Address: 0x95000000, Bytes: bytes.Clone(b), Release: func() error { f.cpu.releases++; return nil }, OrderFrameStores: func() error { return nil }}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		var transfer *ConfigurationTransfer
		if cycle == 0 {
			transfer, _, err = plan.NewTransfer(h, r.f.arena.Reserve)
		} else if role == programRoleComputer {
			transfer, _, err = previousMenu.NewStoredReconfigurationTransfer(plan, h, r.f.arena.Reserve)
		} else {
			transfer, _, err = plan.NewReconfigurationTransfer(h, r.f.arena.Reserve)
		}
		if err != nil {
			t.Fatal(err)
		}
		r.f.cpu.wire = &transferWire{t: t, source: bytes.Clone(transfer.source), region: transfer.region}
		port.activate = func() {
			joined.kind = role
			if role == programRoleMenu {
				f.cpu.registers = maps.Clone(initialRegisters)
				f.cpu.accepted, f.cpu.displayed, f.cpu.slot = 0, 0, 0
				f.cpu.lastDrainCookie = 0
				build, _ := BuildWords(plan.Identity().BuildID)
				for i, w := range build {
					f.cpu.registers[g.FesEmbeddedRegBuild0+uint32(i)*4] = w
				}
				if cycle == 2 && fault == "menu-identity" {
					f.cpu.registers[g.FesEmbeddedRegAbi] ^= 1
				}
			} else {
				for i := range 8 {
					word := uint16(transfer.source[0]) * 0x11
					r.f.cpu.words[8+i] = word | word<<8
				}
				if fault == "computer-identity" {
					r.f.cpu.words[8] ^= 1
				}
			}
		}
		if cycle == 1 && fault == "before-start-arena" {
			l.boot.readerMemoryRegion.Bytes[(8<<20)-1] ^= 1
		}
		for n := 0; n < 40 && !transfer.Status().Done; n++ {
			if _, _, err = transfer.Advance(); err != nil {
				if cycle == 1 && fault == "before-start-arena" {
					if !transfer.Status().Failed || transfer.Status().RetainedBytes == 0 || port.publications != 1 || f.cpu.releases != 0 || r.f.lease.r.Active {
						t.Fatal("bad arena reached secure preparation or released resources")
					}
					calls := r.cpu.calls
					if _, _, again := previousMenu.NewStoredReconfigurationTransfer(plan, h, r.f.arena.Reserve); again == nil || calls != r.cpu.calls {
						t.Fatal("failed menu return replayed")
					}
					return
				}
				t.Fatal(cycle, err)
			}
		}
		if !transfer.Status().Done || joined.kind != role {
			t.Fatal("incomplete role activation", cycle)
		}
		if role == programRoleMenu {
			session, err := l.OpenMenu(context.Background(), transfer, scanout.MenuLimits{ReaderLimits: windowReadLimits(), PublicationTimeout: 4 * time.Second})
			if err != nil {
				if cycle == 2 && fault == "menu-identity" {
					calls := r.cpu.calls
					if session == nil || session.ResumeStorageAfterReturn() == nil || r.f.lease.r.Active || f.cpu.releases != 0 || f.cpu.state.ResetMask != 15 {
						t.Fatal("bad menu identity granted return or lost containment")
					}
					if _, again := l.OpenMenu(context.Background(), transfer, scanout.MenuLimits{ReaderLimits: windowReadLimits(), PublicationTimeout: 4 * time.Second}); again == nil || calls != r.cpu.calls {
						t.Fatal("bad menu identity replayed")
					}
					return
				}
				t.Fatal(cycle, err)
			}
			if _, err = session.PresentFrame(context.Background(), menuFrame(byte(cycle+1))); err != nil {
				t.Fatal(err)
			}
			if cycle == 2 && fault == "menu-close" {
				f.cpu.fault = "master-stop"
			}
			returned, err := session.Close()
			if err != nil {
				if cycle == 2 && fault == "menu-close" {
					calls := r.cpu.calls
					if returned != (MenuSessionReturn{}) || session.ResumeStorageAfterReturn() == nil || r.f.lease.r.Active || f.cpu.releases != 0 {
						t.Fatal("uncertain menu close granted storage")
					}
					if _, again := session.Close(); again == nil || calls != r.cpu.calls {
						t.Fatal("uncertain menu close replayed")
					}
					return
				}
				t.Fatal(err)
			}
			if returned.Frames != 1 || returned.After.ResetMask != 15 || returned.Source.Digest != bootDigest || returned.Memory.MapSHA256 != sha256.Sum256(plan.state.program.menu.memoryRaw) || f.cpu.releases != 0 {
				t.Fatal("menu return lost source, selection or retained arena", returned)
			}
			previousMenu = session
			if cycle == 0 {
				if err := session.ResumeStorageAfterReturn(); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			if fault == "computer-open" {
				r.f.cpu.loseOpen = true
			}
			c, err := l.OpenComputer(context.Background(), transfer, computerLimits())
			if err != nil {
				if fault == "computer-open" || fault == "computer-identity" {
					calls := r.cpu.calls
					if _, again := l.OpenComputer(context.Background(), transfer, computerLimits()); again == nil || calls != r.cpu.calls {
						t.Fatal("failed core opening replayed")
					}
					if l.boot.ResumeStorageAfterReturn(r.f.store) == nil || calls != r.cpu.calls || r.f.lease.r.Active || f.cpu.releases != 0 {
						t.Fatal("failed core opening resumed SD or released frame memory")
					}
					return
				}
				t.Fatal(err)
			}
			if err := c.Computer().Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := l.boot.ResumeStorageAfterReturn(r.f.store); err != nil {
				t.Fatal(err)
			}
		}
		if l.boot.Identity() != bootID || l.boot.digest != bootDigest || port.publications != cycle+1 || f.cpu.releases != 0 {
			t.Fatal("role replaced boot anchor or released framebuffer")
		}
	}
	if r.f.cpu.configurations != 3 || f.cpu.independentStops != 2 || r.f.lease.returns != 2 {
		t.Fatal("missing complete role transitions", r.f.cpu.configurations, f.cpu.independentStops, r.f.lease.returns)
	}
	if fault != "" {
		t.Fatal("fault did not reach its intended failure path", fault)
	}
}

func loadRole(t *testing.T, r *romProgramFixture, plan *StoredConfigurationPlan, s *packagestore.Selection) *ConfigurationTransfer {
	t.Helper()
	h, err := r.f.store.Contain(s)
	if err != nil {
		t.Fatal(err)
	}
	transfer, _, err := plan.NewTransfer(h, r.f.arena.Reserve)
	if err != nil {
		t.Fatal(err)
	}
	r.f.cpu.wire = &transferWire{t: t, source: bytes.Clone(transfer.source), region: transfer.region}
	for n := 0; n < 40 && !transfer.Status().Done; n++ {
		if _, _, err := transfer.Advance(); err != nil {
			t.Fatal(err)
		}
	}
	if !transfer.Status().Done {
		t.Fatal("role configuration incomplete")
	}
	return transfer
}

func TestSelectedRolesPublishThroughRealPreparationAndSecureDecoder(t *testing.T) {
	for _, role := range []programRole{programRoleMenu, programRoleComputer} {
		t.Run(fmt.Sprint(role), func(t *testing.T) {
			r, _ := mixedMenuTest(t)
			port := &selectedRoleCPU{romProgramCPU: r.cpu, role: role}
			r.f.loader.boot.cpu = port
			var s *packagestore.Selection
			if role == programRoleMenu {
				req, record, _ := addMenuSelection(t, r, 0)
				s = retainMenu(t, r, req, record)
			} else {
				req, _, _ := addROMProgram(t, r, 0, 8192)
				var err error
				s, err = r.f.store.Retain(context.Background(), req)
				if err != nil {
					t.Fatal(err)
				}
			}
			plan, err := r.f.loader.Preflight(s)
			if err != nil {
				t.Fatal(err)
			}
			transfer := loadRole(t, r, plan, s)
			if port.publications != 1 || transfer.binding.program.role != role || r.f.cpu.configurations != 1 {
				t.Fatal("role did not use owned preparation/configuration")
			}
			calls := r.cpu.calls
			if role == programRoleMenu {
				if _, err := r.f.loader.OpenComputer(context.Background(), transfer, computerLimits()); err == nil {
					t.Fatal("menu opened computer controls")
				}
			} else {
				if _, err := r.f.loader.OpenMenu(context.Background(), transfer, scanout.MenuLimits{ReaderLimits: windowReadLimits(), PublicationTimeout: 4 * time.Second}); err == nil {
					t.Fatal("computer opened menu controls")
				}
			}
			if calls != r.cpu.calls {
				t.Fatal("wrong role touched hardware")
			}
		})
	}
}
