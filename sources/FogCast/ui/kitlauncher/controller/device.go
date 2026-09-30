package controller

import (
	"encoding/binary"
	"errors"
	"strings"
)

// eligible rejects the retained virtual pad (BUS_VIRTUAL) and non-gamepads.
func eligible(bus uint16, name string, buttons bool) bool {
	// A button bitmap alone is not an identity: keyboards and arbitrary
	// evdev nodes can expose BTN_SOUTH too. Auto discovery needs a physical
	// bus and a readable name before considering gamepad capabilities.
	return bus != 0 && bus != 6 && name != "" && name != "FogCast Virtual Gamepad" && buttons
}

// Eligible reports whether an evdev node is a physical gamepad candidate.
// Keep bus/name filtering shared with the tenfoot automatic scanner.
func Eligible(bus uint16, name string, buttons bool) bool { return eligible(bus, name, buttons) }

// eligibleKeyboard admits a physical QWERTY for play-session HID. Gamepads
// stay on the pad path even when they also expose keys.
func eligibleKeyboard(bus uint16, name string, keys bool) bool {
	return bus != 6 && name != "FogCast Virtual Gamepad" && keys
}
func decode(b []byte) (uint16, uint16, int32, error) {
	if len(b) != 16 && len(b) != 24 {
		return 0, 0, 0, errors.New("invalid input record")
	}
	p := b[len(b)-8:]
	return binary.LittleEndian.Uint16(p), binary.LittleEndian.Uint16(p[2:]), int32(binary.LittleEndian.Uint32(p[4:])), nil
}
func cstring(b []byte) string { return strings.TrimRight(string(b), "\x00") }
