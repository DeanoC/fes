// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

// Adapt paired toggle observations to a bulk IN fixture. Physical periodic
// values and halt-first behavior are covered in periodic_retire_test.go.
func nonperiodicToggleIN(t *testing.T) (*Host, *controller) {
	t.Helper()
	c := newController()
	c.address, c.mode, c.configured, c.protocol, c.reportPID = 1, "stuck", true, true, 2
	c.regs[hprt], c.regs[gint] = 0x4180f, 0x5200029
	c.regs[reset], c.regs[ptxstatus] = 0x80000400, 0x100040
	c.regs[nptxstatus], c.regs[hfnum] = 0x1080040, 0xc28c01e4
	h := &Host{io: c, owned: true, address: 1, speed: 2, rootSpeed: 2, interval: 10 * time.Millisecond, toggle: 2}
	if err := h.begin(1, 8, 2, true, 2, nil, 8, c.Now().Add(500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if c.regs[hcchar] != 0x805a8808 || c.regs[hctsiz] != 0x40080008 {
		t.Fatal("capture setup mismatch")
	}
	c.regs[hcchar], c.regs[hcint] = 0xc05a8808, 0x400
	c.rx = []uint32{0xa0000}
	return h, c
}

func observeNonperiodicToggle(h *Host, c *controller) error {
	saved := c.regs[nptxstatus]
	c.regs[nptxstatus] = 64
	_, _, _, err := h.step()
	c.regs[nptxstatus] = saved
	return err
}

func TestNonperiodicToggleDisableContinuesSamePacketUntilRealDataRetires(t *testing.T) {
	h, c := nonperiodicToggleIN(t)
	deadline, pid, size, packet := h.pending.deadline, h.pending.pid, c.regs[hctsiz], h.packetSequence
	want := []byte{0, 0, 4, 0, 0, 0, 0, 0}
	c.mode = ""
	c.reports = [][]byte{want}
	c.tick = c.tick.Add(h.interval)
	data, err := h.pollInput()
	if err != nil || data != nil || h.pending.continuations != 1 || h.pending.lastContinue.reason != "queued" || h.pending.received || h.pending.done {
		t.Fatal("captured disable was not continued without accepting data", data, err, h.pending)
	}
	if c.regs[hcchar]&channelDisable != 0 || h.pending.pid != pid || h.pending.deadline != deadline || c.regs[hctsiz] != size || h.packetSequence != packet || h.toggle != 2 {
		t.Fatal("continuation restarted, retimed or retoggled packet")
	}
	if h.channelCommands.count != 2 || h.channelCommands.entries[1].reason != "continue" || h.channelCommands.entries[1].packet != packet {
		t.Fatal("continuation was not owned by original packet", h.channelCommands)
	}
	data, err = h.pollInput()
	if err != nil || data != nil || !h.pending.received || !h.pending.done || !h.pending.active || !h.pending.halting {
		t.Fatal("report escaped before channel retirement", data, err, h.pending)
	}
	data, err = h.pollInput()
	if err != nil || !bytes.Equal(data, want) || h.pending.active || h.toggle != 0 || h.packetSequence != packet || h.pending.deadline != deadline || c.regs[hctsiz] != size {
		t.Fatal("real expected-PID data did not retire original packet", data, err, h.pending)
	}
}

func TestToggleDisableContinuationRetainsAllOwnershipAndCompletionGuards(t *testing.T) {
	modes := []string{"no-RX-toggle", "no-IRQ-toggle", "no-first-observation", "not-owned", "fault", "halting", "done", "received", "NAK", "OUT", "split", "disabled", "new-RX", "new-status", "queue-full", "spent-count", "bad-count", "PID-changed", "size-changed", "foreign-DMA", "foreign-interrupts", "foreign-descriptor-DMA", "disconnected", "unknown-PID", "different-channel", "unexpected-control-bit"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			h, c := nonperiodicToggleIN(t)
			// Process the real notification while withholding a request queue slot.
			if err := observeNonperiodicToggle(h, c); err != nil || h.pending.toggleRX != 1 || h.pending.toggleIRQs != 1 || h.pending.continuations != 0 {
				t.Fatal(err, h.pending)
			}
			c.tick = c.tick.Add(h.interval)
			wantError := false
			switch mode {
			case "no-RX-toggle":
				h.pending.toggleRX = 0
			case "no-IRQ-toggle":
				h.pending.toggleIRQs = 0
			case "no-first-observation":
				h.pending.firstToggle = nil
			case "not-owned":
				h.owned = false
			case "fault":
				h.fault = errors.New("already retired")
			case "halting":
				h.pending.halting = true
			case "done":
				h.pending.done = true
			case "received":
				h.pending.received = true
			case "NAK":
				h.pending.nak = true
			case "OUT":
				h.pending.in = false
			case "split":
				h.pending.split = true
			case "disabled":
				c.regs[hcchar] &^= channelEnable
			case "new-RX":
				c.rx = []uint32{3 << 17}
			case "new-status":
				c.regs[hcint] = 1
			case "queue-full":
				c.regs[nptxstatus] = 64
			case "spent-count":
				c.regs[hctsiz] = 0
			case "bad-count":
				c.regs[hctsiz] = 2 << 19
				wantError = true
			case "unknown-PID":
				h.pending.pid = 3
			case "different-channel":
				c.regs[hcchar] ^= 1 << 22
			case "unexpected-control-bit":
				c.regs[hctsiz] |= 1 << 31
			case "PID-changed":
				c.regs[hctsiz] ^= 2 << 29
			case "size-changed":
				c.regs[hctsiz]--
			case "foreign-DMA":
				c.regs[gahb] |= 1 << 5
				wantError = true
			case "foreign-interrupts":
				c.regs[gintmask] = 1
				wantError = true
			case "foreign-descriptor-DMA":
				c.regs[hcfg] |= 1 << 23
				wantError = true
			case "disconnected":
				c.regs[hprt] &^= 1
				wantError = true
			}
			commands, starts, pid, deadline, size := h.channelCommands.count, len(c.starts), h.pending.pid, h.pending.deadline, c.regs[hctsiz]
			err := h.continueIN()
			if (err != nil) != wantError || h.channelCommands.count != commands || len(c.starts) != starts || h.pending.pid != pid || h.pending.deadline != deadline || c.regs[hctsiz] != size || h.pending.continuations != 0 {
				t.Fatal("excluded condition queued or changed a packet", mode, err, h.pending, h.channelCommands)
			}
		})
	}
}

