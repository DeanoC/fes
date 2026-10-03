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
	if len(got) != 3 || got[0].Player != 0 || got[0].Action != remoteinput.ActionRelease || !remoteinput.IsLocalPlayerDeparture(got[1]) || got[1].Player != 0 || got[2].Player != 1 {
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
	if err != nil || len(got) != 2 || got[0].Player != 0 || got[0].Action != remoteinput.ActionRelease || !remoteinput.IsLocalPlayerDeparture(got[1]) || got[1].Player != 0 {
		t.Fatalf("last release %+v %v", got, err)
	}
	if _, err = h.Poll(); err == nil {
		t.Fatal("missing disconnected state")
	}
}

func TestHubNeutralPadDepartureStillFreesSlot(t *testing.T) {
	pad := &fakePad{id: "a"}
	h := NewHub(nil, []padSource{pad})
	if _, err := h.Poll(); err != nil {
		t.Fatal(err)
	}
	pad.err = errors.New("unplug")
	got, err := h.Poll()
	if err != nil || len(got) != 1 || got[0] != remoteinput.LocalPlayerDeparture(0) {
		t.Fatalf("neutral pad departure: %+v %v", got, err)
	}
	if _, err := h.Poll(); err == nil {
		t.Fatal("missing disconnected state")
	}
}

func TestHubUnassignedPadDepartureDoesNotFreeSurvivors(t *testing.T) {
	first := &fakePad{id: "a"}
	second := &fakePad{id: "b"}
	extra := &fakePad{id: "c"}
	h := NewHub(nil, []padSource{first, second, extra})
	if _, err := h.Poll(); err != nil {
		t.Fatal(err)
	}
	extra.err = errors.New("unplug excess pad")
	got, err := h.Poll()
	if err != nil || len(got) != 0 || h.ports["a"] != 0 || h.ports["b"] != 1 {
		t.Fatalf("unassigned departure: %+v ports=%+v %v", got, h.ports, err)
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
