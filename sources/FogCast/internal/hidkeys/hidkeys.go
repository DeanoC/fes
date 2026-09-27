// Package hidkeys carries physical keys as USB HID Keyboard/Keypad usages
// (page 0x07) for fes.keyboard.hid 1.0 sessions. FogCast forwards key state
// only; the core owns every character, shifted-symbol and repeat mapping.
package hidkeys

import (
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

// Base is the start of the HID key-code range in FogCast input events. Code
// Base+u is usage u; the range does not overlap gamepad or ZX81 matrix codes.
const Base remoteinput.Code = 0x1000

// Rows is the fes.keyboard.hid 1.0 state: rows 0..7 hold usages 16r..16r+15
// in bits 0..15 and row 8 holds the modifiers 0xe0..0xe7 in bits 0..7.
type Rows [9]uint16

// Valid reports whether the ABI can carry usage u: 0x04..0x7f (0..3 are the
// HID error codes) and the eight modifiers 0xe0..0xe7.
func Valid(usage uint8) bool {
	return usage >= 0x04 && usage <= 0x7f || usage >= 0xe0 && usage <= 0xe7
}

// Code returns the input event code of a representable usage.
func Code(usage uint8) (remoteinput.Code, bool) {
	if !Valid(usage) {
		return 0, false
	}
	return Base + remoteinput.Code(usage), true
}

// Usage returns the HID usage carried by an input event code.
func Usage(code remoteinput.Code) (uint8, bool) {
	if code < Base || code > Base+0xff {
		return 0, false
	}
	usage := uint8(code - Base)
	return usage, Valid(usage)
}

// Encode builds the nine rows from a set of held usages.
func Encode(pressed map[uint8]bool) Rows {
	var rows Rows
	for usage, down := range pressed {
		if !down || !Valid(usage) {
			continue
		}
		if usage >= 0xe0 {
			rows[8] |= 1 << (usage - 0xe0)
			continue
		}
		rows[usage/16] |= 1 << (usage % 16)
	}
	return rows
}

// Event is a keyboard press or release of one HID usage.
func Event(usage uint8, down bool) (remoteinput.Event, bool) {
	code, ok := Code(usage)
	if !ok {
		return remoteinput.Event{}, false
	}
	action := remoteinput.ActionRelease
	if down {
		action = remoteinput.ActionPress
	}
	return remoteinput.Event{Device: remoteinput.DeviceKeyboard, Kind: remoteinput.KindKey, Action: action, Code: code}, true
}

// linuxUsage maps Linux evdev KEY_* codes (linux/input-event-codes.h) to HID
// Keyboard/Keypad usages, following the kernel's hid-input table.
var linuxUsage = map[uint16]uint8{
	1: 0x29, 2: 0x1e, 3: 0x1f, 4: 0x20, 5: 0x21, 6: 0x22, 7: 0x23, 8: 0x24, 9: 0x25, 10: 0x26, 11: 0x27,
	12: 0x2d, 13: 0x2e, 14: 0x2a, 15: 0x2b,
	16: 0x14, 17: 0x1a, 18: 0x08, 19: 0x15, 20: 0x17, 21: 0x1c, 22: 0x18, 23: 0x0c, 24: 0x12, 25: 0x13,
	26: 0x2f, 27: 0x30, 28: 0x28, 29: 0xe0,
	30: 0x04, 31: 0x16, 32: 0x07, 33: 0x09, 34: 0x0a, 35: 0x0b, 36: 0x0d, 37: 0x0e, 38: 0x0f,
	39: 0x33, 40: 0x34, 41: 0x35, 42: 0xe1, 43: 0x31,
	44: 0x1d, 45: 0x1b, 46: 0x06, 47: 0x19, 48: 0x05, 49: 0x11, 50: 0x10,
	51: 0x36, 52: 0x37, 53: 0x38, 54: 0xe5, 55: 0x55, 56: 0xe2, 57: 0x2c, 58: 0x39,
	59: 0x3a, 60: 0x3b, 61: 0x3c, 62: 0x3d, 63: 0x3e, 64: 0x3f, 65: 0x40, 66: 0x41, 67: 0x42, 68: 0x43,
	69: 0x53, 70: 0x47, 71: 0x5f, 72: 0x60, 73: 0x61, 74: 0x56, 75: 0x5c, 76: 0x5d, 77: 0x5e, 78: 0x57,
	79: 0x59, 80: 0x5a, 81: 0x5b, 82: 0x62, 83: 0x63, 86: 0x64, 87: 0x44, 88: 0x45,
	96: 0x58, 97: 0xe4, 98: 0x54, 99: 0x46, 100: 0xe6,
	102: 0x4a, 103: 0x52, 104: 0x4b, 105: 0x50, 106: 0x4f, 107: 0x4d, 108: 0x51, 109: 0x4e, 110: 0x49, 111: 0x4c,
	117: 0x67, 119: 0x48, 125: 0xe3, 126: 0xe7, 127: 0x65,
	183: 0x68, 184: 0x69, 185: 0x6a, 186: 0x6b, 187: 0x6c, 188: 0x6d, 189: 0x6e, 190: 0x6f,
	191: 0x70, 192: 0x71, 193: 0x72, 194: 0x73,
}

// FromLinuxKey maps one evdev key to its HID usage.
func FromLinuxKey(code uint16) (uint8, bool) {
	usage, ok := linuxUsage[code]
	return usage, ok
}

// Legacy maps a HID usage to the session-neutral codes FogCast used before
// HID keys: ZX81 matrix keys (letters, digits, Enter, Space, Shift, period)
// and the four FogCast keyboard arrows. Other usages have no legacy form.
func Legacy(usage uint8) (remoteinput.Code, bool) {
	switch {
	case usage >= 0x04 && usage <= 0x1d:
		return zx81keys.Letter('A' + usage - 0x04), true
	case usage >= 0x1e && usage <= 0x26:
		return zx81keys.Digit(usage - 0x1e + 1), true
	}
	switch usage {
	case 0x27:
		return zx81keys.Digit(0), true
	case 0x28:
		return zx81keys.KeyEnter, true
	case 0x2c:
		return zx81keys.KeySpace, true
	case 0x37:
		return zx81keys.KeyPeriod, true
	case 0xe1, 0xe5:
		return zx81keys.KeyShift, true
	case 0x4f:
		return remoteinput.KeyRight, true
	case 0x50:
		return remoteinput.KeyLeft, true
	case 0x51:
		return remoteinput.KeyDown, true
	case 0x52:
		return remoteinput.KeyUp, true
	}
	return 0, false
}

// FromLegacy maps a ZX81 matrix or FogCast arrow code to the physical key's
// usage, so older producers still reach an HID session as physical keys.
func FromLegacy(code remoteinput.Code) (uint8, bool) {
	switch code {
	case remoteinput.KeyRight:
		return 0x4f, true
	case remoteinput.KeyLeft:
		return 0x50, true
	case remoteinput.KeyDown:
		return 0x51, true
	case remoteinput.KeyUp:
		return 0x52, true
	case zx81keys.KeyEnter:
		return 0x28, true
	case zx81keys.KeySpace:
		return 0x2c, true
	case zx81keys.KeyPeriod:
		return 0x37, true
	case zx81keys.KeyShift:
		return 0xe1, true
	}
	if code >= zx81keys.KeyA && code <= zx81keys.KeyZ {
		return uint8(code-zx81keys.KeyA) + 0x04, true
	}
	if code >= zx81keys.Key0 && code <= zx81keys.Key9 {
		digit := uint8(code - zx81keys.Key0)
		if digit == 0 {
			return 0x27, true
		}
		return 0x1e + digit - 1, true
	}
	return 0, false
}
