// SPDX-License-Identifier: GPL-3.0-or-later
package controllers

import "encoding/binary"

const (
	// XInputInputLen is the standard 20-byte wired Xbox 360 XInput report size.
	XInputInputLen = 20
	// XInputLEDOutputLen is the 3-byte XInput player ring LED command length.
	XInputLEDOutputLen = 3
	// XInputRumbleOutputLen is the 8-byte XInput rumble command length.
	XInputRumbleOutputLen = 8
)

// ParseXInput decodes a wired Xbox 360 / XInput interrupt-IN report (class
// 0xff/0x5d/0x01) into out with zero heap allocations.
func ParseXInput(report []byte, out *State) bool {
	if out == nil || len(report) < XInputInputLen || report[0] != 0x00 || report[1] < XInputInputLen {
		return false
	}
	b2, b3 := report[2], report[3]
	var b Button
	if b2&0x01 != 0 {
		b |= ButtonDPadUp
	}
	if b2&0x02 != 0 {
		b |= ButtonDPadDown
	}
	if b2&0x04 != 0 {
		b |= ButtonDPadLeft
	}
	if b2&0x08 != 0 {
		b |= ButtonDPadRight
	}
	if b2&0x10 != 0 {
		b |= ButtonStart
	}
	if b2&0x20 != 0 {
		b |= ButtonSelect
	}
	if b2&0x40 != 0 {
		b |= ButtonL3
	}
	if b2&0x80 != 0 {
		b |= ButtonR3
	}
	if b3&0x01 != 0 {
		b |= ButtonL1
	}
	if b3&0x02 != 0 {
		b |= ButtonR1
	}
	if b3&0x04 != 0 {
		b |= ButtonHome
	}
	if b3&0x10 != 0 {
		b |= ButtonSouth // A
	}
	if b3&0x20 != 0 {
		b |= ButtonEast // B
	}
	if b3&0x40 != 0 {
		b |= ButtonWest // X
	}
	if b3&0x80 != 0 {
		b |= ButtonNorth // Y
	}
	lt, rt := report[4], report[5]
	if lt >= 128 {
		b |= ButtonL2
	}
	if rt >= 128 {
		b |= ButtonR2
	}

	*out = State{
		Buttons:      b,
		LeftX:        int16(binary.LittleEndian.Uint16(report[6:8])),
		LeftY:        negateS16(int16(binary.LittleEndian.Uint16(report[8:10]))),
		RightX:       int16(binary.LittleEndian.Uint16(report[10:12])),
		RightY:       negateS16(int16(binary.LittleEndian.Uint16(report[12:14]))),
		LeftTrigger:  lt,
		RightTrigger: rt,
	}
	return true
}

// BuildXInputLED writes a 3-byte XInput player ring LED command (slot 0..3 -> solid LED 1..4).
func BuildXInputLED(dst []byte, playerSlot uint8) int {
	if len(dst) < XInputLEDOutputLen {
		return 0
	}
	dst[0] = 0x01
	dst[1] = 0x03
	dst[2] = 0x06 + (playerSlot & 3)
	return XInputLEDOutputLen
}

// BuildXInputRumble writes an 8-byte XInput rumble command.
func BuildXInputRumble(dst []byte, leftHeavy, rightLight uint8) int {
	if len(dst) < XInputRumbleOutputLen {
		return 0
	}
	dst[0] = 0x00
	dst[1] = 0x08
	dst[2] = 0x00
	dst[3] = leftHeavy
	dst[4] = rightLight
	dst[5] = 0x00
	dst[6] = 0x00
	dst[7] = 0x00
	return XInputRumbleOutputLen
}
