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
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

// The production store, transfer and computer drivers share a modeled platform
// here. Separate SD tests execute the actual C closure/lease and PIO drivers.
type computerStorageLease struct {
	cpu     *reloadCPU
	r       sdcontrol.Receipt
	returns int
	fault   bool
}

func (l *computerStorageLease) LastReceipt() (sdcontrol.Receipt, bool) { return l.r, true }
func (l *computerStorageLease) Quiesce() error {
	if !l.r.Active || l.r.Generation != l.cpu.state.Generation {
		return errors.New("not the live SD lease")
	}
	l.r.Active, l.r.Prepared = false, false
	l.r.Generation++
	l.cpu.state.Generation++
	return nil
}
func (l *computerStorageLease) Resume() error {
	return errors.New("cold resume cannot cross a core return")
}
func (l *computerStorageLease) ResumeAfterReturn(owner, generation uint64) error {
	l.returns++
	if l.fault {
		return errors.New("uncertain SD grant")
	}
	if l.r.Active || l.cpu.state.Owner != owner || l.cpu.state.Generation != generation ||
		l.cpu.state.ResetMask != 15 || !l.cpu.state.MastersContained || !l.cpu.finished || l.cpu.prepared || l.cpu.closes == 0 {
		return errors.New("SD grant lacks the completed owned computer return")
	}
	l.r = sdcontrol.Receipt{Owner: owner, Generation: generation + 2, Active: true, Prepared: true}
	l.cpu.state.Generation = l.r.Generation
	return nil
}

func storedComputer(t *testing.T) (*HPSContextVerifier, *reloadCPU, *computerStorageLease, *packagestore.Store, *packagestore.Selection, []byte, []byte, []byte, []byte) {
	t.Helper()
	prototype, port, control, computerRaw := computerFixture(t, "")
	_, configuration, region := configurationSource(t)
	data := bytes.Clone(region.Bytes)
	identity := prototype.context
	digest := sha256.Sum256(data)
	identity.CoreSHA256 = hex.EncodeToString(digest[:])
	identity.CoreSize = int64(len(data))
	dir := t.TempDir()
	manifest := fmt.Sprintf(`format = 2
[core]
id = %q
name = "Storage return fixture"
description = "Synthetic software fixture; never deploy."
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
repository = "https://example.invalid/storage-return"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "synthetic fixture; test only"
`, identity.CoreID, identity.Platform, identity.Device, identity.ProgrammingProfile, len(data), identity.CoreSHA256, identity.ABI, identity.BuildID)
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
	raw := canonicalConfiguration(t, identity)
	var c hpsControlMap
	var m coldConfigurationMap
	var computer computerMap
	if err := json.Unmarshal(control, &c); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(configuration, &m); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(computerRaw, &computer); err != nil {
		t.Fatal(err)
	}
	m.ChunkBytes = 64
	m.ChunkSHA384 = nil
	for offset := 0; offset < len(data); offset += 64 {
		hash := sha512.Sum384(data[offset : offset+64])
		m.ChunkSHA384 = append(m.ChunkSHA384, hex.EncodeToString(hash[:]))
	}
	c.PackageID, m.PackageID, computer.PackageID = identity.PackageID, identity.PackageID, identity.PackageID
	control = canonicalConfiguration(t, c)
	sum := sha256.Sum256(control)
	computer.ControlMapSHA256 = hex.EncodeToString(sum[:])
	configuration = canonicalConfiguration(t, m)
	computerRaw = canonicalConfiguration(t, computer)
	port.digest = sha256.Sum256(raw)
	port.state.Generation = 3
	cpu := &reloadCPU{computerCPU: port}
	v, err := NewHPSContextVerifier(cpu, raw)
	if err != nil {
		t.Fatal(err)
	}
	lease := &computerStorageLease{cpu: cpu, r: sdcontrol.Receipt{Owner: 17, Generation: 3, Active: true, Prepared: true}}
	store, err := packagestore.New(fstest.MapFS{"FES/PKGTEST.BIN": &fstest.MapFile{Data: body}}, lease)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := store.Retain(context.Background(), packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: identity.PackageID})
	if err != nil {
		t.Fatal(err)
	}
	return v, cpu, lease, store, selected, control, configuration, computerRaw, data
}

