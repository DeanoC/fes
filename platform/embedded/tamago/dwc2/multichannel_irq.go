// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// EnableIRQ binds MultiHost to a GICv3 SPI (de25nano.IRQUSB0 = 125) so callers
// can sleep in WFI on Ready() across all active channels and hotplug events.
func (h *MultiHost) EnableIRQ(g *gic.Controller, irqID uint32) error {
	return h.enableIRQ(g, irqID, nil)
}

// EnableIRQGuarded binds a GICv3 SPI guarded by an external firmware lease check.
func (h *MultiHost) EnableIRQGuarded(g *gic.Controller, irqID uint32, guard func() bool) error {
	if guard == nil {
		return errors.New("dwc2: nil IRQ ownership guard")
	}
	return h.enableIRQ(g, irqID, guard)
}

func (h *MultiHost) enableIRQ(g *gic.Controller, irqID uint32, guard func() bool) error {
	if h == nil || h.io == nil || !h.owned {
		return fmt.Errorf("dwc2: host not ready for IRQ")
	}
	if g == nil {
		return errors.New("dwc2: nil GIC controller")
	}
	if irqID < gic.FirstSPI || irqID >= g.Lines() {
		return gic.ErrInvalidConfig
	}
	if h.fault != nil {
		return h.fault
	}
	if h.paused {
		return fmt.Errorf("dwc2: cannot enable IRQ while polling is paused")
	}
	if guard != nil && !guard() {
		return fmt.Errorf("dwc2: IRQ ownership guard inactive")
	}
	if err := h.pioCheck(); err != nil {
		return err
	}
	h.irqMu.Lock()
	if h.irqGIC != nil {
		id := h.irqID
		h.irqMu.Unlock()
		return fmt.Errorf("dwc2: IRQ already enabled on INTID %d", id)
	}
	if h.irqReady == nil {
		h.irqReady = make(chan struct{}, 1)
	}
	g.Disable(irqID)
	h.irqCount.Store(0)
	h.io.Write32(gintmask, 0)
	h.io.Write32(haintmsk, h.activeChannelMask())
	h.io.Write32(gahb, gahbGlobalIRQ)
	h.irqGuard = guard
	g.Register(irqID, func() {
		h.irqMu.Lock()
		if !h.irqActive.Load() {
			h.irqMu.Unlock()
			return
		}
		if h.irqGuard != nil && !h.irqGuard() {
			hadIRQ := h.retireIRQLocked(false)
			h.irqMu.Unlock()
			if hadIRQ {
				h.signalReady()
			}
			return
		}
		h.io.Write32(gintmask, 0)
		if nowNS := h.io.Now().UnixNano(); nowNS > 0 {
			h.irqTimestampNS.CompareAndSwap(0, nowNS)
		}
		h.irqCount.Add(1)
		h.irqMu.Unlock()
		h.signalReady()
	})
	activeMask, needSOF := h.pendingIRQLocked()
	mask := multiIRQMask
	if needSOF {
		h.io.Write32(gint, gintSOF)
		mask = multiIRQMaskSF
	}
	h.io.Write32(haintmsk, activeMask)
	h.irqGIC = g
	h.irqID = irqID
	h.irqSuspended = false
	h.irqMode.Store(true)
	h.irqActive.Store(true)
	h.io.Write32(gintmask, mask)
	g.Enable(irqID)
	pending := needSOF || h.io.Read32(gint)&mask != 0
	h.irqMu.Unlock()
	if pending {
		h.signalReady()
	}
	return nil
}

// DisableIRQ masks DWC2 interrupts and unregisters the GICv3 SPI.
func (h *MultiHost) DisableIRQ() {
	if h == nil {
		return
	}
	allowMMIO := h.io != nil && h.owned && !h.paused && h.fault == nil
	h.retireIRQ(allowMMIO)
}

// RetireIRQ unregisters the GIC SPI without accessing DWC2 registers.
func (h *MultiHost) RetireIRQ() {
	if h == nil {
		return
	}
	h.retireIRQ(false)
}

// Ready returns the GICv3 interrupt wakeup channel when IRQ mode is enabled.
func (h *MultiHost) Ready() <-chan struct{} {
	if h == nil {
		return nil
	}
	h.irqMu.Lock()
	defer h.irqMu.Unlock()
	if h.irqGIC == nil {
		return nil
	}
	return h.irqReady
}

// IRQCount returns the number of handled DWC2 interrupts since EnableIRQ.
func (h *MultiHost) IRQCount() uint64 {
	if h == nil {
		return 0
	}
	return h.irqCount.Load()
}

// Latency returns recorded IRQ-to-delivery latency stats across active IN pipes.
func (h *MultiHost) Latency() LatencyStats {
	if h == nil {
		return LatencyStats{}
	}
	return h.latency.snapshot()
}

// ResetLatency clears recorded latency stats.
func (h *MultiHost) ResetLatency() {
	if h == nil {
		return
	}
	h.latency.reset()
}

func (h *MultiHost) signalReady() {
	if h.irqReady == nil {
		return
	}
	select {
	case h.irqReady <- struct{}{}:
	default:
	}
}

