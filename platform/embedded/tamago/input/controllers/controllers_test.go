// SPDX-License-Identifier: GPL-3.0-or-later
package controllers

import (
	"encoding/binary"
	"hash/crc32"
	"testing"
)

func pack12Pair(x, y uint16) (byte, byte, byte) {
	return byte(x & 0xff), byte((x>>8)&0x0f) | byte((y&0x0f)<<4), byte((y >> 4) & 0xff)
}

func TestIdentifyControllers(t *testing.T) {
	if IdentifyByVIDPID(0x054c, 0x0ce6) != KindDualSense || IdentifyByVIDPID(0x054c, 0x0df2) != KindDualSense {
		t.Fatal("DualSense VID:PID identification failed")
	}
	if IdentifyByVIDPID(0x054c, 0x05c4) != KindDualShock4 || IdentifyByVIDPID(0x054c, 0x09cc) != KindDualShock4 {
		t.Fatal("DualShock 4 VID:PID identification failed")
	}
	if IdentifyByVIDPID(0x057e, 0x2009) != KindSwitchPro {
		t.Fatal("Switch Pro VID:PID identification failed")
	}
	if IdentifyByVIDPID(0x1234, 0x5678) != KindUnknown {
		t.Fatal("unknown VID:PID misidentified")
	}
	if IdentifyByName("DualSense Wireless Controller") != KindDualSense ||
		IdentifyByName("Wireless Controller") != KindUnknown ||
		IdentifyByName("Pro Controller") != KindSwitchPro ||
		IdentifyByName("Other Device") != KindUnknown {
		t.Fatal("name identification failed")
	}
	if KindDualSense.String() != "dualsense" || KindDualShock4.String() != "dualshock4" ||
		KindSwitchPro.String() != "switchpro" || KindGenericHID.String() != "hid" || KindUnknown.String() != "unknown" {
		t.Fatal("Kind.String mismatch")
	}
	if DualSenseUnlockFeatureReportID(KindDualSense) != 0x05 || DualSenseUnlockFeatureReportID(KindDualShock4) != 0x05 {
		t.Fatal("unlock feature report ID mismatch")
	}
}

func sealSonyBTInput(rep []byte) {
	sum := crc32.Update(0, crc32.IEEETable, []byte{0xa1})
	sum = crc32.Update(sum, crc32.IEEETable, rep[:74])
	binary.LittleEndian.PutUint32(rep[74:78], sum)
}

