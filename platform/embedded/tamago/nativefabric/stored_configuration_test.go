// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
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
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

type storedLease struct{ receipt sdcontrol.Receipt }

func (s *storedLease) LastReceipt() (sdcontrol.Receipt, bool) { return s.receipt, true }
func (s *storedLease) Quiesce() error {
	s.receipt.Active, s.receipt.Prepared = false, false
	s.receipt.Generation++
	return nil
}
func (s *storedLease) Resume() error {
	s.receipt.Active, s.receipt.Prepared = true, true
	s.receipt.Generation += 2
	return nil
}

type storedFiles struct {
	files fstest.MapFS
	lease *storedLease
	opens int
}

func (s *storedFiles) Open(path string) (fs.File, error) {
	s.opens++
	if !s.lease.receipt.Active {
		return nil, errors.New("filesystem access after containment")
	}
	return s.files.Open(path)
}

func storedSelection(t *testing.T) ([]byte, []byte, []byte, []byte, *storedFiles, *storedLease, *packagestore.Store, *packagestore.Selection) {
	t.Helper()
	var source HPSBootContext
	if err := json.Unmarshal(hpsContextFixture(t), &source); err != nil {
		t.Fatal(err)
	}
	chunk := 0xfffc0
	data := bytes.Repeat([]byte{0x5a}, chunk+128)
	sum, sum384 := sha256.Sum256(data), sha512.Sum384(data)
	source.CoreSize, source.CoreSHA256 = int64(len(data)), hex.EncodeToString(sum[:])
	manifest := fmt.Sprintf(`format = 2
[core]
id = %q
name = "Retained configuration fixture"
description = "Test only; never deploy."
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
repository = "https://example.invalid/retained-cold"
revision = "1111111111111111111111111111111111111111"
recipe_sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
toolchain = "synthetic fixture; test only"
`, source.CoreID, source.Platform, source.Device, source.ProgrammingProfile, len(data), source.CoreSHA256, source.ABI, source.BuildID)
	dir := t.TempDir()
	for name, raw := range map[string][]byte{"manifest.toml": []byte(manifest), "core.rbf": data} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := corepackage.CanonicalArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	source.PackageID = inspection.PackageID
	var control hpsControlMap
	if err := json.Unmarshal(controlMapFixture(t), &control); err != nil {
		t.Fatal(err)
	}
	control.PackageID = source.PackageID
	mapping := coldConfigurationMap{Format: 1, Scope: "sealed-cold-configuration-v1", PackageID: source.PackageID,
		BuildID: source.BuildID, CoreSHA256: source.CoreSHA256, CoreSHA384: hex.EncodeToString(sum384[:]),
		CoreSize: len(data), Base: 0x94800000, Size: 8 << 20, DRAMBase: 0x80000000, ChunkBytes: chunk}
	for offset := 0; offset < len(data); offset += chunk {
		digest := sha512.Sum384(data[offset:min(offset+chunk, len(data))])
		mapping.ChunkSHA384 = append(mapping.ChunkSHA384, hex.EncodeToString(digest[:]))
	}
	lease := &storedLease{sdcontrol.Receipt{Owner: 17, Generation: 3, Active: true, Prepared: true}}
	files := &storedFiles{files: fstest.MapFS{"FES/PKGTEST.BIN": &fstest.MapFile{Data: archive}}, lease: lease}
	store, err := packagestore.New(files, lease)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := store.Retain(context.Background(), packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: source.PackageID})
	if err != nil {
		t.Fatal(err)
	}
	// A later read of the card would be both forbidden and corrupt. The loader
	// must consume the real validator's privately retained bytes instead.
	clear(archive)
	return canonicalConfiguration(t, source), canonicalConfiguration(t, control), canonicalConfiguration(t, mapping), data, files, lease, store, selection
}

func storedPackage(t *testing.T) ([]byte, []byte, []byte, []byte, *storedFiles, *storedLease, *packagestore.Store, *packagestore.Handoff) {
	t.Helper()
	raw, control, mapping, data, files, lease, store, selection := storedSelection(t)
	handoff, err := store.Contain(selection)
	if err != nil {
		t.Fatal(err)
	}
	return raw, control, mapping, data, files, lease, store, handoff
}

// These fault fixtures begin with SD already contained. The separate preflight
// tests and joined return test exercise preparation before the real SD stop.
func storedTransfer(v *HPSContextVerifier, control, mapping []byte, handoff *packagestore.Handoff, reserve func([]byte) (*Region, error), rearm bool) (*ConfigurationTransfer, HPSBootObservation, error) {
	selection, err := handoff.Selection()
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	plan, err := v.PreflightStoredConfiguration(control, mapping, selection)
	if err != nil {
		return nil, HPSBootObservation{}, err
	}
	if rearm {
		return plan.NewReconfigurationTransfer(handoff, reserve)
	}
	return plan.NewTransfer(handoff, reserve)
}

