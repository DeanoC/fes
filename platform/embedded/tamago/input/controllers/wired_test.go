// SPDX-License-Identifier: GPL-3.0-or-later
package controllers

import (
	"encoding/binary"
	"testing"
)

func TestWiredKindStrings(t *testing.T) {
	if KindXInput.String() != "xinput" || KindXboxGIP.String() != "xboxgip" {
		t.Fatalf("unexpected wired kind strings: %q %q", KindXInput.String(), KindXboxGIP.String())
	}
}

func TestParseXInputAndBuilders(t *testing.T) {
	var rep [XInputInputLen]byte
	rep[0] = 0x00
	rep[1] = 0x14
	rep[2] = 0x01 | 0x10 | 0x20 | 0x40 // DPadUp | Start | Select | L3
	rep[3] = 0x01 | 0x04 | 0x10 | 0x80 // L1 | Home | A(South) | Y(North)
	rep[4] = 200                       // LT >= 128 -> ButtonL2
	rep[5] = 64                        // RT < 128
	binary.LittleEndian.PutUint16(rep[6:8], uint16(0x4000))
	lx := -32768
	binary.LittleEndian.PutUint16(rep[8:10], uint16(int16(lx)))
	binary.LittleEndian.PutUint16(rep[10:12], uint16(0xf000))
	binary.LittleEndian.PutUint16(rep[12:14], uint16(int16(12345)))

	var st State
	if ParseXInput(rep[:19], &st) {
		t.Fatal("truncated XInput packet must be rejected")
	}
	if !ParseXInput(rep[:], &st) {
		t.Fatal("ParseXInput failed")
	}
	wantBtns := ButtonDPadUp | ButtonStart | ButtonSelect | ButtonL3 | ButtonL1 | ButtonHome | ButtonSouth | ButtonNorth | ButtonL2
	if st.Buttons != wantBtns || st.LeftX != 0x4000 || st.LeftY != 32767 || st.RightY != -12345 || st.LeftTrigger != 200 || st.RightTrigger != 64 {
		t.Fatalf("unexpected XInput state: %+v", st)
	}

	var led [XInputLEDOutputLen]byte
	if BuildXInputLED(led[:], 2) != XInputLEDOutputLen || led != [3]byte{0x01, 0x03, 0x08} {
		t.Fatalf("unexpected XInput LED packet: %x", led)
	}
	var rumble [XInputRumbleOutputLen]byte
	if BuildXInputRumble(rumble[:], 0xaa, 0x55) != XInputRumbleOutputLen || rumble[3] != 0xaa || rumble[4] != 0x55 {
		t.Fatalf("unexpected XInput rumble packet: %x", rumble)
	}
}

func TestParseXboxGIPInputGuidePreservationAndBuilders(t *testing.T) {
	var st State
	// Ignored non-input message (e.g. 0x02 announce)
	if upd, _, _ := ParseXboxGIPInput([]byte{0x02, 0x20, 0x01, 0x04}, &st); upd {
		t.Fatal("expected non-input GIP packet to be ignored")
	}

	// Guide press 0x07 with ACK requested (flags=0x10)
	guide := []byte{XboxGIPCommandGuide, 0x10, 0x07, 0x02, 0x01, 0x00}
	upd, seq, ack := ParseXboxGIPInput(guide, &st)
	if !upd || seq != 0x07 || !ack || st.Buttons != ButtonHome {
		t.Fatalf("unexpected GIP guide press: upd=%t seq=%d ack=%t st=%+v", upd, seq, ack, st)
	}

	// Subsequent 0x20 input report must preserve ButtonHome and decode Series Share
	var in [23]byte
	in[0] = XboxGIPCommandInput
	in[3] = 0x13
	in[4] = 0x04 | 0x20                          // Start | B(East)
	in[5] = 0x02 | 0x20                          // DPadDown | R1
	binary.LittleEndian.PutUint16(in[6:8], 600)  // LT -> 150 >= 128
	binary.LittleEndian.PutUint16(in[8:10], 200) // RT -> 50 < 128
	lxGIP, lyGIP := int16(-10000), int16(-32768)
	binary.LittleEndian.PutUint16(in[10:12], uint16(lxGIP))
	binary.LittleEndian.PutUint16(in[12:14], uint16(lyGIP)) // Y inverted -> +32767
	binary.LittleEndian.PutUint16(in[14:16], uint16(int16(5000)))
	binary.LittleEndian.PutUint16(in[16:18], uint16(int16(20000)))
	in[22] = 0x01 // Series Share (ButtonCapture)
	upd, _, ack = ParseXboxGIPInput(in[:], &st)
	wantBtns := ButtonHome | ButtonStart | ButtonEast | ButtonDPadDown | ButtonR1 | ButtonL2 | ButtonCapture
	if !upd || ack || st.Buttons != wantBtns || st.LeftTrigger != 150 || st.RightTrigger != 50 ||
		st.LeftX != -10000 || st.LeftY != 32767 || st.RightY != -20000 {
		t.Fatalf("unexpected GIP 0x20 state: upd=%t ack=%t st=%+v", upd, ack, st)
	}

	// Guide release clears only ButtonHome
	guide[4] = 0x00
	guide[1] = 0x00
	upd, _, ack = ParseXboxGIPInput(guide, &st)
	if !upd || ack || st.Buttons&ButtonHome != 0 || st.Buttons&(ButtonEast|ButtonCapture) != (ButtonEast|ButtonCapture) {
		t.Fatalf("unexpected GIP guide release: upd=%t ack=%t st=%+v", upd, ack, st)
	}

	var out [XboxGIPRumbleLen]byte
	if BuildXboxGIPPowerOn(out[:], 1) != XboxGIPPowerLen || out[0] != 0x05 || out[2] != 1 {
		t.Fatalf("unexpected GIP power-on: %x", out[:XboxGIPPowerLen])
	}
	if BuildXboxGIPAck(out[:], 7, XboxGIPCommandGuide, 2) != XboxGIPAckLen || out[0] != 0x01 || out[2] != 7 || out[5] != 0x07 || out[7] != 2 {
		t.Fatalf("unexpected GIP ack: %x", out[:])
	}
	if BuildXboxGIPRumble(out[:], 2, 255, 128) != XboxGIPRumbleLen || out[0] != 0x09 || out[8] != 100 || out[9] != 50 || out[10] != 0xff {
		t.Fatalf("unexpected GIP rumble: %x", out[:])
	}
}

