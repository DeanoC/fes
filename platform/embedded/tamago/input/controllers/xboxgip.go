// SPDX-License-Identifier: GPL-3.0-or-later
package controllers

import "encoding/binary"

const (
	// XboxGIPCommandAck is the host-to-controller acknowledgement opcode.
	XboxGIPCommandAck = 0x01
	// XboxGIPCommandPower is the host-to-controller power-on opcode.
	XboxGIPCommandPower = 0x05
	// XboxGIPCommandGuide is the Xbox Guide button event packet ID.
	XboxGIPCommandGuide = 0x07
	// XboxGIPCommandRumble is the Xbox GIP actuator rumble opcode.
	XboxGIPCommandRumble = 0x09
	// XboxGIPCommandInput is the standard gamepad state report packet ID.
	XboxGIPCommandInput = 0x20

	// XboxGIPPowerLen is the 5-byte power-on packet length.
	XboxGIPPowerLen = 5
	// XboxGIPLedOnLen is the 7-byte GIP LED-on initialization packet length.
	XboxGIPLedOnLen = 7
	// XboxGIPAuthLen is the 6-byte GIP auth-complete initialization packet length.
	XboxGIPAuthLen = 6
	// XboxGIPAckLen is the 13-byte GIP ACK packet length.
	XboxGIPAckLen = 13
	// XboxGIPRumbleLen is the 13-byte GIP rumble packet length.
	XboxGIPRumbleLen = 13
)

// ParseXboxGIPInput decodes a wired Xbox One / Series GIP interrupt-IN packet
// (class 0xff/0x47/0xd0) into out with zero heap allocations. Report 0x20
// preserves an active ButtonHome previously delivered by report 0x07, while
// report 0x07 updates ButtonHome and signals needsAck when the device requested
// an acknowledgement.
func ParseXboxGIPInput(report []byte, out *State) (updated bool, ackSeq uint8, needsAck bool) {
	if out == nil || len(report) < 4 {
		return false, 0, false
	}
	switch report[0] {
	case XboxGIPCommandInput:
		if len(report) < 18 || report[3] < 14 {
			return false, 0, false
		}
		b := out.Buttons & ButtonHome
		b4, b5 := report[4], report[5]
		if b4&0x04 != 0 {
			b |= ButtonStart
		}
		if b4&0x08 != 0 {
			b |= ButtonSelect
		}
		if b4&0x10 != 0 {
			b |= ButtonSouth // A
		}
		if b4&0x20 != 0 {
			b |= ButtonEast // B
		}
		if b4&0x40 != 0 {
			b |= ButtonWest // X
		}
		if b4&0x80 != 0 {
			b |= ButtonNorth // Y
		}
		if b5&0x01 != 0 {
			b |= ButtonDPadUp
		}
		if b5&0x02 != 0 {
			b |= ButtonDPadDown
		}
		if b5&0x04 != 0 {
			b |= ButtonDPadLeft
		}
		if b5&0x08 != 0 {
			b |= ButtonDPadRight
		}
		if b5&0x10 != 0 {
			b |= ButtonL1
		}
		if b5&0x20 != 0 {
			b |= ButtonR1
		}
		if b5&0x40 != 0 {
			b |= ButtonL3
		}
		if b5&0x80 != 0 {
			b |= ButtonR3
		}
		if len(report) >= 23 && report[3] >= 0x13 && report[22]&0x01 != 0 {
			b |= ButtonCapture // Series Share
		}
		ltRaw := binary.LittleEndian.Uint16(report[6:8])
		if ltRaw > 1023 {
			ltRaw = 1023
		}
		rtRaw := binary.LittleEndian.Uint16(report[8:10])
		if rtRaw > 1023 {
			rtRaw = 1023
		}
		lt := uint8(ltRaw >> 2)
		rt := uint8(rtRaw >> 2)
		if lt >= 128 {
			b |= ButtonL2
		}
		if rt >= 128 {
			b |= ButtonR2
		}

		out.Buttons = b
		out.LeftTrigger = lt
		out.RightTrigger = rt
		out.LeftX = int16(binary.LittleEndian.Uint16(report[10:12]))
		out.LeftY = negateS16(int16(binary.LittleEndian.Uint16(report[12:14])))
		out.RightX = int16(binary.LittleEndian.Uint16(report[14:16]))
		out.RightY = negateS16(int16(binary.LittleEndian.Uint16(report[16:18])))
		return true, 0, false

	case XboxGIPCommandGuide:
		if len(report) < 6 {
			return false, 0, false
		}
		if report[4]&0x01 != 0 {
			out.Buttons |= ButtonHome
		} else {
			out.Buttons &^= ButtonHome
		}
		return true, report[2], report[1]&0x10 != 0

	default:
		return false, 0, false
	}
}

