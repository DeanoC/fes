package playhid

// Linux KEY_* codes used by kit evdev keyboards (linux/input-event-codes.h).
const (
	linuxKeyUp    uint16 = 103
	linuxKeyLeft  uint16 = 105
	linuxKeyRight uint16 = 106
	linuxKeyDown  uint16 = 108
)

func linuxArrowName(code uint16) (string, bool) {
	switch code {
	case linuxKeyUp:
		return "up", true
	case linuxKeyDown:
		return "down", true
	case linuxKeyLeft:
		return "left", true
	case linuxKeyRight:
		return "right", true
	default:
		return "", false
	}
}
