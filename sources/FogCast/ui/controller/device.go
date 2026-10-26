package controller

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
)

// Device is a pad's evdev identity and capabilities: EVIOCGNAME, EVIOCGID,
// the EV_KEY and EV_ABS bitmaps from EVIOCGBIT, and EVIOCGABS ranges.
type Device struct {
	Name                          string
	Bus, Vendor, Product, Version uint16
	Keys, Abs                     []byte
	Axes                          map[uint16]Range
}

// GUID is the key tenfoot files a pad's saved profile under. It is built as
// SDL_CreateJoystickGUID builds an evdev node's GUID: little-endian bus and
// name CRC, then vendor, product and version, each followed by a zero word.
// A node without a vendor carries the first 11 bytes of its name after the
// CRC instead. SDL first adjusts the version of a few pads, and its HIDAPI
// driver builds GUIDs another way, so SDL can report another GUID for the
// same pad.
func (d Device) GUID() string {
	var g [16]byte
	binary.LittleEndian.PutUint16(g[0:], d.Bus)
	binary.LittleEndian.PutUint16(g[2:], sdlCRC16(d.Name))
	if d.Vendor != 0 {
		binary.LittleEndian.PutUint16(g[4:], d.Vendor)
		binary.LittleEndian.PutUint16(g[8:], d.Product)
		binary.LittleEndian.PutUint16(g[12:], d.Version)
	} else {
		copy(g[4:15], d.Name)
	}
	return hex.EncodeToString(g[:])
}

// eligible rejects the retained virtual pad (BUS_VIRTUAL) and non-gamepads.
// Bus 5 is Linux BUS_BLUETOOTH. A paired HID or HoG pad on that bus is a
// physical gamepad.
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
func eligibleMouse(bus uint16, name string, relative, buttons bool) bool {
	return bus != 0 && bus != 6 && name != "" && name != "FogCast Virtual Gamepad" && relative && buttons
}

func decode(b []byte) (uint16, uint16, int32, error) {
	if len(b) != 16 && len(b) != 24 {
		return 0, 0, 0, errors.New("invalid input record")
	}
	p := b[len(b)-8:]
	return binary.LittleEndian.Uint16(p), binary.LittleEndian.Uint16(p[2:]), int32(binary.LittleEndian.Uint32(p[4:])), nil
}

// EligibleMouse shares the physical relative-device classifier with tenfoot.
func EligibleMouse(bus uint16, name string, relative, buttons bool) bool {
	return eligibleMouse(bus, name, relative, buttons)
}
