// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import "fmt"

// GamepadHost is a typed raw-report view. It never implements PollKeyboard.
type GamepadHost struct{ host *Host }

func OpenGamepad(io Registers, config Config) (*GamepadHost, error) {
	h, err := openInput(io, config, true)
	if h == nil {
		return nil, err
	}
	return &GamepadHost{h}, err
}
func (g *GamepadHost) PollGamepad() ([]byte, error) {
	if g == nil || g.host == nil {
		return nil, fmt.Errorf("dwc2: gamepad host absent")
	}
	return g.host.pollInput()
}

// PollGamepadInto is the zero-allocation fast path for PollGamepad.
func (g *GamepadHost) PollGamepadInto(dst []byte) (int, error) {
	if g == nil || g.host == nil {
		return 0, fmt.Errorf("dwc2: gamepad host absent")
	}
	r, err := g.host.pollInputRaw()
	if err != nil || r == nil {
		return 0, err
	}
	if len(r) == 0 || len(dst) < len(r) {
		return 0, fmt.Errorf("dwc2: invalid gamepad report length %d for buffer %d", len(r), len(dst))
	}
	return copy(dst, r), nil
}

// The descriptor is copied so the report schema cannot change under a live
// owner. A menu/core decoder constructs its own state from these same bytes.
func (g *GamepadHost) ReportDescriptor() []byte {
	if g == nil || g.host == nil {
		return nil
	}
	return append([]byte(nil), g.host.reportDescriptor...)
}
