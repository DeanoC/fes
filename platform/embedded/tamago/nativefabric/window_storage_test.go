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
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

type windowStorageCPU struct {
	*windowCPU
	configuration *reloadCPU
}

func (c *windowStorageCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] != uint64(g.FesEmbeddedBridgeCall) || a[1] == uint64(g.FesEmbeddedBridgePrepareConfiguration) {
		return c.configuration.Invoke(a)
	}
	return c.windowCPU.Invoke(a)
}

type windowStorageLease struct {
	cpu     *windowCPU
	r       sdcontrol.Receipt
	returns int
	fault   string
}

func (l *windowStorageLease) LastReceipt() (sdcontrol.Receipt, bool) { return l.r, true }
func (l *windowStorageLease) Quiesce() error {
	if !l.r.Active || l.r.Generation != l.cpu.state.Generation {
		return errors.New("changed SD owner")
	}
	l.r.Active, l.r.Prepared = false, false
	l.r.Generation++
	l.cpu.state.Generation++
	return nil
}
func (l *windowStorageLease) Resume() error {
	return errors.New("cold resume cannot cross the display return")
}
func (l *windowStorageLease) ResumeAfterReturn(owner, generation uint64) error {
	l.returns++
	if l.r.Active || l.cpu.state.Owner != owner || l.cpu.state.Generation != generation || l.cpu.state.ResetMask != 15 ||
		!l.cpu.state.MastersContained || l.cpu.closes != 1 || l.cpu.lastDrainCookie == 0 {
		return errors.New("missing complete owned display return")
	}
	if l.fault == "grant" {
		l.r = sdcontrol.Receipt{Owner: owner, Generation: generation + 1, Active: true}
		l.cpu.state.Generation = l.r.Generation
		return errors.New("uncertain SD grant; keep ownership")
	}
	l.r = sdcontrol.Receipt{Owner: owner, Generation: generation + 2, Active: true, Prepared: true}
	if l.fault == "receipt" {
		l.r.Generation++
	}
	l.cpu.state.Generation = l.r.Generation
	return nil
}

type windowStorageFiles struct {
	fs.FS
	lease *windowStorageLease
	opens int
}

func (f *windowStorageFiles) Open(path string) (fs.File, error) {
	f.opens++
	if !f.lease.r.Active {
		return nil, errors.New("filesystem read while contained")
	}
	return f.FS.Open(path)
}

// This joins the actual retained package, pinned transfer and window driver.
// Secure calls/CSRs/SD are modeled; the separate C/RTL fixture tests real firmware.
func storedWindow(t *testing.T) (*presentationFixture, *packagestore.Store, *packagestore.Handoff, *windowStorageLease, *windowStorageFiles, packagestore.Request) {
	t.Helper()
	f := newPresentationFixture(t)
	identity := f.v.context
	data := bytes.Repeat([]byte{0x5a}, 128)
	sum, sum384 := sha256.Sum256(data), sha512.Sum384(data)
	identity.CoreSize, identity.CoreSHA256 = int64(len(data)), hex.EncodeToString(sum[:])
	manifest := fmt.Sprintf(`format = 2
[core]
id = %q
name = "Window storage return"
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
repository = "https://example.invalid/window-return"
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
	var access ddrReadMap
	if err := json.Unmarshal(f.control, &control); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(f.access, &access); err != nil {
		t.Fatal(err)
	}
	control["package_id"] = identity.PackageID
	f.control = canonicalConfiguration(t, control)
	memory := *f.v.windowMemory
	memory.PackageID = identity.PackageID
	memoryRaw := canonicalConfiguration(t, memory)
	controlHash, memoryHash := sha256.Sum256(f.control), sha256.Sum256(memoryRaw)
	access.PackageID = identity.PackageID
	access.ControlMapSHA256, access.ReaderMemorySHA256 = hex.EncodeToString(controlHash[:]), hex.EncodeToString(memoryHash[:])
	f.access = canonicalConfiguration(t, access)
	source := canonicalConfiguration(t, identity)
	f.cpu.digest = sha256.Sum256(source)
	cpu := &windowStorageCPU{f.cpu, &reloadCPU{computerCPU: &f.cpu.computerCPU}}
	f.v, err = NewWindowHPSContextVerifier(cpu, source, f.control, memoryRaw)
	if err != nil {
		t.Fatal(err)
	}
	f.cpu.state.Generation = 3
	lease := &windowStorageLease{cpu: f.cpu, r: sdcontrol.Receipt{Owner: 17, Generation: 3, Active: true, Prepared: true}}
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
	_, err = f.v.ReserveWindowMemory(windowCPUFrame(), func(b []byte) (*Region, error) { f.region.Bytes = bytes.Clone(b); return f.region, nil })
	if err != nil {
		t.Fatal(err)
	}
	f.cpu.crc = f.v.windowPublication.CRC32
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

func TestWindowReturnReopensOnlyOriginalStoreAndFreshlyRetains(t *testing.T) {
	f, store, handoff, lease, files, request := storedWindow(t)
	s := f.open(t, context.Background())
	if err := s.ResumeStorageAfterReturn(); err == nil || lease.returns != 0 {
		t.Fatal("open reader resumed storage")
	}
	if _, err := s.PresentFrame(context.Background(), f.frame); err != nil {
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

func TestWindowStorageReturnFailuresConsumeGrantWithoutReplay(t *testing.T) {
	for _, fault := range []string{"missing-handoff", "no-frame", "memory", "padding", "owner", "source", "close", "grant", "receipt"} {
		t.Run(fault, func(t *testing.T) {
			f, _, _, lease, files, _ := storedWindow(t)
			if fault == "missing-handoff" {
				f.transfer.binding.handoff = nil
			}
			s := f.open(t, context.Background())
			if fault != "no-frame" {
				if _, err := s.PresentFrame(context.Background(), f.frame); err != nil {
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
				s.bytes[32<<10] ^= 1
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