func TestNativeComputerReturnReopensSDReselectsAndRearmsUnderNewGeneration(t *testing.T) {
	for _, fault := range []string{"pass", "grant", "changed-closure", "hold", "close", "closure-proof", "source"} {
		t.Run(fault, func(t *testing.T) {
			v, cpu, lease, store, selected, control, configuration, computerMap, data := storedComputer(t)
			plan, err := v.PreflightStoredConfiguration(control, configuration, selected)
			if err != nil {
				t.Fatal(err)
			}
			handoff, err := store.Contain(selected)
			if err != nil {
				t.Fatal(err)
			}
			arena, err := NewConfigurationArena(0x94800000, make([]byte, 256), func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			for cycle := 0; cycle < 3; cycle++ {
				calls, grants := cpu.calls, lease.returns
				if v.ResumeStorageAfterReturn(store) == nil || cpu.calls != calls || lease.returns != grants {
					t.Fatal("storage reopened without an unused successful computer return")
				}
				var transfer *ConfigurationTransfer
				if cycle == 0 {
					transfer, _, err = plan.NewTransfer(handoff, arena.Reserve)
				} else {
					transfer, _, err = plan.NewReconfigurationTransfer(handoff, arena.Reserve)
				}
				if err != nil {
					t.Fatal(cycle, err)
				}
				cpu.wire = &transferWire{t: t, source: bytes.Clone(data), region: transfer.region}
				for step := 0; step < 30 && !transfer.Status().Done; step++ {
					if _, _, err = transfer.Advance(); err != nil {
						t.Fatal(cycle, err)
					}
				}
				if !transfer.Status().Done {
					t.Fatal("transfer incomplete")
				}
				session, err := v.OpenComputer(context.Background(), control, computerMap, transfer, computerLimits())
				if err != nil {
					t.Fatal(cycle, err)
				}
				if err = session.Computer().Run(context.Background()); err != nil {
					t.Fatal(err)
				}
				calls, grants = cpu.calls, lease.returns
				if v.ResumeStorageAfterReturn(store) == nil || cpu.calls != calls || lease.returns != grants {
					t.Fatal("storage reopened while the computer was active")
				}
				switch fault {
				case "hold":
					cpu.stall = true
				case "close":
					cpu.loseClose = true
				case "closure-proof":
					cpu.badClosure = true
				case "source":
					cpu.digest[0] ^= 1
				}
				closed, err := session.Close(context.Background())
				if fault == "hold" || fault == "close" || fault == "closure-proof" || fault == "source" {
					if err == nil {
						t.Fatal("failed computer return accepted")
					}
					calls, grants = cpu.calls, lease.returns
					for attempt := 0; attempt < 2; attempt++ {
						if v.ResumeStorageAfterReturn(store) == nil || cpu.calls != calls || lease.returns != grants {
							t.Fatal("failed return granted or replayed storage access")
						}
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if closed.After.Generation != uint64(6+5*cycle) {
					t.Fatal(closed.After)
				}
				if cycle == 2 {
					break
				}
				if fault == "grant" {
					lease.fault = true
				}
				if fault == "changed-closure" {
					cpu.state.Generation++
				}
				err = v.ResumeStorageAfterReturn(store)
				if fault != "pass" {
					if err == nil {
						t.Fatal("unsafe SD resume accepted")
					}
					calls, grants := cpu.calls, lease.returns
					if v.ResumeStorageAfterReturn(store) == nil || cpu.calls != calls || lease.returns != grants {
						t.Fatal("uncertain SD resume replayed")
					}
					return
				}
				if err != nil {
					t.Fatal(cycle, err)
				}
				if _, err := handoff.Selection(); !errors.Is(err, packagestore.ErrState) {
					t.Fatal("old handoff survived SD ownership", err)
				}
				selected, err := store.Retain(context.Background(), packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: v.Identity().PackageID})
				if err != nil {
					t.Fatal(err)
				}
				plan, err = v.PreflightStoredConfiguration(control, configuration, selected)
				if err != nil {
					t.Fatal(err)
				}
				handoff, err = store.Contain(selected)
				if err != nil {
					t.Fatal(err)
				}
				if handoff.ReturnedFrom().Generation != closed.After.Generation {
					t.Fatal("storage lost its core-return provenance")
				}
			}
			if cpu.configurations != 3 || cpu.opens != 3 || cpu.closes != 3 || lease.returns != 2 {
				t.Fatal(cpu.configurations, cpu.opens, cpu.closes, lease.returns)
			}
		})
	}
}
