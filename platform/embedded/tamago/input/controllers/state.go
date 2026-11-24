// SPDX-License-Identifier: GPL-3.0-or-later
package controllers

import (
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// Kind identifies a first-class gaming controller protocol supported by the
// embedded input stack (both Classic BT HIDP and wired USB).
type Kind uint8

const (
	KindUnknown Kind = iota
	KindDualSense
	KindDualShock4
	KindSwitchPro
	KindGenericHID
)

func (k Kind) String() string {
	switch k {
	case KindDualSense:
		return "dualsense"
	case KindDualShock4:
		return "dualshock4"
	case KindSwitchPro:
		return "switchpro"
	case KindGenericHID:
		return "hid"
	default:
		return "unknown"
	}
}

// Button is a canonical low-latency controller button bitmask.
type Button uint32

const (
	ButtonSouth Button = 1 << iota // Cross (PS) / B (Nintendo) / A (Xbox)
	ButtonEast                     // Circle (PS) / A (Nintendo) / B (Xbox)
	ButtonWest                     // Square (PS) / Y (Nintendo) / X (Xbox)
	ButtonNorth                    // Triangle (PS) / X (Nintendo) / Y (Xbox)
	ButtonL1
	ButtonR1
	ButtonL2
	ButtonR2
	ButtonSelect   // Create / Share / Minus / View
	ButtonStart    // Options / Plus / Menu
	ButtonL3
	ButtonR3
	ButtonHome     // PS / Home / Guide
	ButtonCapture  // Mute / Capture
	ButtonTouchpad // DualSense/DS4 touchpad click
	ButtonDPadUp
	ButtonDPadDown
	ButtonDPadLeft
	ButtonDPadRight
)

// State is a zero-pointer, zero-allocation snapshot of one gamepad's inputs.
// Axes use signed 16-bit full-scale coordinates (-32768..32767, center 0,
// negative up/left, positive down/right), matching usb.GamepadState.
type State struct {
	Buttons      Button
	LeftX        int16
	LeftY        int16
	RightX       int16
	RightY       int16
	LeftTrigger  uint8
	RightTrigger uint8
	Battery      uint8 // 0..100 percent (approximate)
	Charging     bool
	TimestampNS  int64
}

// Neutral reports whether no buttons, D-Pad directions, or stick deflections
// beyond deadzone are active (used for menu/core ownership transition fences).
func (s State) Neutral() bool {
	if s.Buttons != 0 {
		return false
	}
	const deadzone = 16384
	return s.LeftX > -deadzone && s.LeftX < deadzone &&
		s.LeftY > -deadzone && s.LeftY < deadzone
}

// ToUSBGamepadState converts State into the normalized usb.GamepadState used
// by Tenfoot menu navigation and existing ProgramGamepad routes.
func (s State) ToUSBGamepadState() usb.GamepadState {
	hat := int8(-1)
	up := s.Buttons&ButtonDPadUp != 0
	down := s.Buttons&ButtonDPadDown != 0
	left := s.Buttons&ButtonDPadLeft != 0
	right := s.Buttons&ButtonDPadRight != 0
	switch {
	case up && right:
		hat = 1
	case down && right:
		hat = 3
	case down && left:
		hat = 5
	case up && left:
		hat = 7
	case up:
		hat = 0
	case right:
		hat = 2
	case down:
		hat = 4
	case left:
		hat = 6
	}
	return usb.GamepadState{
		Axes: [6]int16{
			s.LeftX,
			s.LeftY,
			s.RightX,
			s.RightY,
			int16(int32(s.LeftTrigger)<<8 - 32768),
			int16(int32(s.RightTrigger)<<8 - 32768),
		},
		Buttons: uint32(s.Buttons & 0x7fff),
		Hat:     hat,
	}
}

// ToComputerMask packs directional + 4 primary action bits into the 8-bit
// fes.computer ControllerButtons opcode mask:
// bit0=Right, bit1=Left, bit2=Down, bit3=Up, bit4=South, bit5=East, bit6=Start, bit7=Select.
func (s State) ToComputerMask() uint8 {
	const threshold = 16384
	var mask uint8
	if s.Buttons&ButtonDPadRight != 0 || s.LeftX > threshold {
		mask |= 1 << 0
	}
	if s.Buttons&ButtonDPadLeft != 0 || s.LeftX < -threshold {
		mask |= 1 << 1
	}
	if s.Buttons&ButtonDPadDown != 0 || s.LeftY > threshold {
		mask |= 1 << 2
	}
	if s.Buttons&ButtonDPadUp != 0 || s.LeftY < -threshold {
		mask |= 1 << 3
	}
	if s.Buttons&ButtonSouth != 0 {
		mask |= 1 << 4
	}
	if s.Buttons&ButtonEast != 0 {
		mask |= 1 << 5
	}
	if s.Buttons&ButtonStart != 0 {
		mask |= 1 << 6
	}
	if s.Buttons&ButtonSelect != 0 {
		mask |= 1 << 7
	}
	return mask
}

// IdentifyByVIDPID classifies known gaming controllers by USB/BT PnP VID:PID.
func IdentifyByVIDPID(vid, pid uint16) Kind {
	switch vid {
	case 0x054c: // Sony Interactive Entertainment
		switch pid {
		case 0x0ce6, 0x0df2: // DualSense, DualSense Edge
			return KindDualSense
		case 0x05c4, 0x09cc, 0x0ba0: // DualShock 4 v1/v2/dongle
			return KindDualShock4
		}
	case 0x057e: // Nintendo
		switch pid {
		case 0x2009: // Switch Pro Controller
			return KindSwitchPro
		}
	}
	return KindUnknown
}

// IdentifyByName classifies Classic Bluetooth controllers from their HCI Remote
// Name response when SDP/PnP IDs are not queried.
func IdentifyByName(name string) Kind {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "dualsense"):
		return KindDualSense
	case lower == "wireless controller":
		return KindDualShock4
	case strings.Contains(lower, "pro controller"):
		return KindSwitchPro
	default:
		return KindUnknown
	}
}

func u8AxisToS16(v uint8) int16 {
	// Map 0..255 with center 128 -> -32768..32512.
	return int16(int32(v)-128) << 8
}

func hatNibbleToDPad(hat uint8) Button {
	switch hat & 0x0f {
	case 0:
		return ButtonDPadUp
	case 1:
		return ButtonDPadUp | ButtonDPadRight
	case 2:
		return ButtonDPadRight
	case 3:
		return ButtonDPadDown | ButtonDPadRight
	case 4:
		return ButtonDPadDown
	case 5:
		return ButtonDPadDown | ButtonDPadLeft
	case 6:
		return ButtonDPadLeft
	case 7:
		return ButtonDPadUp | ButtonDPadLeft
	default:
		return 0
	}
}
