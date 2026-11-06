// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import "fmt"

// samples identifies only registers actually read during this continuation
// check: queue=1, global IRQ=2, channel IRQ=4, channel=8, transfer=16, frame=32.
// Fresh PIO/link admission checks are separate from these stored observations.
// A reason from an earlier check is historical; packet flags are logged too.
type continuationState struct {
	reason                                                          string
	samples, queueReg, queue, irq, status, channel, transfer, frame uint32
	waitNS                                                          int64
}

func (s continuationState) String() string {
	reason := s.reason
	if reason == "" {
		reason = "not-checked"
	}
	return fmt.Sprintf("reason=%s sampled=%#x queue_reg=%#x queue=%#x irq=%#x status=%#x channel=%#x transfer=%#x frame=%#x wait_ns=%d",
		reason, s.samples, s.queueReg, s.queue, s.irq, s.status, s.channel, s.transfer, s.frame, s.waitNS)
}

// continueIN queues at most one further IN request within the existing PIO
// packet. A data-toggle notification can consume a request without completing
// that packet. No SETUP, HCTSIZ/PID, buffer or deadline is reinitialized here.
func (h *Host) continueIN() error {
	p := &h.pending
	p.lastContinue = continuationState{reason: "ineligible"}
	s := &p.lastContinue
	if !p.active || !p.in || p.split || p.done || p.received || p.nak || p.halting {
		return nil
	}
	queue := uint32(nptxstatus)
	periodic := (p.char>>18)&3 == 3
	if periodic {
		now := h.io.Now()
		if now.Before(p.requestAfter) {
			s.reason = "interval"
			s.waitNS = p.requestAfter.Sub(now).Nanoseconds()
			return nil
		}
		queue = ptxstatus
	}
	s.queueReg = queue
	s.queue = h.io.Read32(queue)
	s.samples |= 1
	if (s.queue>>16)&255 == 0 {
		s.reason = "queue"
		return nil
	}
	// Service newly arrived RX/status before issuing more requests. The core
	// can complete asynchronously between observations; extra queued tokens
	// are flushed by the core on completion, as in its slave-mode reference.
	s.irq = h.io.Read32(gint)
	s.samples |= 2
	if s.irq&(1<<4) != 0 {
		s.reason = "RX"
		return nil
	}
	s.status = h.io.Read32(hcint)
	s.samples |= 4
	if s.status&0x7ff != 0 {
		s.reason = "status"
		return nil
	}
	char := h.io.Read32(hcchar)
	s.channel = char
	s.samples |= 8
	clearDisable := char&channelDisable != 0
	if char&channelEnable == 0 || clearDisable &&
		(!h.owned || h.fault != nil || p.firstToggle == nil || p.toggleRX == 0 || p.toggleIRQs == 0 || (p.pid != 0 && p.pid != 2) ||
			char&^(channelEnable|channelDisable|1<<29) != p.char) {
		s.reason = "channel"
		return nil // Unqualified disable/completion cannot restart a channel.
	}
	if clearDisable {
		// The slave-mode reference clears CHDIS while continuing an owned
		// IN transfer. Admit only the observed paired toggle case, without a
		// software halt, and recheck PIO/link ownership before that write.
		if err := h.pio(); err != nil {
			s.reason = "ownership"
			return err
		}
		if err := h.link(); err != nil {
			s.reason = "link"
			return err
		}
	}
	s.transfer = h.io.Read32(hctsiz)
	s.samples |= 16
	count := (s.transfer >> 19) & 1023
	if count == 0 {
		s.reason = "completion"
		return nil // Completion may be arriving; never restart a spent packet.
	}
	if count != 1 {
		s.reason = "count"
		return fmt.Errorf("dwc2: pending IN packet count changed: %d", count)
	}
	if clearDisable {
		// No consumed bytes, altered PID or extra transfer-control bits may
		// be hidden behind the toggle notification. Never rewrite HCTSIZ.
		if s.transfer != p.transferSize|1<<19|p.pid<<29 {
			s.reason = "toggle-transfer"
			return nil
		}
		// Let any completion or other event that arrived during admission
		// reach step() before queuing another token for this same packet.
		s.irq = h.io.Read32(gint)
		if s.irq&(1<<4) != 0 {
			s.reason = "RX"
			return nil
		}
		s.status = h.io.Read32(hcint)
		if s.status&0x7ff != 0 {
			s.reason = "status"
			return nil
		}
		char &^= channelDisable
	}
	if periodic {
		char &^= 1 << 29
		s.frame = h.io.Read32(hfnum)
		s.samples |= 32
		if s.frame&1 == 0 {
			char |= 1 << 29
		}
	}
	h.writeChannel("continue", char|channelEnable)
	p.continuations++
	s.reason = "queued"
	if periodic {
		p.requestAfter = h.io.Now().Add(h.interval)
	}
	return nil
}
