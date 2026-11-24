// SPDX-License-Identifier: GPL-3.0-or-later
package controllers

import "encoding/binary"

const (
	// SwitchProReportFull is the 49-byte Standard Full Input Report (60-120 Hz).
	SwitchProReportFull = 0x30
	// SwitchProReportSubcmdReply carries both input state and SPI/subcommand replies.
	SwitchProReportSubcmdReply = 0x21
	// SwitchProReportBasic is the 12-byte simple report emitted before 0x03 0x30.
	SwitchProReportBasic = 0x3f

	// SwitchProOutputLen is the standard 49-byte HIDP output command length.
	SwitchProOutputLen = 49

	SwitchProSubcmdSetMode     = 0x03
	SwitchProSubcmdSPIRead     = 0x10
	SwitchProSubcmdSetPlayerLD = 0x30

	// SwitchProFactoryStickCalAddr is the 18-byte factory left+right stick
	// calibration block in Switch Pro SPI flash.
	SwitchProFactoryStickCalAddr = 0x603d
	SwitchProFactoryStickCalLen  = 18
)

// SwitchAxisCal holds 12-bit (0..4095) factory calibration for one stick axis.
type SwitchAxisCal struct {
	Center uint16
	MinOff uint16 // max deflection below Center
	MaxOff uint16 // max deflection above Center
}

// SwitchStickCal stores left and right stick factory calibration read from
// Switch Pro SPI 0x603D. Zero value falls back to default 2048 ± 1600 scaling.
type SwitchStickCal struct {
	Valid  bool
	LeftX  SwitchAxisCal
	LeftY  SwitchAxisCal
	RightX SwitchAxisCal
	RightY SwitchAxisCal
}

func unpack12Pair(b0, b1, b2 byte) (uint16, uint16) {
	x := uint16(b0) | (uint16(b1&0x0f) << 8)
	y := (uint16(b1) >> 4) | (uint16(b2) << 4)
	return x, y
}

// ParseSwitchProSPICalibration parses a 0x21 subcommand reply containing the
// 18-byte SPI 0x603D factory calibration block.
func ParseSwitchProSPICalibration(report []byte, cal *SwitchStickCal) bool {
	// Report 0x21 layout:
	// [0]=0x21 [1..12]=standard input [13]=ack [14]=subcmd(0x10)
	// [15..18]=spi addr LE [19]=len [20..37]=18-byte calibration payload
	if cal == nil || len(report) < 38 {
		return false
	}
	if report[0] != SwitchProReportSubcmdReply || report[13]&0x80 == 0 || report[14] != SwitchProSubcmdSPIRead {
		return false
	}
	addr := binary.LittleEndian.Uint32(report[15:19])
	length := report[19]
	if addr != SwitchProFactoryStickCalAddr || length < SwitchProFactoryStickCalLen {
		return false
	}
	p := report[20:38]
	// Left stick (9 bytes): [0..2]=maxOff, [3..5]=center, [6..8]=minOff
	lxMax, lyMax := unpack12Pair(p[0], p[1], p[2])
	lxCtr, lyCtr := unpack12Pair(p[3], p[4], p[5])
	lxMin, lyMin := unpack12Pair(p[6], p[7], p[8])
	// Right stick (9 bytes): [9..11]=center, [12..14]=minOff, [15..17]=maxOff
	rxCtr, ryCtr := unpack12Pair(p[9], p[10], p[11])
	rxMin, ryMin := unpack12Pair(p[12], p[13], p[14])
	rxMax, ryMax := unpack12Pair(p[15], p[16], p[17])

	if lxCtr == 0 || lxCtr == 0xfff || rxCtr == 0 || rxCtr == 0xfff {
		return false
	}
	cal.LeftX = SwitchAxisCal{Center: lxCtr, MinOff: lxMin, MaxOff: lxMax}
	cal.LeftY = SwitchAxisCal{Center: lyCtr, MinOff: lyMin, MaxOff: lyMax}
	cal.RightX = SwitchAxisCal{Center: rxCtr, MinOff: rxMin, MaxOff: rxMax}
	cal.RightY = SwitchAxisCal{Center: ryCtr, MinOff: ryMin, MaxOff: ryMax}
	cal.Valid = true
	return true
}

