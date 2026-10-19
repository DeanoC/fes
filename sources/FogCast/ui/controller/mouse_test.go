package controller

import (
	"errors"
	"github.com/DeanoC/FogCast/remoteinput"
	"testing"
)

func TestMouseSYNAtomicAndBounds(t *testing.T) {
	m := NewMouseMapper()
	for _, v := range [][3]int32{{2, 0, 11}, {2, 1, -7}, {1, 272, 1}, {1, 273, 1}, {2, 8, 99}} {
		if _, ok := m.Map(uint16(v[0]), uint16(v[1]), v[2]); ok {
			t.Fatal("partial packet")
		}
	}
	e, ok := m.Map(0, 0, 0)
	x, y, b, valid := remoteinput.MouseVector(e)
	if !ok || !valid || x != 11 || y != -7 || b != 3 {
		t.Fatalf("%+v", e)
	}
	if _, ok := m.Map(0, 0, 0); ok {
		t.Fatal("duplicate SYN")
	}
	m.Map(2, 0, 32767)
	m.Map(2, 0, 1)
	if _, ok := m.Map(0, 0, 0); ok || !m.mouseFault {
		t.Fatal("overflow silently wrapped")
	}
}

type fakeMouse struct{ *fakePad }

func (*fakeMouse) IsMouse() bool { return true }
func TestMouseHubUnionAndUnplug(t *testing.T) {
	a := &fakeMouse{&fakePad{id: "event1", events: []remoteinput.Event{remoteinput.MouseEvent(3, 0, 1)}}}
	b := &fakeMouse{&fakePad{id: "event2", events: []remoteinput.Event{remoteinput.MouseEvent(0, 4, 2)}}}
	pad := &fakePad{id: "event3", events: []remoteinput.Event{{Device: remoteinput.DeviceGamepad, Kind: remoteinput.KindButton, Action: remoteinput.ActionPress, Code: remoteinput.ButtonA}}}
	h := NewHub(nil, []padSource{a, b, pad})
	got, err := h.Poll()
	if err != nil || len(got) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	_, _, buttons, _ := remoteinput.MouseVector(got[1])
	if buttons != 3 || got[2].Player != 0 {
		t.Fatal(got)
	}
	a.err = errors.New("disconnected")
	a.events = nil
	b.events = nil
	pad.events = nil
	got, _ = h.Poll()
	if len(got) != 1 {
		t.Fatal(got)
	}
	x, y, buttons, ok := remoteinput.MouseVector(got[0])
	if !ok || x != 0 || y != 0 || buttons != 2 {
		t.Fatal(got)
	}
}
