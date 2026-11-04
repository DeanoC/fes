// SPDX-License-Identifier: GPL-3.0-or-later
package usb

import (
	"crypto/sha256"
	"fmt"
)

const MaxHIDReportBytes = 64

// GamepadState is transport-independent. Axes are X,Y,Z,Rx,Ry,Rz, scaled
// to [-32767,32767]. Hat is -1 when neutral or 0..7 clockwise from up.
// A report ID updates its own fields without releasing another report's keys.
type GamepadState struct {
	Axes    [6]int16
	Buttons uint32
	Hat     int8
}

type padField struct {
	offset, bits uint16
	usage        uint32
	min, max     int64
	null         bool
}
type padReport struct {
	bits   uint16
	fields []padField
}
type padGlobals struct {
	page, size, count uint32
	id                byte
	min               int64
	maxValue          uint32
	maxBytes          int
}
type padLocals struct {
	usages             []uint32
	min, max           uint32
	rangeMin, rangeMax bool
}

// Gamepad decodes one bounded Joystick/Game Pad application collection.
// It does not enumerate devices or claim a USB lease. Buffered/relative/array
// controls, vendor fields and output/feature reports are not exposed as input.
type Gamepad struct {
	reports map[byte]padReport
	ids     bool
	state   GamepadState
	digest  [32]byte
}

func signedItem(v uint32, size int) int64 {
	if size > 0 && v&(uint32(1)<<uint(size*8-1)) != 0 {
		return int64(v) - int64(uint64(1)<<uint(size*8))
	}
	return int64(v)
}

// ParseGamepad accepts HID 1.11 short items, global Push/Pop, report IDs,
// constant padding and absolute variable axes/buttons/hat. Bounds are fixed:
// 4096 descriptor bytes, 16 report IDs, 8 nesting levels, 64 usages per item,
// and 64 bytes per input report. Unsupported layouts fail before input use.
func ParseGamepad(data []byte) (*Gamepad, error) {
	bad := func(s string) (*Gamepad, error) { return nil, fmt.Errorf("USB gamepad descriptor: %s", s) }
	if len(data) == 0 || len(data) > MaxConfigurationBytes {
		return bad("length")
	}
	pad := &Gamepad{reports: make(map[byte]padReport), state: GamepadState{Hat: -1}, digest: sha256.Sum256(data)}
	var g padGlobals
	var l padLocals
	var stack []padGlobals
	depth, applications, controls := 0, 0, 0
	used := make(map[uint32]bool)
	usage := func(v uint32, size int) uint32 {
		if size == 4 {
			return v
		}
		return g.page<<16 | v
	}
	for off := 0; off < len(data); {
		prefix := data[off]
		off++
		if prefix == 0xfe {
			return bad("long item")
		}
		size := int(prefix & 3)
		if size == 3 {
			size = 4
		}
		if off+size > len(data) {
			return bad("truncated item")
		}
		var value uint32
		for n := 0; n < size; n++ {
			value |= uint32(data[off+n]) << uint(8*n)
		}
		off += size
		typ, tag := (prefix>>2)&3, prefix>>4
		switch typ {
		case 1: // Global items persist across main items and collections.
			switch tag {
			case 0:
				if size == 0 || value > 65535 {
					return bad("usage page")
				}
				g.page = value
			case 1:
				g.min = signedItem(value, size)
			case 2:
				g.maxValue, g.maxBytes = value, size
			case 3, 4, 5, 6: // Physical units do not change logical input values.
			case 7:
				if value == 0 || value > 32 {
					return bad("report size")
				}
				g.size = value
			case 8:
				if size == 0 || value == 0 || value > 255 {
					return bad("report ID")
				}
				if r, ok := pad.reports[0]; ok && r.bits != 0 {
					return bad("mixed numbered/unnumbered reports")
				}
				g.id = byte(value)
				pad.ids = true
			case 9:
				if value == 0 || value > 64 {
					return bad("report count")
				}
				g.count = value
			case 10:
				if size != 0 || len(stack) == 8 {
					return bad("global push")
				}
				stack = append(stack, g)
			case 11:
				if size != 0 || len(stack) == 0 {
					return bad("global pop")
				}
				g = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			default:
				return bad("global item")
			}
		case 2:
			switch tag {
			case 0:
				if size == 0 || len(l.usages) == 64 {
					return bad("usage list")
				}
				l.usages = append(l.usages, usage(value, size))
			case 1:
				if size == 0 || l.rangeMin {
					return bad("usage minimum")
				}
				l.min = usage(value, size)
				l.rangeMin = true
			case 2:
				if size == 0 || l.rangeMax {
					return bad("usage maximum")
				}
				l.max = usage(value, size)
				l.rangeMax = true
			case 3, 4, 5, 7, 8, 9: // Designator/string indices carry no input bits.
			default:
				return bad("local item")
			}
		case 0:
			if l.rangeMin != l.rangeMax || l.rangeMin && (l.min>>16 != l.max>>16 || l.max < l.min || l.max-l.min >= 64 || len(l.usages) != 0) {
				return bad("usage range")
			}
			if l.rangeMin {
				for n := uint32(0); n <= l.max-l.min; n++ {
					l.usages = append(l.usages, l.min+n)
				}
			}
			switch tag {
			case 10:
				if size != 1 || depth == 8 || value > 2 {
					return bad("collection")
				}
				if depth == 0 {
					if value != 1 || applications != 0 || len(l.usages) != 1 || (l.usages[0] != 0x10004 && l.usages[0] != 0x10005) {
						return bad("gamepad application required")
					}
					applications++
				} else if value == 1 {
					return bad("nested application")
				}
				depth++
			case 12:
				if size != 0 || depth == 0 {
					return bad("end collection")
				}
				depth--
			case 8, 9, 11:
				if depth == 0 || g.size == 0 || g.count == 0 || pad.ids && g.id == 0 || g.size*g.count > 512 {
					return bad("report bounds/scope")
				}
				if tag != 8 {
					break
				} // Output/Feature have independent bit streams.
				maximum := int64(g.maxValue)
				if g.min < 0 {
					maximum = signedItem(g.maxValue, g.maxBytes)
				}
				r, exists := pad.reports[g.id]
				if !exists && len(pad.reports) == 16 {
					return bad("report ID count")
				}
				bits := uint32(r.bits) + g.size*g.count
				limit := uint32(512)
				if pad.ids {
					limit -= 8
				}
				if bits > limit {
					return bad("input report too large")
				}
				if value&1 == 0 {
					if value&0x19e != 2 || len(l.usages) != int(g.count) {
						return bad("absolute variable usages required")
					}
					if maximum <= g.min || g.min < -(int64(1)<<31) || maximum > int64(^uint32(0)) {
						return bad("logical range")
					}
					for i, u := range l.usages {
						page, id := u>>16, u&65535
						known := page == 9 && id >= 1 && id <= 32 || page == 1 && (id >= 0x30 && id <= 0x35 || id == 0x39)
						if !known {
							continue
						}
						if used[u] {
							return bad("duplicate control")
						}
						used[u] = true
						if page == 9 && (g.min != 0 || maximum != 1) {
							return bad("button range")
						}
						if page == 1 && id == 0x39 && (maximum-g.min != 7 && maximum-g.min != 3) {
							return bad("hat range")
						}
						if g.min < 0 && (g.min < -(int64(1)<<(g.size-1)) || maximum >= (int64(1)<<(g.size-1))) || g.min >= 0 && uint64(maximum) >= (uint64(1)<<g.size) {
							return bad("range does not fit field")
						}
						r.fields = append(r.fields, padField{offset: r.bits + uint16(uint32(i)*g.size), bits: uint16(g.size), usage: u, min: g.min, max: maximum, null: value&64 != 0})
						controls++
					}
				}
				r.bits = uint16(bits)
				pad.reports[g.id] = r
			default:
				return bad("main item")
			}
			l = padLocals{}
		default:
			return bad("reserved item type")
		}
	}
	if depth != 0 || len(stack) != 0 || applications != 1 || controls == 0 {
		return bad("incomplete application")
	}
	return pad, nil
}

