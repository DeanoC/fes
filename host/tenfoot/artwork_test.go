package tenfoot

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDecodeCoverScalesJPEGSizedPNG(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(0, 0, 40, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 40; x++ {
			src.Set(x, y, color.RGBA{R: 200, G: 10, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeCover(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() > coverMaxW || got.Bounds().Dy() > coverMaxH {
		t.Fatalf("size = %s", got.Bounds())
	}
	r, g, b, a := got.At(0, 0).RGBA()
	if r>>8 < 150 || g>>8 > 40 || b>>8 > 40 || a>>8 != 255 {
		t.Fatalf("pixel = %d %d %d %d", r, g, b, a)
	}
}

func TestDecodeCoverRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := DecodeCover(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestCoverDestRectPreservesAspect(t *testing.T) {
	t.Parallel()
	x, y, w, h := coverDestRect(10, 20, 210, 284, 256, 256)
	if w != 210 || h != 210 || x != 10 || y != 57 {
		t.Fatalf("square dest = %v %v %v %v", x, y, w, h)
	}
	x, y, w, h = coverDestRect(0, 0, 210, 284, 256, 320)
	if w != 210 || h != 262.5 || x != 0 || y != 10.75 {
		t.Fatalf("portrait dest = %v %v %v %v", x, y, w, h)
	}
	x, y, w, h = coverDestRect(5, 5, 210, 284, 200, 300)
	if h != 284 || w <= 0 || w >= 210 {
		t.Fatalf("2:3 dest = %v %v %v %v", x, y, w, h)
	}
	if y < 4.99 || y > 5.01 {
		t.Fatalf("2:3 should fill height, y=%v", y)
	}
	if x <= 5 {
		t.Fatalf("2:3 should pillarbox, x=%v", x)
	}
}
