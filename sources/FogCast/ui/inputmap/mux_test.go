package inputmap

import (
	"errors"
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func TestMuxMergeOrdering(t *testing.T) {
	t.Parallel()
	mux := NewMux(IdentityRemapper())
	a, _ := remoteinput.NormalizeGamepad("a", true)
	right, _ := remoteinput.NormalizeGamepad("dpad-right", true)
	left, _ := remoteinput.NormalizeGamepad("dpad-left", true)
	out, live := mux.Merge([]Source{
		{ID: "pad-b", Name: "Second", Events: []remoteinput.Event{a}},
		{ID: "pad-a", Name: "First", Events: []remoteinput.Event{right, left}},
	})
	if len(live) != 2 {
		t.Fatalf("live %d", len(live))
	}
	if len(out) != 3 {
		t.Fatalf("events %d", len(out))
	}
	if out[0].DeviceID != "pad-a" || out[0].Event.Code != remoteinput.ButtonDPadRight {
		t.Fatalf("first %+v", out[0])
	}
	if out[1].DeviceID != "pad-a" || out[1].Event.Code != remoteinput.ButtonDPadLeft {
		t.Fatalf("second %+v", out[1])
	}
	if out[2].DeviceID != "pad-b" || out[2].Event.Code != remoteinput.ButtonA || out[2].DeviceName != "Second" {
		t.Fatalf("third %+v", out[2])
	}
	stripped := Events(out)
	if len(stripped) != 3 || stripped[2].Code != remoteinput.ButtonA {
		t.Fatalf("stripped %+v", stripped)
	}
}

func TestMuxAppliesRemap(t *testing.T) {
	t.Parallel()
	r, err := NewRemapper(SwapAB())
	if err != nil {
		t.Fatal(err)
	}
	mux := NewMux(r)
	a, _ := remoteinput.NormalizeGamepad("a", true)
	out, _ := mux.Merge([]Source{{ID: "pad-a", Name: "USB", Events: []remoteinput.Event{a}}})
	if len(out) != 1 || out[0].Event.Code != remoteinput.ButtonB {
		t.Fatalf("remap %+v", out)
	}
}

func TestMuxDropsFailedSourceKeepsOther(t *testing.T) {
	t.Parallel()
	mux := NewMux(nil)
	a, _ := remoteinput.NormalizeGamepad("a", true)
	out, live := mux.Merge([]Source{
		{ID: "dead", Name: "Gone", Err: errors.New("disconnected")},
		{ID: "live", Name: "USB", Events: []remoteinput.Event{a}},
	})
	if len(live) != 1 || live[0].ID != "live" {
		t.Fatalf("live %+v", live)
	}
	if len(out) != 1 || out[0].DeviceID != "live" {
		t.Fatalf("out %+v", out)
	}
}

func TestMuxEmptyWhenAllFailed(t *testing.T) {
	t.Parallel()
	mux := NewMux(IdentityRemapper())
	out, live := mux.Merge([]Source{
		{ID: "a", Err: errors.New("gone")},
		{ID: "b", Err: errors.New("gone")},
	})
	if len(out) != 0 || len(live) != 0 {
		t.Fatalf("out %+v live %+v", out, live)
	}
}

func TestMuxSameIDBreaksTiesByName(t *testing.T) {
	t.Parallel()
	mux := NewMux(IdentityRemapper())
	up, _ := remoteinput.NormalizeGamepad("dpad-up", true)
	down, _ := remoteinput.NormalizeGamepad("dpad-down", true)
	out, _ := mux.Merge([]Source{
		{ID: "dup", Name: "z", Events: []remoteinput.Event{down}},
		{ID: "dup", Name: "a", Events: []remoteinput.Event{up}},
	})
	if len(out) != 2 || out[0].DeviceName != "a" || out[0].Event.Code != remoteinput.ButtonDPadUp {
		t.Fatalf("tie-break %+v", out)
	}
}
