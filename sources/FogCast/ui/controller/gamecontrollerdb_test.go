package controller

import (
	"reflect"
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func bits(size int, codes ...int) []byte {
	b := make([]byte, size)
	for _, code := range codes {
		b[code/8] |= 1 << uint(code%8)
	}
	return b
}

func codeRange(from, to int) []int {
	var codes []int
	for code := from; code <= to; code++ {
		codes = append(codes, code)
	}
	return codes
}

func superFamicomPad(version uint16) Device {
	return Device{Name: "USB Gamepad ", Bus: 3, Vendor: 0x081f, Product: 0xe401, Version: version,
		Keys: bits(96, codeRange(288, 297)...), Abs: bits(8, 0, 1),
		Axes: map[uint16]Range{0: {Min: 0, Max: 255}, 1: {Min: 0, Max: 255}}}
}

func mapButton(t *testing.T, m *Mapper, code uint16) remoteinput.Code {
	t.Helper()
	e, ok := m.Map(1, code, 1)
	if !ok || e.Kind != remoteinput.KindButton {
		t.Fatalf("button %d: %+v %v", code, e, ok)
	}
	return e.Code
}

func TestSDLCRC16MatchesCRC16ARC(t *testing.T) {
	if got := sdlCRC16("123456789"); got != 0xbb3d {
		t.Fatalf("crc %#04x", got)
	}
}

func TestSDLDatabaseParsesEvdevEntries(t *testing.T) {
	if n := len(sdlDatabase()); n < 700 {
		t.Fatalf("parsed %d Linux entries", n)
	}
	if _, ok := parseSDLEntry("03000000de2800000112000001006800,Steam Controller,a:b0,platform:Linux,"); ok {
		t.Fatal("HIDAPI driver GUID parsed as an evdev entry")
	}
}

// SDL's Linux driver numbers joystick-range keys before lower codes, hats by
// ABS_HAT pair, and leaves hat axes out of the axis list.
func TestSDLControlNumbering(t *testing.T) {
	buttons, hats, axes := sdlControls(bits(96, 158, 304, 305, 316), bits(8, 0, 1, 16, 17, 40))
	if !reflect.DeepEqual(buttons, []uint16{304, 305, 316, 158}) ||
		!reflect.DeepEqual(hats, []uint16{16}) || !reflect.DeepEqual(axes, []uint16{0, 1, 40}) {
		t.Fatalf("buttons %v hats %v axes %v", buttons, hats, axes)
	}
}

func TestSuperFamicomPadMatchesAnyVersion(t *testing.T) {
	m := NewMapper(superFamicomPad(0x0111))
	if m.Mapping() != "Super Famicom Controller" {
		t.Fatalf("mapping %q", m.Mapping())
	}
	for code, want := range map[uint16]remoteinput.Code{
		288: remoteinput.ButtonY, 289: remoteinput.ButtonB, 290: remoteinput.ButtonA, 291: remoteinput.ButtonX,
		292: remoteinput.ButtonL, 293: remoteinput.ButtonR, 296: remoteinput.ButtonSelect, 297: remoteinput.ButtonStart,
	} {
		if got := mapButton(t, m, code); got != want {
			t.Errorf("button %d = %v, want %v", code, got, want)
		}
	}
	if e, ok := m.Map(3, 1, 0); !ok || e.Code != remoteinput.AxisLeftY || e.Value != -32768 {
		t.Fatalf("d-pad up: %+v %v", e, ok)
	}
}

// hid-playstation reports Triangle as BTN_NORTH (307); the Linux gamepad
// table reads that code as Xbox X, the DB reads it as North.
func TestDualSenseFaceButtonsFollowDB(t *testing.T) {
	pad := Device{Name: "Sony Interactive Entertainment DualSense Wireless Controller", Bus: 3,
		Vendor: 0x054c, Product: 0x0ce6, Version: 0x8111,
		Keys: bits(96, append([]int{304, 305, 307, 308}, codeRange(310, 318)...)...),
		Abs:  bits(8, 0, 1, 2, 3, 4, 5, 16, 17),
		Axes: map[uint16]Range{0: {Min: 0, Max: 255}, 16: {Min: -1, Max: 1}}}
	m := NewMapper(pad)
	if m.Mapping() != "PS5 Controller" {
		t.Fatalf("mapping %q", m.Mapping())
	}
	if got := mapButton(t, m, 307); got != remoteinput.ButtonY {
		t.Fatalf("triangle = %v", got)
	}
	if got := mapButton(t, m, 308); got != remoteinput.ButtonX {
		t.Fatalf("square = %v", got)
	}
	if got := mapButton(t, NewMapper(Device{}), 307); got != remoteinput.ButtonX {
		t.Fatalf("fallback BTN_NORTH = %v", got)
	}
	if e, ok := m.Map(3, 16, -1); !ok || e.Code != remoteinput.AxisLeftX || e.Value != -32768 {
		t.Fatalf("hat left: %+v %v", e, ok)
	}
}

// The early xpad Xbox 360 entry reports its hat upside down and mirrored.
func TestDBInvertedHat(t *testing.T) {
	m := NewMapper(Device{Bus: 3, Vendor: 0x045e, Product: 0x028e, Version: 0x0001,
		Keys: bits(96, 304, 305, 307, 308, 310, 311, 314, 315, 316, 317, 318), Abs: bits(8, 0, 1, 2, 3, 4, 5, 16, 17),
		Axes: map[uint16]Range{16: {Min: -1, Max: 1}, 17: {Min: -1, Max: 1}}})
	if m.Mapping() != "Microsoft Xbox 360 Controller" {
		t.Fatalf("mapping %q", m.Mapping())
	}
	if e, ok := m.Map(3, 17, 1); !ok || e.Code != remoteinput.AxisLeftY || e.Value != -32767 {
		t.Fatalf("hat 4 is d-pad up: %+v %v", e, ok)
	}
	if e, ok := m.Map(3, 16, -1); !ok || e.Code != remoteinput.AxisLeftX || e.Value != 32767 {
		t.Fatalf("hat 8 is d-pad right: %+v %v", e, ok)
	}
}

// A BTN_TRIGGER-range pad missing from the DB still has Confirm, Back,
// Select and Start (#352).
func TestUnknownTriggerPadUsesGenericLayout(t *testing.T) {
	m := NewMapper(Device{Name: "Generic USB Pad", Bus: 3, Vendor: 0x1234, Product: 0x5678,
		Keys: bits(96, codeRange(288, 299)...), Abs: bits(8, 0, 1)})
	if m.Mapping() != "" {
		t.Fatalf("mapping %q", m.Mapping())
	}
	for code, want := range map[uint16]remoteinput.Code{
		289: remoteinput.ButtonA, 290: remoteinput.ButtonB, 296: remoteinput.ButtonSelect, 297: remoteinput.ButtonStart,
	} {
		if got := mapButton(t, m, code); got != want {
			t.Errorf("button %d = %v, want %v", code, got, want)
		}
	}
	if _, ok := m.Map(1, 294, 1); ok {
		t.Fatal("trigger button mapped")
	}
}

// Without hid-playstation a DualSense is a generic HID pad: PS sits in the
// BTN_TRIGGER range and nothing is on BTN_MODE. With the driver, PS is
// BTN_MODE. The DB knows both.
func TestGuideFollowsTheDBMapping(t *testing.T) {
	generic := NewMapper(Device{Name: "Wireless Controller", Bus: 3, Vendor: 0x054c, Product: 0x0ce6, Version: 0x0111,
		Keys: bits(96, codeRange(288, 301)...), Abs: bits(8, 0, 1, 2, 3, 4, 5, 16, 17),
		Axes: map[uint16]Range{0: {Min: 0, Max: 255}}})
	if generic.Mapping() != "PS5 Controller" || !generic.Guide(300) || generic.Guide(316) {
		t.Fatalf("generic HID DualSense: mapping %q guide(300)=%v guide(316)=%v", generic.Mapping(), generic.Guide(300), generic.Guide(316))
	}
	if _, ok := generic.Map(1, 300, 1); ok {
		t.Fatal("Guide reached the core mapping")
	}
	driver := NewMapper(Device{Name: "Sony Interactive Entertainment DualSense Wireless Controller", Bus: 3,
		Vendor: 0x054c, Product: 0x0ce6, Version: 0x8111,
		Keys: bits(96, append([]int{304, 305, 307, 308}, codeRange(310, 318)...)...), Abs: bits(8, 0, 1, 2, 3, 4, 5, 16, 17)})
	if !driver.Guide(316) || driver.Guide(317) {
		t.Fatalf("hid-playstation DualSense: guide(316)=%v guide(317)=%v", driver.Guide(316), driver.Guide(317))
	}
	if !NewMapper(Device{}).Guide(316) {
		t.Fatal("fallback layout lost BTN_MODE")
	}
	if NewKeyboardMapper().Guide(316) || NewMouseMapper().Guide(316) {
		t.Fatal("a keyboard or mouse has no Guide")
	}
}

// BTN_MODE is Guide only when the DB entry leaves that code free.
func TestBTNModeBoundToAnotherControlIsNotGuide(t *testing.T) {
	keys := bits(96, 304, 305, 316)
	stick, ok := parseSDLEntry("03000000010000000100000001000000,Test Pad,a:b0,b:b1,leftstick:b2,platform:Linux,")
	if !ok {
		t.Fatal("synthetic entry did not parse")
	}
	if l := sdlLayout(stick, keys, bits(8)); l.guide != 0 {
		t.Fatalf("BTN_MODE bound to leftstick still counted as Guide: %d", l.guide)
	}
	free, _ := parseSDLEntry("03000000010000000100000001000000,Test Pad,a:b0,b:b1,platform:Linux,")
	if l := sdlLayout(free, keys, bits(8)); l.guide != 316 {
		t.Fatalf("free BTN_MODE is not Guide: %d", l.guide)
	}
	if l := sdlLayout(free, bits(96, 304, 305), bits(8)); l.guide != 0 {
		t.Fatalf("a pad without BTN_MODE grew a Guide: %d", l.guide)
	}
}

// PowerA's 0x200f entry binds guide and start both to b7, where its siblings
// put guide on b8. With xpad's keys b7 is BTN_START and b8 is BTN_MODE: Start
// stays Start, and the free BTN_MODE is Guide.
func TestGuideBindingOnAForwardedButtonLeavesItToTheCore(t *testing.T) {
	m := NewMapper(Device{Bus: 3, Vendor: 0x20d6, Product: 0x200f, Version: 0x0101,
		Keys: bits(96, 304, 305, 307, 308, 310, 311, 314, 315, 316, 317, 318)})
	if m.Mapping() != "PowerA Xbox Series Controller" || m.Guide(315) || !m.Guide(316) {
		t.Fatalf("mapping %q guide(315)=%v guide(316)=%v", m.Mapping(), m.Guide(315), m.Guide(316))
	}
	if got := mapButton(t, m, 315); got != remoteinput.ButtonStart {
		t.Fatalf("BTN_START = %v, want Start", got)
	}
}
