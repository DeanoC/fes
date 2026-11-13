// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestChannelFaultSamplesReadOnlyStateWithoutConsumingQueuedData(t *testing.T) {
	c := &traceController{controller: newController()}
	c.regs[hcchar], c.regs[hcint], c.regs[hctsiz] = 0x805e8808, 0x82, 0x80008
	c.regs[hfnum], c.regs[hfir] = 0x3ad807d7, 59999
	c.regs[ptxstatus], c.regs[nptxstatus] = 0x40040, 0x100040
	c.rx, c.words = []uint32{2<<17 | 8<<4}, []uint32{0x44332211, 0x88776655}
	h := &Host{io: c, owned: true, rootSpeed: 2, speed: 2,
		pending: packet{active: true, in: true, limit: 8, deadline: c.tick.Add(time.Second)}}
	before := h.pending
	registers := make(map[uint32]uint32)
	for reg, value := range c.regs {
		registers[reg] = value
	}
	err := h.channelFault(0x80)
	want := []uint32{rxpeek, gint, gahb, gintmask, hcfg, hcchar, hcint, hctsiz, hcsplit, hprt, reset, nptxstatus, ptxstatus, hfnum, hfir}
	if len(c.accessesLog) != len(want) || c.clocks != 0 {
		t.Fatal("unexpected diagnostic access or clock", c.accessesLog, c.clocks)
	}
	for i, access := range c.accessesLog {
		if access.write || access.reg != want[i] {
			t.Fatal("diagnostic acknowledged, popped or rewrote hardware", c.accessesLog)
		}
	}
	for _, field := range []string{"channel error 0x80", "channel_snapshot={", "channel=0x805e8808", "interrupts=0x82", "transfer=0x80008", "periodic=0x40040", "frame=0x3ad807d7", "interval=0xea5f", "root_speed=2 device_speed=2 hub=0 port_number=0"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("lost original error or later observation %q: %v", field, err)
		}
	}
	if !reflect.DeepEqual(h.pending, before) || !reflect.DeepEqual(c.regs, registers) || len(c.rx) != 1 || len(c.words) != 2 || h.channelCommands.count != 0 || h.fault != nil {
		t.Fatal("diagnostic consumed data, changed packet or claimed containment")
	}
}

func TestFatalChannelObservationSurvivesFailedHaltAndPermanentRetirement(t *testing.T) {
	for _, interrupts := range []uint32{0x80, 0x200} {
		t.Run(fmt.Sprintf("%x", interrupts), func(t *testing.T) {
			c := newController()
			h := open(t, c)
			c.mode = "halt"
			c.regs[hcchar], c.regs[hcint], c.regs[hctsiz] = 0x805e8808, interrupts, 0x80008
			c.regs[ptxstatus] = 0x40 // No periodic request-queue space.
			h.pending = packet{active: true, in: true, limit: 8, deadline: c.tick.Add(time.Second)}
			deadline, toggle := h.pending.deadline, h.toggle
			commands := h.channelCommands.count
			data, err := h.pollInput()
			if err == nil || data != nil || !strings.Contains(err.Error(), fmt.Sprintf("channel error %#x; channel_snapshot={", interrupts)) || !strings.Contains(err.Error(), "periodic=0x40") || !strings.Contains(err.Error(), "containment: dwc2: register wait exhausted") {
				t.Fatal("lost first error, queue state or failed halt", data, err)
			}
			if h.fault != err || h.pending.deadline != deadline || h.toggle != toggle || h.pending.done || h.pending.received || h.channelCommands.count != commands+1 || h.channelCommands.entries[commands%uint64(len(h.channelCommands.entries))].reason != "containment" {
				t.Fatal("diagnostic retried, released data or altered retirement", h.pending, h.channelCommands)
			}
			accesses := c.accesses
			c.regs[hcint], c.regs[ptxstatus] = 0, 0x100040
			if _, again := h.PollKeyboard(); again != err || c.accesses != accesses {
				t.Fatal("retired host resampled or resumed after state changed")
			}
		})
	}
}