func TestDualSenseBTFullAndBasicAndOutputCRC32(t *testing.T) {
	var rep [78]byte
	rep[0] = DualSenseBTReportFull
	rep[1] = 0x02
	rep[2] = 0x00 // LeftX = -32768
	rep[3] = 0xff // LeftY = +32512
	rep[4] = 0x80 // RightX = 0
	rep[5] = 0x80 // RightY = 0
	rep[6] = 200  // L2 analog
	rep[7] = 150  // R2 analog
	rep[9] = 0x01 | 0x20 | 0x40 // Hat Up+Right + Cross(South) + Circle(East)
	rep[10] = 0x01 | 0x20       // L1 + Options(Start)
	rep[11] = 0x01 | 0x02 | 0x04 // PS(Home) + Touchpad + Mute(Capture)
	rep[54] = 0x18              // 80% + charging
	sealSonyBTInput(rep[:])

	var st State
	if ParseDualSenseBT(rep[:77], &st) {
		t.Fatal("truncated DualSense 0x31 report must be rejected")
	}
	repBad := rep
	repBad[74] ^= 0xff
	if ParseDualSenseBT(repBad[:], &st) {
		t.Fatal("bad-CRC DualSense 0x31 report must be rejected")
	}
	if !ParseDualSenseBT(rep[:], &st) {
		t.Fatal("ParseDualSenseBT(0x31) failed")
	}
	wantBtns := ButtonDPadUp | ButtonDPadRight | ButtonSouth | ButtonEast | ButtonL1 | ButtonStart | ButtonHome | ButtonTouchpad | ButtonCapture
	if st.Buttons != wantBtns || st.LeftX != -32768 || st.LeftY != 32512 || st.LeftTrigger != 200 || st.RightTrigger != 150 || st.Battery != 80 || !st.Charging {
		t.Fatalf("unexpected DualSense 0x31 state: %+v", st)
	}
	if st.Neutral() {
		t.Fatal("active DualSense state reported Neutral")
	}
	usbSt := st.ToUSBGamepadState()
	if usbSt.Hat != 1 || usbSt.Axes[0] != -32768 {
		t.Fatalf("unexpected usb.GamepadState: %+v", usbSt)
	}
	mask := st.ToComputerMask()
	// Up(bit3), Right(bit0), Left(bit1 from LeftX=-32768), Down(bit2 from LeftY=+32512), South(bit4), East(bit5), Start(bit6)
	if mask != (1<<0|1<<1|1<<2|1<<3|1<<4|1<<5|1<<6) {
		t.Fatalf("unexpected computer mask=%#02x", mask)
	}

	// Basic 0x01 report fallback
	basic := [10]byte{0x01, 0x80, 0x80, 0x80, 0x80, 0x08 | 0x10, 0x10, 0x00, 0, 0}
	if !ParseDualSenseBT(basic[:], &st) || st.Buttons != (ButtonWest|ButtonSelect) || st.LeftX != 0 || st.LeftY != 0 {
		t.Fatalf("unexpected DualSense 0x01 state: %+v", st)
	}

	// DualShock 4 0x11 report
	var ds4 [78]byte
	ds4[0] = DualShock4BTReportFull
	ds4[3], ds4[4], ds4[5], ds4[6] = 0x80, 0x80, 0x80, 0x80
	ds4[7] = 0x04 | 0x80 // Down + Triangle(North)
	ds4[8] = 0x02        // R1
	ds4[9] = 0x01        // PS
	ds4[32] = 0x06       // 60%, not charging
	if ParseDualShock4BT(ds4[:77], &st) || ParseDualShock4BT(ds4[:], &st) {
		t.Fatal("truncated or unsealed DS4 0x11 report must be rejected")
	}
	sealSonyBTInput(ds4[:])
	if !ParseDualShock4BT(ds4[:], &st) || st.Buttons != (ButtonDPadDown|ButtonNorth|ButtonR1|ButtonHome) || st.Battery != 60 || st.Charging {
		t.Fatalf("unexpected DS4 0x11 state: %+v", st)
	}

	// Output report 0x31 with IEEE CRC-32 seeded by 0xA2 and rolling seq nibble.
	var out [DualSenseBTOutputLen]byte
	n := BuildDualSenseBTOutput(out[:], 0, 0, 0x40, 0x20)
	if n != DualSenseBTOutputLen || out[0] != 0x31 || out[1] != 0x00 || out[2] != 0x10 ||
		out[3] != 0x03 || out[4] != 0x14 || out[5] != 0x40 || out[6] != 0x20 ||
		out[46] != 0x04 || out[47] != 0x00 || out[48] != 0x20 || out[49] != 0x80 {
		t.Fatalf("unexpected DualSense output header n=%d out[0:7]=%x leds=%x", n, out[:7], out[46:50])
	}
	wantCRC := crc32.Update(crc32.Update(0, crc32.IEEETable, []byte{0xa2}), crc32.IEEETable, out[:74])
	gotCRC := binary.LittleEndian.Uint32(out[74:78])
	if gotCRC != wantCRC {
		t.Fatalf("DualSense CRC32 mismatch got=%#08x want=%#08x", gotCRC, wantCRC)
	}
	BuildDualSenseBTOutput(out[:], 1, 0, 0x10, 0x10)
	if out[1] != 0x10 || binary.LittleEndian.Uint32(out[74:78]) != crc32.Update(crc32.Update(0, crc32.IEEETable, []byte{0xa2}), crc32.IEEETable, out[:74]) {
		t.Fatalf("unexpected seq=1 DualSense output header/CRC: out[1]=%#02x", out[1])
	}
	BuildDualSenseBTOutput(out[:], 16, 0, 0x00, 0x00)
	if out[1] != 0x00 || binary.LittleEndian.Uint32(out[74:78]) != crc32.Update(crc32.Update(0, crc32.IEEETable, []byte{0xa2}), crc32.IEEETable, out[:74]) {
		t.Fatalf("unexpected wrapped seq=16 DualSense output header/CRC: out[1]=%#02x", out[1])
	}

	// DualShock 4 Bluetooth output report 0x11 with HID|CRC flags and IEEE CRC-32.
	n4 := BuildDualShock4BTOutput(out[:], 1, 0x35, 0x15)
	if n4 != DualSenseBTOutputLen || out[0] != DualShock4BTReportFull || out[1] != 0xc0 || out[2] != 0x00 ||
		out[3] != 0x03 || out[6] != 0x35 || out[7] != 0x15 ||
		out[8] != 0x80 || out[9] != 0x10 || out[10] != 0x10 {
		t.Fatalf("unexpected DS4 output n=%d header=%x rgb=%x", n4, out[:11], out[8:11])
	}
	wantDS4CRC := crc32.Update(crc32.Update(0, crc32.IEEETable, []byte{0xa2}), crc32.IEEETable, out[:74])
	if got := binary.LittleEndian.Uint32(out[74:78]); got != wantDS4CRC {
		t.Fatalf("DS4 output CRC32 mismatch got=%#08x want=%#08x", got, wantDS4CRC)
	}
}