// BuildXboxGIPPowerOn writes the 5-byte GIP host power-on initialization packet.
func BuildXboxGIPPowerOn(dst []byte, seq uint8) int {
	if len(dst) < XboxGIPPowerLen {
		return 0
	}
	dst[0] = XboxGIPCommandPower
	dst[1] = 0x20
	dst[2] = seq
	dst[3] = 0x01
	dst[4] = 0x00
	return XboxGIPPowerLen
}

// BuildXboxGIPLedOn writes the 7-byte GIP LED-on initialization packet required
// by third-party (e.g. PDP) Xbox One controllers before emitting input reports.
func BuildXboxGIPLedOn(dst []byte, seq uint8) int {
	if len(dst) < XboxGIPLedOnLen {
		return 0
	}
	dst[0] = 0x0a
	dst[1] = 0x20
	dst[2] = seq
	dst[3] = 0x03
	dst[4] = 0x00
	dst[5] = 0x01
	dst[6] = 0x14
	return XboxGIPLedOnLen
}

// BuildXboxGIPAuthComplete writes the 6-byte GIP authentication/handshake
// completion packet expected after power-on and LED-on.
func BuildXboxGIPAuthComplete(dst []byte, seq uint8) int {
	if len(dst) < XboxGIPAuthLen {
		return 0
	}
	dst[0] = 0x06
	dst[1] = 0x20
	dst[2] = seq
	dst[3] = 0x02
	dst[4] = 0x01
	dst[5] = 0x00
	return XboxGIPAuthLen
}

// BuildXboxGIPAck writes a 13-byte GIP acknowledgement packet for Guide/status events.
func BuildXboxGIPAck(dst []byte, seq, cmdID, payloadLen uint8) int {
	if len(dst) < XboxGIPAckLen {
		return 0
	}
	clear(dst[:XboxGIPAckLen])
	dst[0] = XboxGIPCommandAck
	dst[1] = 0x20
	dst[2] = seq
	dst[3] = 0x09
	dst[4] = 0x00
	dst[5] = cmdID
	dst[6] = 0x20
	dst[7] = payloadLen
	return XboxGIPAckLen
}

// BuildXboxGIPRumble writes a 13-byte Xbox GIP rumble command (scaling 0..255 -> 0..100).
func BuildXboxGIPRumble(dst []byte, seq, leftHeavy, rightLight uint8) int {
	if len(dst) < XboxGIPRumbleLen {
		return 0
	}
	clear(dst[:XboxGIPRumbleLen])
	dst[0] = XboxGIPCommandRumble
	dst[1] = 0x00
	dst[2] = seq
	dst[3] = 0x09
	dst[4] = 0x00
	dst[5] = 0x0f // enable all 4 motors (trigger+main)
	dst[8] = uint8(int(leftHeavy) * 100 / 255)
	dst[9] = uint8(int(rightLight) * 100 / 255)
	dst[10] = 0xff // sustain duration
	return XboxGIPRumbleLen
}
