// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

var ErrPollingUnsupported = errors.New("usbcontrol: report driver cannot pause polling")

type pollingDriver interface{ pollingControl() input.PollingControl }

func (d keyboardDriver) pollingControl() input.PollingControl {
	c, _ := d.USBReports.(input.PollingControl)
	return c
}
func (d gamepadDriver) pollingControl() input.PollingControl {
	c, _ := d.USBGamepadReports.(input.PollingControl)
	return c
}

func (s *Source) PausePolling(ctx context.Context) error {
	if s == nil {
		return ErrRetired
	}
	return s.lease.changePolling(ctx, true)
}
func (s *Source) ResumePolling(ctx context.Context) error {
	if s == nil {
		return ErrRetired
	}
	return s.lease.changePolling(ctx, false)
}
func (s *GamepadSource) PausePolling(ctx context.Context) error {
	if s == nil {
		return ErrRetired
	}
	return s.lease.changePolling(ctx, true)
}
func (s *GamepadSource) ResumePolling(ctx context.Context) error {
	if s == nil {
		return ErrRetired
	}
	return s.lease.changePolling(ctx, false)
}

func (s *reportLease) changePolling(ctx context.Context, paused bool) error {
	if s == nil {
		return ErrRetired
	}
	if ctx == nil {
		return ErrTransition
	}
	if err := ctx.Err(); err != nil {
		return err
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
	if s.paused == paused {
		return ErrTransition
	}
	d, ok := s.driver.(pollingDriver)
	if !ok || d.pollingControl() == nil {
		return ErrPollingUnsupported
	}
	control := d.pollingControl()
	var err error
	if paused {
		err = control.PausePolling(ctx)
	} else {
		err = control.ResumePolling(ctx)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		s.fault = err
		return err
	}
	s.paused = paused
	return nil
}
