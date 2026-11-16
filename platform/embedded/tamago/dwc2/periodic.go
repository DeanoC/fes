// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import "fmt"

// A single-packet periodic service gets one queued request. A paired toggle
// notification may retire that attempt, but cannot continue an enabled channel
// into a later service interval. Only a real halt permits the retained request
// to be retried or explicitly discarded by PausePolling.
func (p *packet) periodicTogglePending() bool {
	return (p.char>>18)&3 == 3 && p.in && !p.split && !p.done && !p.received && !p.nak &&
		(p.toggleRetiring || p.toggleRetry || !p.halting && p.firstToggle != nil &&
			p.toggleRX > p.handledToggleRX && p.toggleIRQs > p.handledToggleIRQs && (p.pid == 0 || p.pid == 2))
}

func (h *Host) retirePeriodicToggle() error {
	p := &h.pending
	s := &p.lastContinue
	s.reason = "periodic-single-request"
	if !p.active || !h.owned || h.fault != nil || !p.periodicTogglePending() {
		return nil
	}
	if p.toggleRetry {
		return h.rearmPeriodicToggle()
	}
	s.queueReg = ptxstatus
	s.queue = h.io.Read32(ptxstatus)
	s.samples |= 1
	s.irq = h.io.Read32(gint)
	s.samples |= 2
	if s.irq&(1<<4) != 0 {
		s.reason = "RX"
		return nil
	}
	s.status = h.io.Read32(hcint)
	s.samples |= 4
	if s.status&^uint32(1<<1) != 0 {
		s.reason = "status"
		return nil
	}
	s.channel = h.io.Read32(hcchar)
	s.samples |= 8
	if s.channel&^(channelEnable|channelDisable|1<<29) != p.char {
		s.reason = "channel"
		return nil
	}
	s.transfer = h.io.Read32(hctsiz)
	s.samples |= 16
	if s.transfer != p.transferSize|1<<19|p.pid<<29 {
		s.reason = "toggle-transfer"
		return nil
	}
	if err := h.pio(); err != nil {
		s.reason = "ownership"
		return err
	}
	if err := h.link(); err != nil {
		s.reason = "link"
		return err
	}
	s.irq = h.io.Read32(gint)
	if s.irq&(1<<4) != 0 {
		s.reason = "RX"
		return nil
	}
	s.status = h.io.Read32(hcint)
	if s.status&^uint32(1<<1) != 0 {
		s.reason = "status"
		return nil
	}
	if s.channel&channelEnable != 0 {
		if p.toggleRetiring || s.status&(1<<1) != 0 {
			s.reason = "toggle-halt-wait"
			return nil
		}
		if (s.queue>>16)&255 == 0 {
			s.reason = "queue"
			return nil
		}
		s.reason = "toggle-retire"
		h.writeChannel("toggle-retire", s.channel|channelEnable|channelDisable)
		p.toggleRetiring, p.halting = true, true
		return nil
	}
	if s.channel&channelDisable != 0 || s.status&(1<<1) == 0 {
		s.reason = "toggle-halt-wait"
		return nil
	}
	// Both the channel state and CHHLTD acknowledge retirement. No payload, PID
	// advance, NAK or completion is invented. Clear only the observed halt bit.
	h.io.Write32(hcint, 1<<1)
	p.toggleRetiring, p.halting = false, false
	p.toggleRetry = true
	p.handledToggleRX, p.handledToggleIRQs = p.toggleRX, p.toggleIRQs
	p.toggleRetirements++
	return h.rearmPeriodicToggle()
}

// This phase owns an observed idle channel and no RX payload. Every check is
// repeated after a caller gap; HCTSIZ is written only after actual retirement.
// The public request, buffer, PID and original deadline remain unchanged.
func (h *Host) rearmPeriodicToggle() error {
	p := &h.pending
	p.lastContinue = continuationState{reason: "toggle-retry-wait"}
	s := &p.lastContinue
	if !p.active || !p.toggleRetry || !h.owned || h.fault != nil || p.split || !p.in || (p.char>>18)&3 != 3 || p.done || p.received || p.nak || p.halting {
		return fmt.Errorf("dwc2: invalid periodic retry ownership")
	}
	if err := h.pio(); err != nil {
		return err
	}
	if err := h.link(); err != nil {
		return err
	}
	s.irq = h.io.Read32(gint)
	s.samples |= 2
	s.status = h.io.Read32(hcint)
	s.samples |= 4
	s.channel = h.io.Read32(hcchar)
	s.samples |= 8
	s.transfer = h.io.Read32(hctsiz)
	s.samples |= 16
	if s.irq&(1<<4) != 0 || s.status != 0 || s.channel&(channelEnable|channelDisable) != 0 || s.channel&^uint32(1<<29) != p.char || s.transfer != p.transferSize|1<<19|p.pid<<29 {
		return fmt.Errorf("dwc2: retired periodic attempt changed before retry")
	}
	if h.draining {
		if p.requestAfter.After(h.next) {
			h.next = p.requestAfter
		}
		p.toggleRetry = false
		p.toggleCancelled = true
		p.active = false
		s.reason = "toggle-cancelled-for-pause"
		return nil
	}
	now := h.io.Now()
	if !now.Before(p.deadline) {
		return fmt.Errorf("dwc2: periodic retry exceeded packet deadline")
	}
	if now.Before(p.requestAfter) {
		return nil
	}
	s.queueReg = ptxstatus
	s.queue = h.io.Read32(ptxstatus)
	s.samples |= 1
	if (s.queue>>16)&255 == 0 {
		s.reason = "queue"
		return nil
	}
	// Reject a new event or foreign PIO/link owner before programming a new
	// attempt. It must not be swallowed by armPacket's stale-status clearing.
	if err := h.pio(); err != nil {
		return err
	}
	if err := h.link(); err != nil {
		return err
	}
	if h.io.Read32(gint)&(1<<4) != 0 || h.io.Read32(hcint) != 0 || h.io.Read32(hcchar)&(channelEnable|channelDisable) != 0 {
		return fmt.Errorf("dwc2: event arrived before periodic retry")
	}
	if !h.io.Now().Before(p.deadline) {
		return fmt.Errorf("dwc2: periodic retry exceeded packet deadline")
	}
	s.reason = "toggle-rearm"
	p.toggleRetry = false
	return h.armPacket(false)
}
