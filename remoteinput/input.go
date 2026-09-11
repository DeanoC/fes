package remoteinput

import (
	"fmt"
	"math"
	"sort"

	"github.com/DeanoC/FogCast/protocol"
)

type Device uint8

const (
	DeviceKeyboard Device = iota
	DeviceGamepad
)

type Kind uint8

const (
	KindKey Kind = iota
	KindButton
	KindAxis
	KindSystem
)

type Action uint8

const (
	ActionRelease Action = iota
	ActionPress
	ActionAbsolute
)

type Code uint16

const (
	KeyEscape       Code = 1
	KeyA            Code = 2
	KeyLeft         Code = 3
	KeyRight        Code = 4
	KeyUp           Code = 5
	KeyDown         Code = 6
	ButtonDPadUp    Code = 100
	ButtonDPadDown  Code = 101
	ButtonDPadLeft  Code = 102
	ButtonDPadRight Code = 103
	ButtonA         Code = 104
	ButtonB         Code = 105
	ButtonStart     Code = 106
	ButtonSelect    Code = 107
	ButtonC         Code = Code(protocol.InputCodeButtonC)
	ButtonX         Code = Code(protocol.InputCodeButtonX)
	ButtonY         Code = Code(protocol.InputCodeButtonY)
	ButtonL         Code = Code(protocol.InputCodeButtonL)
	ButtonR         Code = Code(protocol.InputCodeButtonR)

	AxisLeftX Code = 200
	AxisLeftY Code = 201
)

type Event struct {
	Device Device
	Kind   Kind
	Action Action
	Code   Code
	Value  int32
}
type Snapshot struct {
	Pressed []Code
	Axes    map[Code]int16
}
type State struct {
	pressed map[Code]bool
	axes    map[Code]int16
}

func (s *State) Apply(e Event) error {
	if e.Device > DeviceGamepad || e.Kind > KindAxis || e.Action > ActionAbsolute {
		return fmt.Errorf("unsupported input event")
	}
	if s.pressed == nil {
		s.pressed = make(map[Code]bool)
	}
	if s.axes == nil {
		s.axes = make(map[Code]int16)
	}
	if e.Kind == KindAxis {
		if e.Code != AxisLeftX && e.Code != AxisLeftY {
			return fmt.Errorf("unsupported axis code %d", e.Code)
		}
		if e.Action != ActionAbsolute {
			return fmt.Errorf("axis action must be absolute")
		}
		s.axes[e.Code] = clampAxis(e.Value)
		return nil
	}
	if e.Action == ActionPress {
		s.pressed[e.Code] = true
	} else if e.Action == ActionRelease {
		delete(s.pressed, e.Code)
	} else {
		return fmt.Errorf("non-axis event cannot be absolute")
	}
	return nil
}
func (s *State) Pressed(c Code) bool { return s.pressed[c] }
func (s *State) ReleaseAll()         { s.pressed = make(map[Code]bool); s.axes = make(map[Code]int16) }
func (s *State) Snapshot() Snapshot {
	p := make([]Code, 0, len(s.pressed))
	for c := range s.pressed {
		p = append(p, c)
	}
	sort.Slice(p, func(i, j int) bool { return p[i] < p[j] })
	a := make(map[Code]int16, len(s.axes))
	for c, v := range s.axes {
		a[c] = v
	}
	return Snapshot{Pressed: p, Axes: a}
}
func clampAxis(v int32) int16 {
	if v > math.MaxInt16 {
		return math.MaxInt16
	}
	if v < math.MinInt16 {
		return math.MinInt16
	}
	return int16(v)
}
func NormalizeKeyboard(key string, pressed bool) (Event, error) {
	m := map[string]Code{"Escape": KeyEscape, "a": KeyA, "ArrowLeft": KeyLeft, "ArrowRight": KeyRight, "ArrowUp": KeyUp, "ArrowDown": KeyDown}
	c, ok := m[key]
	if !ok {
		return Event{}, fmt.Errorf("unsupported keyboard key %q", key)
	}
	return Event{Device: DeviceKeyboard, Kind: KindKey, Action: action(pressed), Code: c}, nil
}
func NormalizeGamepad(button string, pressed bool) (Event, error) {
	m := map[string]Code{"dpad-up": ButtonDPadUp, "dpad-down": ButtonDPadDown, "dpad-left": ButtonDPadLeft, "dpad-right": ButtonDPadRight, "a": ButtonA, "b": ButtonB, "c": ButtonC, "x": ButtonX, "y": ButtonY, "l": ButtonL, "r": ButtonR, "start": ButtonStart, "select": ButtonSelect}
	c, ok := m[button]
	if !ok {
		return Event{}, fmt.Errorf("unsupported gamepad button %q", button)
	}
	return Event{Device: DeviceGamepad, Kind: KindButton, Action: action(pressed), Code: c}, nil
}
func NormalizeAxis(axis string, value int32) (Event, error) {
	m := map[string]Code{"left-x": AxisLeftX, "left-y": AxisLeftY}
	c, ok := m[axis]
	if !ok {
		return Event{}, fmt.Errorf("unsupported gamepad axis %q", axis)
	}
	return Event{Device: DeviceGamepad, Kind: KindAxis, Action: ActionAbsolute, Code: c, Value: int32(clampAxis(value))}, nil
}
func action(pressed bool) Action {
	if pressed {
		return ActionPress
	}
	return ActionRelease
}

// EventForCode reconstructs Device/Kind from a held code. Keyboard is 0–99,
// gamepad buttons 100–199, axes 200–255, and ZX81 matrix keys 256+.
func EventForCode(code Code, action Action) Event {
	event := Event{Code: code, Action: action}
	switch {
	case code < 100:
		event.Device = DeviceKeyboard
		event.Kind = KindKey
	case code < 200:
		event.Device = DeviceGamepad
		event.Kind = KindButton
	case code < 256:
		event.Device = DeviceGamepad
		event.Kind = KindAxis
	default:
		event.Device = DeviceKeyboard
		event.Kind = KindKey
	}
	return event
}
