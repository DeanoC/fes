// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

type busDriver struct{ usb.BusHost }

func (d busDriver) pollReport() ([]byte, error)     { return nil, d.Service() }
func (d busDriver) pollReportInto([]byte) (int, error) { return 0, d.Service() }
func (d busDriver) pollingControl() input.PollingControl {
	c, _ := d.BusHost.(input.PollingControl)
	return c
}
func (d busDriver) irqControl() irqControl {
	c, _ := d.BusHost.(irqControl)
	return c
}

// BusHost binds an exclusive multi-device usb.BusHost (such as dwc2.MultiHost)
// to an EL3 USB firmware PIO lease. Retain BusHost and Client until reboot,
// including when OpenBus returns an error.
type BusHost struct {
	lease *reportLease
	host  usb.BusHost
}

// OpenBus acquires and confirms the EL3 USB PIO lease around factory, returning
// a lease-guarded usb.BusHost with GICv3 IRQ and PausePolling/ResumePolling
// support.
func OpenBus(ctx context.Context, c *Client, factory func() (usb.BusHost, error)) (*BusHost, error) {
	if factory == nil {
		return nil, ErrTransition
	}
	var host usb.BusHost
	lease, err := openReports(ctx, c, func() (reportDriver, error) {
		var derr error
		host, derr = factory()
		if host == nil {
			return nil, derr
		}
		return busDriver{host}, derr
	})
	if lease == nil {
		return nil, err
	}
	return &BusHost{lease: lease, host: host}, err
}

func (b *BusHost) checkActiveLocked() error {
	s := b.lease
	if s.closed || s.driver == nil || b.host == nil {
		return errors.Join(ErrRetired, s.fault)
	}
	if s.fault != nil {
		return errors.Join(ErrRetired, s.fault)
	}
	current, valid := s.client.LastReceipt()
	if !valid || current != s.receipt || !current.Active || !current.Confirmed {
		return ErrRetired
	}
	if s.paused {
		return ErrTransition
	}
	return nil
}

// Service advances the underlying bus host while the lease is active, confirmed
// and unpaused. If the lease is busy or paused, Service is a non-blocking no-op.
func (b *BusHost) Service() error {
	if b == nil || b.lease == nil {
		return ErrRetired
	}
	s := b.lease
	if !s.operation.TryLock() {
		return nil
	}
	defer s.operation.Unlock()
	if s.closed || s.driver == nil || b.host == nil {
		return errors.Join(ErrRetired, s.fault)
	}
	if s.fault != nil {
		return errors.Join(ErrRetired, s.fault)
	}
	c := s.client
	if !c.operation.TryLock() {
		return nil
	}
	defer c.operation.Unlock()
	current, valid := c.LastReceipt()
	if !valid || current != s.receipt || !current.Active || !current.Confirmed {
		return ErrRetired
	}
	if s.paused {
		return nil
	}
	if err := b.host.Service(); err != nil {
		s.fault = err
		return err
	}
	return nil
}

// NextEvent pops the next queued dynamic attach/detach event while active.
func (b *BusHost) NextEvent() (usb.HostEvent, bool) {
	if b == nil || b.lease == nil || b.host == nil {
		return usb.HostEvent{}, false
	}
	s := b.lease
	if !s.operation.TryLock() {
		return usb.HostEvent{}, false
	}
	defer s.operation.Unlock()
	if !s.client.operation.TryLock() {
		return usb.HostEvent{}, false
	}
	defer s.client.operation.Unlock()
	if b.checkActiveLocked() != nil {
		return usb.HostEvent{}, false
	}
	return b.host.NextEvent()
}

// Devices returns currently enumerated non-hub devices while active.
func (b *BusHost) Devices() []usb.DeviceInfo {
	if b == nil || b.lease == nil || b.host == nil {
		return nil
	}
	s := b.lease
	if !s.operation.TryLock() {
		return nil
	}
	defer s.operation.Unlock()
	if !s.client.operation.TryLock() {
		return nil
	}
	defer s.client.operation.Unlock()
	if b.checkActiveLocked() != nil {
		return nil
	}
	return b.host.Devices()
}

// ControlTransfer executes a bounded EP0 control transfer while the lease is
// active, confirmed and unpaused.
func (b *BusHost) ControlTransfer(addr uint8, reqType, req byte, value, index uint16, data []byte) (int, error) {
	if b == nil || b.lease == nil {
		return 0, ErrRetired
	}
	s := b.lease
	if !s.operation.TryLock() {
		return 0, ErrBusy
	}
	defer s.operation.Unlock()
	if !s.client.operation.TryLock() {
		return 0, ErrBusy
	}
	defer s.client.operation.Unlock()
	if err := b.checkActiveLocked(); err != nil {
		return 0, err
	}
	return b.host.ControlTransfer(addr, reqType, req, value, index, data)
}

// OpenPipe allocates a hardware pipe on an enumerated device while the lease is
// active, confirmed and unpaused.
func (b *BusHost) OpenPipe(addr uint8, ep usb.EndpointDescriptor) (usb.Pipe, error) {
	if b == nil || b.lease == nil {
		return nil, ErrRetired
	}
	s := b.lease
	if !s.operation.TryLock() {
		return nil, ErrBusy
	}
	defer s.operation.Unlock()
	if !s.client.operation.TryLock() {
		return nil, ErrBusy
	}
	defer s.client.operation.Unlock()
	if err := b.checkActiveLocked(); err != nil {
		return nil, err
	}
	return b.host.OpenPipe(addr, ep)
}

// EnableIRQ binds the underlying multi-channel host to a GICv3 SPI guarded by
// this EL3 lease receipt.
func (b *BusHost) EnableIRQ(g *gic.Controller, irqID uint32) error {
	if b == nil {
		return ErrRetired
	}
	return b.lease.enableIRQ(g, irqID)
}

// DisableIRQ masks and unregisters any GICv3 SPI attached to this bus host.
func (b *BusHost) DisableIRQ() {
	if b == nil {
		return
	}
	b.lease.disableIRQ()
}

// Ready returns the driver's interrupt wakeup channel when IRQ mode is active.
func (b *BusHost) Ready() <-chan struct{} {
	if b == nil {
		return nil
	}
	return b.lease.ready()
}

// IRQCount returns the number of interrupts handled by the underlying host.
func (b *BusHost) IRQCount() uint64 {
	if b == nil {
		return 0
	}
	return b.lease.irqCount()
}

// PausePolling halts and drains all active hardware channels before FPGA
// reconfiguration or firmware Stop.
func (b *BusHost) PausePolling(ctx context.Context) error {
	if b == nil {
		return ErrRetired
	}
	return b.lease.changePolling(ctx, true)
}

// ResumePolling exits the paused state without re-enumerating devices.
func (b *BusHost) ResumePolling(ctx context.Context) error {
	if b == nil {
		return ErrRetired
	}
	return b.lease.changePolling(ctx, false)
}

// Close revokes local bus access, drains polling, stops the EL3 lease, and
// disables/retires any registered GICv3 SPI.
func (b *BusHost) Close(ctx context.Context) error {
	if b == nil || b.lease == nil {
		return ErrRetired
	}
	return b.lease.close(ctx)
}

var _ usb.BusHost = (*BusHost)(nil)
var _ input.PollingControl = (*BusHost)(nil)