func (g *Gamepad) DescriptorSHA256() [32]byte { return g.digest }
func (g *Gamepad) MaxReportBytes() int {
	n := 0
	for _, r := range g.reports {
		size := (int(r.bits) + 7) / 8
		if g.ids {
			size++
		}
		n = max(n, size)
	}
	return n
}

// Feed commits all fields atomically after validating the exact report length,
// ID and logical values. Errors leave the previous complete state unchanged.
func (g *Gamepad) Feed(report []byte) (GamepadState, bool, error) {
	bad := func() (GamepadState, bool, error) { return g.state, false, fmt.Errorf("invalid USB gamepad report") }
	id := byte(0)
	data := report
	if g.ids {
		if len(data) == 0 {
			return bad()
		}
		id = data[0]
		data = data[1:]
	}
	r, ok := g.reports[id]
	if !ok || len(data) != (int(r.bits)+7)/8 {
		return bad()
	}
	next := g.state
	for _, f := range r.fields {
		var raw uint32
		for n := uint16(0); n < f.bits; n++ {
			bit := f.offset + n
			raw |= uint32(data[bit/8]>>(bit%8)&1) << n
		}
		v := int64(raw)
		if f.min < 0 && raw&(uint32(1)<<(f.bits-1)) != 0 {
			v -= int64(uint64(1) << f.bits)
		}
		page, u := f.usage>>16, f.usage&65535
		if v < f.min || v > f.max {
			if page == 1 && u == 0x39 && f.null {
				next.Hat = -1
				continue
			}
			return bad()
		}
		if page == 9 {
			mask := uint32(1) << (u - 1)
			next.Buttons &^= mask
			if v != 0 {
				next.Buttons |= mask
			}
		} else if u == 0x39 {
			next.Hat = int8((v - f.min) * 8 / (f.max - f.min + 1))
		} else {
			next.Axes[u-0x30] = int16((v-f.min)*65534/(f.max-f.min) - 32767)
		}
	}
	changed := next != g.state
	g.state = next
	return next, changed, nil
}
