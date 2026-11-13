// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// SMC responses are injected; nano_source_release_test.c independently runs
// the actual secure release gate. The real strong source supplier is denied
// by default until its mailbox/SMMU/return proof is connected.
type retainedMenuCPU struct {
	*menuStorageCPU
	fault    string
	releases int
	before   func()
}

func (c *retainedMenuCPU) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] != uint64(g.FesEmbeddedBridgeCall) || a[1] != uint64(g.FesEmbeddedBridgeReleaseSource) {
		return c.menuStorageCPU.Invoke(a)
	}
	c.releases++
	if c.before != nil {
		c.before()
	}
	state := c.state
	if a != ([7]uint64{uint64(g.FesEmbeddedBridgeCall), uint64(g.FesEmbeddedBridgeReleaseSource),
		state.Owner, uint64(g.FesEmbeddedBridgeCookie), state.Generation}) || state.ResetMask != 15 ||
		!state.MastersContained || state.NeverEnabled {
		return [4]uint64{}, errors.New("source release outside returned ownership")
	}
	r := [4]uint64{0, uint64(g.FesEmbeddedBridgeSourceReleaseTag) << 32, state.Owner, state.Generation}
	switch c.fault {
	case "transport":
		return [4]uint64{}, errors.New("unknown source release outcome")
	case "not-ready":
		r[0] = 5
	case "tag":
		r[1] = uint64(g.FesEmbeddedBridgeRearmTag) << 32
	case "owner":
		r[2]++
	case "generation":
		r[3]++
	case "flags":
		r[1]++
	case "after-owner":
		c.state.Owner++
	case "after-generation":
		c.state.Generation++
	}
	return r, nil
}

func TestRetainedMenuUsesAtomicSourceReleaseAcrossThreeReloads(t *testing.T) {
	f, store, _, _, _, request := storedMenuArena(t, true)
	cpu := &retainedMenuCPU{menuStorageCPU: f.v.cpu.(*menuStorageCPU)}
	f.v.cpu = cpu
	registers := maps.Clone(f.cpu.registers)
	for cycle := 0; cycle < 3; cycle++ {
		s := f.open(t, context.Background())
		if _, err := s.PresentFrame(context.Background(), menuFrame(byte(cycle+1))); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if err := s.ResumeStorageAfterReturn(); err != nil {
			t.Fatal(err)
		}
		oldRegion := f.transfer.binding.sourceRegion
		before := bytes.Clone(oldRegion.Bytes)
		cpu.before = func() {
			if !bytes.Equal(before, oldRegion.Bytes) {
				t.Fatal("source overwritten before secure release")
			}
		}
		plan, handoff := nextStoredMenu(t, f, store, request)
		transfer, source, err := s.NewStoredReconfigurationTransfer(plan, handoff, func([]byte) (*Region, error) {
			t.Fatal("retained source escaped its original locked arena")
			return nil, nil
		})
		if err != nil || transfer == nil || source.Bridges.Generation != uint64(10+6*cycle) || cpu.releases != cycle+1 {
			t.Fatal("retained successor failed", err, source.Bridges.Generation, cpu.releases)
		}
		if transfer.region == oldRegion || transfer.region.Address != oldRegion.Address {
			t.Fatal("retained successor lost arena or lease identity")
		}
		cpu.configuration.wire = &transferWire{t: t, source: bytes.Clone(transfer.source), region: transfer.region}
		for step := 0; step < 40 && !transfer.Status().Done; step++ {
			if _, _, err := transfer.Advance(); err != nil {
				t.Fatal(err)
			}
		}
		if !transfer.Status().Done || f.v.completedConfiguration != transfer {
			t.Fatal("successor did not complete under retained native ownership")
		}
		if _, err := oldRegion.reserveNext([]byte{9}, func() error { t.Fatal("stale source grant replayed"); return nil }); err == nil {
			t.Fatal("old source callback affected the new lease")
		}
		f.cpu.registers = maps.Clone(registers)
		f.cpu.lastDrainCookie = 0
		f.cpu.accepted, f.cpu.displayed, f.cpu.slot = 0, 0, 0
		f.transfer = transfer
	}
}

func TestRetainedMenuReleaseFailureCannotCopyOrRetry(t *testing.T) {
	for _, fault := range []string{"transport", "not-ready", "tag", "owner", "generation", "flags", "after-owner", "after-generation"} {
		t.Run(fault, func(t *testing.T) {
			f, store, _, _, _, request := storedMenuArena(t, true)
			cpu := &retainedMenuCPU{menuStorageCPU: f.v.cpu.(*menuStorageCPU), fault: fault}
			f.v.cpu = cpu
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
			previous := f.transfer.binding.sourceRegion
			before := bytes.Clone(previous.Bytes)
			plan, handoff := nextStoredMenu(t, f, store, request)
			reserve := func([]byte) (*Region, error) { t.Fatal("failed release reached external allocator"); return nil, nil }
			if _, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, reserve); err == nil || cpu.releases != 1 {
				t.Fatal("invalid source release passed", err, cpu.releases)
			}
			if !bytes.Equal(before, previous.Bytes) {
				t.Fatal("failed release copied source")
			}
			if _, _, err := s.NewStoredReconfigurationTransfer(plan, handoff, reserve); err == nil || cpu.releases != 1 {
				t.Fatal("failed return/source release replayed")
			}
		})
	}
}
