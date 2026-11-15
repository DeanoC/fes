// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	g "github.com/DeanoC/fes/platform/embedded/tamago/embeddedcontrol/generated"
)

// Inject cost only into live control-scope ownership observations. This drives
// the actual service, computer driver and per-CSR firmware bridge brackets; it
// does not estimate hardware duration or replace the FPGA/firmware transport.
type costedComputerOwnership struct {
	CPUTransport
	now          *time.Time
	controlOpen  func() bool
	observations int
}

func (c *costedComputerOwnership) Invoke(a [7]uint64) ([4]uint64, error) {
	if a[0] == uint64(g.FesEmbeddedBridgeCall) && a[1] == uint64(g.FesEmbeddedBridgeSnapshot) && c.controlOpen() {
		*c.now = c.now.Add(25 * time.Millisecond)
		c.observations++
	}
	return c.CPUTransport.Invoke(a)
}

func TestProgramServiceSlowComputerOwnershipCannotPublishOrReplay(t *testing.T) {
	f := serviceTest(t)
	req, _, _ := addProgram(t, f.loader, f.files, 0)
	now := time.Unix(100, 0)
	cost := &costedComputerOwnership{CPUTransport: f.loader.boot.cpu.(CPUTransport), now: &now, controlOpen: func() bool { return f.cpu.state.ResetMask == 13 }}
	f.loader.boot.cpu = cost
	f.s.state.limits.Now = func() time.Time { return now }
	_, err := f.s.Launch(context.Background(), 0, req)
	if !errors.Is(err, ErrProgramRetired) || !errors.Is(err, computer.ErrLimit) {
		t.Fatal("slow identity published", err)
	}
	for _, field := range []string{"reason=deadline", "operation=open", "phase=after-read", "elapsed=1s", "limit=1s", "polls=1/4", "reads=19 writes=1"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("missing %q: %v", field, err)
		}
	}
	if cost.observations != 40 || f.cpu.opens != 1 || f.cpu.closes != 1 || f.lease.r.Active || f.lease.returns != 0 || f.s.Status().State != ProgramRebootRequired {
		t.Fatal("uncertain control not contained once", cost.observations, f.cpu.opens, f.cpu.closes, f.s.Status())
	}
	if f.s.state.plan == nil || f.s.state.transfer == nil || !f.s.state.transfer.done || f.s.state.computer != nil {
		t.Fatal("completed transfer or failed opening lost", f.s.Status())
	}
	calls, writes, opens := f.cpu.calls, f.cpu.writes, f.files.opens
	if _, err := f.s.Launch(context.Background(), 1, req); !errors.Is(err, ErrProgramRetired) {
		t.Fatal(err)
	}
	if _, err := f.s.Return(context.Background(), ProgramSession{}); !errors.Is(err, ErrProgramRetired) {
		t.Fatal(err)
	}
	if calls != f.cpu.calls || writes != f.cpu.writes || opens != f.files.opens {
		t.Fatal("retired service retried hardware or storage")
	}
}
