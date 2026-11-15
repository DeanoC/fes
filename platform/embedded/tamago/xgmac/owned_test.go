// SPDX-License-Identifier: GPL-3.0-or-later
package xgmac

import (
	"context"
	"errors"
	"testing"

	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/maccontrol"
)

type startupCaller func([7]uint64) ([4]uint64, error)

func (f startupCaller) Invoke(a [7]uint64) ([4]uint64, error) { return f(a) }

type startupMemory struct {
	*memory
	size uint64
}

func (m *startupMemory) Size() uint64 { return m.size }

func TestControlledStartupFailureRetainsLeaseWithoutCleanup(t *testing.T) {
	for _, mode := range []string{"unavailable", "wrong-owner", "acquire-rejected", "acquire-lost", "phy", "confirm-rejected", "confirm-lost", "confirm-cancelled", "changed-window"} {
		t.Run(mode, func(t *testing.T) {
			h := newHardware()
			fw := newFirmwareMAC(t, h)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mem := &startupMemory{h.mem, h.mem.Size()}
			var calls []uint64
			boundaryAccesses, boundaryWrites := -1, -1
			injected := errors.New("injected lost firmware reply")
			cpu := startupCaller(func(a [7]uint64) ([4]uint64, error) {
				op := a[1]
				calls = append(calls, op)
				if op == uint64(g.FesEmbeddedMacQuery) && mode == "unavailable" ||
					op == uint64(g.FesEmbeddedMacAcquire) && mode == "acquire-rejected" ||
					op == uint64(g.FesEmbeddedMacConfirm) && mode == "confirm-rejected" {
					boundaryAccesses, boundaryWrites = h.accesses, h.mem.writes
					return [4]uint64{uint64(g.FesEmbeddedMacNotReady), uint64(g.FesEmbeddedMacTag) << 32}, nil
				}
				r, err := fw.Invoke(a)
				if op == uint64(g.FesEmbeddedMacAcquire) && mode == "acquire-lost" ||
					op == uint64(g.FesEmbeddedMacConfirm) && mode == "confirm-lost" {
					boundaryAccesses, boundaryWrites = h.accesses, h.mem.writes
					return r, injected
				}
				if op == uint64(g.FesEmbeddedMacConfirm) {
					if mode == "confirm-cancelled" {
						cancel()
					}
					if mode == "changed-window" {
						mem.size -= 64
					}
					boundaryAccesses, boundaryWrites = h.accesses, h.mem.writes
				}
				return r, err
			})
			owner := uint64(17)
			if mode == "wrong-owner" {
				owner = 18
			}
			if mode == "phy" {
				h.phy[2] ^= 1
			}
			d, control, err := OpenControlled(ctx, cpu, owner, h, mem, configuration())
			if err == nil || control.Stop != nil || control.Start != nil || control.Confirm != nil {
				t.Fatal("failed startup exposed packet/lifecycle controls", err)
			}
			if boundaryAccesses >= 0 && (h.accesses != boundaryAccesses || h.mem.writes != boundaryWrites) {
				t.Fatal("driver accessed registers/descriptors after rejected/lost boundary")
			}
			for _, op := range calls {
				if op == uint64(g.FesEmbeddedMacStop) {
					t.Fatal("automatic cleanup after uncertain startup")
				}
			}
			if mode == "unavailable" || mode == "wrong-owner" || mode == "acquire-rejected" || mode == "acquire-lost" {
				if d != nil || h.mem.writes != 0 {
					t.Fatal("driver initialized before accepted grant")
				}
			} else {
				if d == nil || d.Err() == nil || d.lease == nil {
					t.Fatal("failed driver/lease not retained and retired")
				}
				before, writes := h.accesses, h.mem.writes
				if _, err := d.Receive(make([]byte, 1600)); err == nil {
					t.Fatal("retired receive accepted")
				}
				if err := d.Transmit(make([]byte, 64)); err == nil {
					t.Fatal("retired transmit accepted")
				}
				if h.accesses != before || h.mem.writes != writes {
					t.Fatal("retired driver accessed hardware")
				}
				last, valid := d.lease.LastReceipt()
				if mode == "confirm-cancelled" && (!valid || !last.Active || !last.Confirmed || last.Generation != 2) {
					t.Fatal("completed confirmation lost on cancellation", last, valid)
				}
			}
			// A completed or uncertain acquisition cannot be adopted by a fresh
			// client, even though its prior Go handle is inaccessible.
			if mode != "unavailable" && mode != "wrong-owner" && mode != "acquire-rejected" {
				before, writes := h.accesses, h.mem.writes
				if _, err := maccontrol.New(context.Background(), cpu, 17); err == nil {
					t.Fatal("fresh client adopted used startup")
				}
				if h.accesses != before || h.mem.writes != writes {
					t.Fatal("failed adoption touched driver")
				}
			}
		})
	}
}

func TestControlledStartupCancellationBeforeFirmware(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := newHardware()
	called := false
	cpu := startupCaller(func([7]uint64) ([4]uint64, error) { called = true; return [4]uint64{}, nil })
	if d, _, err := OpenControlled(ctx, cpu, 17, h, h.mem, configuration()); !errors.Is(err, context.Canceled) || d != nil || called || h.accesses != 0 || h.mem.writes != 0 {
		t.Fatal("cancelled startup performed work", err, called)
	}
}
