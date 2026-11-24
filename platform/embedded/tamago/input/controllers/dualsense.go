// SPDX-License-Identifier: GPL-3.0-or-later
package controllers

import (
	"encoding/binary"
	"hash/crc32"
)

const (
	// DualSenseBTReportFull is the unlocked 78-byte high-rate (250-800 Hz) BT report.
	DualSenseBTReportFull = 0x31
	// DualSenseBTReportBasic is the 10-byte reduced report emitted before unlock.
	DualSenseBTReportBasic = 0x01
	// DualSenseFeatureCalib is the Feature report ID whose GET_REPORT unlocks 0x31.
	DualSenseFeatureCalib = 0x05

	// DualShock4BTReportFull is the unlocked 78-byte BT report.
	DualShock4BTReportFull = 0x11
	// DualShock4FeatureCalib is the BT Feature report ID (0x05) queried during
	// Bluetooth initialization alongside CRC-32 output report 0x11.
	DualShock4FeatureCalib = 0x05

	// DualSenseBTOutputLen is the exact payload length for Sony BT full reports
	// 0x31 and 0x11 (excluding the 1-byte HIDP 0xA1/0xA2 header).
	DualSenseBTOutputLen = 78
)

var (
	dualSenseInputCRCSeed  = [1]byte{0xa1}
	dualSenseOutputCRCSeed = [1]byte{0xa2}
)

func sonyBTCRC32(seed []byte, body []byte) uint32 {
	sum := crc32.Update(0, crc32.IEEETable, seed)
	return crc32.Update(sum, crc32.IEEETable, body)
}

func validSonyBTFullReport(report []byte) bool {
	if len(report) != DualSenseBTOutputLen {
		return false
	}
	return binary.LittleEndian.Uint32(report[74:78]) == sonyBTCRC32(dualSenseInputCRCSeed[:], report[:74])
}

// DualSenseUnlockFeatureReportID returns the HID Feature report ID that must be
// fetched via HIDP GET_REPORT on L2CAP PSM 0x0011 to switch Sony controllers
// from basic 10-byte report 0x01 into full high-rate report 0x31 / 0x11.
func DualSenseUnlockFeatureReportID(kind Kind) uint8 {
	if kind == KindDualShock4 {
		return DualShock4FeatureCalib
	}
	return DualSenseFeatureCalib
}

