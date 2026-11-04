// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import "fmt"

// continueIN queues at most one further IN request within the existing PIO
// packet. A data-toggle notification can consume a request without completing
// that packet. No SETUP, HCTSIZ/PID, buffer or deadline is reinitialized here.
func (h *Host) continueIN() error {
	p := &h.pending
	if !p.active || !p.in || p.split || p.done || p.received || p.nak || p.halting {
		return nil
	}
	queue := uint32(nptxstatus)
	periodic := (p.char>>18)&3 == 3
	if periodic {
		if h.io.Now().Before(p.requestAfter) {
			return nil
		}
		queue = ptxstatus
	}
	if (h.io.Read32(queue)>>16)&255 == 0 {
		return nil
	}
	// Service newly arrived RX/status before issuing more requests. The core
	// can complete asynchronously between observations; extra queued tokens
	// are flushed by the core on completion, as in its slave-mode reference.
	if h.io.Read32(gint)&(1<<4) != 0 || h.io.Read32(hcint)&0x7ff != 0 {
		return nil
	}
	char := h.io.Read32(hcchar)
	if char&channelEnable == 0 || char&channelDisable != 0 {
		return nil // A completion/halt may have raced the status sample.
	}
	count := (h.io.Read32(hctsiz) >> 19) & 1023
	if count == 0 {
		return nil // Completion may be arriving; never restart a spent packet.
	}
	if count != 1 {
		return fmt.Errorf("dwc2: pending IN packet count changed: %d", count)
	}
	if periodic {
		char &^= 1 << 29
		if h.io.Read32(hfnum)&1 == 0 {
			char |= 1 << 29
		}
	}
	h.io.Write32(hcchar, char|channelEnable)
	p.continuations++
	if periodic {
		p.requestAfter = h.io.Now().Add(h.interval)
	}
	return nil
}
