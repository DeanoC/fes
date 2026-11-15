// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestPeriodicToggleMustHaltBeforeRetryingSameRequest(t *testing.T) {
	c := newPeriodicController()
	c.toggles = 1
	want := []byte{0, 0, 4, 0, 0, 0, 0, 0}
	c.reports = [][]byte{want}
	h := periodicHost(t, c)
	if got, err := h.pollInput(); err != nil || got != nil {
		t.Fatal(got, err)
	}
	deadline, pid, packet := h.pending.deadline, h.pending.pid, h.packetSequence
	c.advance(2 * time.Millisecond)
	if got, err := h.pollInput(); err != nil || got != nil {
		t.Fatal(got, err)
	}
	last := h.channelCommands.entries[(h.channelCommands.count-1)%8]
	if last.reason != "toggle-retire" || last.value&(channelEnable|channelDisable) != channelEnable|channelDisable || h.pending.continuations != 0 {
		t.Fatal("periodic toggle was continued before real halt", last, h.pending)
	}
	if len(c.starts) != 1 {
		t.Fatal("second transaction before halt")
	}
	// Re-observation cannot issue another halt or overwrite the request lifetime.
	if _, err := h.pollInput(); err != nil {
		t.Fatal(err)
	}
	if h.pending.deadline != deadline || h.pending.pid != pid || h.packetSequence != packet {
		t.Fatal("request identity changed")
	}
	var got []byte
	var err error
	for n := 0; n < 1000; n++ {
		c.advance(100 * time.Microsecond)
		got, err = h.pollInput()
		if got != nil || err != nil {
			break
		}
	}
	if err != nil || !bytes.Equal(got, want) || h.pending.active || h.toggle != 0 || h.pending.deadline != deadline || h.packetSequence != packet {
		t.Fatal(got, err, h.pending, c.history)
	}
	for i, e := range c.history {
		if e.kind == "data" && (i == 0 || c.history[i-1].kind != "halt") {
			t.Fatal("retry lacked observed halt", c.history)
		}
	}
}

func TestPeriodicTogglePauseCanDiscardOnlyAfterObservedHalt(t *testing.T) {
	for _, stalled := range []bool{false, true} {
		t.Run(map[bool]string{false: "retired", true: "stalled"}[stalled], func(t *testing.T) {
			c := newPeriodicController()
			c.toggles = 1
			c.stalledHalt = stalled
			h := periodicHost(t, c)
			h.channels = 1
			if _, err := h.pollInput(); err != nil {
				t.Fatal(err)
			}
			deadline, pid := h.pending.deadline, h.pending.pid
			c.advance(2 * time.Millisecond)
			err := h.PausePolling(context.Background())
			if stalled {
				if err == nil || h.pollingPaused || h.fault == nil || c.regs[hcchar]&channelEnable == 0 {
					t.Fatal("unhalted toggle escaped", err)
				}
			} else {
				if err != nil || !h.pollingPaused || h.pending.active || h.fault != nil || c.regs[hcchar]&channelEnable != 0 {
					t.Fatal(err, h.pending, c.history)
				}
				if len(c.starts) != 1 || h.toggle != 2 || h.pending.deadline != deadline || h.pending.pid != pid {
					t.Fatal("pause fabricated data, PID or retry")
				}
				if err := h.ResumePolling(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if strings.Contains(h.channelCommands.String(), "reason=continue") {
				t.Fatal("periodic continuation survived", h.channelCommands)
			}
		})
	}
}

func pairedPeriodicToggle(t *testing.T) (*Host, *periodicController) {
	t.Helper()
	c := newPeriodicController()
	c.toggles = 1
	h := periodicHost(t, c)
	if _, err := h.pollInput(); err != nil {
		t.Fatal(err)
	}
	c.advance(2 * time.Millisecond)
	c.queueReserved = 16
	if _, err := h.pollInput(); err != nil || h.pending.toggleRX != 1 || h.pending.toggleIRQs != 1 || h.pending.halting {
		t.Fatal(err, h.pending)
	}
	c.queueReserved = 0
	return h, c
}

func TestPeriodicToggleRetirementPreservesOwnershipAndTransferExclusions(t *testing.T) {
	modes := []string{"no-RX", "no-IRQ", "no-first", "not-owned", "fault", "software-halt", "done", "received", "NAK", "OUT", "split", "unknown-PID", "recipe", "size", "PID", "zero-count", "extra-count", "transfer-bits", "DMA", "IRQ", "descriptor-DMA", "disconnect", "RX", "status", "queue", "halt-with-enable", "disabled-without-halt"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			h, c := pairedPeriodicToggle(t)
			switch mode {
			case "no-RX":
				h.pending.toggleRX = 0
			case "no-IRQ":
				h.pending.toggleIRQs = 0
			case "no-first":
				h.pending.firstToggle = nil
			case "not-owned":
				h.owned = false
			case "fault":
				h.fault = context.Canceled
			case "software-halt":
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
			case "unknown-PID":
				h.pending.pid = 3
			case "recipe":
				c.regs[hcchar] ^= 1 << 22
			case "size":
				c.regs[hctsiz]--
			case "PID":
				c.regs[hctsiz] ^= 2 << 29
			case "zero-count":
				c.regs[hctsiz] = 2 << 29
			case "extra-count":
				c.regs[hctsiz] = 8 | 2<<19 | 2<<29
			case "transfer-bits":
				c.regs[hctsiz] |= 1 << 31
			case "DMA":
				c.regs[gahb] |= 1 << 5
			case "IRQ":
				c.regs[gintmask] = 1
			case "descriptor-DMA":
				c.regs[hcfg] |= 1 << 23
			case "disconnect":
				c.regs[hprt] &^= 1
			case "RX":
				c.rx = []uint32{3 << 17}
			case "status":
				c.regs[hcint] = 1 << 9
			case "queue":
				c.queueReserved = 16
			case "halt-with-enable":
				c.regs[hcint] = 2
			case "disabled-without-halt":
				c.regs[hcchar] &^= channelEnable | channelDisable
			}
			count, writes, rx := h.channelCommands.count, len(c.writes), len(c.rx)
			deadline, pid := h.pending.deadline, h.pending.pid
			err := h.retirePeriodicToggle()
			wantError := mode == "DMA" || mode == "IRQ" || mode == "descriptor-DMA" || mode == "disconnect"
			if (err != nil) != wantError || h.channelCommands.count != count || len(c.writes) != writes || len(c.rx) != rx || h.pending.deadline != deadline || h.pending.pid != pid || h.pending.toggleRetry || h.pending.toggleCancelled {
				t.Fatal("excluded state retired, rewrote or consumed a packet", mode, err, h.pending)
			}
		})
	}
}