// ParseDualSenseBT decodes a Sony DualSense Bluetooth HIDP input payload
// (starting at the report ID byte, after stripping the 0xA1 HIDP header) into
// out with zero heap allocations.
func ParseDualSenseBT(report []byte, out *State) bool {
	if out == nil || len(report) < 10 {
		return false
	}
	switch report[0] {
	case DualSenseBTReportFull:
		// Full 78-byte BT report 0x31:
		// [0]=0x31 [1]=bt_seq [2]=LX [3]=LY [4]=RX [5]=RY [6]=L2 [7]=R2
		// [8]=seq [9]=hat|face [10]=shoulders|start|select|l3|r3 [11]=ps|touch|mute
		// [54]=battery nibble (0..10) + charging bit (0x10)
		// [74..77]=little-endian IEEE CRC-32 seeded with HIDP DATA|INPUT (0xA1)
		if !validSonyBTFullReport(report) {
			return false
		}
		var b Button
		b |= hatNibbleToDPad(report[9] & 0x0f)
		if report[9]&0x10 != 0 {
			b |= ButtonWest // Square
		}
		if report[9]&0x20 != 0 {
			b |= ButtonSouth // Cross
		}
		if report[9]&0x40 != 0 {
			b |= ButtonEast // Circle
		}
		if report[9]&0x80 != 0 {
			b |= ButtonNorth // Triangle
		}
		if report[10]&0x01 != 0 {
			b |= ButtonL1
		}
		if report[10]&0x02 != 0 {
			b |= ButtonR1
		}
		if report[10]&0x04 != 0 {
			b |= ButtonL2
		}
		if report[10]&0x08 != 0 {
			b |= ButtonR2
		}
		if report[10]&0x10 != 0 {
			b |= ButtonSelect // Create
		}
		if report[10]&0x20 != 0 {
			b |= ButtonStart // Options
		}
		if report[10]&0x40 != 0 {
			b |= ButtonL3
		}
		if report[10]&0x80 != 0 {
			b |= ButtonR3
		}
		if report[11]&0x01 != 0 {
			b |= ButtonHome // PS
		}
		if report[11]&0x02 != 0 {
			b |= ButtonTouchpad
		}
		if report[11]&0x04 != 0 {
			b |= ButtonCapture // Mute
		}

		out.Buttons = b
		out.LeftX = u8AxisToS16(report[2])
		out.LeftY = u8AxisToS16(report[3])
		out.RightX = u8AxisToS16(report[4])
		out.RightY = u8AxisToS16(report[5])
		out.LeftTrigger = report[6]
		out.RightTrigger = report[7]
		if len(report) > 54 {
			lvl := report[54] & 0x0f
			if lvl > 10 {
				lvl = 10
			}
			out.Battery = lvl * 10
			out.Charging = report[54]&0x10 != 0
		}
		return true

	case DualSenseBTReportBasic:
		// Basic 10-byte BT report 0x01 (before GET_REPORT 0x05 unlock):
		// [0]=0x01 [1]=LX [2]=LY [3]=RX [4]=RY [5]=hat|face [6]=shoulders|start [7]=ps|touch [8]=L2 [9]=R2
		var b Button
		b |= hatNibbleToDPad(report[5] & 0x0f)
		if report[5]&0x10 != 0 {
			b |= ButtonWest
		}
		if report[5]&0x20 != 0 {
			b |= ButtonSouth
		}
		if report[5]&0x40 != 0 {
			b |= ButtonEast
		}
		if report[5]&0x80 != 0 {
			b |= ButtonNorth
		}
		if report[6]&0x01 != 0 {
			b |= ButtonL1
		}
		if report[6]&0x02 != 0 {
			b |= ButtonR1
		}
		if report[6]&0x04 != 0 {
			b |= ButtonL2
		}
		if report[6]&0x08 != 0 {
			b |= ButtonR2
		}
		if report[6]&0x10 != 0 {
			b |= ButtonSelect
		}
		if report[6]&0x20 != 0 {
			b |= ButtonStart
		}
		if report[6]&0x40 != 0 {
			b |= ButtonL3
		}
		if report[6]&0x80 != 0 {
			b |= ButtonR3
		}
		if report[7]&0x01 != 0 {
			b |= ButtonHome
		}
		if report[7]&0x02 != 0 {
			b |= ButtonTouchpad
		}
		out.Buttons = b
		out.LeftX = u8AxisToS16(report[1])
		out.LeftY = u8AxisToS16(report[2])
		out.RightX = u8AxisToS16(report[3])
		out.RightY = u8AxisToS16(report[4])
		out.LeftTrigger = report[8]
		out.RightTrigger = report[9]
		return true
	default:
		return false
	}
}

// ParseDualShock4BT decodes a Sony DualShock 4 Bluetooth HIDP input payload
// (report 0x11 full or 0x01 basic) into out with zero heap allocations.
func ParseDualShock4BT(report []byte, out *State) bool {
	if out == nil || len(report) < 10 {
		return false
	}
	if report[0] == DualSenseBTReportBasic {
		return ParseDualSenseBT(report, out)
	}
	if report[0] != DualShock4BTReportFull || !validSonyBTFullReport(report) {
		return false
	}
	// Full 78-byte DS4 BT report 0x11:
	// [0]=0x11 [1]=0xC0 [2]=0x00 [3]=LX [4]=LY [5]=RX [6]=RY
	// [7]=hat|face [8]=shoulders|share|options|l3|r3 [9]=ps|touch [10]=L2 [11]=R2
	var b Button
	b |= hatNibbleToDPad(report[7] & 0x0f)
	if report[7]&0x10 != 0 {
		b |= ButtonWest
	}
	if report[7]&0x20 != 0 {
		b |= ButtonSouth
	}
	if report[7]&0x40 != 0 {
		b |= ButtonEast
	}
	if report[7]&0x80 != 0 {
		b |= ButtonNorth
	}
	if report[8]&0x01 != 0 {
		b |= ButtonL1
	}
	if report[8]&0x02 != 0 {
		b |= ButtonR1
	}
	if report[8]&0x04 != 0 {
		b |= ButtonL2
	}
	if report[8]&0x08 != 0 {
		b |= ButtonR2
	}
	if report[8]&0x10 != 0 {
		b |= ButtonSelect
	}
	if report[8]&0x20 != 0 {
		b |= ButtonStart
	}
	if report[8]&0x40 != 0 {
		b |= ButtonL3
	}
	if report[8]&0x80 != 0 {
		b |= ButtonR3
	}
	if report[9]&0x01 != 0 {
		b |= ButtonHome
	}
	if report[9]&0x02 != 0 {
		b |= ButtonTouchpad
	}
	out.Buttons = b
	out.LeftX = u8AxisToS16(report[3])
	out.LeftY = u8AxisToS16(report[4])
	out.RightX = u8AxisToS16(report[5])
	out.RightY = u8AxisToS16(report[6])
	out.LeftTrigger = report[10]
	out.RightTrigger = report[11]
	if len(report) > 32 {
		lvl := report[32] & 0x0f
		if lvl > 10 {
			lvl = 10
		}
		out.Battery = lvl * 10
		out.Charging = report[32]&0x10 != 0
	}
	return true
}

