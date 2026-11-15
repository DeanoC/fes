// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Count and order the real register interface. Diagnostic formatting must not
// consume RX, resample the controller or turn an observation into a command.
type traceAccess struct {
	reg, value uint32
	write      bool
}
type traceController struct {
	*controller
	accessesLog []traceAccess
	clocks      int
}

func (c *traceController) Read32(reg uint32) uint32 {
	v := c.controller.Read32(reg)
	c.accessesLog = append(c.accessesLog, traceAccess{reg: reg, value: v})
	return v
}
func (c *traceController) Write32(reg, value uint32) {
	c.accessesLog = append(c.accessesLog, traceAccess{reg: reg, value: value, write: true})
	c.controller.Write32(reg, value)
}
func (c *traceController) Now() time.Time { c.clocks++; return c.controller.Now() }

func TestPeriodicTraceBracketsOneUnchangedCommandWithReadOnlySamples(t *testing.T) {
	c := &traceController{controller: newController()}
	c.mode, c.address = "stuck", 1
	h := &Host{io: c, owned: true, packetSequence: 9, pending: packet{pid: 2, active: true, in: true, limit: 8, deadline: c.tick.Add(time.Second)}}
	c.regs[hctsiz] = 8 | 1<<19 | 2<<29
	c.regs[hfnum] = 0x1234ffff
	selectedAt := c.tick
	const value = uint32(8|1<<11|3<<18|1<<15|1<<17|1<<20|1<<22) | channelEnable
	before := h.pending
	h.writePeriodicChannel("continue", value, 0x5678ffff, selectedAt)
	want := []traceAccess{{reg: hfnum, value: 0x1234ffff}, {reg: hcchar, value: value, write: true}, {reg: hfnum, value: 0x1234ffff}}
	if !reflect.DeepEqual(c.accessesLog, want) || c.clocks != 2 || h.channelCommands.count != 1 || !reflect.DeepEqual(h.pending, before) {
		t.Fatal("observations added commands, destructive reads or packet changes", c.accessesLog, c.clocks, h.pending)
	}
	p := h.channelCommands.entries[0].periodic
	if !p.sampled || p.selected != 0x5678ffff || p.before != 0x1234ffff || p.after != 0x1234ffff || p.selectionToPreNS != 100000 || p.windowNS != 100000 {
		t.Fatal("lost sample identity or clock window", p)
	}
	log := append([]traceAccess(nil), c.accessesLog...)
	clocks := c.clocks
	s := h.channelCommands.String()
	if !strings.Contains(s, "periodic_timing={sampled=true selected=0x5678ffff before=0x1234ffff after=0x1234ffff select_to_pre_ns=100000 window_ns=100000}") || !reflect.DeepEqual(log, c.accessesLog) || c.clocks != clocks {
		t.Fatal("formatting changed samples or accessed hardware", s)
	}
}

func TestPeriodicTraceObservesDelayedCommitAndFrameWrapWithoutRepair(t *testing.T) {
	c := newPeriodicController()
	c.frameBase = 0xffff
	c.elapsed = 100 * time.Microsecond
	c.channelDelay = 1800 * time.Microsecond
	c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
	h := periodicHost(t, c)
	if data, err := h.pollInput(); err != nil || data != nil {
		t.Fatal(data, err)
	}
	deadline, pid := h.pending.deadline, h.pending.pid
	command := h.channelCommands.entries[0]
	p := command.periodic
	if !p.sampled || uint16(p.selected) != 0xffff || uint16(p.before) != 0xffff || uint16(p.after) != 0 || p.windowNS < int64(c.channelDelay) || command.value&(1<<29) != 0 || h.pending.received || h.pending.done || h.toggle != 2 {
		t.Fatal("trace lost delayed commit, changed parity or fabricated data", command, h.pending)
	}
	c.advance(300 * time.Microsecond)
	data, err := h.pollInput()
	if err == nil || data != nil || !strings.Contains(err.Error(), "channel error 0x200") || h.fault == nil || h.pending.deadline != deadline || h.pending.pid != pid || h.toggle != 2 || h.pending.received {
		t.Fatal("observed overrun repaired or admitted", data, err, h.pending)
	}
	accesses, writes := c.accesses, len(c.writes)
	history := h.channelCommands
	if _, err := h.pollInput(); err == nil || accesses != c.accesses || writes != len(c.writes) || !reflect.DeepEqual(history, h.channelCommands) {
		t.Fatal("failed trace was resampled or retired host accessed hardware")
	}
}

func TestPacketServiceTimingReusesDeadlineClockAndPreservesTimeout(t *testing.T) {
	c := &traceController{controller: newController()}
	c.address, c.mode = 1, "stuck"
	c.regs[hprt] = 0x4180f
	h := &Host{io: c, owned: true, address: 1, speed: 2, rootSpeed: 2}
	deadline := c.tick.Add(500 * time.Millisecond)
	if err := h.begin(1, 8, 0, true, 2, nil, 8, deadline); err != nil {
		t.Fatal(err)
	}
	if c.clocks != 0 {
		t.Fatal("nonperiodic arm added clock samples")
	}
	if _, _, _, err := h.step(); err != nil {
		t.Fatal(err)
	}
	if c.clocks != 1 || h.pending.stepGapNS != 0 || h.pending.maxStepGapNS != 0 {
		t.Fatal("first step invented a gap", c.clocks, h.pending)
	}
	c.tick = c.tick.Add(12 * time.Millisecond)
	if _, _, _, err := h.step(); err != nil {
		t.Fatal(err)
	}
	if c.clocks != 2 || h.pending.stepGapNS != int64(12100*time.Microsecond) || h.pending.maxStepGapNS != h.pending.stepGapNS {
		t.Fatal("service observation added clocks or lost gap", c.clocks, h.pending)
	}
	c.tick = deadline.Add(time.Millisecond)
	_, _, _, err := h.step()
	if err == nil || !strings.Contains(err.Error(), "packet timeout") || !strings.Contains(err.Error(), "service={last_gap_ns=") || c.clocks != 3 || h.pending.deadline != deadline || h.pending.pid != 2 || h.pending.done || h.pending.received {
		t.Fatal("timing trace changed timeout or packet identity", err, c.clocks, h.pending)
	}
}
