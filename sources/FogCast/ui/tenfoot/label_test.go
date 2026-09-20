package tenfoot

import (
	"bytes"
	"image"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

func init() {
	labelTTFOverride = goregular.TTF
}

func goRegularFace(t *testing.T, sizePx int) font.Face {
	t.Helper()
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    float64(sizePx),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		t.Fatal(err)
	}
	return face
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

func TestRasterizeLabelCJKFallback(t *testing.T) {
	t.Parallel()
	primary := goRegularFace(t, 16)
	if _, ok := primary.GlyphAdvance('日'); ok {
		t.Fatal("Go Regular unexpectedly covers CJK")
	}
	titles := []string{"日本語", "中文", "한글", "ファイナルファンタジー VII"}
	for _, title := range titles {
		got := rasterizeLabel(title, 800, 16)
		if !labelHasInk(got) {
			t.Fatalf("%q produced no glyphs", title)
		}
		tofu := rasterizeLabelWithFace(primary, title, 800, 16)
		if tofu != nil && got.Bounds().Eq(tofu.Bounds()) && bytes.Equal(got.Pix, tofu.Pix) {
			if !labelHasRealGlyph('日') {
				t.Skip("no system fallback font with CJK glyphs")
			}
			t.Fatalf("%q rasterized as Go Regular .notdef boxes", title)
		}
	}
}

func TestLabelCacheKeyIncludesRenderedText(t *testing.T) {
	t.Parallel()
	titleA := labelCacheKey("d-title", "Mario", 800, 26)
	titleB := labelCacheKey("d-title", "Sonic", 800, 26)
	chromeA := labelCacheKey("chrome", "All  ·  Title  ·  Search  ·  2 titles", 1232, 18)
	chromeB := labelCacheKey("chrome", "Super NES  ·  Title  ·  Search  ·  1 titles", 1232, 18)
	if titleA == titleB {
		t.Fatal("detail title key reused across different games")
	}
	if chromeA == chromeB {
		t.Fatal("chrome key reused after filter text changed")
	}
	if titleA != labelCacheKey("d-title", " Mario ", 800, 26) {
		t.Fatal("trimmed text must share a key")
	}
	sum0 := labelCacheKey("d-sum-0", "Jump on turtles.", 800, 16)
	sum1 := labelCacheKey("d-sum-0", "Gotta go fast.", 800, 16)
	if sum0 == sum1 {
		t.Fatal("summary line key reused across different text")
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