var dualSensePlayerLEDs = [4]byte{
	0x04, // Player 1: center LED
	0x0a, // Player 2: inner pair
	0x15, // Player 3: center + outer pair
	0x1b, // Player 4: 4 LEDs
}

var dualSensePlayerRGB = [4][3]byte{
	{0x00, 0x20, 0x80}, // Player 1: blue
	{0x80, 0x10, 0x10}, // Player 2: red
	{0x10, 0x70, 0x10}, // Player 3: green
	{0x70, 0x10, 0x70}, // Player 4: magenta
}

// BuildDualSenseBTOutput writes a valid 78-byte DualSense Bluetooth output
// report 0x31 (setting rolling 4-bit seq tag, player 1..4 LEDs, lightbar color,
// and rumble motors) with the required IEEE CRC-32 seeded with 0xA2.
func BuildDualSenseBTOutput(dst []byte, seq, playerSlot, rumbleRight, rumbleLeft uint8) int {
	if len(dst) < DualSenseBTOutputLen {
		return 0
	}
	clear(dst[:DualSenseBTOutputLen])
	slot := playerSlot & 3
	dst[0] = DualSenseBTReportFull
	dst[1] = (seq & 0x0f) << 4 // seq_tag: rolling 4-bit sequence in high nibble, tag 0x0 in low nibble
	dst[2] = 0x10              // DS_OUTPUT_TAG
	dst[3] = 0x03              // valid_flag0: compatible vibration + haptics select
	dst[4] = 0x14              // valid_flag1: lightbar (0x04) + player LEDs (0x10)
	dst[5] = rumbleRight
	dst[6] = rumbleLeft
	dst[46] = dualSensePlayerLEDs[slot]
	dst[47] = dualSensePlayerRGB[slot][0]
	dst[48] = dualSensePlayerRGB[slot][1]
	dst[49] = dualSensePlayerRGB[slot][2]

	// CRC-32 over [0xA2] || dst[0:74] stored little-endian in dst[74:78].
	binary.LittleEndian.PutUint32(dst[74:78], sonyBTCRC32(dualSenseOutputCRCSeed[:], dst[:74]))
	return DualSenseBTOutputLen
}

// BuildDualShock4BTOutput writes a valid 78-byte DualShock 4 Bluetooth output
// report 0x11 (setting HID+CRC flags 0xc0, valid flags 0x03, rumble, and
// player lightbar RGB) with the required IEEE CRC-32 seeded with 0xA2.
func BuildDualShock4BTOutput(dst []byte, playerSlot, rumbleRight, rumbleLeft uint8) int {
	if len(dst) < DualSenseBTOutputLen {
		return 0
	}
	clear(dst[:DualSenseBTOutputLen])
	slot := playerSlot & 3
	dst[0] = DualShock4BTReportFull
	dst[1] = 0xc0 // HID | CRC32 flags required on Bluetooth 0x11 output reports
	dst[2] = 0x00
	dst[3] = 0x03 // enable rumble (0x01) + lightbar RGB (0x02)
	dst[6] = rumbleRight
	dst[7] = rumbleLeft
	dst[8] = dualSensePlayerRGB[slot][0]
	dst[9] = dualSensePlayerRGB[slot][1]
	dst[10] = dualSensePlayerRGB[slot][2]

	// CRC-32 over [0xA2] || dst[0:74] stored little-endian in dst[74:78].
	binary.LittleEndian.PutUint32(dst[74:78], sonyBTCRC32(dualSenseOutputCRCSeed[:], dst[:74]))
	return DualSenseBTOutputLen
}
