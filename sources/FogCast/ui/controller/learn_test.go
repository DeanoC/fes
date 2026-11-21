package controller

import (
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

// learnPad has six buttons, a hat, a centred stick and a trigger that
// rests at 0.
func learnPad() Device {
	return Device{Name: "Unknown Pad", Bus: 3, Vendor: 0x1234, Product: 0x5678,
		Keys: bits(96, codeRange(288, 293)...), Abs: bits(8, 0, 1, 2, 16, 17),
		Axes: map[uint16]Range{0: {Min: 0, Max: 255}, 1: {Min: 0, Max: 255}, 2: {Min: 0, Max: 255},
			16: {Min: -1, Max: 1}, 17: {Min: -1, Max: 1}}}
}

func TestLearnerNamesEachKindOfControl(t *testing.T) {
	l := NewLearner(learnPad(), map[uint16]int32{0: 128, 1: 128, 2: 0})
	steps := []struct {
		typ, code uint16
		value     int32
		want      string
	}{
		{1, 290, 1, "b2"},
		{1, 290, 0, ""},
		{3, 17, -1, "h0.1"},
		{3, 17, 0, ""},
		{3, 16, 1, "h0.2"},
		{3, 16, 0, ""},
		{3, 0, 10, "-a0"},
		{3, 0, 120, ""},
		{3, 1, 250, "+a1"},
		{3, 1, 128, ""},
		{3, 2, 200, "a2"},
		{3, 2, 0, ""},
	}
	for _, s := range steps {
		got, ok := l.Feed(s.typ, s.code, s.value)
		if !ok {
			got = ""
		}
		if got != s.want {
			t.Fatalf("event %d/%d=%d gave %q, want %q", s.typ, s.code, s.value, got, s.want)
		}
	}
	if !l.Idle() {
		t.Fatal("not idle after every control came back")
	}
}

// One press can arrive as a key and an axis. Only the first control to
// leave rest counts, and nothing counts until both are back.
func TestLearnerCountsOnePressOnce(t *testing.T) {
	l := NewLearner(learnPad(), map[uint16]int32{2: 0})
	if s, ok := l.Feed(1, 293, 1); !ok || s != "b5" {
		t.Fatalf("key: %q %v", s, ok)
	}
	if s, ok := l.Feed(3, 2, 255); ok {
		t.Fatalf("axis of the same press counted: %q", s)
	}
	l.Feed(1, 293, 0)
	if s, ok := l.Feed(1, 288, 1); ok {
		t.Fatalf("counted %q while the trigger was still pulled", s)
	}
	l.Feed(1, 288, 0)
	l.Feed(3, 2, 0)
	if s, ok := l.Feed(1, 288, 1); !ok || s != "b0" {
		t.Fatalf("after rest: %q %v", s, ok)
	}
}

// A stick wobbling near the threshold stays down until it is nearly home.
func TestLearnerAxisHysteresis(t *testing.T) {
	l := NewLearner(learnPad(), map[uint16]int32{0: 128})
	if s, ok := l.Feed(3, 0, 250); !ok || s != "+a0" {
		t.Fatalf("push: %q %v", s, ok)
	}
	l.Feed(3, 0, 180)
	if l.Idle() {
		t.Fatal("released at 180")
	}
	l.Feed(3, 0, 140)
	if !l.Idle() {
		t.Fatal("still held near rest")
	}
}

// What a Learner names, NewLearnedMapper reads back as the same controls.
func TestLearnedMappingReadsBack(t *testing.T) {
	m, err := NewLearnedMapper(learnPad(),
		"dpup:h0.1,dpdown:h0.4,dpleft:-a0,dpright:+a0,a:b2,b:b3,x:b0,y:b1,leftshoulder:b4,rightshoulder:a2,back:b5,start:b4")
	if err != nil {
		t.Fatal(err)
	}
	if !m.Learned() || m.Mapping() != LearnedMapping {
		t.Fatalf("learned %v mapping %q", m.Learned(), m.Mapping())
	}
	if got := mapButton(t, m, 290); got != remoteinput.ButtonA {
		t.Fatalf("b2 = %v", got)
	}
	if e, ok := m.Map(3, 17, -1); !ok || e.Code != remoteinput.AxisLeftY || e.Value != -32768 {
		t.Fatalf("hat up: %+v %v", e, ok)
	}
	if e, ok := m.Map(3, 0, 255); !ok || e.Code != remoteinput.AxisLeftX || e.Value != 32767 {
		t.Fatalf("stick right: %+v %v", e, ok)
	}
	events := m.Events(3, 2, 200)
	if len(events) != 1 || events[0].Code != remoteinput.ButtonR || events[0].Action != remoteinput.ActionPress {
		t.Fatalf("trigger pull: %+v", events)
	}
	if events := m.Events(3, 2, 210); len(events) != 0 {
		t.Fatalf("trigger moved while held: %+v", events)
	}
	events = m.Events(3, 2, 10)
	if len(events) != 1 || events[0].Code != remoteinput.ButtonR || events[0].Action != remoteinput.ActionRelease {
		t.Fatalf("trigger let go: %+v", events)
	}
}

// Directions that are not a pair on one axis are separate buttons.
func TestLearnedDirectionsOnButtons(t *testing.T) {
	m, err := NewLearnedMapper(learnPad(), "dpup:b0,dpdown:b1,dpleft:h0.8,dpright:b3,a:b4,b:b5")
	if err != nil {
		t.Fatal(err)
	}
	if got := mapButton(t, m, 288); got != remoteinput.ButtonDPadUp {
		t.Fatalf("b0 = %v", got)
	}
	events := m.Events(3, 16, -1)
	if len(events) != 1 || events[0].Code != remoteinput.ButtonDPadLeft || events[0].Action != remoteinput.ActionPress {
		t.Fatalf("hat left: %+v", events)
	}
}

func TestLearnedMappingRejectsUnknownControls(t *testing.T) {
	for _, mapping := range []string{"", "turbo:b0", "a", "rightx:a3"} {
		if _, err := NewLearnedMapper(learnPad(), mapping); err == nil {
			t.Errorf("%q compiled", mapping)
		}
	}
}

// Two DB entries bind buttons to axes: the Mayflash adapter's shoulders and
// the Gravis Xterminator's A.
func TestDBButtonsOnAxes(t *testing.T) {
	mayflash := Device{Bus: 3, Vendor: 0x0925, Product: 0x03e8, Version: 0x0101,
		Keys: bits(96, codeRange(288, 298)...), Abs: bits(8, 0, 1, 2, 3, 4, 5, 16, 17),
		Axes: map[uint16]Range{4: {Min: 0, Max: 255}, 5: {Min: 0, Max: 255}}}
	m := NewMapper(mayflash)
	if m.Mapping() != "Mayflash Wii Classic Adapter" {
		t.Fatalf("mapping %q", m.Mapping())
	}
	events := m.Events(3, 4, 255)
	if len(events) != 1 || events[0].Code != remoteinput.ButtonL || events[0].Action != remoteinput.ActionPress {
		t.Fatalf("left shoulder: %+v", events)
	}
	gravis := Device{Bus: 3, Vendor: 0x047d, Product: 0x4007, Version: 0x0100,
		Keys: bits(96, codeRange(288, 297)...), Abs: bits(8, 0, 1, 2),
		Axes: map[uint16]Range{2: {Min: -1, Max: 1}}}
	m = NewMapper(gravis)
	if m.Mapping() != "Gravis Xterminator Force" {
		t.Fatalf("mapping %q", m.Mapping())
	}
	events = m.Events(3, 2, 1)
	if len(events) != 1 || events[0].Code != remoteinput.ButtonA || events[0].Action != remoteinput.ActionPress {
		t.Fatalf("a: %+v", events)
	}
}

// A learned left stick is the left stick: its whole axis, the right way
// round, beside directions on a hat.
func TestLearnedStick(t *testing.T) {
	m, err := NewLearnedMapper(learnPad(), "dpup:h0.1,dpdown:h0.4,dpleft:h0.8,dpright:h0.2,a:b0,b:b1,leftx:a0,lefty:a1~")
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := m.Map(3, 0, 255); !ok || e.Code != remoteinput.AxisLeftX || e.Value != 32767 {
		t.Fatalf("stick right: %+v %v", e, ok)
	}
	if e, ok := m.Map(3, 0, 128); !ok || e.Code != remoteinput.AxisLeftX || e.Value != 0 {
		t.Fatalf("stick home: %+v %v", e, ok)
	}
	// lefty is reversed: the pad reads low when pushed down.
	if e, ok := m.Map(3, 1, 0); !ok || e.Code != remoteinput.AxisLeftY || e.Value <= 0 {
		t.Fatalf("stick down: %+v %v", e, ok)
	}
	if e, ok := m.Map(3, 17, -1); !ok || e.Code != remoteinput.AxisLeftY || e.Value != -32768 {
		t.Fatalf("hat up: %+v %v", e, ok)
	}
}