func TestPeriodicToggleAlreadyHaltedCanPauseWithoutQueueSlot(t *testing.T) {
	h, c := pairedPeriodicToggle(t)
	h.draining = true
	c.queueReserved = 16
	c.regs[hcchar] &^= channelEnable | channelDisable
	c.regs[hcint] = 2
	count := h.channelCommands.count
	deadline := h.pending.deadline
	if err := h.retirePeriodicToggle(); err != nil || h.pending.active || !h.pending.toggleCancelled || h.pending.nak || h.pending.done || h.pending.received || h.pending.deadline != deadline || h.toggle != 2 || h.channelCommands.count != count {
		t.Fatal("observed idle channel required a halt slot or invented completion", err, h.pending)
	}
}

func TestPeriodicRetiredAttemptRejectsNewEventsAndForeignState(t *testing.T) {
	modes := []string{"DMA", "IRQ", "descriptor-DMA", "disconnect", "RX", "status", "active", "disable", "recipe", "transfer", "queue", "deadline"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			h, c := pairedPeriodicToggle(t)
			if _, err := h.pollInput(); err != nil {
				t.Fatal(err)
			}
			c.advance(100 * time.Microsecond)
			if _, err := h.pollInput(); err != nil || !h.pending.toggleRetry {
				t.Fatal(err, h.pending)
			}
			c.advance(20 * time.Millisecond)
			switch mode {
			case "DMA":
				c.regs[gahb] |= 1 << 5
			case "IRQ":
				c.regs[gintmask] = 1
			case "descriptor-DMA":
				c.regs[hcfg] |= 1 << 23
			case "disconnect":
				c.regs[hprt] &^= 1
			case "RX":
				c.rx = []uint32{3 << 17}
			case "status":
				c.regs[hcint] = 1 << 9
			case "active":
				c.regs[hcchar] |= channelEnable
			case "disable":
				c.regs[hcchar] |= channelDisable
			case "recipe":
				c.regs[hcchar] ^= 1 << 22
			case "transfer":
				c.regs[hctsiz] ^= 2 << 29
			case "queue":
				c.queueReserved = 16
			case "deadline":
				c.advance(600 * time.Millisecond)
			}
			deadline, pid := h.pending.deadline, h.pending.pid
			offset := len(c.writes)
			got, err := h.pollInput()
			if got != nil || h.pending.deadline != deadline || h.pending.pid != pid || h.toggle != 2 || (err == nil) != (mode == "queue") {
				t.Fatal("changed idle attempt admitted", got, err, h.pending)
			}
			for _, op := range c.writes[offset:] {
				if op.reg == hctsiz {
					t.Fatal("uncertain idle channel reprogrammed")
				}
			}
			if err != nil {
				accesses, writes := c.accesses, len(c.writes)
				if _, err := h.pollInput(); err == nil || accesses != c.accesses || writes != len(c.writes) {
					t.Fatal("failed owner resumed hardware")
				}
			}
		})
	}
}

