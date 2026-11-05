// SPDX-License-Identifier: GPL-3.0-or-later
package computer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	g "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
)

func TestLimitDiagnosticsDistinguishSlowReadWriteClockAndACK(t *testing.T) {
	for _, mode := range []string{"open-read", "write-deadline", "backwards-clock", "ack-polls"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Unix(100, 0)
			var err error
			var f *faultIO
			var c *Computer
			var fields []string
			if mode == "open-read" {
				f = fixture(wanted())
				l := limits()
				l.Now = func() time.Time { return now }
				f.readHook = func(uint32) (uint32, error, bool) { now = now.Add(time.Second); return 0, nil, false }
				c, err = Open(context.Background(), f, l, wanted())
				if c != nil || len(f.writes) != 0 || f.reads != 1 {
					t.Fatal("late adapter read published or continued", c, f)
				}
				fields = []string{"reason=deadline", "operation=open", "phase=after-read", "elapsed=1s", "step=1s", "offset=0x0", "polls=0/4", "reads=1 writes=0"}
			} else {
				c, f = opened(t)
				c.limits.Now = func() time.Time { return now }
				before := len(f.writes)
				switch mode {
				case "write-deadline":
					f.writeHook = func(uint32) error { now = now.Add(time.Second); return nil }
					fields = []string{"reason=deadline", "phase=after-write", "elapsed=1s", "step=1s", "offset=0xc", "polls=0/4", "reads=8 writes=1"}
				case "backwards-clock":
					f.writeHook = func(uint32) error { now = now.Add(-time.Nanosecond); return nil }
					fields = []string{"reason=backwards-clock", "phase=after-write", "elapsed=-1ns", "step=-1ns", "reads=8 writes=1"}
				case "ack-polls":
					f.writeHook = func(word uint32) error {
						f.gpo = word
						f.status = g.FesDe25ComputerStatusReady | g.FesDe25ComputerStatusBusy
						return nil
					}
					fields = []string{"reason=ack-polls", "phase=ack-polls", "elapsed=0s", "step=0s", "polls=4/4", "reads=40 writes=1"}
				}
				err = c.Run(context.Background())
				if len(f.writes) != before+1 {
					t.Fatal("uncertain command replayed", len(f.writes)-before)
				}
				reads := f.reads
				if next := c.Hold(context.Background()); !errors.Is(next, ErrRetired) || f.reads != reads || len(f.writes) != before+1 {
					t.Fatal("retired command touched IO", next)
				}
				fields = append(fields, "operation=execution", fmt.Sprintf("opcode=%d", g.FesComputerOpcodeExecution), "index=0")
			}
			if !errors.Is(err, ErrLimit) {
				t.Fatal("limit identity lost", err)
			}
			for _, field := range fields {
				if !strings.Contains(err.Error(), field) {
					t.Fatalf("missing %q: %v", field, err)
				}
			}
		})
	}
}

type timedIO struct {
	IO
	timing  AccessTiming
	samples int
}

func (t *timedIO) LastAccessTiming() AccessTiming { t.samples++; return t.timing }

func TestLimitIncludesSavedAccessTimingWithoutChangingAcceptance(t *testing.T) {
	c, f := opened(t)
	now := time.Unix(100, 0)
	c.limits.Now = func() time.Time { return now }
	io := &timedIO{IO: f, timing: AccessTiming{Offset: g.FesDe25ComputerStatusOffset, Before: 2 * time.Millisecond, Register: 3 * time.Millisecond, After: 5 * time.Millisecond, Completed: true}}
	c.io = io
	// Successful operations do not request diagnostic data.
	if err := c.Run(context.Background()); err != nil || io.samples != 0 {
		t.Fatal(err, io.samples)
	}
	f.readHook = func(uint32) (uint32, error, bool) { now = now.Add(time.Second); return 0, nil, false }
	before := f.reads
	err := c.Hold(context.Background())
	if !errors.Is(err, ErrLimit) || io.samples != 1 || f.reads != before+1 {
		t.Fatal(err, io.samples, f.reads-before)
	}
	for _, field := range []string{"ownership_before=2ms", "register_io=3ms", "ownership_after=5ms", "access_complete=true", "access_offset=0x8", "access_write=false"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("missing %s: %v", field, err)
		}
	}
	if err := c.Run(context.Background()); !errors.Is(err, ErrRetired) || io.samples != 1 || f.reads != before+1 {
		t.Fatal("diagnostic revived failed operation", err)
	}
}
