package tenfoot

import (
	"bytes"
	"image"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func init() {
	labelTTFOverride = goregular.TTF
}

func TestRasterizeLabelUTF8(t *testing.T) {
	t.Parallel()
	accented := rasterizeLabel("Éclair", 200, 16)
	if !labelHasInk(accented) {
		t.Fatal("Éclair produced no glyphs")
	}
	ascii := rasterizeLabel("Eclair", 200, 16)
	if !labelHasInk(ascii) {
		t.Fatal("Eclair produced no glyphs")
	}
	if accented.Bounds().Eq(ascii.Bounds()) && bytes.Equal(accented.Pix, ascii.Pix) {
		t.Fatal("Éclair rasterized identically to Eclair")
	}
	combining := rasterizeLabel("E\u0301clair", 200, 16)
	if !labelHasInk(combining) {
		t.Fatal("combining-accent title produced no glyphs")
	}
	if rasterizeLabel("", 200, 16) != nil {
		t.Fatal("empty label")
	}
	clipped := rasterizeLabel("Éclair Super Nintendo Entertainment System", 40, 16)
	if clipped == nil || clipped.Bounds().Dx() > 40 {
		t.Fatalf("clipped width = %v", clipped)
	}
}

func TestUnpremultiplyRGBAStraightensEdges(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Pix[0], img.Pix[1], img.Pix[2], img.Pix[3] = 118, 120, 124, 128
	unpremultiplyRGBA(img)
	if img.Pix[0] < 220 || img.Pix[1] < 220 || img.Pix[2] < 230 || img.Pix[3] != 128 {
		t.Fatalf("straight alpha = %v", img.Pix)
	}
	opaque := image.NewRGBA(image.Rect(0, 0, 1, 1))
	opaque.Pix[0], opaque.Pix[1], opaque.Pix[2], opaque.Pix[3] = 236, 240, 248, 255
	unpremultiplyRGBA(opaque)
	if opaque.Pix[0] != 236 || opaque.Pix[3] != 255 {
		t.Fatalf("opaque changed = %v", opaque.Pix)
	}
}