func TestSwitchProCalibrationAndFullReportAndSubcommands(t *testing.T) {
	// Build a synthetic 0x21 SPI calibration reply for 0x603D (18 bytes).
	var calReply [49]byte
	calReply[0] = SwitchProReportSubcmdReply
	calReply[13] = 0x90
	calReply[14] = SwitchProSubcmdSPIRead
	binary.LittleEndian.PutUint32(calReply[15:19], SwitchProFactoryStickCalAddr)
	calReply[19] = SwitchProFactoryStickCalLen
	// Left stick: maxOff=(1500,1500), center=(2000,2100), minOff=(1500,1500)
	calReply[20], calReply[21], calReply[22] = pack12Pair(1500, 1500)
	calReply[23], calReply[24], calReply[25] = pack12Pair(2000, 2100)
	calReply[26], calReply[27], calReply[28] = pack12Pair(1500, 1500)
	// Right stick: center=(2048,2048), minOff=(1600,1600), maxOff=(1600,1600)
	calReply[29], calReply[30], calReply[31] = pack12Pair(2048, 2048)
	calReply[32], calReply[33], calReply[34] = pack12Pair(1600, 1600)
	calReply[35], calReply[36], calReply[37] = pack12Pair(1600, 1600)

	var cal SwitchStickCal
	if !ParseSwitchProSPICalibration(calReply[:], &cal) || !cal.Valid || cal.LeftX.Center != 2000 || cal.LeftY.Center != 2100 {
		t.Fatalf("ParseSwitchProSPICalibration failed: %+v", cal)
	}

	// Build a Standard Full Input Report 0x30:
	// Left stick at full right (3500) and full up (3600 -> canonical Y=-32768).
	var full [49]byte
	full[0] = SwitchProReportFull
	full[2] = (3 << 5) | 0x10 // 75% battery, charging
	full[3] = 0x04 | 0x80     // B(South) + ZR(R2)
	full[4] = 0x02 | 0x10     // Plus(Start) + Home
	full[5] = 0x02 | 0x40     // DPadUp + L(L1)
	full[6], full[7], full[8] = pack12Pair(3500, 3600)
	full[9], full[10], full[11] = pack12Pair(2048, 2048)

	var st State
	if !ParseSwitchProInput(full[:], &cal, &st) {
		t.Fatal("ParseSwitchProInput(0x30) failed")
	}
	wantBtns := ButtonSouth | ButtonR2 | ButtonStart | ButtonHome | ButtonDPadUp | ButtonL1
	if st.Buttons != wantBtns || st.LeftX != 32767 || st.LeftY != -32768 || st.RightX != 0 || st.RightY != 0 || st.RightTrigger != 255 || st.Battery != 75 || !st.Charging {
		t.Fatalf("unexpected Switch Pro 0x30 state: %+v", st)
	}

	// Basic 0x3F report fallback
	var basic [12]byte
	basic[0] = SwitchProReportBasic
	basic[1] = 0x02 // A(East)
	basic[3] = 0x06 // Left
	binary.LittleEndian.PutUint16(basic[4:6], 32768)
	binary.LittleEndian.PutUint16(basic[6:8], 32768)
	binary.LittleEndian.PutUint16(basic[8:10], 32768)
	binary.LittleEndian.PutUint16(basic[10:12], 32768)
	if !ParseSwitchProInput(basic[:], &cal, &st) || st.Buttons != (ButtonEast|ButtonDPadLeft) || st.LeftX != 0 {
		t.Fatalf("unexpected Switch Pro 0x3F state: %+v", st)
	}

	// Subcommand builders
	var cmd [SwitchProOutputLen]byte
	if BuildSwitchProReadCalibration(cmd[:], 1) != SwitchProOutputLen || cmd[10] != SwitchProSubcmdSPIRead || binary.LittleEndian.Uint32(cmd[11:15]) != SwitchProFactoryStickCalAddr {
		t.Fatalf("unexpected SPI read subcmd: %x", cmd[:16])
	}
	if BuildSwitchProSetFullMode(cmd[:], 2) != SwitchProOutputLen || cmd[10] != SwitchProSubcmdSetMode || cmd[11] != SwitchProReportFull {
		t.Fatalf("unexpected set mode subcmd: %x", cmd[:16])
	}
	if BuildSwitchProSetPlayerLEDs(cmd[:], 3, 2) != SwitchProOutputLen || cmd[10] != SwitchProSubcmdSetPlayerLD || cmd[11] != 0x04 {
		t.Fatalf("unexpected player LED subcmd: %x", cmd[:16])
	}
}

func TestControllersParseZeroAllocs(t *testing.T) {
	var dsRep [78]byte
	dsRep[0] = DualSenseBTReportFull
	dsRep[2], dsRep[3], dsRep[4], dsRep[5] = 0x80, 0x80, 0x80, 0x80
	sealSonyBTInput(dsRep[:])
	var swRep [49]byte
	swRep[0] = SwitchProReportFull
	swRep[6], swRep[7], swRep[8] = pack12Pair(2048, 2048)
	swRep[9], swRep[10], swRep[11] = pack12Pair(2048, 2048)
	var cal SwitchStickCal
	var st State
	var out [DualSenseBTOutputLen]byte

	allocs := testing.AllocsPerRun(200, func() {
		ParseDualSenseBT(dsRep[:], &st)
		ParseSwitchProInput(swRep[:], &cal, &st)
		_ = st.ToComputerMask()
		_ = st.ToUSBGamepadState()
		_ = BuildDualSenseBTOutput(out[:], 0, 0, 0, 0)
		_ = BuildDualShock4BTOutput(out[:], 0, 0, 0)
	})
	if allocs > 0 {
		t.Fatalf("controller parse/build allocated %.2f allocs/run, want 0", allocs)
	}
}
