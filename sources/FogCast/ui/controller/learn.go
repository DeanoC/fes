package controller

import "fmt"

// Learner reads a pad's raw controls while its player maps it step by step.
// It names each control as a GameControllerDB binding source, numbered the
// way NewLearnedMapper reads it back: bN for a key, hN.M for a hat
// direction, +aN or -aN for one half of an axis that rests in the middle,
// and aN or aN~ for an axis that rests at its low or high end, as a
// trigger does.
type Learner struct {
	buttons map[uint16]int
	hats    map[uint16]int
	axes    map[uint16]int
	ranges  map[uint16]Range
	rest    map[uint16]int32
	// down is each control that is off its rest, by its source.
	down map[string]bool
	// axisDown is the source each axis holds down, if any.
	axisDown map[uint16]string
	// keys are the evdev keys held down.
	keys map[uint16]bool
}

// NewLearner reads a pad whose axes are at rest at the given values. An
// axis missing from rest is taken to rest in the middle of its range.
func NewLearner(d Device, rest map[uint16]int32) *Learner {
	buttons, hats, axes := sdlControls(d.Keys, d.Abs)
	l := &Learner{buttons: map[uint16]int{}, hats: map[uint16]int{}, axes: map[uint16]int{},
		ranges: d.Axes, rest: map[uint16]int32{}, down: map[string]bool{}, axisDown: map[uint16]string{},
		keys: map[uint16]bool{}}
	for i, code := range buttons {
		l.buttons[code] = i
	}
	for i, code := range hats {
		l.hats[code], l.hats[code+1] = i, i
	}
	for i, code := range axes {
		l.axes[code] = i
	}
	for code, r := range d.Axes {
		l.rest[code] = r.Min + (r.Max-r.Min)/2
	}
	for code, v := range rest {
		l.rest[code] = v
	}
	return l
}

// Feed reads one raw evdev event. It returns a control that has just left
// its rest while every other control was at rest, so one press counts once
// even on a pad that reports it as a key and an axis together.
func (l *Learner) Feed(typ, code uint16, value int32) (string, bool) {
	idle := l.Idle()
	source, pressed := "", false
	switch {
	case typ == 1:
		n, ok := l.buttons[code]
		if !ok || (value != 0 && value != 1) {
			return "", false
		}
		source, pressed = fmt.Sprintf("b%d", n), value == 1
		l.set(source, pressed)
		if pressed {
			l.keys[code] = true
		} else {
			delete(l.keys, code)
		}
	case typ == 3:
		if n, ok := l.hats[code]; ok {
			source, pressed = l.hat(code, n, value)
		} else if _, ok := l.axes[code]; ok {
			source, pressed = l.axis(code, value)
		} else {
			return "", false
		}
	default:
		return "", false
	}
	return source, pressed && idle && source != ""
}

func (l *Learner) set(source string, down bool) {
	if down {
		l.down[source] = true
	} else {
		delete(l.down, source)
	}
}

// hat reads one axis of a hat. SDL's masks are 1 up, 2 right, 4 down,
// 8 left; the x axis is the pair's even code.
func (l *Learner) hat(code uint16, n int, value int32) (string, bool) {
	masks := [2][2]int{{8, 2}, {1, 4}}[(code-absHat0X)%2]
	if old := l.axisDown[code]; old != "" {
		l.set(old, false)
		delete(l.axisDown, code)
	}
	if value == 0 {
		return "", false
	}
	mask := masks[0]
	if value > 0 {
		mask = masks[1]
	}
	source := fmt.Sprintf("h%d.%d", n, mask)
	l.axisDown[code] = source
	l.set(source, true)
	return source, true
}

// axis reads a stick or trigger. A control counts as moved half way from
// its rest towards the end it moves to, and back at rest within a quarter
// of that, so a noisy axis does not chatter.
func (l *Learner) axis(code uint16, value int32) (string, bool) {
	r, ok := l.ranges[code]
	if !ok || r.Max <= r.Min {
		return "", false
	}
	rest := int64(l.rest[code])
	span := int64(r.Max) - int64(r.Min)
	trigger := 0
	switch {
	case rest-int64(r.Min) < span/4:
		trigger = 1
	case int64(r.Max)-rest < span/4:
		trigger = -1
	}
	offset := int64(value) - rest
	reach := int64(r.Max) - rest
	if offset < 0 {
		reach = rest - int64(r.Min)
	}
	if offset < 0 {
		offset = -offset
	}
	n := l.axes[code]
	old := l.axisDown[code]
	if old != "" {
		if offset*4 > reach {
			return old, false
		}
		l.set(old, false)
		delete(l.axisDown, code)
		return old, false
	}
	if reach <= 0 || offset*2 < reach {
		return "", false
	}
	var source string
	switch {
	case trigger > 0:
		source = fmt.Sprintf("a%d", n)
	case trigger < 0:
		source = fmt.Sprintf("a%d~", n)
	case int64(value) > rest:
		source = fmt.Sprintf("+a%d", n)
	default:
		source = fmt.Sprintf("-a%d", n)
	}
	l.axisDown[code] = source
	l.set(source, true)
	return source, true
}

// Idle reports whether every control is at rest.
func (l *Learner) Idle() bool { return len(l.down) == 0 }

// Held reports whether a source is off its rest now.
func (l *Learner) Held(source string) bool { return l.down[source] }

// HeldKeys are the evdev keys down now, for the pad's mapper to ignore
// until they come up.
func (l *Learner) HeldKeys() []uint16 {
	keys := make([]uint16, 0, len(l.keys))
	for code := range l.keys {
		keys = append(keys, code)
	}
	return keys
}
