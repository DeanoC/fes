package controller

import (
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
)

//go:embed gamecontrollerdb.txt
var gameControllerDB string

// Linux input-event-codes.h limits that SDL's joystick driver numbers against.
const (
	btnJoystick = 0x120
	btnMode     = 0x13c
	keyMax      = 0x2ff
	absHat0X    = 0x10
	absHat3Y    = 0x17
	absMax      = 0x3f
)

// sdlEntry is one GameControllerDB mapping for a Linux evdev GUID.
type sdlEntry struct {
	bus, crc, vendor, product, version uint16
	name                               string
	bindings                           map[string]string
}

var (
	sdlOnce    sync.Once
	sdlEntries []sdlEntry
)

func sdlDatabase() []sdlEntry {
	sdlOnce.Do(func() {
		for _, line := range strings.Split(gameControllerDB, "\n") {
			if e, ok := parseSDLEntry(line); ok {
				sdlEntries = append(sdlEntries, e)
			}
		}
	})
	return sdlEntries
}

// parseSDLEntry accepts SDL's evdev GUID layout: little-endian bus, name
// CRC, vendor, 0, product, 0, version, then a zero driver signature. SDL's
// HIDAPI and name-only GUIDs cannot describe an evdev node and are skipped.
func parseSDLEntry(line string) (sdlEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] == '#' {
		return sdlEntry{}, false
	}
	fields := strings.Split(strings.TrimSuffix(line, ","), ",")
	guid, err := hex.DecodeString(fields[0])
	if err != nil || len(guid) != 16 || len(fields) < 3 {
		return sdlEntry{}, false
	}
	word := func(i int) uint16 { return binary.LittleEndian.Uint16(guid[i:]) }
	if word(6) != 0 || word(10) != 0 || word(14) != 0 || (word(4) == 0 && word(8) == 0) {
		return sdlEntry{}, false
	}
	e := sdlEntry{bus: word(0), crc: word(2), vendor: word(4), product: word(8), version: word(12),
		name: fields[1], bindings: map[string]string{}}
	for _, field := range fields[2:] {
		if target, source, ok := strings.Cut(field, ":"); ok {
			e.bindings[target] = source
		}
	}
	return e, true
}

// lookupSDL picks the entry SDL would: a version match before any version,
// and at each step a matching name CRC before an entry that ignores it.
func lookupSDL(d Device) (sdlEntry, bool) {
	crc := sdlCRC16(d.Name)
	for _, pass := range [4]struct{ crc, version bool }{{true, true}, {false, true}, {true, false}, {false, false}} {
		for _, e := range sdlDatabase() {
			if e.bus == d.Bus && e.vendor == d.Vendor && e.product == d.Product &&
				(!pass.crc || e.crc == crc) && (!pass.version || e.version == d.Version) {
				return e, true
			}
		}
	}
	return sdlEntry{}, false
}

