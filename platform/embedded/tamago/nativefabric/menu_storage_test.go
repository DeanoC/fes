// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

type menuStorageCPU struct {
	*menuCPU
	configuration *reloadCPU
}

func (c *menuStorageCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] != uint64(g.FesEmbeddedBridgeCall) || a[1] == uint64(g.FesEmbeddedBridgePrepareConfiguration) || a[1] == uint64(g.FesEmbeddedBridgeRearmConfiguration) {
		return c.configuration.Invoke(a)
	}
	return c.menuCPU.Invoke(a)
}

// Join actual retained packages, transfers and menu sessions with modeled platform I/O.
func storedMenu(t *testing.T) (*menuFixture, *packagestore.Store, *packagestore.Handoff, *windowStorageLease, *windowStorageFiles, packagestore.Request) {
	t.Helper()
	f := newMenuFixture(t)
	identity := f.v.context
	data := bytes.Repeat([]byte{0x5a}, 128)
	sum, sum384 := sha256.Sum256(data), sha512.Sum384(data)
	identity.CoreSize, identity.CoreSHA256 = int64(len(data)), hex.EncodeToString(sum[:])
	manifest := fmt.Sprintf(`format = 2
[core]
id = %q
name = "Menu storage return"
description = "Synthetic test package; never deploy."
version = "0.1.0"
[target]
platform = %q
device = %q
programming_profile = %q
[payload]
file = "core.rbf"
size = %d
sha256 = %q
[abi]
id = %q
major = 1
minor = 0
[[interfaces]]
id = "fes.embedded.control"
major = 1
minor = 0
required = true
[build]
id = %q
repository = "https://example.invalid/menu-return"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "synthetic software fixture"
`, identity.CoreID, identity.Platform, identity.Device, identity.ProgrammingProfile, len(data), identity.CoreSHA256, identity.ABI, identity.BuildID)
	dir := t.TempDir()
	for name, raw := range map[string][]byte{"manifest.toml": []byte(manifest), "core.rbf": data} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	body, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	identity.PackageID = inspection.PackageID
	var control map[string]any
	var access menuReadMap
	if err := json.Unmarshal(f.control, &control); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(f.access, &access); err != nil {
		t.Fatal(err)
	}
	control["package_id"] = identity.PackageID
	f.control = canonicalConfiguration(t, control)
	memory := *f.v.menuMemory
	memory.PackageID = identity.PackageID
	memoryRaw := canonicalConfiguration(t, memory)
	controlHash, memoryHash := sha256.Sum256(f.control), sha256.Sum256(memoryRaw)
	access.PackageID = identity.PackageID
	access.ControlMapSHA256, access.ReaderMemorySHA256 = hex.EncodeToString(controlHash[:]), hex.EncodeToString(memoryHash[:])
	f.access = canonicalConfiguration(t, access)
	source := canonicalConfiguration(t, identity)
	f.cpu.digest = sha256.Sum256(source)
	cpu := &menuStorageCPU{f.cpu, &reloadCPU{computerCPU: &f.cpu.computerCPU}}
	f.v, err = NewMenuHPSContextVerifier(cpu, source, f.control, memoryRaw, f.access)
	if err != nil {
		t.Fatal(err)
	}
	f.cpu.state.Generation = 3
	lease := &windowStorageLease{cpu: &f.cpu.windowCPU, r: sdcontrol.Receipt{Owner: 17, Generation: 3, Active: true, Prepared: true}}
	files := &windowStorageFiles{FS: fstest.MapFS{"FES/PKGTEST.BIN": &fstest.MapFile{Data: body}}, lease: lease}
	store, err := packagestore.New(files, lease)
	if err != nil {
		t.Fatal(err)
	}
	request := packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: identity.PackageID}
	selected, err := store.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	mapping := coldConfigurationMap{Format: 1, Scope: "sealed-cold-configuration-v1", PackageID: identity.PackageID, BuildID: identity.BuildID,
		CoreSHA256: identity.CoreSHA256, CoreSHA384: hex.EncodeToString(sum384[:]), CoreSize: len(data), Base: 0x94800000, Size: 8 << 20, DRAMBase: 0x80000000, ChunkBytes: 64}
	for offset := 0; offset < len(data); offset += 64 {
		digest := sha512.Sum384(data[offset : offset+64])
		mapping.ChunkSHA384 = append(mapping.ChunkSHA384, hex.EncodeToString(digest[:]))
	}
	plan, err := f.v.PreflightStoredConfiguration(f.control, canonicalConfiguration(t, mapping), selected)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := store.Contain(selected)
	if err != nil {
		t.Fatal(err)
	}
	f.source, f.memory = source, memoryRaw
	_, err = f.v.ReserveMenuMemory(func(b []byte) (*Region, error) {
		return &Region{Address: 0x95000000, Bytes: bytes.Clone(b), Release: func() error { f.cpu.releases++; return nil }, OrderFrameStores: func() error {
			f.publications++
			if f.barrier != nil {
				return f.barrier()
			}
			return nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	f.transfer, _, err = plan.NewTransfer(handoff, arena.Reserve)
	if err != nil {
		t.Fatal(err)
	}
	cpu.configuration.wire = &transferWire{t: t, source: bytes.Clone(data), region: f.transfer.region}
	for step := 0; step < 40 && !f.transfer.Status().Done; step++ {
		if _, _, err = f.transfer.Advance(); err != nil {
			t.Fatal(err)
		}
	}
	if !f.transfer.Status().Done || f.transfer.binding.handoff != handoff {
		t.Fatal("completed transfer lost its original private storage handoff")
	}
	return f, store, handoff, lease, files, request
}

func TestMenuReturnReopensOnlyOriginalStoreAndFreshlyRetains(t *testing.T) {
	f, store, handoff, lease, files, request := storedMenu(t)
	s := f.open(t, context.Background())
	if err := s.ResumeStorageAfterReturn(); err == nil || lease.returns != 0 {
		t.Fatal("open reader resumed storage")
	}
	if _, err := s.PresentFrame(context.Background(), menuFrame(1)); err != nil {
		t.Fatal(err)
	}
	returned, err := s.Close()
	if err != nil {
		t.Fatal(err)
	}
	before := files.opens
	if err := s.ResumeStorageAfterReturn(); err != nil {
		t.Fatal(err)
	}
	if lease.returns != 1 || lease.r.Generation != returned.After.Generation+2 || files.opens != before || f.cpu.releases != 0 {
		t.Fatal("storage return changed files or released pinned arena")
	}
	calls := f.cpu.calls
	if err := s.ResumeStorageAfterReturn(); err == nil || lease.returns != 1 || f.cpu.calls != calls {
		t.Fatal("return grant replayed")
	}
	if _, err := handoff.Selection(); err == nil {
		t.Fatal("old handoff survived SD resume")
	}
	copied := *handoff
	if err := copied.ResumeAfterReturn(returned.After.Owner, returned.After.Generation); err == nil || lease.returns != 1 {
		t.Fatal("copied stale handoff replayed grant")
	}
	fresh, err := store.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if files.opens <= before {
		t.Fatal("selection was not freshly read from active SD")
	}
	next, err := store.Contain(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if next.Receipt().Generation != returned.After.Generation+3 || next.ReturnedFrom().Generation != returned.After.Generation || f.cpu.releases != 0 {
		t.Fatal("fresh handoff lost display-return provenance")
	}
}

func TestMenuStorageReturnRejectsCopiedAndBusyHandlesWithoutIO(t *testing.T) {
	f, _, _, lease, files, _ := storedMenu(t)
	s := f.open(t, context.Background())
	if _, err := s.PresentFrame(context.Background(), menuFrame(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Close(); err != nil {
		t.Fatal(err)
	}
	calls, reads := f.cpu.calls, files.opens
	copied := *s
	if err := copied.ResumeStorageAfterReturn(); err == nil || f.cpu.calls != calls || files.opens != reads || lease.returns != 0 {
		t.Fatal("copied session resumed SD", err)
	}
	s.active.Store(true)
	if err := s.ResumeStorageAfterReturn(); !errors.Is(err, scanout.ErrReaderBusy) || f.cpu.calls != calls || lease.returns != 0 {
		t.Fatal("busy session reached firmware", err)
	}
	s.active.Store(false)
	if err := s.ResumeStorageAfterReturn(); err != nil || lease.returns != 1 {
		t.Fatal("rejected handle consumed the original valid return", err)
	}
}

func TestMenuStoredRelaunchRejectsForeignStoreWithMatchingReceipts(t *testing.T) {
	f, store, _, _, _, request := storedMenu(t)
	foreign, otherStore, _, _, _, otherRequest := storedMenu(t)
	sessions := []*MenuSession{f.open(t, context.Background()), foreign.open(t, context.Background())}
	for _, s := range sessions {
		if _, err := s.PresentFrame(context.Background(), menuFrame(1)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if err := s.ResumeStorageAfterReturn(); err != nil {
			t.Fatal(err)
		}
	}
	plan, owned := nextStoredMenu(t, f, store, request)
	_, other := nextStoredMenu(t, foreign, otherStore, otherRequest)
	if owned.Receipt() != other.Receipt() || owned.ReturnedFrom() != other.ReturnedFrom() {
		t.Fatal("foreign control did not reproduce the same public observations")
	}
	calls, reservations := f.cpu.calls, 0
	reserve := func([]byte) (*Region, error) { reservations++; return nil, errors.New("unexpected reservation") }
	if _, _, err := sessions[0].NewStoredReconfigurationTransfer(plan, other, reserve); err == nil || calls != f.cpu.calls || reservations != 0 {
		t.Fatal("matching public receipts substituted another store", err)
	}
	if _, _, err := sessions[0].NewStoredReconfigurationTransfer(plan, owned, reserve); err == nil || calls != f.cpu.calls || reservations != 0 {
		t.Fatal("failed return was retried", err)
	}
}

func TestMenuStorageReturnFailuresConsumeGrantWithoutReplay(t *testing.T) {
	for _, fault := range []string{"missing-handoff", "no-frame", "memory", "padding", "owner", "source", "close", "grant", "receipt"} {
		t.Run(fault, func(t *testing.T) {
			f, _, _, lease, files, _ := storedMenu(t)
			if fault == "missing-handoff" {
				f.transfer.binding.handoff = nil
			}
			s := f.open(t, context.Background())
			if fault != "no-frame" {
				if _, err := s.PresentFrame(context.Background(), menuFrame(1)); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "close" {
				f.cpu.fault = "close"
			}
			_, closeErr := s.Close()
			if (closeErr != nil) != (fault == "close") {
				t.Fatal(closeErr)
			}
			switch fault {
			case "memory":
				s.bytes[(4<<20)+(32<<10)] ^= 1
			case "padding":
				s.bytes[len(s.bytes)-1] ^= 1
			case "owner":
				f.cpu.state.Owner++
			case "source":
				f.cpu.digest[0] ^= 1
			case "grant", "receipt":
				lease.fault = fault
			}
			before := files.opens
			if err := s.ResumeStorageAfterReturn(); err == nil {
				t.Fatal("invalid return granted SD")
			}
			grants, calls := lease.returns, f.cpu.calls
			if _, err := s.Close(); (err != nil) != (closeErr != nil) {
				t.Fatal("cached close changed or replayed")
			}
			if err := s.ResumeStorageAfterReturn(); err == nil || lease.returns != grants || f.cpu.calls != calls || files.opens != before || f.cpu.releases != 0 {
				t.Fatal("uncertain return retried, read files or released memory")
			}
			if fault == "grant" || fault == "receipt" {
				if grants != 1 {
					t.Fatal("grant fault never reached lease")
				}
			} else if grants != 0 {
				t.Fatal("known bad return reached SD grant")
			}
		})
	}
}
func nextStoredMenu(t *testing.T, f *menuFixture, store *packagestore.Store, request packagestore.Request) (*StoredConfigurationPlan, *packagestore.Handoff) {
	t.Helper()
	selection, err := store.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	data := f.transfer.source
	sum := sha512.Sum384(data)
	m := coldConfigurationMap{Format: 1, Scope: "sealed-cold-configuration-v1", PackageID: f.v.context.PackageID, BuildID: f.v.context.BuildID,
		CoreSHA256: f.v.context.CoreSHA256, CoreSHA384: hex.EncodeToString(sum[:]), CoreSize: len(data), Base: 0x94800000, Size: 8 << 20, DRAMBase: 0x80000000, ChunkBytes: 64}
	for offset := 0; offset < len(data); offset += 64 {
		sum := sha512.Sum384(data[offset:min(offset+64, len(data))])
		m.ChunkSHA384 = append(m.ChunkSHA384, hex.EncodeToString(sum[:]))
	}
	plan, err := f.v.PreflightStoredConfiguration(f.control, canonicalConfiguration(t, m), selection)
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := store.Contain(selection)
	if err != nil {
		t.Fatal(err)
	}
	return plan, handoff
}

func TestMenuStoredRelaunchThreeFreshCycles(t *testing.T) {
	f, store, _, lease, files, request := storedMenu(t)
	registers := maps.Clone(f.cpu.registers)
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		s := f.open(t, context.Background())
		if _, err := s.PresentFrame(context.Background(), menuFrame(byte(cycle+1))); err != nil {
			t.Fatal(err)
		}
		returned, err := s.Close()
		if err != nil {
			t.Fatal(err)
		}
		if returned.After.Generation != uint64(7+cycle*6) || f.cpu.releases != 0 {
			t.Fatal("wrong closure or freed arena", returned)
		}
		if cycle == 2 {
			break
		}
		if err := s.ResumeStorageAfterReturn(); err != nil {
			t.Fatal(err)
		}
		reads := files.opens
		plan, handoff := nextStoredMenu(t, f, store, request)
		if files.opens <= reads {
			t.Fatal("relaunch did not freshly read SD")
		}
		transfer, source, err := s.NewStoredReconfigurationTransfer(plan, handoff, arena.Reserve)
		if err != nil {
			t.Fatal(err)
		}
		if source.Bridges.Generation != returned.After.Generation+3 || transfer.binding.menuReturn != s {
			t.Fatal("lost private menu return")
		}
		calls := f.cpu.calls
		if _, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, arena.Reserve); err == nil || f.cpu.calls != calls {
			t.Fatal("old return replayed")
		}
		cpu := f.v.cpu.(*menuStorageCPU)
		cpu.configuration.wire = &transferWire{t: t, source: bytes.Clone(transfer.source), region: transfer.region}
		for step := 0; step < 40 && !transfer.Status().Done; step++ {
			if _, _, err := transfer.Advance(); err != nil {
				t.Fatal(err)
			}
		}
		if !transfer.Status().Done {
			t.Fatal("new configuration incomplete")
		}
		// The configuration resets device CSRs, never secure owner/generation.
		f.cpu.registers = maps.Clone(registers)
		f.cpu.lastDrainCookie = 0
		f.cpu.accepted, f.cpu.displayed, f.cpu.slot = 0, 0, 0
		f.transfer = transfer
	}
	if lease.returns != 2 || f.cpu.opens != 3 || f.cpu.closes != 3 || f.cpu.releases != 0 {
		t.Fatal("wrong lifecycle counts", lease.returns, f.cpu.opens, f.cpu.closes)
	}
}

func TestMenuStoredRelaunchFailuresConsumeGrantAndPinMemory(t *testing.T) {
	for _, fault := range []string{"arena", "padding", "source", "receipt", "old-handoff", "reserve", "before-start-arena", "before-start-owner", "before-start-source", "before-start-handoff", "write", "open", "close"} {
		t.Run(fault, func(t *testing.T) {
			f, store, old, lease, _, request := storedMenu(t)
			registers := maps.Clone(f.cpu.registers)
			s := f.open(t, context.Background())
			if _, err := s.PresentFrame(context.Background(), menuFrame(1)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := s.ResumeStorageAfterReturn(); err != nil {
				t.Fatal(err)
			}
			plan, handoff := nextStoredMenu(t, f, store, request)
			switch fault {
			case "arena":
				f.v.readerMemoryRegion.Bytes[0] ^= 1
			case "padding":
				f.v.readerMemoryRegion.Bytes[len(f.v.readerMemoryRegion.Bytes)-1] ^= 1
			case "source":
				f.cpu.digest[0] ^= 1
			case "receipt":
				f.cpu.state.Generation++
			case "old-handoff":
				handoff = old
			}
			arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			reservations := 0
			reserve := func(data []byte) (*Region, error) {
				reservations++
				r, err := arena.Reserve(data)
				if err == nil && fault == "reserve" {
					err = errors.New("uncertain reservation")
				}
				return r, err
			}
			transfer, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, reserve)
			if fault == "arena" || fault == "padding" || fault == "source" || fault == "receipt" || fault == "old-handoff" || fault == "reserve" {
				if err == nil {
					t.Fatal("bad handoff granted transfer")
				}
				if fault != "reserve" && reservations != 0 {
					t.Fatal("invalid proof reserved DMA")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				cpu := f.v.cpu.(*menuStorageCPU)
				cpu.configuration.wire = &transferWire{t: t, source: bytes.Clone(transfer.source), region: transfer.region}
				if fault == "before-start-arena" {
					f.v.readerMemoryRegion.Bytes[0] ^= 1
				}
				if fault == "before-start-owner" {
					f.cpu.state.Owner++
				}
				if fault == "before-start-source" {
					f.cpu.digest[0] ^= 1
				}
				if fault == "before-start-handoff" {
					lease.r.Generation++
				}
				if fault == "write" {
					cpu.configuration.wire.fault = "write"
				}
				for step := 0; step < 40 && !transfer.Status().Done; step++ {
					if _, _, err = transfer.Advance(); err != nil {
						break
					}
				}
				if fault == "before-start-arena" || fault == "before-start-owner" || fault == "before-start-source" || fault == "before-start-handoff" || fault == "write" {
					if err == nil || !transfer.Status().Failed || transfer.Status().RetainedBytes == 0 {
						t.Fatal("uncertain transfer was released")
					}
				} else {
					if err != nil || !transfer.Status().Done {
						t.Fatal(err)
					}
					f.cpu.registers = maps.Clone(registers)
					f.cpu.lastDrainCookie = 0
					f.cpu.accepted, f.cpu.displayed, f.cpu.slot = 0, 0, 0
					f.transfer = transfer
					if fault == "open" {
						f.cpu.fault = "data-open"
					}
					next, err := f.v.OpenMenuSession(context.Background(), f.control, f.access, transfer, scanout.MenuLimits{ReaderLimits: windowReadLimits(), PublicationTimeout: 4 * time.Second})
					if fault == "open" {
						if err == nil {
							t.Fatal("uncertain opening accepted")
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						if _, err := next.PresentFrame(context.Background(), menuFrame(1)); err != nil {
							t.Fatal(err)
						}
						f.cpu.fault = "close"
						if _, err := next.Close(); err == nil {
							t.Fatal("failed close accepted")
						}
					}
				}
			}
			if f.cpu.releases != 0 {
				t.Fatal("failure freed frame arena")
			}
			calls := f.cpu.calls
			if _, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, reserve); err == nil || f.cpu.calls != calls {
				t.Fatal("failed grant replayed")
			}
		})
	}
}

func TestMenuStoredRelaunchCannotBorrowAnOpenCopiedOrUnresumedSession(t *testing.T) {
	f, _, handoff, _, _, _ := storedMenu(t)
	s := f.open(t, context.Background())
	plan := &StoredConfigurationPlan{state: &storedConfigurationPlan{v: f.v}}
	reserved := 0
	reserve := func([]byte) (*Region, error) { reserved++; return nil, errors.New("unexpected reservation") }
	calls := f.cpu.calls
	if _, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, reserve); err == nil || reserved != 0 || f.cpu.calls != calls {
		t.Fatal("open session granted rearm")
	}
	if _, err := s.PresentFrame(context.Background(), menuFrame(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Close(); err != nil {
		t.Fatal(err)
	}
	calls = f.cpu.calls
	if _, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, reserve); err == nil || reserved != 0 || f.cpu.calls != calls {
		t.Fatal("closed session skipped SD return")
	}
	if err := s.ResumeStorageAfterReturn(); err != nil {
		t.Fatal(err)
	}
	copied := *s
	calls = f.cpu.calls
	if _, _, err := copied.NewStoredReconfigurationTransfer(plan, handoff, reserve); err == nil || reserved != 0 || f.cpu.calls != calls {
		t.Fatal("copied session borrowed rearm")
	}
}
