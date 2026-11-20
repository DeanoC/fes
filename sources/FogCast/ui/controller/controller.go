// Package controller normalizes physical kit gamepads independently of UI layout.
package controller

import (
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/internal/playhid"
	"github.com/DeanoC/FogCast/remoteinput"
)

type Range struct{ Min, Max int32 }
type Mapper struct {
	keyboard               bool
	mouse                  bool
	mouseButtons           uint8
	mouseX, mouseY         int32
	mouseDirty, mouseFault bool
	layout                 layout
	mapping                string
	learned                bool
	axes                   map[uint16]Range
	suppressed             map[uint16]bool
	// The left stick and a D-pad hat share left-x/left-y but keep separate
	// state, so stick jitter cannot cancel a held hat.
	stick, hat [2]int32
	// held is each axis button that is down, by its axis and its place in
	// layout.axisButtons.
	held map[uint16][]bool
}

// layout binds a pad's evdev codes to remoteinput gamepad controls.
type layout struct {
	buttons map[uint16]string
	axes    map[uint16]axisBinding
	// axisButtons are buttons a mapping binds to a hat direction or an
	// axis, by the evdev axis they read.
	axisButtons map[uint16][]axisButton
	// guide is the launcher button's evdev code, or 0 when the pad has none.
	// It never reaches a core.
	guide uint16
}

// axisBinding feeds left-x (0) or left-y (1). A hat source owns its axis
// while held.
type axisBinding struct {
	axis        int
	hat, invert bool
}

// axisButton is a button that is down while its axis is at or past
// threshold: above it, or below it when above is false.
type axisButton struct {
	code      uint16
	name      string
	threshold int32
	above     bool
}

// fallbackLayout reads a pad GameControllerDB does not list: Linux
// gamepad-API codes as the kit has always read them, and BTN_TRIGGER-range
// keys in the layout the DB's generic HID pads most often share.
func fallbackLayout() layout {
	return layout{
		buttons: map[uint16]string{
			304: "a", 305: "b", 306: "c", 307: "x", 308: "y", 310: "l", 311: "r", 314: "select", 315: "start",
			544: "dpad-up", 545: "dpad-down", 546: "dpad-left", 547: "dpad-right",
			288: "x", 289: "a", 290: "b", 291: "y", 292: "l", 293: "r", 296: "select", 297: "start",
		},
		axes:  map[uint16]axisBinding{0: {axis: 0}, 1: {axis: 1}, 16: {axis: 0, hat: true}, 17: {axis: 1, hat: true}},
		guide: btnMode,
	}
}

// NewMapper reads a pad through its GameControllerDB mapping, or through
// the fallback layout when the DB does not list it.
func NewMapper(d Device) *Mapper {
	m := &Mapper{layout: fallbackLayout(), axes: d.Axes, suppressed: map[uint16]bool{}}
	if e, ok := lookupSDL(d); ok {
		m.layout, m.mapping = sdlLayout(e, d.Keys, d.Abs, d.Axes), e.name
	}
	return m
}

// Recognised reports whether GameControllerDB has a mapping for the pad.
func (d Device) Recognised() bool {
	_, ok := lookupSDL(d)
	return ok
}

// LearnedMapping is the name Mapping reports for a pad read through a
// mapping its player made step by step.
const LearnedMapping = "your own mapping"

// NewLearnedMapper reads a pad through a mapping in GameControllerDB's
// binding form ("a:b0,b:b1,dpup:h0.1,..."), as Settings › Controllers saves
// one for a pad its player mapped step by step.
func NewLearnedMapper(d Device, mapping string) (*Mapper, error) {
	e := sdlEntry{name: LearnedMapping, bindings: map[string]string{}}
	for _, field := range strings.Split(mapping, ",") {
		if field = strings.TrimSpace(field); field == "" {
			continue
		}
		target, source, ok := strings.Cut(field, ":")
		if _, known := sdlButtons[target]; !ok || !known {
			return nil, fmt.Errorf("controller: mapping %q: unknown control %q", mapping, target)
		}
		e.bindings[target] = source
	}
	if len(e.bindings) == 0 {
		return nil, fmt.Errorf("controller: empty mapping")
	}
	m := &Mapper{layout: sdlLayout(e, d.Keys, d.Abs, d.Axes), mapping: LearnedMapping, learned: true,
		axes: d.Axes, suppressed: map[uint16]bool{}}
	return m, nil
}

