// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProgramPostCompletionRejectionRetainsOriginalCauseAndBlocksReuse(t *testing.T) {
	for _, mode := range []string{"before-deadline", "at-deadline", "late", "backwards", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			f := serviceTest(t)
			req, _, _ := addProgram(t, f.loader, f.files, 0)
			start := time.Unix(100, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wrapped := false
			f.s.state.limits.Now = func() time.Time {
				transfer := f.s.state.transfer
				if mode == "canceled" && transfer != nil && transfer.phase == "retire" && !wrapped {
					release := transfer.region.Release
					transfer.region.Release = func() error { err := release(); cancel(); return err }
					wrapped = true
				}
				if transfer != nil && transfer.done {
					switch mode {
					case "before-deadline":
						return start.Add(sourceOperationTimeout - time.Nanosecond)
					case "at-deadline":
						return start.Add(sourceOperationTimeout)
					case "late":
						return start.Add(sourceOperationTimeout + time.Microsecond)
					case "backwards":
						return start.Add(-time.Second)
					}
				}
				return start
			}
			_, err := f.s.Launch(ctx, 0, req)
			if mode == "before-deadline" {
				if err != nil || f.cpu.opens != 1 || f.s.Status().State != ProgramRunning {
					t.Fatal(err, f.s.Status())
				}
				return
			}
			transfer := f.s.state.transfer
			status := transfer.Status()
			if !errors.Is(err, ErrProgramRetired) || !status.Done || !status.Failed || f.cpu.opens != 0 || f.lease.r.Active || f.s.Status().State != ProgramRebootRequired {
				t.Fatal("late completion escaped ownership retirement", err, status)
			}
			if strings.Contains(err.Error(), "configuration already completed") {
				t.Fatal("masked original failure", err)
			}
			switch mode {
			case "at-deadline", "late":
				if !strings.Contains(err.Error(), "configuration deadline: elapsed=25s") && !strings.Contains(err.Error(), "configuration deadline: elapsed=25.000001s") {
					t.Fatal(err)
				}
			case "backwards":
				if !strings.Contains(err.Error(), "configuration backwards clock") {
					t.Fatal(err)
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
			if !strings.Contains(err.Error(), "phase=retire->complete completed=true") {
				t.Fatal("missing terminal boundary", err)
			}
			calls := f.cpu.calls
			if done, _, later := transfer.Advance(); done || later == nil || f.cpu.calls != calls {
				t.Fatal("rejected completion advanced/replayed", done, later)
			}
			if _, later := f.s.Launch(context.Background(), 1, req); !errors.Is(later, ErrProgramRetired) || f.cpu.calls != calls {
				t.Fatal("rejected service relaunched", later)
			}
			if _, err := f.loader.OpenComputer(context.Background(), transfer, f.s.state.limits); err == nil || f.cpu.calls != calls {
				t.Fatal("late completed binding opened control directly", err)
			}
			// Every control/source successor entry requires this same transfer.err==nil.
			if transfer.err == nil {
				t.Fatal("completed binding stayed reusable")
			}
		})
	}
}

func TestWindowPostCompletionDeadlineIsNotMasked(t *testing.T) {
	r := windowServiceTest(t)
	if _, err := r.s.Return(context.Background(), r.h); err != nil {
		t.Fatal(err)
	}
	start := time.Unix(100, 0)
	r.s.state.limits.Now = func() time.Time {
		if transfer := r.s.state.transfer; transfer != nil && transfer.done {
			return start.Add(sourceOperationTimeout)
		}
		return start
	}
	_, err := r.s.Launch(context.Background(), r.s.Status().Generation, r.request, r.f.frame)
	if !errors.Is(err, ErrWindowServiceRetired) || !strings.Contains(err.Error(), "configuration deadline: elapsed=25s") || strings.Contains(err.Error(), "already completed") || !r.s.state.transfer.Status().Failed || r.s.Status().State != ProgramRebootRequired {
		t.Fatal(err, r.s.Status())
	}
}

func TestConfigurationPhaseTimingUsesOnlyExistingBoundarySamples(t *testing.T) {
	now := time.Unix(100, 0)
	reads := 0
	c := newConfigurationClock(func() time.Time { reads++; return now }, "test")
	transfer := &ConfigurationTransfer{phase: "start"}
	if err := c.check(context.Background(), transfer, "start", false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Second)
	transfer.phase = "write"
	if err := c.check(context.Background(), transfer, "start", true); err != nil {
		t.Fatal(err)
	}
	if reads != 3 || c.phases[0] != 3*time.Second {
		t.Fatal("added clock samples or lost phase", reads, c.phases)
	}
	if err := c.check(context.Background(), transfer, "write", false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(22 * time.Second)
	transfer.phase = "complete"
	transfer.done = true
	err := c.check(context.Background(), transfer, "write", true)
	if err == nil || reads != 5 || !strings.Contains(err.Error(), "phase_times={start=3s write=22s") {
		t.Fatal(err, reads)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = c.check(ctx, transfer, "write", true); !errors.Is(err, context.Canceled) || reads != 5 {
		t.Fatal("canceled check read a new clock", err, reads)
	}
}
