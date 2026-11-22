// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// DWC2 host interrupt register offsets and mask bits (Linux v6.12 hw.h).
const (
	haintmsk      = 0x418
	haintCh0      = uint32(1 << 0)
	gahbGlobalIRQ = uint32(1 << 0)
	gintRXFLVL    = uint32(1 << 4)
	gintHCH       = uint32(1 << 25)
	gintDisconn   = uint32(1 << 29)
	hcintChHltd   = uint32(1 << 1)
	irqMask       = gintRXFLVL | gintHCH | gintDisconn
)

// EnableIRQ binds this DWC2 host controller to a GICv3 SPI (for example
// de25nano.IRQUSB0 = 125 on Agilex 5) so callers can sleep in WFI on Ready()
// instead of waking periodically to poll for HID reports or channel completion.
//
// The level-triggered handler masks GINTMSK=0 immediately and leaves GINTSTS,
// RX FIFO status and HCINT intact for pollInput() to inspect and clear. DMA
// and descriptor-DMA remain permanently forbidden.
func (h *Host) EnableIRQ(g *gic.Controller, irqID uint32) error {
	if h == nil || h.io == nil || !h.owned || h.initializing {
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
	if h.pollingPaused {
		return fmt.Errorf("dwc2: cannot enable IRQ while polling is paused")
	}
	if err := h.pio(); err != nil {
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
	h.io.Write32(haintmsk, haintCh0)
	h.io.Write32(gahb, gahbGlobalIRQ)
	g.Register(irqID, func() {
		h.irqMu.Lock()
		if !h.irqActive.Load() {
			h.irqMu.Unlock()
			return
		}
		// Mask GINTMSK immediately so the level-sensitive SPI cannot re-trigger
		// before the owner goroutine services GINTSTS/RX FIFO/HCINT.
		h.io.Write32(gintmask, 0)
		h.irqCount.Add(1)
		h.irqMu.Unlock()
		h.signalReady()
	})
	h.irqGIC = g
	h.irqID = irqID
	h.irqSuspended = false
	h.irqMode.Store(true)
	h.irqActive.Store(true)
	if h.pending.active && h.pending.halting {
		h.io.Write32(hcmask, hcintChHltd)
	}
	h.io.Write32(gintmask, irqMask)
	g.Enable(irqID)
	pending := h.io.Read32(gint)&irqMask != 0
	h.irqMu.Unlock()
	if pending {
		h.signalReady()
	}
	return nil
}

// DisableIRQ masks DWC2 interrupts and unregisters the GICv3 SPI. When the
// host is paused or faulted it unregisters the SPI without any MMIO access.
func (h *Host) DisableIRQ() {
	if h == nil {
		return
	}
	allowMMIO := h.io != nil && h.owned && !h.pollingPaused && h.fault == nil
	h.retireIRQ(allowMMIO)
}

// Ready returns the interrupt wakeup channel while GIC IRQ mode is enabled on
// this host, or nil when running in polling-only mode or after DisableIRQ.
func (h *Host) Ready() <-chan struct{} {
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

// IRQCount returns the number of DWC2 interrupts handled since EnableIRQ.
func (h *Host) IRQCount() uint64 {
	if h == nil {
		return 0
	}
	return h.irqCount.Load()
}

func (h *Host) signalReady() {
	if h.irqReady == nil {
		return
	}
	select {
	case h.irqReady <- struct{}{}:
	default:
	}
}

// armIRQ re-enables GINTMSK after pollInput() finishes servicing the current
// hardware state and re-checks GINTSTS so an event arriving while masked is
// never missed. While retiring a channel it narrows HCINTMSK to CHHLTD so
// already-observed completion/NAK bits do not re-trigger before halt completes.
func (h *Host) armIRQ() {
	if !h.irqMode.Load() {
		return
	}
	h.irqMu.Lock()
	if !h.irqActive.Load() {
		h.irqMu.Unlock()
		return
	}
	if h.pending.active && h.pending.halting {
		h.io.Write32(hcmask, hcintChHltd)
	}
	h.io.Write32(gintmask, irqMask)
	pending := h.io.Read32(gint)&irqMask != 0
	h.irqMu.Unlock()
	if pending {
		h.signalReady()
	}
}

// suspendIRQ disables GIC delivery and clears DWC2 interrupt enables before
// entering PausePolling so zero MMIO occurs while paused and firmware Stop
// observes masked PIO state.
func (h *Host) suspendIRQ() {
	if !h.irqMode.Load() {
		return
	}
	h.irqMu.Lock()
	defer h.irqMu.Unlock()
	if h.irqGIC == nil {
		return
	}
	h.irqActive.Store(false)
	h.irqSuspended = true
	h.irqGIC.Disable(h.irqID)
	h.io.Write32(gintmask, 0)
	h.io.Write32(haintmsk, 0)
	h.io.Write32(gahb, 0)
}

// resumeIRQ restores DWC2 interrupt enables and re-enables the GIC SPI on the
// first pollInput() after ResumePolling, after pio() and link() have verified
// ownership and link state.
func (h *Host) resumeIRQ() {
	h.irqMu.Lock()
	defer h.irqMu.Unlock()
	if h.irqGIC == nil {
		h.irqSuspended = false
		return
	}
	h.io.Write32(gintmask, 0)
	h.io.Write32(haintmsk, haintCh0)
	h.io.Write32(gahb, gahbGlobalIRQ)
	h.irqSuspended = false
	h.irqActive.Store(true)
	h.irqGIC.Enable(h.irqID)
}

// retireIRQ permanently disables and unregisters any attached GIC SPI. It only
// writes DWC2 registers when IRQ mode was active, allowMMIO is true, and DMA
// has not been enabled externally.
func (h *Host) retireIRQ(allowMMIO bool) {
	h.irqMu.Lock()
	hadIRQ := h.irqGIC != nil || h.irqMode.Load()
	h.irqActive.Store(false)
	h.irqMode.Store(false)
	h.irqSuspended = false
	if h.irqGIC != nil {
		h.irqGIC.Disable(h.irqID)
		h.irqGIC.Register(h.irqID, nil)
		h.irqGIC = nil
		h.irqID = 0
	}
	if hadIRQ && allowMMIO && h.io != nil && h.io.Read32(gahb)&(1<<5) == 0 {
		h.io.Write32(gintmask, 0)
		h.io.Write32(haintmsk, 0)
		h.io.Write32(gahb, 0)
	}
	h.irqMu.Unlock()
	if hadIRQ {
		h.signalReady()
	}
}

// EnableIRQ forwards GICv3 SPI binding to the underlying DWC2 host.
func (g *GamepadHost) EnableIRQ(ctrl *gic.Controller, irqID uint32) error {
	if g == nil || g.host == nil {
		return fmt.Errorf("dwc2: gamepad host absent")
	}
	return g.host.EnableIRQ(ctrl, irqID)
}

// DisableIRQ forwards GICv3 SPI teardown to the underlying DWC2 host.
func (g *GamepadHost) DisableIRQ() {
	if g == nil || g.host == nil {
		return
	}
	g.host.DisableIRQ()
}

// Ready returns the underlying DWC2 host's interrupt wakeup channel.
func (g *GamepadHost) Ready() <-chan struct{} {
	if g == nil || g.host == nil {
		return nil
	}
	return g.host.Ready()
}

// IRQCount returns the underlying DWC2 host's handled interrupt count.
func (g *GamepadHost) IRQCount() uint64 {
	if g == nil || g.host == nil {
		return 0
	}
	return g.host.IRQCount()
}