// sdlCRC16 is SDL_crc16 (CRC-16/ARC) of the evdev name, as SDL folds it into the GUID.
func sdlCRC16(name string) uint16 {
	var crc uint16
	for i := 0; i < len(name); i++ {
		crc ^= uint16(name[i])
		for bit := 0; bit < 8; bit++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// sdlControls numbers a pad's evdev codes as SDL's Linux joystick driver
// does, which is what a mapping's bN, hN and aN mean: keys from BTN_JOYSTICK
// up and then below it; hats by ABS_HAT pair; every other axis in code order.
func sdlControls(keys, abs []byte) (buttons, hats, axes []uint16) {
	for code := btnJoystick; code < keyMax; code++ {
		if hasBit(keys, code) {
			buttons = append(buttons, uint16(code))
		}
	}
	for code := 0; code < btnJoystick; code++ {
		if hasBit(keys, code) {
			buttons = append(buttons, uint16(code))
		}
	}
	for code := absHat0X; code <= absHat3Y; code += 2 {
		if hasBit(abs, code) || hasBit(abs, code+1) {
			hats = append(hats, uint16(code))
		}
	}
	for code := 0; code < absMax; code++ {
		if hasBit(abs, code) && (code < absHat0X || code > absHat3Y) {
			axes = append(axes, uint16(code))
		}
	}
	return buttons, hats, axes
}

func hasBit(bits []byte, code int) bool {
	return code/8 < len(bits) && bits[code/8]&(1<<uint(code%8)) != 0
}

// sdlButtons are the GameControllerDB targets the kit forwards. SDL's names
// are positional, matching remoteinput: a is South, x is West, y is North.
var sdlButtons = map[string]string{
	"a": "a", "b": "b", "x": "x", "y": "y",
	"leftshoulder": "l", "rightshoulder": "r", "back": "select", "start": "start",
	"dpup": "dpad-up", "dpdown": "dpad-down", "dpleft": "dpad-left", "dpright": "dpad-right",
}

// sdlLayout compiles an entry against the pad's own capabilities. A d-pad on
// a hat or a full axis becomes the held hat source of left-x/left-y, as an
// evdev hat already is; a d-pad on buttons stays buttons.
func sdlLayout(e sdlEntry, keys, abs []byte) layout {
	buttons, hats, axes := sdlControls(keys, abs)
	l := layout{buttons: map[uint16]string{}, axes: map[uint16]axisBinding{}}
	for target, name := range sdlButtons {
		if n, ok := sdlIndex(e.bindings[target], "b"); ok && n < len(buttons) {
			l.buttons[buttons[n]] = name
		}
	}
	// A generic HID pad reports Guide in the BTN_TRIGGER range, and on some
	// pads BTN_MODE is another control. The launcher asks the layout. A guide
	// binding on a button the kit forwards leaves that button to the core:
	// PowerA's 0x200f entry binds guide and start both to b7.
	if n, ok := sdlIndex(e.bindings["guide"], "b"); ok && n < len(buttons) && l.buttons[buttons[n]] == "" {
		l.guide = buttons[n]
	} else if hasBit(keys, btnMode) && !sdlBindsCode(e, buttons, btnMode) {
		l.guide = btnMode
	}
	for axis, target := range [2]string{"leftx", "lefty"} {
		source := e.bindings[target]
		if n, ok := sdlIndex(strings.TrimSuffix(source, "~"), "a"); ok && n < len(axes) {
			l.axes[axes[n]] = axisBinding{axis: axis, invert: strings.HasSuffix(source, "~")}
		}
	}
	for axis, pair := range [2][2]string{{"dpleft", "dpright"}, {"dpup", "dpdown"}} {
		negCode, negPositive, okNeg := sdlDirection(e.bindings[pair[0]], hats, axes)
		posCode, posPositive, okPos := sdlDirection(e.bindings[pair[1]], hats, axes)
		if okNeg && okPos && negCode == posCode && negPositive != posPositive {
			l.axes[negCode] = axisBinding{axis: axis, hat: true, invert: negPositive}
		}
	}
	return l
}

// sdlDirection resolves one d-pad source to an evdev axis and the sign that
// presses it. SDL hat masks are 1 up, 2 right, 4 down, 8 left.
func sdlDirection(source string, hats, axes []uint16) (code uint16, positive, ok bool) {
	if strings.HasPrefix(source, "h") {
		hat, mask, _ := strings.Cut(source[1:], ".")
		n, err := strconv.Atoi(hat)
		if err != nil || n < 0 || n >= len(hats) {
			return 0, false, false
		}
		switch mask {
		case "1", "4":
			return hats[n] + 1, mask == "4", true
		case "2", "8":
			return hats[n], mask == "2", true
		}
		return 0, false, false
	}
	if len(source) < 2 || (source[0] != '+' && source[0] != '-') {
		return 0, false, false
	}
	n, ok := sdlIndex(source[1:], "a")
	if !ok || n >= len(axes) {
		return 0, false, false
	}
	return axes[n], source[0] == '+', true
}

// sdlBindsCode reports whether any button binding in the entry resolves to code.
func sdlBindsCode(e sdlEntry, buttons []uint16, code uint16) bool {
	for _, source := range e.bindings {
		if n, ok := sdlIndex(source, "b"); ok && n < len(buttons) && buttons[n] == code {
			return true
		}
	}
	return false
}

func sdlIndex(source, kind string) (int, bool) {
	if !strings.HasPrefix(source, kind) {
		return 0, false
	}
	n, err := strconv.Atoi(source[len(kind):])
	return n, err == nil && n >= 0
}
