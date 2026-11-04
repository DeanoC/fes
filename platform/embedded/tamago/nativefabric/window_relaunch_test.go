// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"maps"
	"testing"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

func nextStoredWindow(t *testing.T, f *presentationFixture, store *packagestore.Store, request packagestore.Request) (*StoredConfigurationPlan, *packagestore.Handoff) {
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

func TestWindowStoredRelaunchThreeFreshCycles(t *testing.T) {
	f, store, _, lease, files, request := storedWindow(t)
	registers := maps.Clone(f.cpu.registers)
	arena, err := NewProgramArena(0x94800000, make([]byte, ProgramMetadataBytes+256), func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		s := f.open(t, context.Background())
		f.colour(byte(cycle + 1))
		if _, err := s.PresentFrame(context.Background(), f.frame); err != nil {
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
		plan, handoff := nextStoredWindow(t, f, store, request)
		if files.opens <= reads {
			t.Fatal("relaunch did not freshly read SD")
		}
		transfer, source, err := s.NewStoredReconfigurationTransfer(plan, handoff, arena.Reserve)
		if err != nil {
			t.Fatal(err)
		}
		if source.Bridges.Generation != returned.After.Generation+3 || transfer.binding.windowReturn != s {
			t.Fatal("lost private window return")
		}
		calls := f.cpu.calls
		if _, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, arena.Reserve); err == nil || f.cpu.calls != calls {
			t.Fatal("old return replayed")
		}
		cpu := f.v.cpu.(*windowStorageCPU)
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
		f.transfer = transfer
	}
	if lease.returns != 2 || f.cpu.opens != 3 || f.cpu.closes != 3 || f.cpu.releases != 0 {
		t.Fatal("wrong lifecycle counts", lease.returns, f.cpu.opens, f.cpu.closes)
	}
}

func TestWindowStoredRelaunchFailuresConsumeGrantAndPinMemory(t *testing.T) {
	for _, fault := range []string{"arena", "padding", "source", "receipt", "old-handoff", "reserve", "before-start-arena", "write", "open", "close"} {
		t.Run(fault, func(t *testing.T) {
			f, store, old, _, _, request := storedWindow(t)
			registers := maps.Clone(f.cpu.registers)
			s := f.open(t, context.Background())
			if _, err := s.PresentFrame(context.Background(), f.frame); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := s.ResumeStorageAfterReturn(); err != nil {
				t.Fatal(err)
			}
			plan, handoff := nextStoredWindow(t, f, store, request)
			switch fault {
			case "arena":
				f.region.Bytes[0] ^= 1
			case "padding":
				f.region.Bytes[len(f.region.Bytes)-1] ^= 1
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
				cpu := f.v.cpu.(*windowStorageCPU)
				cpu.configuration.wire = &transferWire{t: t, source: bytes.Clone(transfer.source), region: transfer.region}
				if fault == "before-start-arena" {
					f.region.Bytes[0] ^= 1
				}
				if fault == "write" {
					cpu.configuration.wire.fault = "write"
				}
				for step := 0; step < 40 && !transfer.Status().Done; step++ {
					if _, _, err = transfer.Advance(); err != nil {
						break
					}
				}
				if fault == "before-start-arena" || fault == "write" {
					if err == nil || !transfer.Status().Failed || transfer.Status().RetainedBytes == 0 {
						t.Fatal("uncertain transfer was released")
					}
				} else {
					if err != nil || !transfer.Status().Done {
						t.Fatal(err)
					}
					f.cpu.registers = maps.Clone(registers)
					f.cpu.lastDrainCookie = 0
					f.transfer = transfer
					if fault == "open" {
						f.cpu.fault = "data-open"
					}
					next, err := f.v.OpenWindowSession(context.Background(), f.control, f.access, transfer, windowReadLimits())
					if fault == "open" {
						if err == nil {
							t.Fatal("uncertain opening accepted")
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						if _, err := next.PresentFrame(context.Background(), f.frame); err != nil {
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

func TestWindowStoredRelaunchCannotBorrowAnOpenCopiedOrUnresumedSession(t *testing.T) {
	f, _, handoff, _, _, _ := storedWindow(t)
	s := f.open(t, context.Background())
	plan := &StoredConfigurationPlan{state: &storedConfigurationPlan{v: f.v}}
	reserved := 0
	reserve := func([]byte) (*Region, error) { reserved++; return nil, errors.New("unexpected reservation") }
	calls := f.cpu.calls
	if _, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, reserve); err == nil || reserved != 0 || f.cpu.calls != calls {
		t.Fatal("open session granted rearm")
	}
	if _, err := s.PresentFrame(context.Background(), f.frame); err != nil {
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