// A terminal event can arrive after the earlier status checks. The qualified
// CHDIS path must sample it before issuing a continuation, without swallowing it.
type eventDuringToggleAdmission struct {
	*controller
	event    string
	injected bool
}

func (c *eventDuringToggleAdmission) Read32(reg uint32) uint32 {
	value := c.controller.Read32(reg)
	if reg == hctsiz && !c.injected {
		c.injected = true
		if c.event == "RX" {
			c.complete([]byte{0, 0, 4, 0, 0, 0, 0, 0}, true)
		} else {
			c.regs[hcint] = 1 << 3
		}
	}
	return value
}
func TestToggleContinuationCannotPassNewCompletionOrChannelError(t *testing.T) {
	for _, event := range []string{"RX", "error"} {
		t.Run(event, func(t *testing.T) {
			h, c := nonperiodicToggleIN(t)
			if err := observeNonperiodicToggle(h, c); err != nil {
				t.Fatal(err)
			}
			io := &eventDuringToggleAdmission{controller: c, event: event}
			h.io = io
			c.tick = c.tick.Add(h.interval)
			commands := h.channelCommands.count
			if err := h.continueIN(); err != nil || !io.injected || h.channelCommands.count != commands || h.pending.continuations != 0 {
				t.Fatal("new event was bypassed", err, h.pending)
			}
			if event == "RX" {
				if _, data, _, err := h.step(); err != nil || data != nil || !h.pending.received || !h.pending.halting {
					t.Fatal("RX escaped retirement", data, err)
				}
				done, data, nak, err := h.step()
				if err != nil || !done || nak || len(data) != 8 || data[2] != 4 || h.pending.continuations != 0 {
					t.Fatal(done, data, nak, err)
				}
			} else {
				if done, data, _, err := h.step(); err == nil || done || data != nil || h.pending.continuations != 0 {
					t.Fatal("new channel error was accepted", done, data, err)
				}
			}
		})
	}
}

func TestToggleContinuationCannotExtendDeadlineOrAcceptWrongPID(t *testing.T) {
	for _, mode := range []string{"deadline", "wrong-PID", "short-report"} {
		t.Run(mode, func(t *testing.T) {
			h, c := nonperiodicToggleIN(t)
			deadline, pid := h.pending.deadline, h.pending.pid
			if mode == "deadline" {
				if err := observeNonperiodicToggle(h, c); err != nil {
					t.Fatal(err)
				}
				c.tick = deadline
			} else {
				c.mode = ""
				c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
				if mode == "wrong-PID" {
					c.mode = "pid"
				} else {
					c.reports[0] = []byte{0, 0, 4}
				}
				c.tick = c.tick.Add(h.interval)
			}
			var err error
			for range 4 {
				var report []byte
				report, err = h.pollInput()
				if report != nil {
					t.Fatal("invalid packet became input", report)
				}
				if err != nil {
					break
				}
			}
			if err == nil || h.fault == nil || h.pending.deadline != deadline || h.pending.pid != pid || h.toggle != 2 || h.packetSequence != 1 {
				t.Fatal("packet was repaired or retoggled", err, h.pending)
			}
			if mode == "deadline" && h.pending.continuations != 0 {
				t.Fatal("expired transfer was continued")
			}
			accesses := c.accesses
			if err := h.ResumePolling(context.Background()); err == nil || c.accesses != accesses {
				t.Fatal("failed continuation was resumed", err)
			}
		})
	}
}
