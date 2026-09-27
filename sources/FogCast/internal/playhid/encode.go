// Package playhid encodes USB keyboard HID for an attached play session.
package playhid

import (
	"strings"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

// KeyboardMode is how the attached core consumes keyboard frames. It comes
// from the exact negotiated interfaces, never a core ID.
type KeyboardMode uint8

const (
	// NativeKeys rewrites a few keys onto gamepad buttons for native cores.
	NativeKeys KeyboardMode = iota
	// MatrixKeys keeps ZX81 matrix codes for fes.keyboard 1.0.
	MatrixKeys
	// HIDKeys keeps physical HID usages for fes.keyboard.hid 1.0.
	HIDKeys
)

// ChromeStop reports session-stop keys that sofa chrome must keep while play
// HID is attached. Esc/Backspace stop; letter s stays a core key (ZX81 S).
func ChromeStop(name string) bool {
	switch normalize(name) {
	case "escape", "backspace":
		return true
	default:
		return false
	}
}

// Event maps a USB key onto the attached core's HID encoding.
// fes.keyboard uses the ZX81 matrix (codes 256+). Native SNES/MD use gamepad
// buttons (codes 100–112) so muxSink does not route them to set_keyboard and
// reconnect replay does not treat them as axes.
func Event(name string, down, coreKeyboard bool) (remoteinput.Event, bool) {
	name = normalize(name)
	if name == "" || ChromeStop(name) {
		return remoteinput.Event{}, false
	}
	if coreKeyboard {
		return zx81Event(name, down)
	}
	return nativeEvent(name, down)
}

// PhysicalEvent maps an evdev KEY_* onto the mapper's session-neutral form:
// the key's USB HID usage. Keys without a Keyboard/Keypad usage are dropped.
// StreamEvent remaps that form for the attached core.
func PhysicalEvent(linuxKey uint16, down bool) (remoteinput.Event, bool) {
	usage, ok := hidkeys.FromLinuxKey(linuxKey)
	if !ok {
		return remoteinput.Event{}, false
	}
	return hidkeys.Event(usage, down)
}

// StreamEvent remaps a mapper event for the attached core. Gamepad frames pass
// through. HID sessions keep physical usages, including Esc and Backspace, and
// map older ZX81/arrow codes to their physical keys. fes.keyboard keeps ZX81
// keys and drops non-matrix keyboard frames. Native cores rewrite keyboard
// frames onto gamepad buttons. For the matrix and native modes a HID usage
// first becomes its legacy code, so those cores see exactly the keys the
// pre-HID mapper produced.
func StreamEvent(e remoteinput.Event, mode KeyboardMode) (remoteinput.Event, bool) {
	if e.Device == remoteinput.DeviceGamepad || e.Kind == remoteinput.KindButton || e.Kind == remoteinput.KindAxis {
		return e, true
	}
	if e.Device != remoteinput.DeviceKeyboard && e.Kind != remoteinput.KindKey {
		return remoteinput.Event{}, false
	}
	if usage, ok := hidkeys.Usage(e.Code); ok {
		if mode == HIDKeys {
			return e, true
		}
		legacy, ok := hidkeys.Legacy(usage)
		if !ok {
			return remoteinput.Event{}, false
		}
		e.Code = legacy
	} else if mode == HIDKeys {
		usage, ok := hidkeys.FromLegacy(e.Code)
		if !ok {
			return remoteinput.Event{}, false
		}
		return hidkeys.Event(usage, e.Action == remoteinput.ActionPress)
	}
	if mode == MatrixKeys {
		return e, e.Code >= zx81keys.KeyShift
	}
	name, ok := nameFromPlayCode(e.Code)
	if !ok {
		return remoteinput.Event{}, false
	}
	return nativeEvent(name, e.Action == remoteinput.ActionPress)
}

func zx81Event(name string, down bool) (remoteinput.Event, bool) {
	var key remoteinput.Code
	switch name {
	case "return":
		key = zx81keys.KeyEnter
	case "space":
		key = zx81keys.KeySpace
	case "shift":
		key = zx81keys.KeyShift
	case "period":
		key = zx81keys.KeyPeriod
	default:
		if len(name) == 1 && name[0] >= '0' && name[0] <= '9' {
			key = zx81keys.Digit(name[0] - '0')
			break
		}
		if len(name) == 1 && name[0] >= 'a' && name[0] <= 'z' {
			key = zx81keys.Letter(name[0])
			break
		}
		return remoteinput.Event{}, false
	}
	if key == 0 {
		return remoteinput.Event{}, false
	}
	return keyEvent(remoteinput.DeviceKeyboard, remoteinput.KindKey, key, down), true
}

func nativeEvent(name string, down bool) (remoteinput.Event, bool) {
	var key remoteinput.Code
	switch name {
	case "up", "w":
		key = remoteinput.ButtonDPadUp
	case "down", "s":
		key = remoteinput.ButtonDPadDown
	case "left", "a":
		key = remoteinput.ButtonDPadLeft
	case "right", "d":
		key = remoteinput.ButtonDPadRight
	case "return", "space", "z":
		key = remoteinput.ButtonA
	case "x":
		key = remoteinput.ButtonB
	case "c":
		key = remoteinput.ButtonC
	case "v":
		key = remoteinput.ButtonY
	case "shift":
		key = remoteinput.ButtonSelect
	case "tab":
		key = remoteinput.ButtonStart
	default:
		return remoteinput.Event{}, false
	}
	return keyEvent(remoteinput.DeviceGamepad, remoteinput.KindButton, key, down), true
}

func nameFromPlayCode(code remoteinput.Code) (string, bool) {
	switch code {
	case remoteinput.KeyLeft:
		return "left", true
	case remoteinput.KeyRight:
		return "right", true
	case remoteinput.KeyUp:
		return "up", true
	case remoteinput.KeyDown:
		return "down", true
	default:
		return zx81keys.Name(code)
	}
}

func keyEvent(device remoteinput.Device, kind remoteinput.Kind, code remoteinput.Code, down bool) remoteinput.Event {
	action := remoteinput.ActionRelease
	if down {
		action = remoteinput.ActionPress
	}
	return remoteinput.Event{Device: device, Kind: kind, Action: action, Code: code}
}

func normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
