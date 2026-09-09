package tenfoot

import (
	"image"
	"image/color"
	"testing"
)

func TestFauxBoxProjectsSpineAndKeepsFront(t *testing.T) {
	t.Parallel()
	if FauxBox(nil) != nil {
		t.Fatal("nil")
	}
	src := image.NewRGBA(image.Rect(0, 0, 16, 24))
	front := color.RGBA{R: 200, G: 40, B: 80, A: 255}
	for y := 0; y < 24; y++ {
		for x := 0; x < 16; x++ {
			src.SetRGBA(x, y, front)
		}
	}
	got := FauxBox(src)
	if got == nil {
		t.Fatal("empty")
	}
	b := got.Bounds()
	if b.Dx() <= 16 || b.Dy() != 24 {
		t.Fatalf("size %dx%d", b.Dx(), b.Dy())
	}
	cx, cy := b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2
	mid := got.RGBAAt(cx, cy)
	if mid != front {
		t.Fatalf("front %+v", mid)
	}
	spine := got.RGBAAt(b.Min.X+1, cy)
	if spine.A == 0 || spine.R >= front.R || spine.G >= front.G || spine.B >= front.B {
		t.Fatalf("spine %+v", spine)
	}
	topRight := got.RGBAAt(b.Max.X-1, b.Min.Y)
	if topRight.A != 0 {
		t.Fatalf("top-right should stay empty %+v", topRight)
	}
	if src.RGBAAt(0, 0) != front {
		t.Fatal("source mutated")
	}
}
