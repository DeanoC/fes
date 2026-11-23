// SPDX-License-Identifier: GPL-3.0-or-later

package xgmac

import (
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

// DesignWare XGMAC 2.10 DMA Channel 0 interrupt enable / status bits.
const (
	irqNIE    = uint32(1 << 15) // Normal Interrupt Summary Enable
	irqRIE    = uint32(1 << 6)  // Receive Interrupt Enable
	statusNIS = uint32(1 << 15) // Normal Interrupt Summary (W1C)
	statusRI  = uint32(1 << 6)  // Receive Interrupt (W1C)
	rxIOC     = uint32(1 << 30) // RDES3 read-format Interrupt On Completion
)

// RXReady returns the RX interrupt wakeup channel while GIC IRQ mode is active,
// satisfying network.Link's optional rxWaiter interface so Link.Start sleeps
// in WFI between packets instead of spinning.
func (d *Device) RXReady() <-chan struct{} {
	if !d.irqActive.Load() {
		return nil
	}
	return d.rxReady
}

// IRQCount returns the number of XGMAC RX interrupts handled since EnableIRQ.
func (d *Device) IRQCount() uint64 { return d.irqCount.Load() }

// EnableIRQ routes Config.IRQ (e.g. de25nano.IRQEMAC0/1/2) through g to wake
// network.Link.Start on RX completion. Returns gic.ErrUnsupported when the
// device was opened with Config.IRQ == 0.
//
// The level-triggered handler masks DMA channel interrupts immediately, clears
// only RI|NIS in channelStatus (leaving fault/stop bits intact for health()),
// and wakes RXReady(). Receive() re-enables channelInterrupt once the ring
// drains and re-checks the next descriptor so no packet can be missed.
func (d *Device) EnableIRQ(g *gic.Controller) error {
	if d.config.IRQ == 0 {
		return gic.ErrUnsupported
	}
	if g == nil {
		return errors.New("xgmac: nil GIC controller")
	}
	if d.config.IRQ >= g.Lines() {
		return gic.ErrInvalidConfig
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.Err(); err != nil {
		return err
	}
	if d.paused {
		return network.ErrPaused
	}
	d.irqMu.Lock()
	defer d.irqMu.Unlock()
	if d.irqGIC != nil {
		return fmt.Errorf("xgmac: IRQ already enabled on INTID %d", d.config.IRQ)
	}
	id := d.config.IRQ
	g.Disable(id)
	d.irqCount.Store(0)
	d.io.Write32(channelInterrupt, 0)
	d.io.Write32(channelStatus, statusRI|statusNIS)
	g.Register(id, func() {
		d.irqMu.Lock()
		if !d.irqActive.Load() {
			d.irqMu.Unlock()
			return
		}
		d.io.Write32(channelInterrupt, 0)
		d.io.Write32(channelStatus, statusRI|statusNIS)
		d.irqCount.Add(1)
		d.irqMu.Unlock()
		d.signalRXReady()
	})
	d.irqGIC = g
	d.irqActive.Store(true)
	d.io.Write32(channelInterrupt, irqNIE|irqRIE)
	g.Enable(id)
	d.signalRXReady()
	return nil
}

// DisableIRQ masks XGMAC channel interrupts and unregisters the GICv3 SPI.
func (d *Device) DisableIRQ(g *gic.Controller) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.irqMu.Lock()
	d.irqActive.Store(false)
	ctrl := d.irqGIC
	if ctrl == nil {
		ctrl = g
	}
	if ctrl != nil && d.config.IRQ != 0 {
		ctrl.Disable(d.config.IRQ)
		ctrl.Register(d.config.IRQ, nil)
	}
	d.irqGIC = nil
	if !d.paused && d.Err() == nil && d.started {
		d.io.Write32(channelInterrupt, 0)
	}
	d.irqMu.Unlock()
}

func (d *Device) signalRXReady() {
	if d.rxReady == nil {
		return
	}
	select {
	case d.rxReady <- struct{}{}:
	default:
	}
}

// rearmRXIRQLocked re-enables RX DMA interrupts after the ring drains and
// immediately re-reads the head descriptor so a packet arriving in the masked
// window is never missed. Caller must hold d.mu.
func (d *Device) rearmRXIRQLocked(desc uint32) uint32 {
	d.irqMu.Lock()
	defer d.irqMu.Unlock()
	if !d.irqActive.Load() {
		return own
	}
	d.io.Write32(channelInterrupt, irqNIE|irqRIE)
	// Order the Device MMIO unmask before re-reading the Normal-NC descriptor.
	d.memory.Barrier()
	return d.memory.Read32(desc + 12)
}

// suspendIRQLocked pauses IRQ delivery before DMA stop / platform containment,
// retaining d.irqGIC so resumeIRQLocked restores it on BindControl.Start.
// Caller must hold d.mu.
func (d *Device) suspendIRQLocked() {
	d.irqMu.Lock()
	defer d.irqMu.Unlock()
	if d.irqGIC == nil {
		return
	}
	d.irqActive.Store(false)
	d.irqGIC.Disable(d.config.IRQ)
	d.io.Write32(channelInterrupt, 0)
}

// resumeIRQLocked restores RX IRQ delivery after BindControl.Start completes
// driver reinitialization and platform confirmation. Caller must hold d.mu.
func (d *Device) resumeIRQLocked() {
	d.irqMu.Lock()
	if d.irqGIC == nil {
		d.irqMu.Unlock()
		return
	}
	d.io.Write32(channelStatus, statusRI|statusNIS)
	d.irqActive.Store(true)
	d.io.Write32(channelInterrupt, irqNIE|irqRIE)
	d.irqGIC.Enable(d.config.IRQ)
	d.irqMu.Unlock()
	d.signalRXReady()
}

// retireIRQLocked permanently disables and unregisters the GIC SPI on fatal
// fault or retirement. When allowMMIO is false (e.g. retireControl after a
// platform boundary), it never touches XGMAC registers.
func (d *Device) retireIRQLocked(allowMMIO bool) {
	d.irqMu.Lock()
	d.irqActive.Store(false)
	if d.irqGIC != nil {
		d.irqGIC.Disable(d.config.IRQ)
		d.irqGIC.Register(d.config.IRQ, nil)
		d.irqGIC = nil
	}
	if allowMMIO {
		d.io.Write32(channelInterrupt, 0)
	}
	d.irqMu.Unlock()
	d.signalRXReady()
}
