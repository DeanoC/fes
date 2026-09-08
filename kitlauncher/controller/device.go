package controller

import (
	"encoding/binary"
	"errors"
	"strings"
)

// eligible rejects the retained virtual pad (BUS_VIRTUAL) and non-gamepads.
func eligible(bus uint16, name string, buttons bool) bool {
	return bus != 6 && name != "FogCast Virtual Gamepad" && buttons
}
func decode(b []byte) (uint16, uint16, int32, error) {
	if len(b) != 16 && len(b) != 24 {
		return 0, 0, 0, errors.New("invalid input record")
	}
	p := b[len(b)-8:]
	return binary.LittleEndian.Uint16(p), binary.LittleEndian.Uint16(p[2:]), int32(binary.LittleEndian.Uint32(p[4:])), nil
}
func cstring(b []byte) string { return strings.TrimRight(string(b), "\x00") }
