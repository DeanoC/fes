// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPollingDrainFailureCapturesStateBeforeContainment(t *testing.T) {
	for _, frozen := range []bool{false, true} {
		t.Run(map[bool]string{false: "time-bound", true: "step-bound"}[frozen], func(t *testing.T) {
			c := newController()
			c.address, c.mode, c.frozen = 1, "halt", frozen
			c.regs[hprt] = 0x41005
			h := &Host{io: c, owned: true, address: 1, speed: 2, rootSpeed: 2, interval: time.Hour}
			if err := h.begin(1, 8, 3, true, 0, nil, 8, c.Now().Add(2*time.Second)); err != nil {
				t.Fatal(err)
			}
			before := h.pending
			h.pending.firstToggle = errors.New("retained first toggle observation")
			err := h.PausePolling(context.Background())
			if err == nil {
				t.Fatal("ambiguous drain completed")
			}
			bound := "bound=time"
			if frozen {
				bound = "bound=steps steps=4096 elapsed_ns=0"
			}
			for _, text := range []string{
				"dwc2: polling drain timeout; drain_snapshot={" + bound,
				"active=true", "pid=0", "done=false", "nak=false", "received=false", "halting=false",
				"channel=0xa05e8808", "transfer=0x80008", "peek=0x0", "first_toggle={retained first toggle observation}",
				"continuation={reason=interval", "software_channel_commands={seq=1 packet=1 reason=arm",
				"containment: dwc2: register wait exhausted address=0x500 value=0xe05e8808",
				"reason=containment value=0xe05e8808",
			} {
				if !strings.Contains(err.Error(), text) {
					t.Fatalf("missing pre-containment observation %q in %v", text, err)
				}
			}
			if h.fault != err || h.pollingPaused || !h.pending.active || h.pending.pid != before.pid ||
				h.pending.deadline != before.deadline || len(c.starts) != 1 || h.channelCommands.count != 2 {
				t.Fatal("timeout repaired or released the failed packet", h.pending, h.channelCommands)
			}
			fault := err.Error()
			accesses := c.accesses
			if _, err := h.pollInput(); err == nil {
				t.Fatal("failed packet polled again")
			}
			if err := h.ResumePolling(context.Background()); err == nil || c.accesses != accesses || h.fault.Error() != fault {
				t.Fatal("failed source resumed or first snapshot changed")
			}
		})
	}
}

// This wrapper rejects destructive reads and records every register operation;
// the independently queued FIFO data must survive the diagnostic unchanged.
type pollingFaultIO struct {
	*controller
	reads      []uint32
	writes     []uint32
	clockReads int
}

func (c *pollingFaultIO) Read32(reg uint32) uint32 {
	if reg == rxpop || reg == fifo {
		panic("snapshot consumed queued input")
	}
	c.reads = append(c.reads, reg)
	return c.controller.Read32(reg)
}
func (c *pollingFaultIO) Write32(reg, value uint32) {
	c.writes = append(c.writes, reg)
	c.controller.Write32(reg, value)
}
func (c *pollingFaultIO) Now() time.Time {
	c.clockReads++
	return c.controller.Now()
}
func TestPollingDrainSnapshotCannotChangePendingTransferOrConsumeRX(t *testing.T) {
	c := &pollingFaultIO{controller: newController()}
	c.frozen = true
	c.rx, c.words = []uint32{2<<17 | 8<<4}, []uint32{0x12345678, 0x9abcdef0}
	c.regs[hcchar], c.regs[hctsiz] = 0xa05e8808, 0x80008
	h := &Host{io: c, owned: true, interval: 10 * time.Millisecond, packetSequence: 53, healthStage: 2, healthPolls: 7,
		pending: packet{active: true, in: true, pid: 0, limit: 8, data: []byte{4, 5}, deadline: c.tick.Add(time.Second),
			requestAfter: c.tick.Add(time.Millisecond), polls: 12, toggleRX: 1, toggleIRQs: 2,
			firstToggle: errors.New("first observed toggle"), lastContinue: continuationState{reason: "queue", samples: 1, queueReg: ptxstatus}}}
	before, rx, words := h.pending, append([]uint32(nil), c.rx...), append([]uint32(nil), c.words...)
	err := h.pollingDrainFault(c.tick, 4096)
	if err == nil || !strings.Contains(err.Error(), "health_stage=2 health_polls=7 packet=53 packet_polls=12") ||
		!strings.Contains(err.Error(), "toggle_rx=1 toggle_irqs=2") || !strings.Contains(err.Error(), "peek=0x40080") {
		t.Fatal("lost queued input or pending state", err)
	}
	if !reflect.DeepEqual(before, h.pending) || !reflect.DeepEqual(rx, c.rx) || !reflect.DeepEqual(words, c.words) ||
		len(c.writes) != 0 || len(c.starts) != 0 || h.channelCommands.count != 0 || h.fault != nil || h.pollingPaused || c.clockReads != 1 {
		t.Fatal("snapshot consumed data, issued a command or changed ownership", c, h.pending)
	}
	allowed := map[uint32]int{rxpeek: 1, gint: 1, gahb: 1, hcchar: 1, hcint: 1, hctsiz: 1, hprt: 1, reset: 1, nptxstatus: 1, ptxstatus: 1, hfnum: 1}
	for _, reg := range c.reads {
		if allowed[reg] != 1 {
			t.Fatalf("unexpected or repeated observation %#x", reg)
		}
		allowed[reg]--
	}
	for reg, left := range allowed {
		if left != 0 {
			t.Fatalf("missing observation %#x", reg)
		}
	}
}