// Mapping names the GameControllerDB entry in use; empty is the fallback layout.
func (m *Mapper) Mapping() string {
	if m == nil {
		return ""
	}
	return m.mapping
}

// Guide reports the pad's launcher button: its GameControllerDB guide
// binding, or BTN_MODE on a pad whose mapping leaves that code free.
func (m *Mapper) Guide(code uint16) bool {
	return m != nil && !m.mouse && !m.keyboard && m.layout.guide != 0 && code == m.layout.guide
}

// Learned reports a pad read through a mapping its player made.
func (m *Mapper) Learned() bool { return m != nil && m.learned }

func NewMouseMapper() *Mapper { return &Mapper{mouse: true, suppressed: map[uint16]bool{}} }

func NewKeyboardMapper() *Mapper {
	return &Mapper{keyboard: true, suppressed: map[uint16]bool{}}
}
func (m *Mapper) Suppress(code uint16) { m.suppressed[code] = true }
func (m *Mapper) Map(typ, code uint16, value int32) (remoteinput.Event, bool) {
	if m != nil && m.mouse {
		return m.mapMouse(typ, code, value)
	}
	if m != nil && m.keyboard {
		if typ == 1 && m.suppressed[code] {
			if value == 0 {
				delete(m.suppressed, code)
			}
			return remoteinput.Event{}, false
		}
		return mapKeyboard(typ, code, value)
	}
	events := m.Events(typ, code, value)
	if len(events) == 0 {
		return remoteinput.Event{}, false
	}
	return events[0], true
}

// Events maps one evdev event of a pad to what it moves: a button, the left
// stick or a hat, and any buttons the mapping binds to that axis. One axis
// event can release one such button and press another.
func (m *Mapper) Events(typ, code uint16, value int32) []remoteinput.Event {
	if m == nil || m.mouse || m.keyboard {
		return nil
	}
	if typ == 1 {
		if e, ok := m.mapButton(code, value); ok {
			return []remoteinput.Event{e}
		}
		return nil
	}
	if typ != 3 {
		return nil
	}
	var events []remoteinput.Event
	if e, ok := m.mapAxis(code, value); ok {
		events = append(events, e)
	}
	for i, b := range m.layout.axisButtons[code] {
		down := value >= b.threshold
		if !b.above {
			down = value <= b.threshold
		}
		if m.held == nil {
			m.held = map[uint16][]bool{}
		}
		if m.held[code] == nil {
			m.held[code] = make([]bool, len(m.layout.axisButtons[code]))
		}
		if m.held[code][i] == down {
			continue
		}
		m.held[code][i] = down
		if e, err := remoteinput.NormalizeGamepad(b.name, down); err == nil {
			events = append(events, e)
		}
	}
	return events
}

func (m *Mapper) mapButton(code uint16, value int32) (remoteinput.Event, bool) {
	if m.suppressed[code] {
		if value == 0 {
			delete(m.suppressed, code)
		}
		return remoteinput.Event{}, false
	}
	if value != 0 && value != 1 {
		return remoteinput.Event{}, false
	}
	name, ok := m.layout.buttons[code]
	if !ok {
		return remoteinput.Event{}, false
	}
	e, err := remoteinput.NormalizeGamepad(name, value == 1)
	return e, err == nil
}

