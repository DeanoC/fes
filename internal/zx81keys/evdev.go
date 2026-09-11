package zx81keys

import "github.com/DeanoC/FogCast/remoteinput"

// Linux KEY_* codes used by kit evdev keyboards (linux/input-event-codes.h).
const (
	linuxKeyEsc        uint16 = 1
	linuxKey1          uint16 = 2
	linuxKey0          uint16 = 11
	linuxKeyQ          uint16 = 16
	linuxKeyP          uint16 = 25
	linuxKeyEnter      uint16 = 28
	linuxKeyA          uint16 = 30
	linuxKeyL          uint16 = 38
	linuxKeyLeftShift  uint16 = 42
	linuxKeyZ          uint16 = 44
	linuxKeyM          uint16 = 50
	linuxKeyDot        uint16 = 52
	linuxKeyRightShift uint16 = 54
	linuxKeySpace      uint16 = 57
)

// FromLinuxKey maps a USB/evdev key code onto the ZX81 matrix. Unknown keys
// are not admitted so browse leftovers cannot leak onto the ULA.
func FromLinuxKey(code uint16) (remoteinput.Code, bool) {
	switch code {
	case linuxKeyEnter:
		return KeyEnter, true
	case linuxKeySpace:
		return KeySpace, true
	case linuxKeyLeftShift, linuxKeyRightShift:
		return KeyShift, true
	case linuxKeyDot:
		return KeyPeriod, true
	case linuxKey0:
		return Digit(0), true
	case linuxKeyEsc:
		return 0, false
	}
	if code >= linuxKey1 && code <= linuxKey1+8 {
		return Digit(byte(code - linuxKey1 + 1)), true
	}
	if code >= linuxKeyQ && code <= linuxKeyP {
		letters := []byte("QWERTYUIOP")
		return Letter(letters[code-linuxKeyQ]), true
	}
	if code >= linuxKeyA && code <= linuxKeyL {
		letters := []byte("ASDFGHJKL")
		return Letter(letters[code-linuxKeyA]), true
	}
	if code >= linuxKeyZ && code <= linuxKeyM {
		letters := []byte("ZXCVBNM")
		return Letter(letters[code-linuxKeyZ]), true
	}
	return 0, false
}