func TestPeriodicRearmCannotAcceptWrongPIDOrShortReport(t *testing.T) {
	for _, mode := range []string{"pid", "short"} {
		t.Run(mode, func(t *testing.T) {
			h, c := pairedPeriodicToggle(t)
			c.mode = mode
			c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
			if mode == "short" {
				c.reports[0] = []byte{0, 0, 4}
			}
			deadline := h.pending.deadline
			var err error
			for n := 0; n < 1000; n++ {
				c.advance(100 * time.Microsecond)
				var got []byte
				got, err = h.pollInput()
				if got != nil {
					t.Fatal("invalid retry data escaped", got)
				}
				if err != nil {
					break
				}
			}
			if err == nil || h.fault == nil || h.pending.deadline != deadline || h.toggle != 2 || h.pending.toggleRetirements != 1 {
				t.Fatal("bad retry advanced PID/deadline", err, h.pending)
			}
		})
	}
}

func TestPeriodicAdmissionHistorySurvivesLaterWaitsWithoutIO(t *testing.T) {
	h, c := pairedPeriodicToggle(t)
	if _, err := h.pollInput(); err != nil {
		t.Fatal(err)
	}
	saved := h.channelCommands.entries[1].admission
	if saved.samples != 31 || saved.channel&(channelEnable|channelDisable) != channelEnable|channelDisable || saved.transfer != 0x40080008 {
		t.Fatal("halt admission not frozen", saved)
	}
	c.advance(100 * time.Microsecond)
	if _, err := h.pollInput(); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		if _, err := h.pollInput(); err != nil {
			t.Fatal(err)
		}
	}
	accesses, writes := c.accesses, len(c.writes)
	s := h.channelCommands.String()
	if h.channelCommands.entries[1].admission != saved || !strings.Contains(s, "admission={reason=toggle-retire sampled=0x1f") || accesses != c.accesses || writes != len(c.writes) {
		t.Fatal("later check/formatter replaced or resampled issued admission", s)
	}
}

func TestPeriodicTogglePauseResumeKeepsIntervalAndNewPacketIdentity(t *testing.T) {
	h, c := pairedPeriodicToggle(t)
	h.channels = 1
	deadline, packet := h.pending.deadline, h.packetSequence
	if err := h.PausePolling(context.Background()); err != nil || !h.pending.toggleCancelled || !h.pollingPaused || h.pending.deadline != deadline || h.pending.nak || h.pending.done {
		t.Fatal(err, h.pending)
	}
	if err := h.ResumePolling(context.Background()); err != nil {
		t.Fatal(err)
	}
	starts := len(c.starts)
	if got, err := h.pollInput(); err != nil || got != nil || len(c.starts) != starts {
		t.Fatal("resume bypassed advertised interval", got, err)
	}
	c.reports = [][]byte{{0, 0, 4, 0, 0, 0, 0, 0}}
	c.advance(10 * time.Millisecond)
	var got []byte
	var err error
	for n := 0; n < 100; n++ {
		got, err = h.pollInput()
		if got != nil || err != nil {
			break
		}
		c.advance(100 * time.Microsecond)
	}
	if err != nil || !bytes.Equal(got, []byte{0, 0, 4, 0, 0, 0, 0, 0}) || h.packetSequence != packet+1 || h.toggle != 0 || h.pending.active || h.pending.toggleCancelled || !h.pending.deadline.After(deadline) {
		t.Fatal("resumed original owner failed fresh request", got, err, h.pending, c.history)
	}
}

type periodicAdmissionRace struct {
	*periodicController
	event string
	fired bool
}

func (c *periodicAdmissionRace) Read32(reg uint32) uint32 {
	v := c.periodicController.Read32(reg)
	if reg == hctsiz && !c.fired {
		c.fired = true
		if c.event == "RX" {
			c.controller.complete([]byte{0, 0, 4, 0, 0, 0, 0, 0}, true)
		} else {
			c.regs[hcint] = 1 << 9
		}
	}
	return v
}
func TestPeriodicRetirementCannotPassNewCompletionOrFatalEvent(t *testing.T) {
	for _, event := range []string{"RX", "fatal"} {
		t.Run(event, func(t *testing.T) {
			h, c := pairedPeriodicToggle(t)
			race := &periodicAdmissionRace{periodicController: c, event: event}
			h.io = race
			commands := h.channelCommands.count
			deadline := h.pending.deadline
			if err := h.retirePeriodicToggle(); err != nil || !race.fired || h.channelCommands.count != commands {
				t.Fatal("new event was bypassed", err, h.pending)
			}
			got, err := h.pollInput()
			if event == "fatal" {
				if err == nil || got != nil || h.fault == nil || !strings.Contains(err.Error(), "channel error 0x200") {
					t.Fatal(got, err)
				}
			} else {
				if err != nil || got != nil || !h.pending.received || !h.pending.done || !h.pending.halting || h.pending.toggleRetiring {
					t.Fatal("racing completion escaped original retirement", got, err, h.pending)
				}
				c.advance(100 * time.Microsecond)
				got, err = h.pollInput()
				if err != nil || len(got) != 8 || h.pending.active || h.pending.toggleRetirements != 0 || h.toggle != 0 {
					t.Fatal(got, err, h.pending)
				}
			}
			if h.pending.deadline != deadline {
				t.Fatal("race extended deadline")
			}
		})
	}
}
