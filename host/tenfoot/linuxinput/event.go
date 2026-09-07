// Package linuxinput is a CGO-free Linux evdev and joystick reader for the
// tenfoot linuxfb spike. Parse and map helpers have no device dependency.
package linuxinput

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Record sizes on the MiSTer ARMv7 ABI. Joystick js_event is 8 bytes on
// every Linux; evdev input_event is timeval(8)+type+code+value = 16 bytes.
const (
	EvdevSize = 16
	JSSize    = 8
)

// Kind selects the on-disk record format.
type Kind int

const (
	KindUnknown Kind = iota
	KindEvdev
	KindJoystick
)

func (k Kind) String() string {
	switch k {
	case KindEvdev:
		return "evdev"
	case KindJoystick:
		return "joystick"
	default:
		return "unknown"
	}
}

// Action is one spike command: move a highlight or quit.
type Action int

const (
	ActionNone Action = iota
	ActionUp
	ActionDown
	ActionLeft
	ActionRight
	ActionQuit
)

func (a Action) String() string {
	switch a {
	case ActionUp:
		return "up"
	case ActionDown:
		return "down"
	case ActionLeft:
		return "left"
	case ActionRight:
		return "right"
	case ActionQuit:
		return "quit"
	default:
		return "none"
	}
}

// Mapped is a parse/map result. Active is pressed or analog-outside-deadzone.
// Analog marks abs/js-axis samples so a stick idle does not cancel a d-pad
// hold. Repeat is EV_KEY autorepeat (value 2).
type Mapped struct {
	Action Action
	Active bool
	Source string
	Analog bool
	Repeat bool
}

func (m Mapped) String() string {
	return fmt.Sprintf("%s active=%v analog=%v src=%s", m.Action, m.Active, m.Analog, m.Source)
}

const (
	evSyn = 0
	evKey = 1
	evAbs = 3

	keyEsc   uint16 = 1
	keyQ     uint16 = 16
	keyUp    uint16 = 103
	keyLeft  uint16 = 105
	keyRight uint16 = 106
	keyDown  uint16 = 108

	btnSouth  uint16 = 304
	btnEast   uint16 = 305
	btnNorth  uint16 = 307
	btnWest   uint16 = 308
	btnSelect uint16 = 314
	btnStart  uint16 = 315
	btnMode   uint16 = 316
	btnDpadUp uint16 = 544
	btnDpadDn uint16 = 545
	btnDpadL  uint16 = 546
	btnDpadR  uint16 = 547

	absX     uint16 = 0
	absY     uint16 = 1
	absHat0X uint16 = 16
	absHat0Y uint16 = 17

	axisGate    int32 = 16000
	jsAxisGate  int16 = 16384
	jsQuitBtn7  uint8 = 7
	jsQuitBtn9  uint8 = 9
	jsQuitBtn11 uint8 = 11
)

// JS event type bits (linux/joystick.h).
const (
	JSEventButton = 0x01
	JSEventAxis   = 0x02
	JSEventInit   = 0x80
)

var errShort = errors.New("linuxinput: short record")

// KindFromPath infers evdev vs joystick from a device or fifo path.
func KindFromPath(path string) Kind {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, "js") || strings.HasSuffix(base, ".js") {
		return KindJoystick
	}
	if strings.HasPrefix(base, "event") || strings.Contains(base, "evdev") {
		return KindEvdev
	}
	return KindUnknown
}

// ParseEvdev reads one 16-byte ARMv7 input_event. Timeval is ignored.
func ParseEvdev(b []byte) (typ, code uint16, value int32, err error) {
	if len(b) < EvdevSize {
		return 0, 0, 0, errShort
	}
	typ = binary.LittleEndian.Uint16(b[8:10])
	code = binary.LittleEndian.Uint16(b[10:12])
	value = int32(binary.LittleEndian.Uint32(b[12:16]))
	return typ, code, value, nil
}

// ParseJS reads one 8-byte js_event.
func ParseJS(b []byte) (time uint32, value int16, typ, number uint8, err error) {
	if len(b) < JSSize {
		return 0, 0, 0, 0, errShort
	}
	time = binary.LittleEndian.Uint32(b[0:4])
	value = int16(binary.LittleEndian.Uint16(b[4:6]))
	return time, value, b[6], b[7], nil
}

