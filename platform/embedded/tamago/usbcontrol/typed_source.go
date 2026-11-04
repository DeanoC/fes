// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

// Both typed views share the same ownership state machine. Neither view can
// poll the other protocol, and every view retains its failed partial driver.
type reportDriver interface{ pollReport() ([]byte, error) }
type keyboardDriver struct{ input.USBReports }

func (d keyboardDriver) pollReport() ([]byte, error) { return d.PollKeyboard() }

type gamepadDriver struct{ input.USBGamepadReports }

func (d gamepadDriver) pollReport() ([]byte, error) { return d.PollGamepad() }

type Source struct{ lease *reportLease }
type GamepadSource struct{ lease *reportLease }

func Open(ctx context.Context, c *Client, factory func() (input.USBReports, error)) (*Source, error) {
	if factory == nil {
		return nil, ErrTransition
	}
	lease, err := openReports(ctx, c, func() (reportDriver, error) {
		d, err := factory()
		if d == nil {
			return nil, err
		}
		return keyboardDriver{d}, err
	})
	if lease == nil {
		return nil, err
	}
	return &Source{lease}, err
}
func OpenGamepad(ctx context.Context, c *Client, factory func() (input.USBGamepadReports, error)) (*GamepadSource, error) {
	if factory == nil {
		return nil, ErrTransition
	}
	lease, err := openReports(ctx, c, func() (reportDriver, error) {
		d, err := factory()
		if d == nil {
			return nil, err
		}
		return gamepadDriver{d}, err
	})
	if lease == nil {
		return nil, err
	}
	return &GamepadSource{lease}, err
}
func (s *Source) PollKeyboard() ([]byte, error) {
	if s == nil || s.lease == nil {
		return nil, ErrRetired
	}
	return s.lease.poll()
}
func (s *GamepadSource) PollGamepad() ([]byte, error) {
	if s == nil || s.lease == nil {
		return nil, ErrRetired
	}
	return s.lease.poll()
}
func (s *Source) Close(ctx context.Context) error {
	if s == nil || s.lease == nil {
		return ErrRetired
	}
	return s.lease.close(ctx)
}
func (s *GamepadSource) Close(ctx context.Context) error {
	if s == nil || s.lease == nil {
		return ErrRetired
	}
	return s.lease.close(ctx)
}