func scaleSwitchAxis(raw uint16, cal SwitchAxisCal, valid bool, invertY bool) int16 {
	center := int32(2048)
	minOff := int32(1600)
	maxOff := int32(1600)
	if valid {
		center = int32(cal.Center)
		if cal.MinOff >= 256 {
			minOff = int32(cal.MinOff)
		}
		if cal.MaxOff >= 256 {
			maxOff = int32(cal.MaxOff)
		}
	}
	delta := int32(raw) - center
	if invertY {
		// Switch Pro Y increases upward; canonical State Y increases downward.
		delta = -delta
		minOff, maxOff = maxOff, minOff
	}
	var scaled int32
	if delta >= 0 {
		scaled = (delta * 32767) / maxOff
		if scaled > 32767 {
			scaled = 32767
		}
	} else {
		scaled = (delta * 32768) / minOff
		if scaled < -32768 {
			scaled = -32768
		}
	}
	return int16(scaled)
}

// ParseSwitchProInput decodes a Nintendo Switch Pro Controller HID input report
// (0x30 Standard Full, 0x21 Subcommand Reply, or 0x3F Basic fallback) into out
// with zero heap allocations.
func ParseSwitchProInput(report []byte, cal *SwitchStickCal, out *State) bool {
	if out == nil || len(report) < 12 {
		return false
	}
	switch report[0] {
	case SwitchProReportFull, SwitchProReportSubcmdReply:
		// [0]=id [1]=timer [2]=battery|conn [3]=right buttons [4]=shared [5]=left buttons
		// [6..8]=left 12-bit stick [9..11]=right 12-bit stick
		var b Button
		b3, b4, b5 := report[3], report[4], report[5]
		if b3&0x01 != 0 {
			b |= ButtonWest // Y
		}
		if b3&0x02 != 0 {
			b |= ButtonNorth // X
		}
		if b3&0x04 != 0 {
			b |= ButtonSouth // B
		}
		if b3&0x08 != 0 {
			b |= ButtonEast // A
		}
		if b3&0x40 != 0 {
			b |= ButtonR1 // R
		}
		if b3&0x80 != 0 {
			b |= ButtonR2 // ZR
		}
		if b4&0x01 != 0 {
			b |= ButtonSelect // Minus
		}
		if b4&0x02 != 0 {
			b |= ButtonStart // Plus
		}
		if b4&0x04 != 0 {
			b |= ButtonR3
		}
		if b4&0x08 != 0 {
			b |= ButtonL3
		}
		if b4&0x10 != 0 {
			b |= ButtonHome
		}
		if b4&0x20 != 0 {
			b |= ButtonCapture
		}
		if b5&0x01 != 0 {
			b |= ButtonDPadDown
		}
		if b5&0x02 != 0 {
			b |= ButtonDPadUp
		}
		if b5&0x04 != 0 {
			b |= ButtonDPadRight
		}
		if b5&0x08 != 0 {
			b |= ButtonDPadLeft
		}
		if b5&0x40 != 0 {
			b |= ButtonL1 // L
		}
		if b5&0x80 != 0 {
			b |= ButtonL2 // ZL
		}

		lx, ly := unpack12Pair(report[6], report[7], report[8])
		rx, ry := unpack12Pair(report[9], report[10], report[11])
		valid := cal != nil && cal.Valid
		var lxCal, lyCal, rxCal, ryCal SwitchAxisCal
		if valid {
			lxCal, lyCal, rxCal, ryCal = cal.LeftX, cal.LeftY, cal.RightX, cal.RightY
		}

		out.Buttons = b
		out.LeftX = scaleSwitchAxis(lx, lxCal, valid, false)
		out.LeftY = scaleSwitchAxis(ly, lyCal, valid, true)
		out.RightX = scaleSwitchAxis(rx, rxCal, valid, false)
		out.RightY = scaleSwitchAxis(ry, ryCal, valid, true)
		if b&ButtonL2 != 0 {
			out.LeftTrigger = 255
		} else {
			out.LeftTrigger = 0
		}
		if b&ButtonR2 != 0 {
			out.RightTrigger = 255
		} else {
			out.RightTrigger = 0
		}
		// High nibble of report[2]: bits 7..5 = 0..4 battery level, bit 4 = charging.
		battLvl := (report[2] >> 5) & 0x07
		if battLvl > 4 {
			battLvl = 4
		}
		out.Battery = battLvl * 25
		out.Charging = report[2]&0x10 != 0
		return true

	case SwitchProReportBasic:
		// Basic 12-byte report 0x3F: [1..2]=buttons, [3]=hat, [4..7]=LX/LY u16, [8..11]=RX/RY u16
		var b Button
		b1, b2 := report[1], report[2]
		if b1&0x01 != 0 {
			b |= ButtonSouth // B
		}
		if b1&0x02 != 0 {
			b |= ButtonEast // A
		}
		if b1&0x04 != 0 {
			b |= ButtonWest // Y
		}
		if b1&0x08 != 0 {
			b |= ButtonNorth // X
		}
		if b1&0x10 != 0 {
			b |= ButtonL1
		}
		if b1&0x20 != 0 {
			b |= ButtonR1
		}
		if b1&0x40 != 0 {
			b |= ButtonL2
		}
		if b1&0x80 != 0 {
			b |= ButtonR2
		}
		if b2&0x01 != 0 {
			b |= ButtonSelect
		}
		if b2&0x02 != 0 {
			b |= ButtonStart
		}
		if b2&0x04 != 0 {
			b |= ButtonL3
		}
		if b2&0x08 != 0 {
			b |= ButtonR3
		}
		if b2&0x10 != 0 {
			b |= ButtonHome
		}
		if b2&0x20 != 0 {
			b |= ButtonCapture
		}
		b |= hatNibbleToDPad(report[3])
		out.Buttons = b
		out.LeftX = int16(int32(binary.LittleEndian.Uint16(report[4:6])) - 32768)
		out.LeftY = int16(int32(binary.LittleEndian.Uint16(report[6:8])) - 32768)
		out.RightX = int16(int32(binary.LittleEndian.Uint16(report[8:10])) - 32768)
		out.RightY = int16(int32(binary.LittleEndian.Uint16(report[10:12])) - 32768)
		return true
	default:
		return false
	}
}

