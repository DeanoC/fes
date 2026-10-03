// Package joymatrix maps a first-player pad onto the Coleco first-slice keyboard matrix.
package joymatrix

import (
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

const AxisDeadzone int16 = 8000

func Supports(core string) bool {
	return core == "fes.coleco" || core == "fes.sms" || core == "fes.sg1000"
}

func ButtonKey(code remoteinput.Code) (remoteinput.Code, bool) {
	switch code {
	case remoteinput.ButtonDPadUp:
		return zx81keys.KeyShift, true
	case remoteinput.ButtonDPadRight:
		return zx81keys.Letter('Z'), true
	case remoteinput.ButtonDPadDown:
		return zx81keys.Letter('X'), true
	case remoteinput.ButtonDPadLeft:
		return zx81keys.Letter('C'), true
	case remoteinput.ButtonA:
		return zx81keys.Letter('V'), true
	case remoteinput.ButtonB:
		return zx81keys.Letter('Q'), true
	}
	return 0, false
}

func AxisKey(code remoteinput.Code, value int16) (remoteinput.Code, bool) {
	switch code {
	case remoteinput.AxisLeftX:
		if value < -AxisDeadzone {
			return zx81keys.Letter('C'), true
		}
		if value > AxisDeadzone {
			return zx81keys.Letter('Z'), true
		}
	case remoteinput.AxisLeftY:
		if value < -AxisDeadzone {
			return zx81keys.KeyShift, true
		}
		if value > AxisDeadzone {
			return zx81keys.Letter('X'), true
		}
	}
	return 0, false
}

// Desired contains only matrix controls. Start, Select, and unrelated pad
// buttons have no keyboard meaning. SMS and SG-1000 expose Fire1 only.
func Desired(core string, snapshot remoteinput.Snapshot) map[remoteinput.Code]bool {
	desired := make(map[remoteinput.Code]bool)
	for _, code := range snapshot.Pressed {
		if (core == "fes.sms" || core == "fes.sg1000") && code == remoteinput.ButtonB {
			continue
		}
		if key, ok := ButtonKey(code); ok {
			desired[key] = true
		}
	}
	for code, value := range snapshot.Axes {
		if key, ok := AxisKey(code, value); ok {
			desired[key] = true
		}
	}
	return desired
}
