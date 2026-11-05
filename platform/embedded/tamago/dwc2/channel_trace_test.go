// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Replay the values captured on TRWU8CBL before ROM configuration. Injecting
// CHDIS models the observation, not its cause: the trace must distinguish it
// from an actual driver halt request without clearing it or guessing a PID.
func TestCapturedPeriodicToggleAndDisableCannotAdmitPauseOrInput(t *testing.T) {
	for _, lostDMA := range []bool{false, true} {
		t.Run(map[bool]string{false: "stuck-halt", true: "foreign-DMA"}[lostDMA], func(t *testing.T) {
			c := newController()
			c.address, c.mode = 1, "stuck"
			c.regs[hprt], c.regs[gint] = 0x4180f, 0x5200029
			c.regs[reset], c.regs[ptxstatus] = 0x80000400, 0x100040
			c.regs[nptxstatus], c.regs[hfnum] = 0x1080040, 0x38d1024c
			h := &Host{io: c, owned: true, address: 1, speed: 2, rootSpeed: 2, interval: 10 * time.Millisecond}
			if err := h.begin(1, 8, 3, true, 0, nil, 8, c.Now().Add(500*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if c.regs[hcchar] != 0xa05e8808 || c.regs[hctsiz] != 0x80008 {
				t.Fatal("incorrect packet setup", c.regs[hcchar], c.regs[hctsiz])
			}
			before, deadline := h.pending, h.pending.deadline
			c.mode = "halt" // A halt command will not clear CHENA in this replay.
			c.regs[hcchar], c.regs[hcint] = 0xe05e8808, 0x400
			c.rx = []uint32{0xb0000}
			c.tick = c.tick.Add(h.interval)
			done, data, nak, err := h.step()
			if err != nil || done || nak || len(data) != 0 || h.pending.lastContinue.reason != "channel" ||
				h.pending.lastContinue.channel != 0xe05e8808 || h.pending.continuations != 0 ||
				h.pending.toggleRX != 1 || h.pending.toggleIRQs != 1 || h.pending.halting {
				t.Fatal("captured state was restarted or accepted", done, data, nak, err, h.pending)
			}
			if h.channelCommands.count != 1 || h.channelCommands.entries[0].reason != "arm" ||
				h.channelCommands.entries[0].value&channelDisable != 0 {
				t.Fatal("trace invented a software disable request", h.channelCommands)
			}
			first := h.pending.firstToggle.Error()
			if !strings.Contains(first, "software_channel_commands={seq=1 packet=1 reason=arm value=0xa05e8808") {
				t.Fatal("first observation lost its command history", first)
			}
			if lostDMA {
				c.regs[gahb] |= 1 << 5
			}
			err = h.PausePolling(context.Background())
			if err == nil || h.fault == nil || h.pollingPaused || h.pending.received ||
				h.pending.done || h.pending.nak || !h.pending.active || h.pending.pid != before.pid ||
				h.pending.deadline != deadline || h.toggle != 0 || c.regs[hctsiz] != 0x80008 || len(c.starts) != 1 {
				t.Fatal("failed packet changed PID/deadline or escaped", err, h.pending)
			}
			if lostDMA {
				if h.channelCommands.count != 1 || !strings.Contains(err.Error(), "PIO/interrupt ownership changed") {
					t.Fatal("foreign DMA caused channel access", err, h.channelCommands)
				}
			} else if h.channelCommands.count != 2 || h.channelCommands.entries[1].reason != "containment" ||
				h.channelCommands.entries[1].value != 0xe05e8808 ||
				!strings.Contains(err.Error(), "packet timeout") || !strings.Contains(err.Error(), "containment:") ||
				!strings.Contains(err.Error(), "reason=arm value=0xa05e8808") ||
				!strings.Contains(err.Error(), "reason=containment value=0xe05e8808") {
				t.Fatal("lost first failure or containment command", err, h.channelCommands)
			}
			accesses := c.accesses
			if _, err := h.pollInput(); err == nil {
				t.Fatal("retired host polled")
			}
			if err := h.ResumePolling(context.Background()); err == nil || c.accesses != accesses {
				t.Fatal("retired host touched hardware or resumed", err)
			}
			if h.pending.firstToggle.Error() != first {
				t.Fatal("containment changed the cached first observation")
			}
		})
	}
}

func TestChannelCommandsDistinguishNAKRetirementFromNextPacketArm(t *testing.T) {
	c := newController()
	c.address, c.mode = 1, "nak"
	c.regs[hprt] = 0x41005
	h := &Host{io: c, owned: true, address: 1, speed: 2, rootSpeed: 2, interval: 10 * time.Millisecond}
	if err := h.begin(1, 8, 3, true, 2, nil, 8, c.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, _, _, err := h.step(); err != nil {
			t.Fatal(err)
		}
	}
	if h.pending.active || !h.pending.nak {
		t.Fatal("NAK was not retired")
	}
	c.mode = "stuck"
	if err := h.begin(1, 8, 3, true, 2, nil, 8, c.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	trace := h.channelCommands
	if trace.count != 3 || trace.entries[0].reason != "arm" || trace.entries[0].packet != 1 ||
		trace.entries[1].reason != "retire" || trace.entries[1].packet != 1 || !trace.entries[1].nak ||
		trace.entries[1].value&(channelEnable|channelDisable) != channelEnable|channelDisable ||
		trace.entries[2].reason != "arm" || trace.entries[2].packet != 2 || trace.entries[2].nak ||
		trace.entries[2].value&channelDisable != 0 {
		t.Fatal("lost prior packet's disable or attributed it to the next packet", trace)
	}
}

func TestChannelCommandHistoryRetainsPreviousPacketAndAddsNoReads(t *testing.T) {
	c := newController()
	h := &Host{io: c}
	c.mode = "stuck"
	for n := uint64(1); n <= 12; n++ {
		h.packetSequence = n
		h.pending = packet{pid: uint32(n & 2), done: n&1 != 0}
		before := c.accesses
		h.writeChannel("arm", uint32(n)) // CHENA is clear, so no modeled transfer.
		if c.accesses != before+1 || c.regs[hcchar] != uint32(n) {
			t.Fatal("recording added I/O")
		}
	}
	before := c.accesses
	s := h.channelCommands.String()
	if strings.Contains(s, "seq=4 ") || strings.Count(s, "reason=arm") != 8 ||
		!strings.HasPrefix(s, "seq=5 packet=5 ") || !strings.Contains(s, "seq=12 packet=12 ") || c.accesses != before {
		t.Fatal("history lost ordering, exceeded bound or read hardware", s)
	}
}
