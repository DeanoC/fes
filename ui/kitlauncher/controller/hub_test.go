package controller

import (
	"errors"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

type fakePad struct {
	id, name string
	events   []remoteinput.Event
	err      error
	closed   bool
	polls    int
}

func (p *fakePad) Poll() ([]remoteinput.Event, error) {
	p.polls++
	if p.err != nil {
		return nil, p.err
	}
	return p.events, nil
}
func (p *fakePad) Close() error           { p.closed = true; return nil }
func (p *fakePad) Info() (string, string) { return p.id, p.name }

func TestHubMergesTwoPadsInIDOrder(t *testing.T) {
	a, _ := remoteinput.NormalizeGamepad("a", true)
	right, _ := remoteinput.NormalizeGamepad("dpad-right", true)
	first := &fakePad{id: "event3", name: "Pad B", events: []remoteinput.Event{a}}
	second := &fakePad{id: "event1", name: "Pad A", events: []remoteinput.Event{right}}
	h := NewHub(nil, []padSource{first, second})
	got, err := h.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Code != remoteinput.ButtonDPadRight || got[1].Code != remoteinput.ButtonA {
		t.Fatalf("merge %+v", got)
	}
	if got[0].Player != 0 || got[1].Player != 1 {
		t.Fatalf("players %+v", got)
	}
	devs := h.Devices()
	if len(devs) != 2 {
		t.Fatalf("devices %d", len(devs))
	}
}

func TestHubIdentityPreservesLaunchAndStopCodes(t *testing.T) {
	a, _ := remoteinput.NormalizeGamepad("a", true)
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	start, _ := remoteinput.NormalizeGamepad("start", true)
	h := NewHub(inputmap.IdentityRemapper(), []padSource{
		&fakePad{id: "event0", name: "USB", events: []remoteinput.Event{a, selectPress, start}},
	})
	got, err := h.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Code != remoteinput.ButtonA || got[1].Code != remoteinput.ButtonSelect || got[2].Code != remoteinput.ButtonStart {
		t.Fatalf("identity %+v", got)
	}
}

func TestHubAppliesSwapAB(t *testing.T) {
	r, err := inputmap.NewRemapper(inputmap.SwapAB())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := remoteinput.NormalizeGamepad("a", true)
	h := NewHub(r, []padSource{&fakePad{id: "event0", name: "USB", events: []remoteinput.Event{a}}})
	got, err := h.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Code != remoteinput.ButtonB {
		t.Fatalf("swap %+v", got)
	}
}

func TestHubDropsFailedPadKeepsOther(t *testing.T) {
	a, _ := remoteinput.NormalizeGamepad("a", true)
	dead := &fakePad{id: "event9", name: "Dead", err: errors.New("disconnected")}
	live := &fakePad{id: "event0", name: "Live", events: []remoteinput.Event{a}}
	h := NewHub(nil, []padSource{dead, live})
	got, err := h.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Code != remoteinput.ButtonA {
		t.Fatalf("got %+v", got)
	}
	if !dead.closed {
		t.Fatal("failed pad not closed")
	}
	if live.closed {
		t.Fatal("live pad closed")
	}
	if len(h.Devices()) != 1 || h.Devices()[0].ID != "event0" {
		t.Fatalf("devices %+v", h.Devices())
	}
}

func TestHubReleasesHeldButtonsWhenPadDrops(t *testing.T) {
	a, _ := remoteinput.NormalizeGamepad("a", true)
	selectPress, _ := remoteinput.NormalizeGamepad("select", true)
	held := &fakePad{id: "event1", name: "Held", events: []remoteinput.Event{a, selectPress}}
	live := &fakePad{id: "event0", name: "Live"}
	h := NewHub(nil, []padSource{held, live})
	if _, err := h.Poll(); err != nil {
		t.Fatal(err)
	}
	held.err = errors.New("disconnected")
	held.events = nil
	got, err := h.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Code != remoteinput.ButtonA || got[0].Action != remoteinput.ActionRelease || got[1].Code != remoteinput.ButtonSelect || got[1].Action != remoteinput.ActionRelease {
		t.Fatalf("releases %+v", got)
	}
	if !held.closed {
		t.Fatal("held pad not closed")
	}
}

func TestHubCentersDroppedPadAxis(t *testing.T) {
	right, _ := remoteinput.NormalizeAxis("left-x", 32767)
	held := &fakePad{id: "event1", name: "Stick", events: []remoteinput.Event{right}}
	live := &fakePad{id: "event0", name: "Live"}
	h := NewHub(nil, []padSource{held, live})
	if _, err := h.Poll(); err != nil {
		t.Fatal(err)
	}
	held.err = errors.New("disconnected")
	held.events = nil
	got, err := h.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Code != remoteinput.AxisLeftX || got[0].Action != remoteinput.ActionAbsolute || got[0].Value != 0 {
		t.Fatalf("axis %+v", got)
	}
}

func TestHubDisconnectsWhenLastPadFails(t *testing.T) {
	dead := &fakePad{id: "event0", name: "USB", err: errors.New("controller disconnected")}
	h := NewHub(nil, []padSource{dead})
	if _, err := h.Poll(); err == nil {
		t.Fatal("expected disconnect")
	}
	if !dead.closed {
		t.Fatal("not closed")
	}
	if len(h.Devices()) != 0 {
		t.Fatal("stale device")
	}
}

func TestHubRescanAddsPadWithoutBlocking(t *testing.T) {
	now := time.Unix(10, 0)
	existing := &fakePad{id: "event0", name: "First"}
	added := &fakePad{id: "event1", name: "Second"}
	h := NewHub(nil, []padSource{existing})
	h.clock = func() time.Time { return now }
	h.lastScan = now.Add(-2 * time.Second)
	h.rescan = func(hub *Hub) { hub.add(added) }
	if _, err := h.Poll(); err != nil {
		t.Fatal(err)
	}
	if len(h.Devices()) != 2 {
		t.Fatalf("devices %+v", h.Devices())
	}
	now = now.Add(10 * time.Millisecond)
	h.clock = func() time.Time { return now }
	rescans := 0
	h.rescan = func(*Hub) { rescans++ }
	if _, err := h.Poll(); err != nil {
		t.Fatal(err)
	}
	if rescans != 0 {
		t.Fatalf("rescan on 16ms tick: %d", rescans)
	}
}

func TestHubCloseClosesAll(t *testing.T) {
	a := &fakePad{id: "a", name: "A"}
	b := &fakePad{id: "b", name: "B"}
	h := NewHub(nil, []padSource{a, b})
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if !a.closed || !b.closed {
		t.Fatal("pads open")
	}
}