// EncodeEvdev writes a 16-byte ARMv7 input_event (timeval zero).
func EncodeEvdev(typ, code uint16, value int32) []byte {
	var b [EvdevSize]byte
	binary.LittleEndian.PutUint16(b[8:], typ)
	binary.LittleEndian.PutUint16(b[10:], code)
	binary.LittleEndian.PutUint32(b[12:], uint32(value))
	return b[:]
}

// EncodeJS writes an 8-byte js_event (timestamp zero).
func EncodeJS(value int16, typ, number uint8) []byte {
	var b [JSSize]byte
	binary.LittleEndian.PutUint16(b[4:], uint16(value))
	b[6] = typ
	b[7] = number
	return b[:]
}

// MapEvdev maps one evdev record to a spike action.
func MapEvdev(typ, code uint16, value int32) Mapped {
	switch typ {
	case evKey:
		return mapKey(code, value)
	case evAbs:
		return mapAbs(code, value)
	case evSyn:
		return Mapped{}
	default:
		return Mapped{}
	}
}

// MapJS maps one joystick record. Init events are ignored so open does not
// shove the cursor.
func MapJS(typ uint8, number uint8, value int16) Mapped {
	if typ&JSEventInit != 0 {
		return Mapped{}
	}
	switch typ &^ JSEventInit {
	case JSEventButton:
		return mapJSButton(number, value != 0)
	case JSEventAxis:
		return mapJSAxis(number, value)
	default:
		return Mapped{}
	}
}

func mapKey(code uint16, value int32) Mapped {
	active := value != 0
	repeat := value == 2
	var m Mapped
	switch code {
	case keyEsc, keyQ, btnStart, btnMode:
		m = Mapped{Action: ActionQuit, Active: active}
	case keyUp, btnDpadUp:
		m = Mapped{Action: ActionUp, Active: active}
	case keyDown, btnDpadDn:
		m = Mapped{Action: ActionDown, Active: active}
	case keyLeft, btnDpadL:
		m = Mapped{Action: ActionLeft, Active: active}
	case keyRight, btnDpadR:
		m = Mapped{Action: ActionRight, Active: active}
	default:
		return Mapped{}
	}
	m.Repeat = repeat
	return m
}

func mapAbs(code uint16, value int32) Mapped {
	hat := code == absHat0X || code == absHat0Y
	dir := axisDir32(value, hat)
	switch code {
	case absX, absHat0X:
		return dirToMapped(dir, true)
	case absY, absHat0Y:
		return dirToMapped(dir, false)
	default:
		return Mapped{}
	}
}

func mapJSButton(number uint8, active bool) Mapped {
	switch number {
	case jsQuitBtn7, jsQuitBtn9, jsQuitBtn11:
		return Mapped{Action: ActionQuit, Active: active}
	default:
		return Mapped{}
	}
}

func mapJSAxis(number uint8, value int16) Mapped {
	dir := axisDir16(value)
	switch number {
	case 0:
		return dirToMapped(dir, true)
	case 1:
		return dirToMapped(dir, false)
	default:
		return Mapped{}
	}
}

func dirToMapped(dir int, horizontal bool) Mapped {
	if horizontal {
		if dir < 0 {
			return Mapped{Action: ActionLeft, Active: true, Analog: true}
		}
		if dir > 0 {
			return Mapped{Action: ActionRight, Active: true, Analog: true}
		}
		return Mapped{Action: ActionLeft, Active: false, Analog: true}
	}
	if dir < 0 {
		return Mapped{Action: ActionUp, Active: true, Analog: true}
	}
	if dir > 0 {
		return Mapped{Action: ActionDown, Active: true, Analog: true}
	}
	return Mapped{Action: ActionUp, Active: false, Analog: true}
}

func axisDir32(v int32, hat bool) int {
	if hat {
		if v < 0 {
			return -1
		}
		if v > 0 {
			return 1
		}
		return 0
	}
	if v > axisGate {
		return 1
	}
	if v < -axisGate {
		return -1
	}
	return 0
}

func axisDir16(v int16) int {
	if v > jsAxisGate {
		return 1
	}
	if v < -jsAxisGate {
		return -1
	}
	return 0
}

func mapRecord(kind Kind, rec []byte) Mapped {
	switch kind {
	case KindJoystick:
		_, value, typ, number, err := ParseJS(rec)
		if err != nil {
			return Mapped{}
		}
		return MapJS(typ, number, value)
	default:
		typ, code, value, err := ParseEvdev(rec)
		if err != nil {
			return Mapped{}
		}
		return MapEvdev(typ, code, value)
	}
}
