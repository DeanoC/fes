// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
)

type irqControl interface {
	EnableIRQ(*gic.Controller, uint32) error
	DisableIRQ()
	Ready() <-chan struct{}
	IRQCount() uint64
}

type guardedIRQControl interface {
	EnableIRQGuarded(*gic.Controller, uint32, func() bool) error
	RetireIRQ()
}

type irqDriver interface{ irqControl() irqControl }

func (d keyboardDriver) irqControl() irqControl {
	c, _ := d.USBReports.(irqControl)
	return c
}
func (d gamepadDriver) irqControl() irqControl {
	c, _ := d.USBGamepadReports.(irqControl)
	return c
}

// EnableIRQ binds the owned report driver to a GICv3 SPI while this lease is
// active, confirmed and unpaused. Returns gic.ErrUnsupported if the underlying
// report driver does not support interrupt wakeups.
func (s *Source) EnableIRQ(g *gic.Controller, irqID uint32) error {
	if s == nil {
		return ErrRetired
	}
	return s.lease.enableIRQ(g, irqID)
}

// DisableIRQ masks and unregisters any GICv3 SPI attached to this source.
func (s *Source) DisableIRQ() {
	if s == nil {
		return
	}
	s.lease.disableIRQ()
}

// Ready returns the driver's interrupt wakeup channel when IRQ mode is active.
func (s *Source) Ready() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.lease.ready()
}

// IRQCount returns the number of interrupts handled by the underlying driver.
func (s *Source) IRQCount() uint64 {
	if s == nil {
		return 0
	}
	return s.lease.irqCount()
}

// EnableIRQ binds the owned gamepad report driver to a GICv3 SPI while this
// lease is active, confirmed and unpaused.
func (s *GamepadSource) EnableIRQ(g *gic.Controller, irqID uint32) error {
	if s == nil {
		return ErrRetired
	}
	return s.lease.enableIRQ(g, irqID)
}

// DisableIRQ masks and unregisters any GICv3 SPI attached to this gamepad source.
func (s *GamepadSource) DisableIRQ() {
	if s == nil {
		return
	}
	s.lease.disableIRQ()
}

// Ready returns the gamepad driver's interrupt wakeup channel when IRQ mode is active.
func (s *GamepadSource) Ready() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.lease.ready()
}

// IRQCount returns the number of interrupts handled by the underlying gamepad driver.
func (s *GamepadSource) IRQCount() uint64 {
	if s == nil {
		return 0
	}
	return s.lease.irqCount()
}

func (s *reportLease) enableIRQ(g *gic.Controller, irqID uint32) error {
	if s == nil {
		return ErrRetired
	}
	if !s.operation.TryLock() {
		return ErrBusy
	}
	defer s.operation.Unlock()
	if s.closed || s.fault != nil || s.driver == nil {
		return errors.Join(ErrRetired, s.fault)
	}
	c := s.client
	if !c.operation.TryLock() {
		return ErrBusy
	}
	defer c.operation.Unlock()
	current, valid := c.LastReceipt()
	if !valid || current != s.receipt || !current.Active || !current.Confirmed {
		return ErrRetired
	}
	if s.paused {
		return ErrTransition
	}
	d, ok := s.driver.(irqDriver)
	if !ok || d.irqControl() == nil {
		return gic.ErrUnsupported
	}
	guarded, ok := d.irqControl().(guardedIRQControl)
	if !ok {
		return gic.ErrUnsupported
	}
	return guarded.EnableIRQGuarded(g, irqID, func() bool {
		// A public Client.Stop can invalidate this source without going through
		// Source.Close. The DWC2 handler checks this receipt before touching MMIO.
		current, valid := c.LastReceipt()
		return valid && current == s.receipt && current.Active && current.Confirmed
	})
}

func (s *reportLease) disableIRQ() {
	if s == nil {
		return
	}
	s.operation.Lock()
	defer s.operation.Unlock()
	if s.client == nil {
		return
	}
	s.client.operation.Lock()
	defer s.client.operation.Unlock()
	s.disableIRQLocked()
}

func (s *reportLease) disableIRQLocked() {
	if s.driver == nil {
		return
	}
	if d, ok := s.driver.(irqDriver); ok {
		if ctrl := d.irqControl(); ctrl != nil {
			current, valid := s.client.LastReceipt()
			if valid && current == s.receipt && current.Active && current.Confirmed {
				ctrl.DisableIRQ()
				return
			}
			if guarded, ok := ctrl.(guardedIRQControl); ok {
				guarded.RetireIRQ()
			}
		}
	}
}

func (s *reportLease) retireIRQLocked() {
	if s == nil || s.driver == nil {
		return
	}
	if d, ok := s.driver.(irqDriver); ok {
		if ctrl, ok := d.irqControl().(guardedIRQControl); ok {
			ctrl.RetireIRQ()
		}
	}
}

func (s *reportLease) ready() <-chan struct{} {
	if s == nil {
		return nil
	}
	s.operation.Lock()
	defer s.operation.Unlock()
	if s.closed || s.fault != nil || s.driver == nil {
		return nil
	}
	if s.client == nil || !s.client.operation.TryLock() {
		return nil
	}
	defer s.client.operation.Unlock()
	current, valid := s.client.LastReceipt()
	if !valid || current != s.receipt || !current.Active || !current.Confirmed {
		s.retireIRQLocked()
		return nil
	}
	if d, ok := s.driver.(irqDriver); ok {
		if ctrl := d.irqControl(); ctrl != nil {
			return ctrl.Ready()
		}
	}
	return nil
}

func (s *reportLease) irqCount() uint64 {
	if s == nil {
		return 0
	}
	s.operation.Lock()
	defer s.operation.Unlock()
	if s.driver == nil {
		return 0
	}
	if s.client == nil || !s.client.operation.TryLock() {
		return 0
	}
	defer s.client.operation.Unlock()
	current, valid := s.client.LastReceipt()
	if !valid || current != s.receipt || !current.Active || !current.Confirmed {
		s.retireIRQLocked()
		return 0
	}
	if d, ok := s.driver.(irqDriver); ok {
		if ctrl := d.irqControl(); ctrl != nil {
			return ctrl.IRQCount()
		}
	}
	return 0
}
