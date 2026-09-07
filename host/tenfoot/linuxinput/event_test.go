package linuxinput

import "testing"

func TestKindFromPath(t *testing.T) {
	t.Parallel()
	cases := map[string]Kind{
		"/dev/input/js0":     KindJoystick,
		"/dev/input/js1":     KindJoystick,
		"/tmp/tenfoot.js":    KindJoystick,
		"/dev/input/event0":  KindEvdev,
		"/dev/input/event12": KindEvdev,
		"foo":                KindUnknown,
	}
	for path, want := range cases {
		if got := KindFromPath(path); got != want {
			t.Fatalf("%s: %s want %s", path, got, want)
		}
	}
}

func TestParseMapEvdevMoveAndQuit(t *testing.T) {
	t.Parallel()
	right := EncodeEvdev(evKey, keyRight, 1)
	typ, code, value, err := ParseEvdev(right)
	if err != nil || typ != evKey || code != keyRight || value != 1 {
		t.Fatalf("parse right: %v %d %d %d", err, typ, code, value)
	}
	m := MapEvdev(typ, code, value)
	if m.Action != ActionRight || !m.Active {
		t.Fatalf("map right %+v", m)
	}
	release := MapEvdev(evKey, keyRight, 0)
	if release.Action != ActionRight || release.Active {
		t.Fatalf("release %+v", release)
	}
	esc := MapEvdev(evKey, keyEsc, 1)
	if esc.Action != ActionQuit || !esc.Active {
		t.Fatalf("esc %+v", esc)
	}
	start := MapEvdev(evKey, btnStart, 1)
	if start.Action != ActionQuit || !start.Active {
		t.Fatalf("start %+v", start)
	}
	hat := MapEvdev(evAbs, absHat0X, -1)
	if hat.Action != ActionLeft || !hat.Active {
		t.Fatalf("hat %+v", hat)
	}
	stick := MapEvdev(evAbs, absX, 20000)
	if stick.Action != ActionRight || !stick.Active {
		t.Fatalf("stick %+v", stick)
	}
	dead := MapEvdev(evAbs, absX, 100)
	if dead.Active || !dead.Analog {
		t.Fatalf("deadzone %+v", dead)
	}
	if !stick.Analog {
		t.Fatal("stick analog")
	}
	rep := MapEvdev(evKey, keyRight, 2)
	if !rep.Repeat || !rep.Active || rep.Action != ActionRight {
		t.Fatalf("repeat %+v", rep)
	}
	if MapEvdev(evSyn, 0, 0).Action != ActionNone {
		t.Fatal("syn")
	}
}

func TestParseMapJSMoveAndQuit(t *testing.T) {
	t.Parallel()
	rec := EncodeJS(32767, JSEventAxis, 0)
	_, value, typ, number, err := ParseJS(rec)
	if err != nil || value != 32767 || typ != JSEventAxis || number != 0 {
		t.Fatalf("parse js: %v %d %d %d", err, value, typ, number)
	}
	m := MapJS(typ, number, value)
	if m.Action != ActionRight || !m.Active {
		t.Fatalf("js axis %+v", m)
	}
	down := MapJS(JSEventAxis, 1, 32767)
	if down.Action != ActionDown || !down.Active {
		t.Fatalf("js y %+v", down)
	}
	center := MapJS(JSEventAxis, 0, 0)
	if center.Active || center.Action != ActionLeft {
		t.Fatalf("js center %+v", center)
	}
	quit := MapJS(JSEventButton, 7, 1)
	if quit.Action != ActionQuit || !quit.Active {
		t.Fatalf("js start %+v", quit)
	}
	if MapJS(JSEventButton|JSEventInit, 7, 1).Action != ActionNone {
		t.Fatal("js init")
	}
	if MapJS(JSEventButton, 0, 1).Action != ActionNone {
		t.Fatal("js south is not quit")
	}
}

func TestParseRejectsShort(t *testing.T) {
	t.Parallel()
	if _, _, _, err := ParseEvdev([]byte{1, 2, 3}); err == nil {
		t.Fatal("short evdev")
	}
	if _, _, _, _, err := ParseJS([]byte{1, 2, 3}); err == nil {
		t.Fatal("short js")
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	t.Parallel()
	typ, code, value, err := ParseEvdev(EncodeEvdev(evKey, btnDpadUp, 1))
	if err != nil || typ != evKey || code != btnDpadUp || value != 1 {
		t.Fatalf("evdev roundtrip %d %d %d %v", typ, code, value, err)
	}
	_, v, typb, n, err := ParseJS(EncodeJS(-32767, JSEventAxis, 1))
	if err != nil || v != -32767 || typb != JSEventAxis || n != 1 {
		t.Fatalf("js roundtrip %d %d %d %v", v, typb, n, err)
	}
}
