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
