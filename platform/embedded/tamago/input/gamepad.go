// SPDX-License-Identifier: GPL-3.0-or-later
package input

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// USBGamepadReports delivers gamepad reports under the selected USB owner.
// It deliberately has no keyboard-report method.
type USBGamepadReports interface{ PollGamepad() ([]byte, error) }
type GamepadButtons struct{ Confirm, Back uint8 }
type USBGamepad struct {
	source  USBGamepadReports
	decoder *usb.Gamepad
	buttons GamepadButtons
	pressed [6]bool
	state   usb.GamepadState
}

func NewUSBGamepad(source USBGamepadReports, descriptor []byte, buttons GamepadButtons) (*USBGamepad, error) {
	if source == nil || buttons.Confirm == 0 || buttons.Confirm > 32 || buttons.Back == 0 || buttons.Back > 32 || buttons.Confirm == buttons.Back {
		return nil, fmt.Errorf("USB gamepad source and distinct1..32 menu buttons required")
	}
	decoder, err := usb.ParseGamepad(descriptor)
	if err != nil {
		return nil, err
	}
	return &USBGamepad{source: source, decoder: decoder, buttons: buttons, state: usb.GamepadState{Hat: -1}}, nil
}

// State is a value snapshot for a core-specific input route. Menu navigation
// does not convert gamepad state into a boot-keyboard report.
func (g *USBGamepad) State() usb.GamepadState { return g.state }
func (g *USBGamepad) Decode(report []byte) ([]Event, error) {
	state, _, err := g.decoder.Feed(report)
	if err != nil {
		return nil, err
	}
	next := [6]bool{}
	const threshold = 16384
	next[0], next[1] = state.Axes[1] < -threshold, state.Axes[1] > threshold
	next[2], next[3] = state.Axes[0] < -threshold, state.Axes[0] > threshold
	if state.Hat >= 0 {
		next[0] = state.Hat == 7 || state.Hat <= 1
		next[1] = state.Hat >= 3 && state.Hat <= 5
		next[2] = state.Hat >= 5 && state.Hat <= 7
		next[3] = state.Hat >= 1 && state.Hat <= 3
	}
	next[4] = state.Buttons&(uint32(1)<<(g.buttons.Confirm-1)) != 0
	next[5] = state.Buttons&(uint32(1)<<(g.buttons.Back-1)) != 0
	names := [6]string{"up", "down", "left", "right", "return", "escape"}
	var events []Event
	for i, down := range next {
		if down && !g.pressed[i] {
			events = append(events, Event{Key: names[i]})
		}
	}
	g.pressed = next
	g.state = state
	return events, nil
}
func (g *USBGamepad) Poll() ([]Event, error) {
	var events []Event
	for n := 0; n < 8; n++ {
		r, err := g.source.PollGamepad()
		if err != nil {
			return nil, err
		}
		if r == nil {
			return events, nil
		}
		e, err := g.Decode(r)
		if err != nil {
			return nil, err
		}
		events = append(events, e...)
	}
	return events, nil
}
