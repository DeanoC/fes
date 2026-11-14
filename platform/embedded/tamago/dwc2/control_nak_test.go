// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"strings"
	"testing"
)

// A slave-mode control IN can have several tokens queued when the device
// first returns NAK. Hardware status is independent of the driver's poll:
// accepted data/completion can become visible before or during retirement.
func TestControlINNAKWaitsForRealCompletion(t *testing.T) {
	for _, order := range []string{"before-data", "with-data", "during-retirement"} {
		t.Run(order, func(t *testing.T) {
			h, c, _ := pendingIN(t)
			deadline, pid, size := h.pending.deadline, h.pending.pid, c.regs[hctsiz]
			starts := len(c.starts)
			if order == "before-data" {
				c.regs[hcint] = 1 << 4
				done, data, nak, err := h.step()
				if err != nil || done || nak || len(data) != 0 || h.pending.nak || h.pending.halting ||
					c.regs[hcint] != 0 || c.regs[hcchar]&channelEnable == 0 || h.pending.continuations != 1 {
					t.Fatal("NAK completed or retired control IN", done, data, nak, err)
				}
			}
			want := []byte{9, 2, 25, 0, 1, 1, 0, 0x80}
			c.active.pid = pid
			c.complete(want, true)
			if order == "with-data" {
				c.regs[hcint] |= 1 << 4
			}
			done, data, nak, err := h.step()
			if err != nil || done || nak || len(data) != 0 || !h.pending.halting {
				t.Fatal("data escaped before channel retirement", done, data, nak, err)
			}
			if order == "during-retirement" {
				c.regs[hcint] |= 1 << 4
			}
			done, data, nak, err = h.step()
			if err != nil || !done || nak || !bytes.Equal(data, want) || h.pending.active ||
				c.regs[hcchar]&(channelEnable|channelDisable) != 0 || h.pending.deadline != deadline || h.pending.nakIRQs != 1 ||
				h.pending.pid != pid || c.regs[hctsiz] != size || len(c.starts) != starts+int(h.pending.continuations) {
				t.Fatal("lost data or changed packet identity", done, data, nak, err)
			}
		})
	}
}

func TestControlINNAKCannotHideFaultsOrSupplyData(t *testing.T) {
	for _, mode := range []string{"deadline", "NAK-only", "RX-without-completion", "missing-data", "wrong-PID", "stall", "DMA", "disconnect", "unexplained-halt", "stuck-retirement"} {
		t.Run(mode, func(t *testing.T) {
			h, c, _ := pendingIN(t)
			deadline := h.pending.deadline
			c.regs[hcint] = 1 << 4
			var want string
			switch mode {
			case "deadline":
				c.tick = deadline
				want = "packet timeout"
			case "NAK-only", "RX-without-completion":
				if mode == "RX-without-completion" {
					c.active.pid = 2
					c.complete(make([]byte, 8), true)
					c.regs[hcint] = 1 << 4 // Payload without XFERCOMPL is insufficient.
				}
				for range 40 {
					c.regs[hcint] |= 1 << 4
					done, data, nak, err := h.step()
					if err != nil || done || nak || len(data) != 0 || h.pending.deadline != deadline || !h.pending.active {
						t.Fatal("notification supplied data/completion or extended deadline", done, data, nak, err)
					}
				}
				c.tick = deadline
				want = "packet timeout"
			case "missing-data":
				c.regs[hcint] |= 1
				c.regs[hcchar] = 0
				want = "completion without RX"
			case "wrong-PID":
				c.active.pid = 0
				c.complete(make([]byte, 8), true)
				c.regs[hcint] |= 1 << 4
				want = "invalid RX size/PID/ownership"
			case "stall":
				c.regs[hcint] |= 1 << 3
				want = "channel error"
			case "DMA":
				c.regs[gahb] |= 1 << 5
				want = "PIO/interrupt ownership"
			case "disconnect":
				c.regs[hprt] &^= 1
				want = "root link"
			case "unexplained-halt":
				c.regs[hcint] |= 2
				c.regs[hcchar] = 0
				want = "unexplained channel halt"
			case "stuck-retirement":
				c.active.pid = 2
				c.complete(make([]byte, 8), true)
				c.regs[hcint] |= 1 << 4
				c.mode = "halt"
				want = "packet timeout"
			}
			done, data, nak, err := h.step()
			if mode == "stuck-retirement" && err == nil {
				if done || len(data) != 0 || nak || !h.pending.active {
					t.Fatal("unretired data escaped")
				}
				c.tick = deadline
				done, data, nak, err = h.step()
			}
			if err == nil || !strings.Contains(err.Error(), want) || done || nak || len(data) != 0 || h.pending.deadline != deadline {
				t.Fatal("fault escaped", done, data, nak, err)
			}
		})
	}
}

func TestNAKStillRetiresPeriodicSplitAndOUTPackets(t *testing.T) {
	for _, mode := range []string{"periodic", "split", "OUT"} {
		t.Run(mode, func(t *testing.T) {
			h, c, _ := pendingIN(t)
			switch mode {
			case "periodic":
				h.pending.char |= 3 << 18
			case "split":
				h.pending.split = true
			case "OUT":
				h.pending.in = false
			}
			c.regs[hcint] = 1 << 4
			done, data, nak, err := h.step()
			if err != nil || done || nak || len(data) != 0 || !h.pending.nak || !h.pending.halting {
				t.Fatal(done, data, nak, err)
			}
			done, data, nak, err = h.step()
			if err != nil || !done || !nak || len(data) != 0 || h.pending.active {
				t.Fatal("NAK semantics changed", done, data, nak, err)
			}
		})
	}
}

func TestPeriodicAndSplitNAKWithDataStillRejects(t *testing.T) {
	for _, split := range []bool{false, true} {
		h, c, _ := pendingIN(t)
		if split {
			h.pending.split, h.pending.completeSplit = true, true
		} else {
			h.pending.char |= 3 << 18
		}
		c.active.pid = 2
		c.complete(make([]byte, 8), true)
		c.regs[hcint] |= 1 << 4
		done, data, nak, err := h.step()
		if err == nil || !strings.Contains(err.Error(), "contradictory completion") || done || nak || len(data) != 0 || h.pending.nakIRQs != 0 {
			t.Fatal("contradictory periodic/split result escaped", split, done, data, nak, err)
		}
	}
}
