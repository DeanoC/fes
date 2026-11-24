// SPDX-License-Identifier: GPL-3.0-or-later
package usbcontrol

import (
	"context"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
)

func copyReportStrict(dst, r []byte) (int, error) {
	if len(r) == 0 || len(r) > len(dst) {
		return 0, fmt.Errorf("usbcontrol: invalid report length %d for buffer %d", len(r), len(dst))
	}
	return copy(dst, r), nil
}

// Both typed views share the same ownership state machine. Neither view can
// poll the other protocol, and every view retains its failed partial driver.
type reportDriver interface {
	pollReport() ([]byte, error)
	pollReportInto([]byte) (int, error)
}
type keyboardDriver struct{ input.USBReports }

func (d keyboardDriver) pollReport() ([]byte, error) { return d.PollKeyboard() }
func (d keyboardDriver) pollReportInto(dst []byte) (int, error) {
	if fast, ok := d.USBReports.(interface{ PollKeyboardInto([]byte) (int, error) }); ok {
		return fast.PollKeyboardInto(dst)
	}
	r, err := d.PollKeyboard()
	if err != nil || r == nil {
		return 0, err
	}
	return copyReportStrict(dst, r)
}

type gamepadDriver struct{ input.USBGamepadReports }

func (d gamepadDriver) pollReport() ([]byte, error) { return d.PollGamepad() }
func (d gamepadDriver) pollReportInto(dst []byte) (int, error) {
	if fast, ok := d.USBGamepadReports.(interface{ PollGamepadInto([]byte) (int, error) }); ok {
		return fast.PollGamepadInto(dst)
	}
	r, err := d.PollGamepad()
	if err != nil || r == nil {
		return 0, err
	}
	return copyReportStrict(dst, r)
}

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
func (s *Source) PollKeyboardInto(dst []byte) (int, error) {
	if s == nil || s.lease == nil {
		return 0, ErrRetired
	}
	return s.lease.pollInto(dst)
}
func (s *GamepadSource) PollGamepad() ([]byte, error) {
	if s == nil || s.lease == nil {
		return nil, ErrRetired
	}
	return s.lease.poll()
}
func (s *GamepadSource) PollGamepadInto(dst []byte) (int, error) {
	if s == nil || s.lease == nil {
		return 0, ErrRetired
	}
	return s.lease.pollInto(dst)
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
