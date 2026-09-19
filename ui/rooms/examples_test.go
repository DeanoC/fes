package rooms

import (
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
)

// Every embedded example must load and draw its first frame against a fake
// host without a script error.
func TestEmbeddedExamplesLoadAndDraw(t *testing.T) {
	packs := Examples()
	if len(packs) < 5 {
		t.Fatalf("expected the sample rooms, got %d", len(packs))
	}
	svc := &fakeServices{}
	index := NewIndex(packs)
	for _, p := range packs {
		if p.Err != nil {
			t.Fatalf("%s: %v", p.ID, p.Err)
		}
		r := newRoom(t, p, Options{Services: svc, Index: index, Width: 1280, Height: 720})
		if err := r.Load(); err != nil {
			t.Fatalf("%s load: %v", p.ID, err)
		}
		stepUntil(t, r, func(f Frame) bool { return len(f.Ops) > 0 })
		for _, cmd := range []string{"down", "right", "left", "up", "select"} {
			r.Input(cmd)
			if r.Err() != nil {
				t.Fatalf("%s input %s: %v", p.ID, cmd, r.Err())
			}
		}
		r.Step(time.Unix(9, 0))
		if r.Err() != nil {
			t.Fatalf("%s: %v", p.ID, r.Err())
		}
		r.Close()
	}
}

func TestMushroomKingdomPlayedIsNotCompleted(t *testing.T) {
	var pack Pack
	for _, p := range Examples() {
		if p.ID == "example.mario-world" {
			pack = p
			break
		}
	}
	if !pack.Valid() {
		t.Fatal("example.mario-world missing")
	}
	svc := &fakeServices{
		games: []hostclient.Game{
			{ID: "arcade-dk", Title: "Donkey Kong", System: "arcade", State: "available", RootOnline: true, Launchable: true, PlayCount: 2, LastPlayedAt: 88},
		},
	}
	r := newRoom(t, pack, Options{Services: svc, Width: 1280, Height: 720})
	if err := r.Load(); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, r, func(Frame) bool {
		return r.Destination().History.Played
	})
	d := r.Destination()
	if d.Label != "Donkey Kong" || d.History.Completed || d.History.Line() != "Played" {
		t.Fatalf("first node %+v history %+v", d, d.History)
	}
	playedFill, err := ParseHexColor("#d4a017")
	if err != nil {
		t.Fatal(err)
	}
	doneFill, err := ParseHexColor("#2fb457")
	if err != nil {
		t.Fatal(err)
	}
	assertNodeFill := func(when string) {
		t.Helper()
		f := r.Step(time.Unix(20, 0))
		// Focused Donkey Kong sits near (128, 488) after the room's scale.
		var fill string
		for _, op := range f.Ops {
			if op.Kind != OpRect || op.W != 44 || op.H != 44 {
				continue
			}
			cx, cy := op.X+22, op.Y+22
			if cx < 100 || cx > 160 || cy < 450 || cy > 530 {
				continue
			}
			fill = FormatHexColor(op.Color)
			break
		}
		if fill == "" {
			t.Fatalf("%s: missing Donkey Kong node fill", when)
		}
		if fill != FormatHexColor(playedFill) {
			t.Fatalf("%s: Donkey Kong fill %s want Played %s", when, fill, FormatHexColor(playedFill))
		}
		if fill == FormatHexColor(doneFill) {
			t.Fatalf("%s: Played title used Completed fill", when)
		}
	}
	assertNodeFill("after match")
	r.Resume()
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	d = r.Destination()
	if d.History.Completed || d.History.Line() == "Completed" {
		t.Fatalf("resume after launch claimed Completed: %+v", d.History)
	}
	if !d.History.Played || d.History.Line() != "Played" {
		t.Fatalf("resume should stay Played: %+v", d.History)
	}
	assertNodeFill("after resume")
}
