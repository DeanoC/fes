package tenfoot

import (
	"bytes"
	"slices"
	"strings"
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

// testLabel lays text out as drawLabel does.
func testLabel(face font.Face, text string, maxWidth, sizePx int) (gpuTexture, bool) {
	labelMu.Lock()
	defer labelMu.Unlock()
	if face == nil {
		face = labelFace(sizePx)
	}
	return layoutLabel(face, fitLabel(face, strings.TrimSpace(text), maxWidth), maxWidth, sizePx)
}

// labelPixels is the coverage a label draws, row by row of its box.
func labelPixels(l gpuTexture) []byte {
	out := make([]byte, l.w*l.h)
	if l.mask == nil {
		return out
	}
	for y := 0; y < l.maskSrc.Dy(); y++ {
		for x := 0; x < l.maskSrc.Dx(); x++ {
			out[(l.maskAt.Y+y)*l.w+l.maskAt.X+x] = l.mask.Pix[(l.maskSrc.Min.Y+y)*l.mask.W+l.maskSrc.Min.X+x]
		}
	}
	return out
}

func labelHasInk(l gpuTexture) bool {
	return slices.ContainsFunc(labelPixels(l), func(m byte) bool { return m != 0 })
}

func TestLayoutLabelUTF8(t *testing.T) {
	t.Parallel()
	accented, _ := testLabel(nil, "Éclair", 200, 16)
	if !labelHasInk(accented) {
		t.Fatal("Éclair produced no glyphs")
	}
	ascii, _ := testLabel(nil, "Eclair", 200, 16)
	if !labelHasInk(ascii) {
		t.Fatal("Eclair produced no glyphs")
	}
	if accented.w == ascii.w && bytes.Equal(labelPixels(accented), labelPixels(ascii)) {
		t.Fatal("Éclair laid out identically to Eclair")
	}
	combining, _ := testLabel(nil, "E\u0301clair", 200, 16)
	if !labelHasInk(combining) {
		t.Fatal("combining-accent title produced no glyphs")
	}
	if _, ok := testLabel(nil, "", 200, 16); ok {
		t.Fatal("empty label")
	}
	clipped, ok := testLabel(nil, "Éclair Super Nintendo Entertainment System", 40, 16)
	if !ok || clipped.w > 40 || clipped.maskAt.X+clipped.maskSrc.Dx() > 40 {
		t.Fatalf("clipped label %+v", clipped)
	}
}

func TestLayoutLabelCJKFallback(t *testing.T) {
	t.Parallel()
	primary := goRegularFace(t, 16)
	if _, ok := primary.GlyphAdvance('日'); ok {
		t.Fatal("Go Regular unexpectedly covers CJK")
	}
	titles := []string{"日本語", "中文", "한글", "ファイナルファンタジー VII"}
	for _, title := range titles {
		got, _ := testLabel(nil, title, 800, 16)
		if !labelHasInk(got) {
			t.Fatalf("%q produced no glyphs", title)
		}
		tofu, ok := testLabel(primary, title, 800, 16)
		if ok && got.w == tofu.w && got.h == tofu.h && bytes.Equal(labelPixels(got), labelPixels(tofu)) {
			if !labelHasRealGlyph('日') {
				t.Skip("no system fallback font with CJK glyphs")
			}
			t.Fatalf("%q laid out as Go Regular .notdef boxes", title)
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
