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

// The descriptor is copied so the report schema cannot change under a live
// owner. A menu/core decoder constructs its own state from these same bytes.
func (g *GamepadHost) ReportDescriptor() []byte {
	if g == nil || g.host == nil {
		return nil
	}
	return append([]byte(nil), g.host.reportDescriptor...)
}
