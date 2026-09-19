package rooms

import (
	"testing"
	"time"
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
