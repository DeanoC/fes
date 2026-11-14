// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

type executionCPU struct {
	CPUTransport
	cpu                *reloadCPU
	mode               string
	reads, runs, holds int
	now                *time.Time
	cancel             context.CancelFunc
}

func (c *executionCPU) ReadPhysical32(address uint64) (uint32, error) {
	v, err := c.CPUTransport.ReadPhysical32(address)
	if err != nil {
		return 0, err
	}
	if address == 0x20010300+uint64(cg.FesDe25ComputerFetchAddressOffset) && c.mode == "address" {
		return 0x10000, nil
	}
	if address != 0x20010300+uint64(cg.FesDe25ComputerFetchCountOffset) {
		return v, nil
	}
	c.reads++
	switch c.mode {
	case "cancel":
		c.cancel()
	case "delayed":
		if c.reads <= 4 {
			return 0, nil
		}
		return 2, nil
	case "held-change":
		return uint32(c.reads), nil
	case "stuck":
		return 100, nil
	case "backwards":
		if c.reads <= 2 {
			return 100, nil
		}
		return 90, nil
	case "wrap":
		if c.reads <= 2 {
			return 0xfffffff0, nil
		}
		return 10, nil
	case "bus":
		return 0, errors.New("fetch bus error")
	case "owner":
		c.cpu.state.Generation++
	case "late":
		*c.now = c.now.Add(time.Second)
	case "clock":
		*c.now = c.now.Add(-time.Nanosecond)
	}
	return v, nil
}
func (c *executionCPU) WritePhysical32(address uint64, word uint32) error {
	if address == 0x20010300+uint64(cg.FesDe25ComputerGpoOffset) && word>>24&127 == cg.FesComputerOpcodeExecution {
		if uint16(word) == uint16(cg.FesComputerExecutionRelease) {
			c.runs++
		} else if uint16(word) == uint16(cg.FesComputerExecutionHoldReset) {
			c.holds++
		}
	}
	return c.CPUTransport.WritePhysical32(address, word)
}

func TestProgramLaunchFetchObservationAndReturnAreGenerationBound(t *testing.T) {
	f := serviceTest(t)
	if _, ok := f.s.FetchObservation(); ok {
		t.Fatal("unlaunched execution published")
	}
	for i := 0; i < 3; i++ {
		session := f.launch(t, i)
		o, ok := f.s.FetchObservation()
		if !ok || o.Generation != session.Generation() || o.Owner != f.s.Status().Owner || o.PackageID != f.s.Status().Identity.PackageID || o.Before != o.Held || o.Delta != 1 || o.Running.Cycles-o.Held.Cycles != o.Delta {
			t.Fatal(o, ok)
		}
		original := o
		o.PackageID = "caller mutation"
		if next, _ := f.s.FetchObservation(); next != original {
			t.Fatal("saved observation aliased caller", next)
		}
		if _, err := f.s.Return(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.s.FetchObservation(); ok {
			t.Fatal("returned observation remained live")
		}
	}
}

func TestProgramLaunchFetchFailuresRetainOwnershipAndCannotReplay(t *testing.T) {
	for _, mode := range []string{"held-change", "stuck", "backwards", "address", "bus", "owner", "late", "clock", "wrap", "delayed", "cancel", "yield"} {
		t.Run(mode, func(t *testing.T) {
			f := serviceTest(t)
			now := time.Unix(100, 0)
			f.s.state.limits.Now = func() time.Time { return now }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "yield" {
				f.s.state.limits.Yield = func() { now = now.Add(time.Second) }
			}
			cpu := &executionCPU{CPUTransport: f.loader.boot.cpu.(CPUTransport), cpu: f.cpu, mode: mode, now: &now, cancel: cancel}
			f.loader.boot.cpu = cpu
			req, _, _ := addProgram(t, f.loader, f.files, 0)
			session, err := f.s.Launch(ctx, 0, req)
			if mode == "wrap" || mode == "delayed" {
				o, ok := f.s.FetchObservation()
				expected := uint32(26)
				if mode == "delayed" {
					expected = 2
				}
				if err != nil || !ok || o.Delta != expected || session.Generation() != 1 {
					t.Fatal(err, o, ok)
				}
				return
			}
			if !errors.Is(err, ErrProgramRetired) || !errors.Is(err, ErrProgramExecution) || f.s.Status().State != ProgramRebootRequired || f.lease.r.Active || f.lease.returns != 0 || f.cpu.closes > 1 || cpu.runs > 1 || cpu.holds > 2 {
				t.Fatal(err, f.s.Status(), cpu)
			}
			if mode == "held-change" && cpu.runs != 0 {
				t.Fatal("Run issued after failed Hold proof")
			}
			if (mode == "late" || mode == "clock" || mode == "yield") && !errors.Is(err, computer.ErrLimit) {
				t.Fatal("clock failure lost", err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, ok := f.s.FetchObservation(); ok {
				t.Fatal("failed observation published")
			}
			calls, reads, writes, opens := f.cpu.calls, f.cpu.reads, f.cpu.writes, f.files.opens
			if _, err = f.s.Launch(context.Background(), 1, req); !errors.Is(err, ErrProgramRetired) {
				t.Fatal(err)
			}
			if _, err = f.s.Return(context.Background(), session); !errors.Is(err, ErrProgramRetired) {
				t.Fatal(err)
			}
			if calls != f.cpu.calls || reads != f.cpu.reads || writes != f.cpu.writes || opens != f.files.opens {
				t.Fatal("failed launch replayed IO")
			}
		})
	}
}

func TestProgramSerialEmitsOneFetchObservationBeforeRunningState(t *testing.T) {
	f := serviceTest(t)
	f.launch(t, 0)
	var wire strings.Builder
	p, err := NewProgramSerialPlatform(f.s, func() (byte, bool) { return 0, false }, func(_ context.Context, b []byte) error { _, err := wire.Write(b); return err })
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = p.Present(context.Background(), tenfoot.Snapshot{}); err != nil {
			t.Fatal(err)
		}
	}
	text := wire.String()
	if strings.Count(text, "FES_NATIVE_CORE_FETCH ") != 1 || strings.Index(text, "FES_NATIVE_CORE_FETCH ") > strings.Index(text, "FES_NATIVE_TENFOOT_STATE ") {
		t.Fatal(fmt.Sprintf("unexpected observation order: %s", text))
	}
}
