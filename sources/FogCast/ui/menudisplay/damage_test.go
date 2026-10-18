package menudisplay

import "testing"

func paint(pix []byte, x, y, w, h int) {
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			pix[row*Stride+col*4]++
		}
	}
}

func TestDiffBandsAlignedRects(t *testing.T) {
	prev := make([]byte, FrameBytes)
	next := make([]byte, FrameBytes)
	if rects := Diff(prev, next); rects != nil {
		t.Fatalf("identical frames: %v", rects)
	}
	paint(next, 13, 40, 101, 30)
	paint(next, 1270, 719, 10, 1)
	// Rows 72..73 are within the gap of the first band and join it.
	paint(next, 200, 72, 1, 2)
	rects := Diff(prev, next)
	want := []Rect{{8, 40, 200, 34}, {1264, 719, 16, 1}}
	if len(rects) != len(want) {
		t.Fatalf("rects %v", rects)
	}
	for i := range want {
		if rects[i] != want[i] {
			t.Fatalf("rects %v want %v", rects, want)
		}
	}
}

func TestDiffCapsRectCount(t *testing.T) {
	prev := make([]byte, FrameBytes)
	next := make([]byte, FrameBytes)
	for i := 0; i < 40; i++ {
		paint(next, (i%5)*200, i*17, 8, 1)
	}
	rects := Diff(prev, next)
	if len(rects) == 0 || len(rects) > MaxDamageRects {
		t.Fatalf("%d rects", len(rects))
	}
	// Every changed pixel is covered.
	for i := 0; i < 40; i++ {
		x, y := (i%5)*200, i*17
		covered := false
		for _, r := range rects {
			covered = covered || (x >= r.X && x+8 <= r.X+r.W && y >= r.Y && y < r.Y+r.H)
		}
		if !covered {
			t.Fatalf("change at %d,%d not covered by %v", x, y, rects)
		}
	}
	if !(Damage{BaseSequence: 1, Rects: rects}).valid() {
		t.Fatal("capped damage is not valid")
	}
}

func BenchmarkDiffSmallChange(b *testing.B) {
	prev := make([]byte, FrameBytes)
	next := make([]byte, FrameBytes)
	paint(next, 600, 300, 120, 40)
	b.SetBytes(FrameBytes)
	for b.Loop() {
		Diff(prev, next)
	}
}
