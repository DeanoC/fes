package controller

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/hidkeys"
	"github.com/DeanoC/FogCast/remoteinput"
)

// The kit's SNES-style pad reports its D-pad on unsigned ABS_X/ABS_Y and its
// buttons in the BTN_TRIGGER range; GameControllerDB names both.
func TestSuperFamicomPadUnsignedAxisAndButtons(t *testing.T) {
	m := NewMapper(superFamicomPad(0x0110))
	if got := m.Mapping(); got != "Super Famicom Controller" {
		t.Fatalf("mapping %q", got)
	}
	for _, tt := range []struct{ raw, want int32 }{{0, -32768}, {127, 0}, {128, 0}, {255, 32767}} {
		e, ok := m.Map(3, 0, tt.raw)
		if !ok || e.Value != tt.want || e.Code != remoteinput.AxisLeftX {
			t.Fatalf("axis %d: %+v %v", tt.raw, e, ok)
		}
	}
	e, ok := m.Map(1, 290, 1)
	if !ok || e.Code != remoteinput.ButtonA || e.Action != remoteinput.ActionPress {
		t.Fatalf("confirm %+v", e)
	}
	e, ok = m.Map(1, 297, 1)
	if !ok || e.Code != remoteinput.ButtonStart {
		t.Fatalf("start %+v", e)
	}
	if _, ok = m.Map(1, 290, 2); ok {
		t.Fatal("autorepeat must not confirm")
	}
}

// A resting stick keeps reporting near centre; that jitter must not cancel a
// held D-pad hat direction on the shared left-x axis (#473).
func TestStickJitterDoesNotCancelHeldHat(t *testing.T) {
	m := NewMapper(Device{Vendor: 0x054c, Product: 0x0ce6, Axes: map[uint16]Range{0: {Min: 0, Max: 255}, 16: {Min: -1, Max: 1}}})
	axis := func(code uint16, raw int32) (remoteinput.Event, bool) {
		t.Helper()
		e, ok := m.Map(3, code, raw)
		if ok && e.Code != remoteinput.AxisLeftX {
			t.Fatalf("code %d raw %d: %+v", code, raw, e)
		}
		return e, ok
	}
	if e, ok := axis(16, -1); !ok || e.Value != -32768 {
		t.Fatalf("hat left: %+v %v", e, ok)
	}
	for _, raw := range []int32{127, 131, 124, 128, 255} {
		if e, ok := axis(0, raw); ok {
			t.Fatalf("stick %d overrode held hat: %+v", raw, e)
		}
	}
	// Releasing the hat hands the axis back to the stick's latest position.
	if e, ok := axis(16, 0); !ok || e.Value != 32767 {
		t.Fatalf("hat release with stick right: %+v %v", e, ok)
	}
	if e, ok := axis(0, 128); !ok || e.Value != 0 {
		t.Fatalf("stick centre: %+v %v", e, ok)
	}
	if e, ok := axis(16, 1); !ok || e.Value != 32767 {
		t.Fatalf("hat right: %+v %v", e, ok)
	}
	if e, ok := axis(16, 0); !ok || e.Value != 0 {
		t.Fatalf("hat release with stick centred: %+v %v", e, ok)
	}
}

// The kit forwards physical keys as HID usages; mister-agent shapes them
// for the bound core (HID, ZX81 matrix or native gamepad).
func TestKeyboardMapperPostsHIDUsages(t *testing.T) {
	m := NewKeyboardMapper()
	for _, tc := range []struct {
		linux uint16
		usage uint8
	}{{30, 0x04}, {103, 0x52}, {1, 0x29}, {14, 0x2a}} { // A, Up, Esc, Backspace
		e, ok := m.Map(1, tc.linux, 1)
		usage, isHID := hidkeys.Usage(e.Code)
		if !ok || e.Device != remoteinput.DeviceKeyboard || e.Kind != remoteinput.KindKey || !isHID || usage != tc.usage {
			t.Fatalf("KEY %d %+v ok=%v", tc.linux, e, ok)
		}
	}
	if _, ok := m.Map(1, 304, 1); ok {
		t.Fatal("BTN_SOUTH is not a keyboard key")
	}
	if _, ok := m.Map(1, 30, 2); ok {
		t.Fatal("autorepeat forwarded")
	}
}

func TestInitialHeldButtonRequiresRelease(t *testing.T) {
	m := NewMapper(Device{})
	m.Suppress(304)
	if _, ok := m.Map(1, 304, 1); ok {
		t.Fatal("initial held confirm fired")
	}
	if _, ok := m.Map(1, 304, 0); ok {
		t.Fatal("suppressed release escaped")
	}
	if _, ok := m.Map(1, 304, 1); !ok {
		t.Fatal("fresh press lost")
	}
}

func TestChordRequiresContinuousHoldAndBothReleased(t *testing.T) {
	var c Chord
	now := time.Unix(10, 0)
	c.Update(remoteinput.ButtonStart, true, now)
	if c.Ready(now.Add(2 * time.Second)) {
		t.Fatal("Start alone stopped")
	}
	c.Update(remoteinput.ButtonSelect, true, now)
	if c.Ready(now.Add(999 * time.Millisecond)) {
		t.Fatal("early stop")
	}
	if !c.Ready(now.Add(time.Second)) {
		t.Fatal("missing stop")
	}
	if c.Ready(now.Add(2 * time.Second)) {
		t.Fatal("repeat stop")
	}
	c.Update(remoteinput.ButtonStart, false, now.Add(2*time.Second))
	c.Update(remoteinput.ButtonStart, true, now.Add(3*time.Second))
	if c.Ready(now.Add(5 * time.Second)) {
		t.Fatal("must release both")
	}
	c.Update(remoteinput.ButtonStart, false, now)
	c.Update(remoteinput.ButtonSelect, false, now)
	c.Update(remoteinput.ButtonStart, true, now)
	c.Update(remoteinput.ButtonSelect, true, now)
	if !c.Ready(now.Add(time.Second)) {
		t.Fatal("did not rearm")
	}
}
