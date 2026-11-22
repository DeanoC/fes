// SPDX-License-Identifier: GPL-3.0-or-later

package sdhci

import (
	"errors"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

// EnableIRQ binds this SDHCI controller to a GICv3 SPI (for example
// de25nano.IRQSDMMC = 128 on Agilex 5) so command and data waits sleep on the
// interrupt wakeup channel instead of busy-polling.
//
// The level-triggered handler immediately masks signalEnable=0 and leaves
// status bits latched for wait() to inspect and clear, so signalEnable stays 0
// whenever no wait is active (preserving clock-transition and quiesce checks).
func (d *Device) EnableIRQ(g *gic.Controller, irqID uint32) error {
	if g == nil {
		return errors.New("sdhci: nil GIC controller")
	}
	if irqID >= g.Lines() {
		return gic.ErrInvalidConfig
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failed {
		return ErrRetired
	}
	if d.quiesced || d.stopRequested.Load() {
		return ErrQuiesced
	}
	if d.irqGIC != nil {
		return fmt.Errorf("sdhci: IRQ already enabled on INTID %d", d.irqID)
	}
	if d.irqWake == nil {
		d.irqWake = make(chan struct{}, 1)
	}
	d.io.Write32(signalEnable, 0)
	g.Disable(irqID)
	d.irqCount.Store(0)
	g.Register(irqID, func() {
		// Mask SDHCI interrupt signals immediately so the level-sensitive SPI
		// cannot re-trigger before the goroutine inspects and clears status.
		d.io.Write32(signalEnable, 0)
		d.irqCount.Add(1)
		select {
		case d.irqWake <- struct{}{}:
		default:
		}
	})
	d.irqGIC = g
	d.irqID = irqID
	d.irqActive.Store(true)
	g.Enable(irqID)
	return nil
}

// DisableIRQ masks SDHCI interrupt signals and unregisters the GICv3 SPI.
func (d *Device) DisableIRQ() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disableIRQLocked()
}

func (d *Device) disableIRQLocked() {
	d.irqActive.Store(false)
	if !d.failed && !d.quiesced {
		d.io.Write32(signalEnable, 0)
	}
	if d.irqGIC != nil {
		d.irqGIC.Disable(d.irqID)
		d.irqGIC.Register(d.irqID, nil)
		d.irqGIC = nil
		d.irqID = 0
	}
}

// IRQCount returns the number of SDHCI interrupts handled since EnableIRQ.
func (d *Device) IRQCount() uint64 { return d.irqCount.Load() }
