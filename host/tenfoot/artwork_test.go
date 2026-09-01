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
