package tenfoot

import (
	"bytes"
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
