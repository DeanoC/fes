package inputmap

import (
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/remoteinput"
)

// logicalNames are the controls a profile binds: a pad's buttons after its
// mapping, and the keypad keys a keypad-port core reads.
var logicalNames = map[string]remoteinput.Code{
	"dpad-up":     remoteinput.ButtonDPadUp,
	"dpad-down":   remoteinput.ButtonDPadDown,
	"dpad-left":   remoteinput.ButtonDPadLeft,
	"dpad-right":  remoteinput.ButtonDPadRight,
	"a":           remoteinput.ButtonA,
	"b":           remoteinput.ButtonB,
	"c":           remoteinput.ButtonC,
	"x":           remoteinput.ButtonX,
	"y":           remoteinput.ButtonY,
	"l":           remoteinput.ButtonL,
	"r":           remoteinput.ButtonR,
	"start":       remoteinput.ButtonStart,
	"select":      remoteinput.ButtonSelect,
	"keypad-0":    remoteinput.Keypad0,
	"keypad-1":    remoteinput.Keypad0 + 1,
	"keypad-2":    remoteinput.Keypad0 + 2,
	"keypad-3":    remoteinput.Keypad0 + 3,
	"keypad-4":    remoteinput.Keypad0 + 4,
	"keypad-5":    remoteinput.Keypad0 + 5,
	"keypad-6":    remoteinput.Keypad0 + 6,
	"keypad-7":    remoteinput.Keypad0 + 7,
	"keypad-8":    remoteinput.Keypad0 + 8,
	"keypad-9":    remoteinput.Keypad9,
	"keypad-star": remoteinput.KeypadStar,
	"keypad-hash": remoteinput.KeypadHash,
}

func parseLogical(name string) (remoteinput.Code, error) {
	code, ok := logicalNames[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return 0, fmt.Errorf("inputmap: unknown control %q", name)
	}
	return code, nil
}
