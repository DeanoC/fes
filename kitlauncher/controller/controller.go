// Package controller normalizes physical kit gamepads independently of UI layout.
package controller

import (
	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/remoteinput"
	"time"
)

type Range struct{ Min, Max int32 }
type Mapper struct {
	fixture    bool
	axes       map[uint16]Range
	suppressed map[uint16]bool
}

func NewMapper(vendor, product uint16, axes map[uint16]Range) *Mapper {
	return &Mapper{fixture: vendor == 0x081f && product == 0xe401, axes: axes, suppressed: map[uint16]bool{}}
}
func (m *Mapper) Suppress(code uint16) { m.suppressed[code] = true }
func (m *Mapper) Map(typ, code uint16, value int32) (remoteinput.Event, bool) {
	if typ == 1 {
		if m.suppressed[code] {
			if value == 0 {
				delete(m.suppressed, code)
			}
			return remoteinput.Event{}, false
		}
		if value != 0 && value != 1 {
			return remoteinput.Event{}, false
		}
		name := ""
		if m.fixture {
			name = map[uint16]string{288: "y", 289: "b", 290: "a", 291: "x", 292: "l", 293: "r", 296: "select", 297: "start"}[code]
		}
		if name == "" {
			name = map[uint16]string{304: "a", 305: "b", 306: "c", 307: "x", 308: "y", 310: "l", 311: "r", 314: "select", 315: "start", 544: "dpad-up", 545: "dpad-down", 546: "dpad-left", 547: "dpad-right"}[code]
		}
		if name == "" {
			return remoteinput.Event{}, false
		}
		e, err := remoteinput.NormalizeGamepad(name, value == 1)
		return e, err == nil
	}
	if typ != 3 {
		return remoteinput.Event{}, false
	}
	name := ""
	switch code {
	case 0, 16:
		name = "left-x"
	case 1, 17:
		name = "left-y"
	default:
		return remoteinput.Event{}, false
	}
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
	normalized := (v-int64(r.Min))*65535/(int64(r.Max)-int64(r.Min)) - 32768
	if normalized > -8000 && normalized < 8000 {
		normalized = 0
	}
	e, err := remoteinput.NormalizeAxis(name, int32(normalized))
	return e, err == nil
}

func (m *Mapper) mapWith(remap *inputmap.Remapper, typ, code uint16, value int32) (remoteinput.Event, bool) {
	if typ == 1 && remap != nil {
		if dst, ok := remap.PhysicalButton(code); ok {
			if m.suppressed[code] {
				if value == 0 {
					delete(m.suppressed, code)
				}
				return remoteinput.Event{}, false
			}
			if value != 0 && value != 1 {
				return remoteinput.Event{}, false
			}
			action := remoteinput.ActionRelease
			if value == 1 {
				action = remoteinput.ActionPress
			}
			return remoteinput.Event{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: action, Code: dst}, true
		}
	}
	return m.Map(typ, code, value)
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
