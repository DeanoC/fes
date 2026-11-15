// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import "fmt"

// channelFault observes a terminal channel error before fail attempts its halt.
// The original interrupt sample remains authoritative. These later sequential
// reads are diagnostic: they neither pop receive data nor acknowledge status,
// select another frame, issue a command or establish that containment succeeded.
func (h *Host) channelFault(interrupts uint32) error {
	peek := h.io.Read32(rxpeek)
	irq := h.io.Read32(gint)
	ahb := h.io.Read32(gahb)
	mask := h.io.Read32(gintmask)
	config := h.io.Read32(hcfg)
	channel := h.io.Read32(hcchar)
	status := h.io.Read32(hcint)
	transfer := h.io.Read32(hctsiz)
	split := h.io.Read32(hcsplit)
	port := h.io.Read32(hprt)
	resetState := h.io.Read32(reset)
	nonperiodic := h.io.Read32(nptxstatus)
	periodic := h.io.Read32(ptxstatus)
	frame := h.io.Read32(hfnum)
	interval := h.io.Read32(hfir)
	return fmt.Errorf("dwc2: channel error %#x; channel_snapshot={peek=%#x irq=%#x ahb=%#x mask=%#x config=%#x channel=%#x interrupts=%#x transfer=%#x split=%#x port=%#x reset=%#x nonperiodic=%#x periodic=%#x frame=%#x interval=%#x root_speed=%d device_speed=%d hub=%d port_number=%d}",
		interrupts, peek, irq, ahb, mask, config, channel, status, transfer, split,
		port, resetState, nonperiodic, periodic, frame, interval, h.rootSpeed, h.speed, h.route.hub, h.route.port)
}
