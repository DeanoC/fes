package controller

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/inputmap"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestFixtureUnsignedAxisAndButtons(t *testing.T) {
	m := NewMapper(0x081f, 0xe401, map[uint16]Range{0: {Min: 0, Max: 255}, 1: {Min: 0, Max: 255}})
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

func TestKeyboardMapperPostsZX81MatrixCodes(t *testing.T) {
	m := NewKeyboardMapper()
	e, ok := m.Map(1, 30, 1)
	if !ok || e.Device != remoteinput.DeviceKeyboard || e.Kind != remoteinput.KindKey || e.Code != zx81keys.Letter('A') {
		t.Fatalf("KEY_A %+v ok=%v", e, ok)
	}
	if _, ok = m.Map(1, 304, 1); ok {
		t.Fatal("BTN_SOUTH is not a keyboard key")
	}
}

func TestInitialHeldButtonRequiresRelease(t *testing.T) {
	m := NewMapper(0, 0, nil)
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

func TestPhysicalEvdevOverrideBeatsDefaultSouth(t *testing.T) {
	r, err := inputmap.NewRemapper(inputmap.Profile{Name: "evdev-select", Bindings: map[string]string{"evdev:304": "select"}})
	if err != nil {
		t.Fatal(err)
	}
	m := NewMapper(0, 0, nil)
	e, ok := m.mapWith(r, 1, 304, 1)
	if !ok || e.Code != remoteinput.ButtonSelect {
		t.Fatalf("physical override %+v %v", e, ok)
	}
	e, ok = m.mapWith(nil, 1, 304, 1)
	if !ok || e.Code != remoteinput.ButtonA {
		t.Fatalf("default south %+v %v", e, ok)
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