var switchNeutralRumble = [8]byte{0x00, 0x01, 0x40, 0x40, 0x00, 0x01, 0x40, 0x40}

// BuildSwitchProSubcommand writes a 49-byte Switch Pro output subcommand packet
// (excluding the 1-byte HIDP 0xA2 header).
func BuildSwitchProSubcommand(dst []byte, seq uint8, subcmd byte, args []byte) int {
	if len(dst) < SwitchProOutputLen || len(args) > SwitchProOutputLen-11 {
		return 0
	}
	clear(dst[:SwitchProOutputLen])
	dst[0] = 0x01 // Rumble + subcommand output report ID
	dst[1] = seq & 0x0f
	copy(dst[2:10], switchNeutralRumble[:])
	dst[10] = subcmd
	copy(dst[11:], args)
	return SwitchProOutputLen
}

// BuildSwitchProReadCalibration builds subcommand 0x10 reading 18 bytes from
// SPI address 0x603D (factory left+right stick calibration).
func BuildSwitchProReadCalibration(dst []byte, seq uint8) int {
	var args [5]byte
	binary.LittleEndian.PutUint32(args[0:4], SwitchProFactoryStickCalAddr)
	args[4] = SwitchProFactoryStickCalLen
	return BuildSwitchProSubcommand(dst, seq, SwitchProSubcmdSPIRead, args[:])
}

// BuildSwitchProSetFullMode builds subcommand 0x03 0x30 enabling Standard Full
// Input Report 0x30.
func BuildSwitchProSetFullMode(dst []byte, seq uint8) int {
	args := [1]byte{SwitchProReportFull}
	return BuildSwitchProSubcommand(dst, seq, SwitchProSubcmdSetMode, args[:])
}

// BuildSwitchProSetPlayerLEDs builds subcommand 0x30 lighting the player slot
// indicator LED (slot 0..3 -> LEDs 1..4).
func BuildSwitchProSetPlayerLEDs(dst []byte, seq uint8, playerSlot uint8) int {
	args := [1]byte{byte(1 << (playerSlot & 3))}
	return BuildSwitchProSubcommand(dst, seq, SwitchProSubcmdSetPlayerLD, args[:])
}
