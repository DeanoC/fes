//go:build linux

package tenfoot

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/remoteinput"
)

// key keeps a shared core key held until every other unsuppressed evdev key
// contributing it releases. Contributions stay distinct by physical key,
// including left and right modifiers on one keyboard.
func (ins *nativeInputs) key(app *App, in *nativeInput, code uint16, value int32, now time.Time) bool {
	if value == 0 && in.kind == InputKeyboard && app != nil {
		app.mu.Lock()
		hid := app.session.CoreKeyboardHID
		keyboard := app.session.CoreKeyboard
		app.mu.Unlock()
		coreKey := func(code uint16, name string) (playHIDKey, bool) {
			var event remoteinput.Event
			var ok bool
			if hid {
				event, ok = hidkeys.Event(nativeHIDUsage(code), false)
			} else {
				event, ok = playhid.Event(name, false, keyboard)
			}
			return playHIDKey{event.Player, event.Device, event.Kind, event.Code}, ok
		}
		physical := physicalPlayKey{source: in.fd, key: playHeldKey(nativeKeyName(code), nativeHIDUsage(code))}
		app.mu.Lock()
		key, valid := app.playKeyAdmitted[physical]
		app.mu.Unlock()
		if !valid {
			key, valid = coreKey(code, nativeKeyName(code))
		}
		if valid {
			for _, other := range ins.devices {
				if other == nil || other.kind != InputKeyboard {
					continue
				}
				for heldCode, heldName := range other.keysHeld {
					otherPhysical := physicalPlayKey{source: other.fd, key: playHeldKey(heldName, nativeHIDUsage(heldCode))}
					if otherPhysical == physical {
						continue
					}
					app.mu.Lock()
					otherKey, otherAdmitted := app.playKeyAdmitted[otherPhysical]
					otherSuppressed := app.playKeySuppressed[otherPhysical]
					app.mu.Unlock()
					if otherAdmitted && otherKey == key && !otherSuppressed {
						// Retire this physical hold and its menu command without
						// releasing the core key that the other keyboard holds.
						app.consumeSuppressedPlayKeyFrom(in.fd, nativeKeyName(code), nativeHIDUsage(code), false)
						app.mu.Lock()
						delete(app.playKeyAdmitted, physicalPlayKey{source: in.fd, key: playHeldKey(nativeKeyName(code), nativeHIDUsage(code))})
						app.mu.Unlock()
						delete(in.keysHeld, code)
						if (code == 42 || code == 54) && in.shifts != nil {
							in.shifts[code] = false
						}
						if in.commands != nil {
							app.Release(in.commands[code])
							delete(in.commands, code)
						}
						return false
					}
				}
			}
		}
	}
	return in.key(app, code, value, now)
}

// nativeKeyF12 is MiSTer's menu key. A host session that can change
// removable media opens that selector,
// including when the library has no .p files yet; otherwise it opens the
// playing room, and with no room it stops the session. The key is never
// forwarded.
const nativeKeyF12 = 88

func (in *nativeInput) key(app *App, code uint16, value int32, now time.Time) bool {
	if code == nativeKeyF12 {
		if value == 2 {
			return false
		}
		if in.keysHeld == nil {
			in.keysHeld = make(map[uint16]string)
		}
		if value == 1 {
			in.keysHeld[code] = "f12"
			app.NoteInput(InputKeyboard, in.fd)
		} else {
			delete(in.keysHeld, code)
		}
		app.handlePlayHIDScancode(in.fd, "f12", hidKeyboardF12, value != 0, now)
		return false
	}
	name := nativeKeyName(code)
	if in.keysHeld == nil {
		in.keysHeld = make(map[uint16]string)
	}
	if value != 0 {
		in.keysHeld[code] = name
	} else {
		delete(in.keysHeld, code)
	}
	modifier := code == 42 || code == 54
	if modifier {
		if in.shifts == nil {
			in.shifts = map[uint16]bool{}
		}
		in.shifts[code] = value != 0
	}
	if app.handlePlayHIDScancode(in.fd, name, nativeHIDUsage(code), value != 0, now) {
		return false
	}
	if modifier {
		return false
	}
	shift := in.shifts[42] || in.shifts[54]
	cmd := CommandFromKey(name)
	if shift && cmd == CmdTab {
		cmd = CmdTabPrev
	}
	if value == 0 {
		if in.commands != nil {
			app.Release(in.commands[code])
			delete(in.commands, code)
		}
		return false
	}
	app.NoteInput(InputKeyboard, in.fd)
	if app.OSKOpen() {
		switch name {
		case "backspace":
			app.SearchBackspace(now)
		case "return":
			app.ConfirmSearch(now)
		case "escape", "tab", "up", "down", "left", "right":
			app.Press(cmd, now)
		default:
			app.TypeText(nativeKeyText(code, shift), now)
		}
		if in.commands == nil {
			in.commands = map[uint16]Command{}
		}
		in.commands[code] = cmd
		return false
	}
	if value == 2 {
		return false
	} // App owns navigation repeat.
	if in.commands == nil {
		in.commands = map[uint16]Command{}
	}
	in.commands[code] = cmd
	return app.Press(cmd, now) == CmdQuit
}

var nativeKeys = map[uint16]string{
	29: "ctrl", 42: "shift", 54: "shift", 56: "alt", 97: "ctrl", 100: "alt", 125: "super", 126: "super",
	1: "escape", 2: "1", 3: "2", 4: "3", 5: "4", 6: "5", 7: "6", 8: "7", 9: "8", 10: "9", 11: "0", 12: "minus", 13: "equals", 14: "backspace", 15: "tab",
	16: "q", 17: "w", 18: "e", 19: "r", 20: "t", 21: "y", 22: "u", 23: "i", 24: "o", 25: "p", 26: "leftbracket", 27: "rightbracket", 28: "return",
	30: "a", 31: "s", 32: "d", 33: "f", 34: "g", 35: "h", 36: "j", 37: "k", 38: "l", 39: ";", 40: "'", 41: "`", 43: "\\",
	44: "z", 45: "x", 46: "c", 47: "v", 48: "b", 49: "n", 50: "m", 51: ",", 52: ".", 53: "slash", 57: "space", 102: "home", 103: "up", 105: "left", 106: "right", 108: "down",
}

func nativeKeyName(code uint16) string { return nativeKeys[code] }

func nativeKeyText(code uint16, shift bool) string {
	name := nativeKeyName(code)
	switch name {
	case "space":
		name = " "
	case "minus":
		name = "-"
	case "equals":
		name = "="
	case "leftbracket":
		name = "["
	case "rightbracket":
		name = "]"
	case "slash":
		name = "/"
	}
	if len(name) != 1 {
		return ""
	}
	if !shift {
		return name
	}
	const plain = "1234567890-=[];'`\\,./"
	const shifted = "!@#$%^&*()_+{}:\"~|<>?"
	if i := strings.Index(plain, name); i >= 0 {
		return string(shifted[i])
	}
	return strings.ToUpper(name)
}

// nativeHIDUsage is the USB HID usage of a Linux key, or 0 for a key that
// fes.keyboard.hid sessions do not carry.
func nativeHIDUsage(code uint16) uint8 {
	usage, _ := hidkeys.FromLinuxKey(code)
	return usage
}