func (h *MultiHost) pendingIRQLocked() (activeMask uint32, needSOF bool) {
	for ch := 1; ch < h.channels; ch++ {
		s := &h.slots[ch]
		if !s.allocated || s.fault != nil {
			continue
		}
		activeMask |= 1 << uint(ch)
		// Keep SOF armed whenever a channel is waiting on a split microframe or
		// has an unarmed IN/OUT transfer pending (either waiting for nextDue or
		// deferred because the TX request queue / FIFO was temporarily full).
		if s.waitingSplit || (!s.armed && s.ep.In() && !s.inReady) || (!s.armed && !s.ep.In() && s.outPending) {
			needSOF = true
		}
	}
	if !needSOF {
		if h.rootRetryCount > 0 {
			needSOF = true
		} else {
			for _, hub := range h.hubs {
				if hub.statusPipe == nil || hub.repowerRetries > 0 {
					needSOF = true
					break
				}
				for p := uint8(1); p <= hub.numPorts; p++ {
					if hub.retryCount[p] > 0 {
						needSOF = true
						break
					}
				}
				if needSOF {
					break
				}
			}
		}
	}
	return activeMask, needSOF
}

func (h *MultiHost) armMultiIRQ(now time.Time) {
	if !h.irqMode.Load() {
		return
	}
	h.irqMu.Lock()
	if !h.irqActive.Load() {
		h.irqMu.Unlock()
		return
	}
	if h.irqGuard != nil && !h.irqGuard() {
		hadIRQ := h.retireIRQLocked(false)
		h.irqMu.Unlock()
		if hadIRQ {
			h.signalReady()
		}
		return
	}
	activeMask, needSOF := h.pendingIRQLocked()
	h.io.Write32(haintmsk, activeMask)
	mask := multiIRQMask
	if needSOF {
		h.io.Write32(gint, gintSOF)
		mask = multiIRQMaskSF
	}
	h.io.Write32(gintmask, mask)
	pending := h.io.Read32(gint)&mask != 0
	h.irqMu.Unlock()
	if pending {
		h.signalReady()
	}
}

func (h *MultiHost) suspendIRQ() error {
	if !h.irqMode.Load() {
		return nil
	}
	h.irqMu.Lock()
	if h.irqGIC == nil {
		h.irqMu.Unlock()
		return nil
	}
	h.irqActive.Store(false)
	h.irqSuspended = true
	h.irqGIC.Disable(h.irqID)
	if (h.irqGuard != nil && !h.irqGuard()) || h.io.Read32(gahb)&(1<<5) != 0 || h.io.Read32(hcfg)&(1<<23) != 0 {
		hadIRQ := h.retireIRQLocked(false)
		h.irqMu.Unlock()
		if hadIRQ {
			h.signalReady()
		}
		return fmt.Errorf("dwc2: PIO/IRQ ownership lost during pause")
	}
	h.io.Write32(gintmask, 0)
	h.io.Write32(haintmsk, 0)
	h.io.Write32(gahb, 0)
	h.irqMu.Unlock()
	return nil
}

func (h *MultiHost) resumeIRQ() error {
	h.irqMu.Lock()
	if h.irqGIC == nil {
		h.irqSuspended = false
		h.irqMu.Unlock()
		return nil
	}
	if (h.irqGuard != nil && !h.irqGuard()) || h.io.Read32(gahb)&(1<<5) != 0 || h.io.Read32(hcfg)&(1<<23) != 0 {
		hadIRQ := h.retireIRQLocked(false)
		h.irqMu.Unlock()
		if hadIRQ {
			h.signalReady()
		}
		return fmt.Errorf("dwc2: PIO/IRQ ownership lost during resume")
	}
	h.io.Write32(gintmask, 0)
	h.io.Write32(haintmsk, h.activeChannelMask())
	h.io.Write32(gahb, gahbGlobalIRQ)
	h.irqSuspended = false
	h.irqActive.Store(true)
	h.irqGIC.Enable(h.irqID)
	h.irqMu.Unlock()
	return nil
}

func (h *MultiHost) retireIRQ(allowMMIO bool) {
	h.irqMu.Lock()
	hadIRQ := h.retireIRQLocked(allowMMIO)
	h.irqMu.Unlock()
	if hadIRQ {
		h.signalReady()
	}
}

func (h *MultiHost) retireIRQLocked(allowMMIO bool) bool {
	hadIRQ := h.irqGIC != nil || h.irqMode.Load()
	guard := h.irqGuard
	h.irqActive.Store(false)
	h.irqMode.Store(false)
	h.irqSuspended = false
	h.irqGuard = nil
	if h.irqGIC != nil {
		h.irqGIC.Disable(h.irqID)
		h.irqGIC.Register(h.irqID, nil)
		h.irqGIC = nil
		h.irqID = 0
	}
	if hadIRQ && allowMMIO && h.io != nil && (guard == nil || guard()) && h.io.Read32(gahb)&(1<<5) == 0 && h.io.Read32(hcfg)&(1<<23) == 0 {
		h.io.Write32(gintmask, 0)
		h.io.Write32(haintmsk, 0)
		h.io.Write32(gahb, 0)
	}
	return hadIRQ
}
