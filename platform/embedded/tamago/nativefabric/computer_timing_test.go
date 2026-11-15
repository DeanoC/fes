// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

type timingCPU struct {
	*computerCPU
	failRead bool
}

func (c *timingCPU) ReadPhysical32(address uint64) (uint32, error) {
	if c.failRead {
		return 0, errors.New("injected register bus error")
	}
	return c.computerCPU.ReadPhysical32(address)
}

func TestComputerAccessTimingPreservesPairedChecksAndFailureRetirement(t *testing.T) {
	for _, mode := range []string{"read", "write", "before", "register", "after"} {
		t.Run(mode, func(t *testing.T) {
			v, cpu, control, mapping := computerFixture(t, "")
			s, err := v.OpenComputer(context.Background(), control, mapping, computerCompletionFixture(v, cpu), computerLimits())
			if err != nil {
				t.Fatal(err)
			}
			ticks := []time.Duration{0, 2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond}
			samples := 0
			s.port.clock = func() time.Time { d := ticks[samples]; samples++; return time.Unix(100, 0).Add(d) }
			if mode == "before" {
				cpu.state.Generation++
			}
			if mode == "after" {
				cpu.changeAfterRead = true
			}
			if mode == "register" {
				s.port.cpu = &timingCPU{cpu, true}
			}
			calls, reads, writes := cpu.calls, cpu.reads, cpu.writes
			if mode == "write" {
				err = s.port.Write32(cg.FesDe25ComputerGpoOffset, cg.FesComputerRequestMask)
			} else {
				_, err = s.port.Read32(cg.FesDe25ComputerStatusOffset)
			}
			good := mode == "read" || mode == "write"
			if (err == nil) != good {
				t.Fatal(mode, err)
			}
			want := computer.AccessTiming{Offset: cg.FesDe25ComputerStatusOffset, Before: 2 * time.Millisecond}
			expectedCalls, expectedReads, expectedWrites := 1, 0, 0
			if mode != "before" {
				want.Register = 3 * time.Millisecond
				if mode != "register" {
					want.After = 5 * time.Millisecond
					expectedCalls = 2
					if mode == "write" {
						expectedWrites = 1
					} else {
						expectedReads = 1
					}
				}
			}
			want.Completed = good
			if mode == "write" {
				want.Offset = cg.FesDe25ComputerGpoOffset
				want.Write = true
			}
			if got := s.port.LastAccessTiming(); got != want {
				t.Fatal(got, want)
			}
			if cpu.calls-calls != expectedCalls || cpu.reads-reads != expectedReads || cpu.writes-writes != expectedWrites {
				t.Fatal("timing changed actual access sequence", cpu)
			}
			// Reading diagnostics never queries firmware or touches the aperture.
			calls, reads, writes = cpu.calls, cpu.reads, cpu.writes
			_ = s.port.LastAccessTiming()
			if !good {
				if _, err = s.port.Read32(cg.FesDe25ComputerStatusOffset); err == nil {
					t.Fatal("failed access not retired")
				}
			}
			if cpu.calls != calls || cpu.reads != reads || cpu.writes != writes {
				t.Fatal("diagnostics or retired access touched IO")
			}
		})
	}
}
