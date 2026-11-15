// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

func TestMenuArenaCorruptionCannotSupplyFrameOrReturn(t *testing.T) {
	frame := menuFrame(1)
	// Cover both pixel areas, both padding areas, the prior scratch boundary
	// and the very last byte. No case may hide corruption by overwriting it.
	positions := []int{0, (32 << 10) - 1, 32 << 10, 3686399, 3686400,
		(4 << 20) - 1, 4 << 20, (4 << 20) + 3686399, (4 << 20) + 3686400, (8 << 20) - 1}
	for _, phase := range []string{"before-open", "between-frames", "publication", "after-ack", "return"} {
		for _, at := range positions {
			t.Run(fmt.Sprintf("%s/%d", phase, at), func(t *testing.T) {
				f := newMenuFixture(t)
				mutated := false
				corrupt := func() {
					if !mutated {
						f.v.readerMemoryRegion.Bytes[at] ^= 0x80
						mutated = true
					}
				}
				if phase == "before-open" {
					corrupt()
					before := f.cpu.calls + f.cpu.reads + f.cpu.writes
					s, err := f.v.OpenMenuSession(context.Background(), f.control, f.access, f.transfer,
						scanout.MenuLimits{ReaderLimits: windowReadLimits(), PublicationTimeout: 4 * time.Second})
					if err == nil || s != nil || before != f.cpu.calls+f.cpu.reads+f.cpu.writes || f.cpu.releases != 0 {
						t.Fatal("corrupt initial arena reached device or lost reservation", s, err)
					}
					return
				}
				s := f.open(t, context.Background())
				if phase == "between-frames" || phase == "return" {
					if _, err := s.PresentFrame(context.Background(), frame); err != nil {
						t.Fatal(err)
					}
				}
				wantAccepted := uint32(0)
				switch phase {
				case "between-frames", "return":
					corrupt()
					wantAccepted = 1
				case "publication":
					f.barrier = func() error { corrupt(); return nil }
				case "after-ack":
					wantAccepted = 1
					f.cpu.after = func(event string) {
						if event == "write" && f.cpu.accepted != 0 {
							corrupt()
						}
					}
				}
				if phase != "return" {
					got, err := s.PresentFrame(context.Background(), frame)
					if err == nil || got != (scanout.MenuFrame{}) {
						t.Fatal("corruption supplied a frame receipt", got, err)
					}
				}
				returned, err := s.Close()
				if !mutated || err == nil || returned != (MenuSessionReturn{}) || f.cpu.accepted != wantAccepted ||
					f.cpu.independentStops != 1 || f.cpu.state.ResetMask != 15 || f.cpu.releases != 0 || f.v.readerMemoryRegion == nil {
					t.Fatal("corrupt scope fabricated return or lost independent containment", returned, err, f.cpu.accepted, f.cpu.independentStops)
				}
				before := f.cpu.calls + f.cpu.reads + f.cpu.writes
				s.Close()
				if _, err := s.PresentFrame(context.Background(), frame); !errors.Is(err, scanout.ErrReaderRetired) || before != f.cpu.calls+f.cpu.reads+f.cpu.writes {
					t.Fatal("terminal arena replayed device operations", err)
				}
			})
		}
	}
}
