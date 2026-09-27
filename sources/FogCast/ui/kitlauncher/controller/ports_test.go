package controller

import (
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestHubDisconnectKeepsSurvivorPortAndReusesFreeSlot(t *testing.T) {
	a, _ := remoteinput.NormalizeGamepad("a", true)
	first := &fakePad{id: "a", events: []remoteinput.Event{a}}
	second := &fakePad{id: "b", events: []remoteinput.Event{a}}
	h := NewHub(nil, []padSource{second, first})
	if _, err := h.Poll(); err != nil {
		t.Fatal(err)
	}
	first.err = errors.New("unplug")
	got, err := h.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Player != 0 || got[0].Action != remoteinput.ActionRelease || got[1].Player != 1 {
		t.Fatalf("unplug %+v", got)
	}
	h.add(&fakePad{id: "c", events: []remoteinput.Event{a}})
	got, err = h.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Player != 1 || got[1].Player != 0 {
		t.Fatalf("replug %+v", got)
	}
}

func TestHubFinalDisconnectDeliversNeutralBeforeError(t *testing.T) {
	a, _ := remoteinput.NormalizeGamepad("a", true)
	pad := &fakePad{id: "a", events: []remoteinput.Event{a}}
	h := NewHub(nil, []padSource{pad})
	_, _ = h.Poll()
	pad.err = errors.New("unplug")
	got, err := h.Poll()
	if err != nil || len(got) != 1 || got[0].Player != 0 || got[0].Action != remoteinput.ActionRelease {
		t.Fatalf("last release %+v %v", got, err)
	}
	if _, err = h.Poll(); err == nil {
		t.Fatal("missing disconnected state")
	}
}

type fakeKeyboard struct{ fakePad }

func (*fakeKeyboard) IsKeyboard() bool { return true }

func TestHubKeyboardDoesNotConsumeControllerSlots(t *testing.T) {
	key, ok := NewKeyboardMapper().Map(1, 30, 1)
	if !ok {
		t.Fatal("keyboard A mapping failed")
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	keyboard := &fakeKeyboard{fakePad{id: "event0", events: []remoteinput.Event{key}}}
	first := &fakePad{id: "event3", events: []remoteinput.Event{a}}
	second := &fakePad{id: "event7", events: []remoteinput.Event{a}}
	h := NewHub(nil, []padSource{keyboard, first, second})
	got, err := h.Poll()
	if err != nil || len(got) != 3 {
		t.Fatalf("events %+v: %v", got, err)
	}
	if got[0].Device != remoteinput.DeviceKeyboard || got[0].Player != 0 || got[1].Player != 0 || got[2].Player != 1 {
		t.Fatalf("keyboard and controller players %+v", got)
	}
	keyboard.err = errors.New("unplug keyboard")
	got, err = h.Poll()
	if err != nil || len(got) != 3 || got[0].Device != remoteinput.DeviceKeyboard || got[0].Action != remoteinput.ActionRelease || got[1].Player != 0 || got[2].Player != 1 {
		t.Fatalf("keyboard disconnect %+v: %v", got, err)
	}
	h.add(&fakeKeyboard{fakePad{id: "event1", events: []remoteinput.Event{key}}})
	got, err = h.Poll()
	if err != nil || len(got) != 3 || got[0].Device != remoteinput.DeviceKeyboard || got[0].Player != 0 || got[1].Player != 0 || got[2].Player != 1 {
		t.Fatalf("keyboard reconnect %+v: %v", got, err)
	}
}
