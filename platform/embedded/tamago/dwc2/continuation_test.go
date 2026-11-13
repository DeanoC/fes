// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"testing"
	"time"
)

type continuationIO struct {
	*controller
	writes []uint32
}

func TestPeriodicSinglePacketCannotContinueAnEnabledChannel(t *testing.T) {
	h, c, _ := pendingIN(t)
	io := &continuationIO{controller: c}
	h.io = io
	h.pending.char |= 3 << 18
	c.regs[hcchar] = h.pending.char | channelEnable
	h.interval = 10 * time.Millisecond
	h.pending.requestAfter = c.tick.Add(h.interval)
	deadline := h.pending.deadline
	for n := 0; n < 20; n++ {
		if err := h.continueIN(); err != nil {
			t.Fatal(err)
		}
	}
	if len(io.writes) != 0 {
		t.Fatal("periodic request queued early")
	}
	c.tick = c.tick.Add(h.interval)
	if err := h.continueIN(); err != nil || len(io.writes) != 0 {
		t.Fatal(err, io.writes)
	}
	for n := 0; n < 20; n++ {
		if err := h.continueIN(); err != nil {
			t.Fatal(err)
		}
	}
	if len(io.writes) != 0 || h.pending.deadline != deadline {
		t.Fatal("periodic continuation repeated early or extended deadline")
	}
}

func (c *continuationIO) Write32(reg, value uint32) {
	c.writes = append(c.writes, reg)
	c.controller.Write32(reg, value)
}
func TestContinuationUsesCorrectQueueAndNeverReinitializesPacket(t *testing.T) {
	for _, periodic := range []bool{false, true} {
		for _, frame := range []uint32{0, 1, 65535} {
			h, c, _ := pendingIN(t)
			io := &continuationIO{controller: c}
			h.io = io
			queue := uint32(nptxstatus)
			if periodic {
				queue = ptxstatus
				h.pending.char |= 3 << 18
				c.regs[hcchar] = h.pending.char | channelEnable
			}
			c.regs[hfnum] = frame
			c.regs[queue] = 64
			before := h.pending
			for n := 0; n < 20; n++ {
				if err := h.continueIN(); err != nil {
					t.Fatal(err)
				}
			}
			if len(io.writes) != 0 || h.pending.deadline != before.deadline || h.pending.continuations != 0 {
				t.Fatal("full queue changed transfer")
			}
			c.regs[queue] = 1<<16 | 64
			if err := h.continueIN(); err != nil {
				t.Fatal(err)
			}
			if periodic {
				if len(io.writes) != 0 || h.pending.continuations != 0 || h.pending.deadline != before.deadline {
					t.Fatal("single periodic request continued", io.writes)
				}
				continue
			}
			if len(io.writes) != 1 || io.writes[0] != hcchar || h.pending.pid != before.pid ||
				h.pending.deadline != before.deadline || h.pending.limit != before.limit || h.pending.continuations != 1 ||
				c.regs[hctsiz] != 8|1<<19|2<<29 {
				t.Fatal("continuation rewrote packet state", io.writes)
			}
		}
	}
}
func TestContinuationCannotRestartCompletedOrAbortedTransfers(t *testing.T) {
	for _, mode := range []string{"inactive", "OUT", "split", "received", "done", "NAK", "halting", "new-RX", "new-status", "zero-count", "bad-count", "disabled", "disable-pending"} {
		t.Run(mode, func(t *testing.T) {
			h, c, _ := pendingIN(t)
			io := &continuationIO{controller: c}
			h.io = io
			wantError := false
			switch mode {
			case "inactive":
				h.pending.active = false
			case "OUT":
				h.pending.in = false
			case "split":
				h.pending.split = true
			case "received":
				h.pending.received = true
			case "done":
				h.pending.done = true
			case "NAK":
				h.pending.nak = true
			case "halting":
				h.pending.halting = true
			case "new-RX":
				c.rx = []uint32{3 << 17}
			case "new-status":
				c.regs[hcint] = 1
			case "zero-count":
				c.regs[hctsiz] = 0
			case "bad-count":
				c.regs[hctsiz] = 2 << 19
				wantError = true
			case "disabled":
				c.regs[hcchar] &^= channelEnable
			case "disable-pending":
				c.regs[hcchar] |= channelDisable
			}
			deadline, pid := h.pending.deadline, h.pending.pid
			err := h.continueIN()
			if (err != nil) != wantError || len(io.writes) != 0 || h.pending.deadline != deadline || h.pending.pid != pid {
				t.Fatal(err, io.writes)
			}
			wantReason, wantSamples := "ineligible", uint32(0)
			switch mode {
			case "new-RX":
				wantReason, wantSamples = "RX", 3
			case "new-status":
				wantReason, wantSamples = "status", 7
			case "zero-count":
				wantReason, wantSamples = "completion", 31
			case "bad-count":
				wantReason, wantSamples = "count", 31
			case "disabled", "disable-pending":
				wantReason, wantSamples = "channel", 15
			}
			if h.pending.lastContinue.reason != wantReason || h.pending.lastContinue.samples != wantSamples {
				t.Fatal("incorrect sampled continuation evidence", h.pending.lastContinue)
			}
		})
	}
}

type completingIN struct {
	*controller
	race, deliver bool
}

func (c *completingIN) Read32(reg uint32) uint32 {
	if reg == hcchar && c.race {
		c.race = false
		c.deliver = true
		c.regs[hcchar] = 0
		c.regs[hctsiz] = 0
	}
	if reg == gint && c.deliver {
		c.deliver = false
		c.active.pid = 2
		c.complete([]byte{0, 0, 4, 0, 0, 0, 0, 0}, true)
	}
	return c.controller.Read32(reg)
}
func TestCompletionRacingContinuationCannotRestartPacketOrLosePayload(t *testing.T) {
	h, c, _ := pendingIN(t)
	io := &completingIN{controller: c, race: true}
	h.io = io
	starts := len(c.starts)
	if done, data, nak, err := h.step(); err != nil || done || nak || len(data) != 0 {
		t.Fatal(done, data, nak, err)
	}
	done, data, nak, err := h.step()
	if err != nil || !done || nak || len(data) != 8 || data[2] != 4 || len(c.starts) != starts || h.pending.continuations != 0 {
		t.Fatal(done, data, nak, err)
	}
}