func TestStoredPackageJoinsContainmentToTwoBlockTransferAndRetainsEveryFailure(t *testing.T) {
	for _, name := range []string{"pass", "no-handoff", "bad-map", "no-reservation", "wrong-package", "wrong-owner", "wrong-generation", "not-cold", "resume-before-constructor", "resume-before-start", "resume-in-reserve", "reservation-error", "changed-reservation", "late-generation", "retirement"} {
		t.Run(name, func(t *testing.T) {
			raw, control, mapping, data, files, lease, store, handoff := storedPackage(t)
			if name == "no-handoff" {
				handoff = nil
			}
			if name == "bad-map" {
				mapping = nil
			}
			if name == "wrong-package" {
				for _, input := range []*[]byte{&raw, &control, &mapping} {
					var fields map[string]interface{}
					if err := json.Unmarshal(*input, &fields); err != nil {
						t.Fatal(err)
					}
					fields["package_id"] = fmt.Sprintf("%064x", 1)
					*input = canonicalConfiguration(t, fields)
				}
			}
			if name == "resume-before-constructor" {
				if err := store.Resume(); err != nil {
					t.Fatal(err)
				}
			}
			digest := sha256.Sum256(raw)
			calls, starts, accepted, completedBytes, consumed, reservations, retirements := 0, 0, 0, 0, 0, 0, 0
			var region *Region
			cpu := bridgeFirmwareFunc(func(a [7]uint64) ([4]uint64, error) {
				calls++
				owner, generation, flags := uint64(17), uint64(4), uint64(0xdf)
				if name == "wrong-owner" {
					owner++
				}
				if name == "wrong-generation" || name == "late-generation" && calls > 4 {
					generation++
				}
				if name == "not-cold" {
					flags &^= 0x40
				}
				if a[0] == 0xc2000301 {
					switch a[1] {
					case 0:
						return [4]uint64{0, 0x46424f3100000000 | flags, owner, generation}, nil
					case 4, 5:
						index := int(a[1]-4) * 16
						return [4]uint64{0, 0x4648433100000000 | a[1] - 4, binary.BigEndian.Uint64(digest[index:]), binary.BigEndian.Uint64(digest[index+8:])}, nil
					case 8:
						if lease.receipt.Active || a[2] != owner || a[4] != generation {
							t.Fatal("preparation bypassed live storage ownership", a)
						}
						return [4]uint64{0, 0x4643503100000000 | uint64(len(data)), owner, generation}, nil
					case 9:
						retirements++
						if a[2] != owner || a[4] != generation {
							t.Fatal("retirement changed owner/generation", a)
						}
						return [4]uint64{0, 0x4643523100000000, owner, generation}, nil
					}
				}
				if lease.receipt.Active {
					t.Fatal("active SD reached configuration", a)
				}
				switch a[0] {
				case start:
					starts++
					return [4]uint64{}, nil
				case write:
					if a[1] != 0x94800000+uint64(accepted) || a[2] != uint64(min(0xfffc0, len(data)-accepted)) {
						t.Fatal("wrong stored transfer extent", a)
					}
					accepted += int(a[2])
					return [4]uint64{}, nil
				case completed:
					if name == "retirement" {
						return [4]uint64{1}, nil
					}
					address := uint64(0x14800000 + completedBytes)
					completedBytes += min(0xfffc0, len(data)-completedBytes)
					return [4]uint64{0, address}, nil
				case isdone:
					return [4]uint64{}, nil
				}
				t.Fatal("unexpected firmware operation", a)
				return [4]uint64{}, nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			reserve := func(source []byte) (*Region, error) {
				reservations++
				if !bytes.Equal(source, data) {
					t.Fatal("reservation lost retained payload")
				}
				if name == "reservation-error" {
					return nil, errors.New("reservation unavailable")
				}
				region = &Region{Address: 0x94800000, Bytes: bytes.Clone(source), Release: func() error { consumed++; return nil }}
				if name == "changed-reservation" {
					region.Bytes[0] ^= 1
				}
				if name == "resume-in-reserve" {
					if err := store.Resume(); err != nil {
						t.Fatal(err)
					}
				}
				return region, nil
			}
			if name == "no-reservation" {
				reserve = nil
			}
			opens := files.opens
			transfer, source, err := storedTransfer(v, control, mapping, handoff, reserve, false)
			constructorFailure := name == "no-handoff" || name == "bad-map" || name == "no-reservation" || name == "wrong-package" || name == "wrong-owner" || name == "wrong-generation" || name == "not-cold" || name == "resume-before-constructor" || name == "reservation-error"
			if constructorFailure {
				if err == nil || transfer != nil || starts != 0 || consumed != 0 {
					t.Fatal("invalid stored constructor progressed", err, starts, consumed)
				}
				if name != "reservation-error" && reservations != 0 {
					t.Fatal("invalid source reserved DMA memory")
				}
			} else {
				if err != nil || source.Bridges.Generation != 4 || source.Bridges.Owner != 17 || reservations != 1 {
					t.Fatal(transfer, source, err)
				}
				if name == "resume-before-start" {
					if err = store.Resume(); err != nil {
						t.Fatal(err)
					}
				}
				for step := 0; step < 16 && !transfer.Status().Done; step++ {
					_, _, err = transfer.Advance()
					if err != nil {
						break
					}
					if name == "retirement" && transfer.Status().Outstanding == 2 {
						reason := transfer.Cancel(errors.New("original deadline"))
						observed, err := v.RetireConfiguration(transfer, source, mapping)
						if err != nil || observed.Generation != 4 || retirements != 1 || transfer.err != reason || consumed != 0 {
							t.Fatal(observed, err)
						}
						break
					}
				}
				status := transfer.Status()
				if name == "pass" {
					if !status.Done || status.Failed || starts != 1 || consumed != 1 || accepted != len(data) || completedBytes != len(data) {
						t.Fatal(status, starts, consumed)
					}
				} else {
					if !status.Failed || status.Done || consumed != 0 || status.RetainedBytes != len(data) {
						t.Fatal("failure lost its reservation", status)
					}
					if name != "retirement" && starts != 0 {
						t.Fatal("unverified storage/source reached START")
					}
				}
			}
			if files.opens != opens {
				t.Fatal("configuration reopened contained storage")
			}
		})
	}
}

func TestStoredReloadKeepsArchiveContainedAndRequiresSuccessfulReturn(t *testing.T) {
	for _, mode := range []string{"pass", "no-return", "stale-return", "cold-source", "wrong-owner", "resume-before-constructor", "resume-in-reserve"} {
		t.Run(mode, func(t *testing.T) {
			raw, control, mapping, data, files, _, store, handoff := storedPackage(t)
			opens := files.opens
			replies := hpsContextReplies(sha256.Sum256(raw))
			for _, i := range []int{0, 3} {
				replies[i][1] &^= 64
				replies[i][3] = 6
				if mode == "cold-source" {
					replies[i][1] |= 64
				}
				if mode == "wrong-owner" {
					replies[i][2]++
				}
			}
			replies = append(replies, replies...)
			replies = append(replies, [4]uint64{0, 0x4657523100000000 | uint64(len(data)), 17, 6}, replies[0])
			calls, starts, reservations, releases := 0, 0, 0, 0
			operations := []uint64{0, 4, 5, 0, 0, 4, 5, 0, 11, 0}
			cpu := bridgeFirmwareFunc(func(a [7]uint64) ([4]uint64, error) {
				if a[0] == start {
					if calls != 10 {
						t.Fatal("stored reload START preceded preparation")
					}
					starts++
					return [4]uint64{}, nil
				}
				if calls >= len(replies) || a[1] != operations[calls] {
					t.Fatal("unexpected stored reload request", a)
				}
				r := replies[calls]
				calls++
				return r, nil
			})
			v, err := NewHPSContextVerifier(cpu, raw)
			if err != nil {
				t.Fatal(err)
			}
			// The separate joined cycle drives this state through real Close.
			v.computerGeneration, v.computerClosed = 4, true
			if mode == "no-return" {
				v.computerClosed = false
			}
			if mode == "stale-return" {
				v.computerGeneration = 6
			}
			if mode == "resume-before-constructor" {
				if err := store.Resume(); err != nil {
					t.Fatal(err)
				}
			}
			reserve := func(source []byte) (*Region, error) {
				reservations++
				if !bytes.Equal(source, data) {
					t.Fatal("retained package changed")
				}
				if mode == "resume-in-reserve" {
					if err := store.Resume(); err != nil {
						t.Fatal(err)
					}
				}
				return &Region{Address: 0x94800000, Bytes: bytes.Clone(source), Release: func() error { releases++; return nil }}, nil
			}
			transfer, source, err := storedTransfer(v, control, mapping, handoff, reserve, true)
			if mode != "pass" && mode != "resume-in-reserve" {
				if err == nil || transfer != nil || reservations != 0 || starts != 0 || files.opens != opens {
					t.Fatal("invalid stored return reached reservation", err)
				}
				return
			}
			if err != nil || reservations != 1 || source.Bridges.Generation != 6 || source.Bridges.NeverEnabled || handoff.Receipt().Generation != 4 {
				t.Fatal(source, err)
			}
			_, _, err = transfer.Advance()
			if mode == "pass" {
				if err != nil || starts != 1 || calls != 10 {
					t.Fatal("retained reload not prepared", err, calls, starts)
				}
				_ = transfer.Cancel(errors.New("test retains unfinished transfer"))
			} else if err == nil || starts != 0 || !transfer.Status().Failed {
				t.Fatal("resumed SD reached START", err)
			}
			if releases != 0 || files.opens != opens || transfer.Status().RetainedBytes != len(data) {
				t.Fatal("stored reload reopened SD or released pending memory")
			}
		})
	}
}