func TestParseDualSenseAndDS4USBAndBuilders(t *testing.T) {
	var ds [64]byte
	ds[0] = 0x01
	ds[1], ds[2], ds[3], ds[4] = 0x00, 0xff, 0x80, 0x80
	ds[5], ds[6] = 240, 120
	ds[8] = 0x02 | 0x20  // DPadRight | Cross(South)
	ds[9] = 0x01 | 0x20  // L1 | Options(Start)
	ds[10] = 0x01 | 0x04 // PS(Home) | Mute(Capture)
	ds[53] = 0x15        // 50%, charging
	var st State
	if !ParseDualSenseUSB(ds[:], &st) {
		t.Fatal("ParseDualSenseUSB failed")
	}
	if st.Buttons != (ButtonDPadRight|ButtonSouth|ButtonL1|ButtonStart|ButtonHome|ButtonCapture) ||
		st.LeftX != -32768 || st.LeftY != 32512 || st.LeftTrigger != 240 || st.Battery != 50 || !st.Charging {
		t.Fatalf("unexpected DualSense USB state: %+v", st)
	}

	var ds4 [64]byte
	ds4[0] = 0x01
	ds4[1], ds4[2], ds4[3], ds4[4] = 0x80, 0x80, 0x80, 0x80
	ds4[5] = 0x04 | 0x40 // DPadDown | Circle(East)
	ds4[6] = 0x10        // Share(Select)
	ds4[7] = 0x01        // PS(Home)
	ds4[8], ds4[9] = 180, 90
	ds4[30] = 0x07 // 70%, not charging
	if !ParseDualShock4USB(ds4[:], &st) || st.Buttons != (ButtonDPadDown|ButtonEast|ButtonSelect|ButtonHome) ||
		st.LeftTrigger != 180 || st.RightTrigger != 90 || st.Battery != 70 || st.Charging {
		t.Fatalf("unexpected DS4 USB state: %+v", st)
	}

	var out [DualSenseUSBOutputLen]byte
	if BuildDualSenseUSBOutput(out[:], 1, 0x44, 0x22) != DualSenseUSBOutputLen || out[0] != 0x02 || out[3] != 0x44 || out[4] != 0x22 || out[44] != dualSensePlayerLEDs[1] {
		t.Fatalf("unexpected DualSense USB output: %x", out[:])
	}
	if BuildDualShock4USBOutput(out[:], 0, 0x33, 0x11) != DualShock4USBOutputLen || out[0] != 0x05 || out[4] != 0x33 || out[5] != 0x11 || out[8] != 0x80 {
		t.Fatalf("unexpected DS4 USB output: %x", out[:DualShock4USBOutputLen])
	}

	var sw [2]byte
	if BuildSwitchProUSBHandshake(sw[:]) != 2 || sw != [2]byte{0x80, 0x02} {
		t.Fatalf("unexpected Switch Pro USB handshake: %x", sw)
	}
	if BuildSwitchProUSBEnableHID(sw[:]) != 2 || sw != [2]byte{0x80, 0x04} {
		t.Fatalf("unexpected Switch Pro USB enable HID: %x", sw)
	}
}

func TestWiredControllersZeroAllocs(t *testing.T) {
	var xi [XInputInputLen]byte
	xi[1] = 0x14
	var gip [23]byte
	gip[0], gip[3] = XboxGIPCommandInput, 0x13
	var ds [64]byte
	ds[0] = 0x01
	var ds4 [64]byte
	ds4[0] = 0x01
	var st State
	var out [64]byte

	allocs := testing.AllocsPerRun(200, func() {
		ParseXInput(xi[:], &st)
		ParseXboxGIPInput(gip[:], &st)
		ParseDualSenseUSB(ds[:], &st)
		ParseDualShock4USB(ds4[:], &st)
		BuildXInputLED(out[:], 0)
		BuildXInputRumble(out[:], 10, 20)
		BuildXboxGIPPowerOn(out[:], 1)
		BuildXboxGIPAck(out[:], 1, 0x07, 2)
		BuildXboxGIPRumble(out[:], 2, 10, 20)
		BuildDualSenseUSBOutput(out[:], 0, 10, 20)
		BuildDualShock4USBOutput(out[:], 0, 10, 20)
		BuildSwitchProUSBHandshake(out[:])
		BuildSwitchProUSBEnableHID(out[:])
	})
	if allocs > 0 {
		t.Fatalf("wired controller parse/build allocated %.2f allocs/run, want 0", allocs)
	}
}
