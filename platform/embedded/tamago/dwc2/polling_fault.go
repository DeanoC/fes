// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"fmt"
	"time"
)

// pollingDrainFault samples the first terminal drain failure before fail may
// issue a channel halt. These sequential read-only observations are not a
// coherent controller snapshot or a completion proof. Never pop RX or payload.
func (h *Host) pollingDrainFault(start time.Time, steps int) error {
	now := h.io.Now()
	bound := "time"
	if steps >= 4096 {
		bound = "steps"
	}
	peek := h.io.Read32(rxpeek)
	irq := h.io.Read32(gint)
	ahb := h.io.Read32(gahb)
	channel := h.io.Read32(hcchar)
	interrupts := h.io.Read32(hcint)
	transfer := h.io.Read32(hctsiz)
	port := h.io.Read32(hprt)
	resetState := h.io.Read32(reset)
	nonperiodic := h.io.Read32(nptxstatus)
	periodic := h.io.Read32(ptxstatus)
	frame := h.io.Read32(hfnum)
	p := &h.pending
	return fmt.Errorf("dwc2: polling drain timeout; drain_snapshot={bound=%s steps=%d elapsed_ns=%d health_stage=%d health_polls=%d packet=%d packet_polls=%d active=%t pid=%d split=%t done=%t nak=%t received=%t halting=%t deadline_remaining_ns=%d interval_ns=%d request_remaining_ns=%d continuations=%d toggle_rx=%d toggle_irqs=%d continuation={%s} first_toggle={%v} peek=%#x irq=%#x ahb=%#x channel=%#x interrupts=%#x transfer=%#x port=%#x reset=%#x nonperiodic=%#x periodic=%#x frame=%#x software_channel_commands={%s}}",
		bound, steps, now.Sub(start).Nanoseconds(), h.healthStage, h.healthPolls,
		h.packetSequence, p.polls, p.active, p.pid, p.split, p.done, p.nak, p.received, p.halting,
		p.deadline.Sub(now).Nanoseconds(), h.interval.Nanoseconds(), p.requestAfter.Sub(now).Nanoseconds(),
		p.continuations, p.toggleRX, p.toggleIRQs, p.lastContinue, p.firstToggle,
		peek, irq, ahb, channel, interrupts, transfer, port, resetState, nonperiodic, periodic, frame, h.channelCommands)
}