func (m *Mapper) mapAxis(code uint16, value int32) (remoteinput.Event, bool) {
	binding, ok := m.layout.axes[code]
	if !ok {
		return remoteinput.Event{}, false
	}
	axis := binding.axis
	name := [2]string{"left-x", "left-y"}[axis]
	r, ok := m.axes[code]
	if !ok || r.Max <= r.Min {
		return remoteinput.Event{}, false
	}
	v := int64(value)
	if v < int64(r.Min) {
		v = int64(r.Min)
	}
	if v > int64(r.Max) {
		v = int64(r.Max)
	}
	// Center the stick at the midpoint of the kernel's reported range. This
	// handles both signed ranges (for example -1..1) and unsigned ranges.
	center := int64(r.Min) + (int64(r.Max)-int64(r.Min))/2
	half := int64(r.Max) - center
	if v >= center {
		normalized := (v - center) * 32767 / half
		v = normalized
	} else {
		half = center - int64(r.Min)
		normalized := (v - center) * 32768 / half
		v = normalized
	}
	normalized := v
	if binding.invert {
		normalized = -normalized
	}
	if normalized > -8000 && normalized < 8000 {
		normalized = 0
	}
	// A held hat direction owns the axis; the stick reports again once the
	// hat returns to centre.
	if binding.hat {
		m.hat[axis] = int32(normalized)
		if normalized == 0 {
			normalized = int64(m.stick[axis])
		}
	} else {
		m.stick[axis] = int32(normalized)
		if m.hat[axis] != 0 {
			return remoteinput.Event{}, false
		}
	}
	e, err := remoteinput.NormalizeAxis(name, int32(normalized))
	return e, err == nil
}

func mapKeyboard(typ, code uint16, value int32) (remoteinput.Event, bool) {
	if typ != 1 || (value != 0 && value != 1) {
		return remoteinput.Event{}, false
	}
	return playhid.PhysicalEvent(code, value == 1)
}

// Chord fires once after a continuous one-second Select+Start hold. Both
// buttons must be released after firing before another shortcut can arm.
type Chord struct {
	start, selectHeld, fired bool
	since                    time.Time
}

func (c *Chord) Update(code remoteinput.Code, pressed bool, now time.Time) {
	switch code {
	case remoteinput.ButtonStart:
		c.start = pressed
	case remoteinput.ButtonSelect:
		c.selectHeld = pressed
	default:
		return
	}
	if !c.start && !c.selectHeld {
		c.fired = false
	}
	if c.start && c.selectHeld {
		if c.since.IsZero() {
			c.since = now
		}
	} else {
		c.since = time.Time{}
	}
}
func (c *Chord) Ready(now time.Time) bool {
	if c.fired || c.since.IsZero() || now.Sub(c.since) < time.Second {
		return false
	}
	c.fired = true
	return true
}

// Report motion in raw device counts, independent of HDMI/UI coordinates.
// A report outside the signed16 transport range faults and reconnects instead
// of silently wrapping counts. Wheel and extra buttons are not in this ABI.
func (m *Mapper) mapMouse(typ, code uint16, value int32) (remoteinput.Event, bool) {
	if typ == 2 && (code == 0 || code == 1) {
		if value < -32768 || value > 32767 {
			m.mouseFault = true
			return remoteinput.Event{}, false
		}
		if code == 0 {
			m.mouseX += value
		} else {
			m.mouseY += value
		}
		if m.mouseX < -32768 || m.mouseX > 32767 || m.mouseY < -32768 || m.mouseY > 32767 {
			m.mouseFault = true
		}
		m.mouseDirty = true
	} else if typ == 1 && (code == 272 || code == 273) && (value == 0 || value == 1) {
		if m.suppressed[code] {
			if value == 0 {
				delete(m.suppressed, code)
			}
			return remoteinput.Event{}, false
		}
		mask := uint8(1 << (code - 272))
		if value == 1 {
			m.mouseButtons |= mask
		} else {
			m.mouseButtons &^= mask
		}
		m.mouseDirty = true
	} else if typ == 0 && code == 0 && m.mouseDirty && !m.mouseFault {
		event := remoteinput.MouseEvent(int16(m.mouseX), int16(m.mouseY), m.mouseButtons)
		m.mouseX, m.mouseY, m.mouseDirty = 0, 0, false
		return event, true
	}
	return remoteinput.Event{}, false
}

// MouseFault reports lost relative counts; reconnect instead of silently wrapping.
func (m *Mapper) MouseFault() bool { return m != nil && m.mouseFault }
