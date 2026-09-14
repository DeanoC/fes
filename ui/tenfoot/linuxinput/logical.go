package linuxinput

import (
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/tenfoot/inputmap"
)

// EventFromEvdev converts a known gamepad evdev record to a remoteinput event.
// Keyboard keys stay on MapEvdev.
func EventFromEvdev(typ, code uint16, value int32) (remoteinput.Event, bool) {
	switch typ {
	case evKey:
		if value != 0 && value != 1 && value != 2 {
			return remoteinput.Event{}, false
		}
		name := evdevButtonName(code)
		if name == "" {
			return remoteinput.Event{}, false
		}
		e, err := remoteinput.NormalizeGamepad(name, value != 0)
		return e, err == nil
	case evAbs:
		name := ""
		switch code {
		case absX, absHat0X:
			name = "left-x"
		case absY, absHat0Y:
			name = "left-y"
		default:
			return remoteinput.Event{}, false
		}
		v := value
		if code == absHat0X || code == absHat0Y {
			switch {
			case v < 0:
				v = -32767
			case v > 0:
				v = 32767
			default:
				v = 0
			}
		}
		e, err := remoteinput.NormalizeAxis(name, v)
		return e, err == nil
	default:
		return remoteinput.Event{}, false
	}
}

func evdevButtonName(code uint16) string {
	switch code {
	case btnSouth:
		return "a"
	case 305:
		return "b"
	case 306:
		return "c"
	case btnNorth:
		return "x"
	case btnWest:
		return "y"
	case 310:
		return "l"
	case 311:
		return "r"
	case btnSelect:
		return "select"
	case btnStart, btnMode:
		return "start"
	case btnDpadUp:
		return "dpad-up"
	case btnDpadDn:
		return "dpad-down"
	case btnDpadL:
		return "dpad-left"
	case btnDpadR:
		return "dpad-right"
	default:
		return ""
	}
}

// EventFromJS converts a known joystick record to a remoteinput event.
func EventFromJS(typ uint8, number uint8, value int16) (remoteinput.Event, bool) {
	if typ&JSEventInit != 0 {
		return remoteinput.Event{}, false
	}
	switch typ &^ JSEventInit {
	case JSEventButton:
		name := ""
		switch number {
		case jsConfirmBtn0:
			name = "a"
		case jsQuitBtn7, jsQuitBtn9, jsQuitBtn11:
			name = "start"
		default:
			return remoteinput.Event{}, false
		}
		e, err := remoteinput.NormalizeGamepad(name, value != 0)
		return e, err == nil
	case JSEventAxis:
		name := ""
		switch number {
		case 0:
			name = "left-x"
		case 1:
			name = "left-y"
		default:
			return remoteinput.Event{}, false
		}
		e, err := remoteinput.NormalizeAxis(name, int32(value))
		return e, err == nil
	default:
		return remoteinput.Event{}, false
	}
}

// ActionFromEvent maps a remapped logical event to a spike/grid action.
func ActionFromEvent(e remoteinput.Event) Mapped {
	switch e.Kind {
	case remoteinput.KindButton:
		active := e.Action == remoteinput.ActionPress
		switch e.Code {
		case remoteinput.ButtonA:
			return Mapped{Action: ActionConfirm, Active: active}
		case remoteinput.ButtonStart:
			return Mapped{Action: ActionQuit, Active: active}
		case remoteinput.ButtonDPadUp:
			return Mapped{Action: ActionUp, Active: active}
		case remoteinput.ButtonDPadDown:
			return Mapped{Action: ActionDown, Active: active}
		case remoteinput.ButtonDPadLeft:
			return Mapped{Action: ActionLeft, Active: active}
		case remoteinput.ButtonDPadRight:
			return Mapped{Action: ActionRight, Active: active}
		default:
			return Mapped{}
		}
	case remoteinput.KindAxis:
		dir := axisDir32(e.Value, false)
		switch e.Code {
		case remoteinput.AxisLeftX:
			return dirToMapped(dir, true)
		case remoteinput.AxisLeftY:
			return dirToMapped(dir, false)
		default:
			return Mapped{}
		}
	default:
		return Mapped{}
	}
}

func mapRecordRemapped(kind Kind, rec []byte, remap *inputmap.Remapper) Mapped {
	if remap == nil {
		return mapRecord(kind, rec)
	}
	switch kind {
	case KindJoystick:
		_, value, typ, number, err := ParseJS(rec)
		if err != nil {
			return Mapped{}
		}
		if e, ok := EventFromJS(typ, number, value); ok {
			if dst, hit := remap.PhysicalJS(number); hit && (typ&^JSEventInit) == JSEventButton {
				e.Code = dst
			}
			return ActionFromEvent(remap.Apply(e))
		}
		return MapJS(typ, number, value)
	default:
		typ, code, value, err := ParseEvdev(rec)
		if err != nil {
			return Mapped{}
		}
		if e, ok := EventFromEvdev(typ, code, value); ok {
			if dst, hit := remap.PhysicalButton(code); hit && typ == evKey {
				e.Code = dst
			}
			return ActionFromEvent(remap.Apply(e))
		}
		return MapEvdev(typ, code, value)
	}
}
