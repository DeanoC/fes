package gfx

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestScalePxMatchesFormerDebugGlyphHeight(t *testing.T) {
	t.Parallel()
	if ScalePx(0) != GlyphPx || ScalePx(1) != 8 || ScalePx(2) != 16 || ScalePx(8) != 64 {
		t.Fatalf("ScalePx 0,1,2,8 = %d,%d,%d,%d", ScalePx(0), ScalePx(1), ScalePx(2), ScalePx(8))
	}
}

func TestSoftwareDrawTextWritesInk(t *testing.T) {
	t.Parallel()
	s, err := NewSoftware(64, 32)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(RGB(0, 0, 0))
	s.DrawText(0, 0, "A", 16, RGB(236, 240, 248))
	snap := s.Snapshot()
	if !snapshotHasInk(snap) {
		t.Fatal("expected UI-face ink")
	}
}

func TestSoftwareDrawTextTintsWithColor(t *testing.T) {
	t.Parallel()
	red := drawLetter("H", RGB(255, 0, 0))
	blue := drawLetter("H", RGB(0, 0, 255))
	if !snapshotHasNear(red, color.RGBA{255, 0, 0, 255}) {
		t.Fatal("red glyph missing red ink")
	}
	if !snapshotHasNear(blue, color.RGBA{0, 0, 255, 255}) {
		t.Fatal("blue glyph missing blue ink")
	}
	if sameOpaquePixels(red, blue) {
		t.Fatal("theme colours did not tint glyphs")
	}
}

func TestSoftwareDrawTextWeightBoldDiffersFromRegular(t *testing.T) {
	t.Parallel()
	const w, h = 96, 32
	regular, err := NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	bold, err := NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	regular.Clear(RGB(16, 16, 24))
	bold.Clear(RGB(16, 16, 24))
	col := RGB(236, 240, 248)
	regular.DrawText(2, 2, "FOGCAST", 20, col)
	bold.DrawTextWeight(2, 2, "FOGCAST", 20, WeightBold, col)
	if sameOpaquePixels(regular.Snapshot(), bold.Snapshot()) {
		t.Fatal("bold raster matched regular at the same size")
	}
	if !snapshotHasInk(bold.Snapshot()) {
		t.Fatal("bold face produced no ink")
	}
	if MeasureTextWeight("FOGCAST", 20, WeightBold) < MeasureText("FOGCAST", 20) {
		t.Fatal("expected bold advance to be at least as wide as regular")
	}
	if FitTextWeight("Super Nintendo Entertainment System", 16, 40, WeightBold) == "Super Nintendo Entertainment System" {
		t.Fatal("bold FitText should still truncate")
	}
	if TextHeightWeight(20, WeightBold) < 1 {
		t.Fatal("bold line height")
	}
}

func TestSoftwareDrawTextDiffersFromDebugText(t *testing.T) {
	t.Parallel()
	const w, h = 80, 24
	ui, err := NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	debug, err := NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	ui.Clear(RGB(16, 16, 24))
	debug.Clear(RGB(16, 16, 24))
	ui.DrawText(0, 0, "FOGCAST", 16, RGB(236, 240, 248))
	debug.DebugText(0, 0, "FOGCAST", 2)
	if sameOpaquePixels(ui.Snapshot(), debug.Snapshot()) {
		t.Fatal("DrawText matched DebugText 8x8 HUD")
	}
}

func TestFitTextTruncatesWithEllipsis(t *testing.T) {
	t.Parallel()
	const size = 16
	long := "Super Nintendo Entertainment System"
	full := MeasureText(long, size)
	if full < 80 {
		t.Fatalf("expected a wide string, got %d", full)
	}
	got := FitText(long, size, 40)
	if got == long || !strings.HasSuffix(got, "...") {
		t.Fatalf("truncated %q", got)
	}
	if MeasureText(got, size) > 40 {
		t.Fatalf("ellipsis still overflows: %q width %d", got, MeasureText(got, size))
	}
	if FitText("OK", size, 400) != "OK" {
		t.Fatal("short string must stay intact")
	}
	if FitText("", size, 10) != "" {
		t.Fatal("empty")
	}
}

func TestRasterizeTextHasInkAndRespectsMaxWidth(t *testing.T) {
	t.Parallel()
	img := RasterizeText("TITLE", 16, RGB(255, 240, 220), 0)
	if !textHasInk(img) {
		t.Fatal("TITLE produced no glyphs")
	}
	clipped := RasterizeText("TITLE THAT WILL NOT FIT IN A NARROW CELL", 16, RGB(220, 230, 240), 32)
	if clipped == nil || clipped.Bounds().Dx() > 32 {
		t.Fatalf("clipped width = %v", clipped)
	}
	if RasterizeText("", 16, RGB(255, 255, 255), 0) != nil {
		t.Fatal("empty text")
	}
}

func TestFPGADrawTextRecordsAndReplays(t *testing.T) {
	t.Parallel()
	f, err := NewFPGA(64, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.BeginFrame()
	f.Clear(RGB(8, 8, 12))
	f.DrawText(2, 2, "KIT", 16, RGB(255, 0, 170))
	f.Present()
	var saw bool
	for _, c := range f.Commands() {
		if c.Op == OpDrawText && c.Text == "KIT" && c.SizePx == 16 && c.Color == RGB(255, 0, 170) && c.Weight == WeightRegular {
			saw = true
		}
		if c.Op == OpDebugText {
			t.Fatal("DrawText must not record OpDebugText")
		}
	}
	if !saw {
		t.Fatalf("missing OpDrawText in %v", f.Commands())
	}
	raw, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	sw, err := NewSoftware(64, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayBytes(sw, raw); err != nil {
		t.Fatal(err)
	}
	if !sameOpaquePixels(f.Snapshot(), sw.Snapshot()) {
		t.Fatal("replay pixels differ from FPGA software raster")
	}
}

func TestFPGADrawTextWeightRecordsBold(t *testing.T) {
	t.Parallel()
	f, err := NewFPGA(64, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.BeginFrame()
	f.Clear(RGB(8, 8, 12))
	f.DrawTextWeight(2, 2, "KIT", 16, WeightBold, RGB(255, 0, 170))
	f.Present()
	var saw bool
	for _, c := range f.Commands() {
		if c.Op == OpDrawText && c.Text == "KIT" && c.Weight == WeightBold {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("missing bold OpDrawText in %v", f.Commands())
	}
	raw, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	_, cmds, err := DecodeStream(raw)
	if err != nil {
		t.Fatal(err)
	}
	var decoded bool
	for _, c := range cmds {
		if c.Op == OpDrawText && c.Weight == WeightBold && c.Text == "KIT" {
			decoded = true
		}
	}
	if !decoded {
		t.Fatalf("decoded weight missing in %v", cmds)
	}
	sw, err := NewSoftware(64, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReplayBytes(sw, raw); err != nil {
		t.Fatal(err)
	}
	if !sameOpaquePixels(f.Snapshot(), sw.Snapshot()) {
		t.Fatal("bold replay pixels differ from FPGA software raster")
	}
}

func drawLetter(letter string, c Color) *image.RGBA {
	s, err := NewSoftware(32, 32)
	if err != nil {
		panic(err)
	}
	s.Clear(RGB(0, 0, 0))
	s.DrawText(2, 2, letter, 20, c)
	return s.Snapshot()
}

func snapshotHasInk(img *image.RGBA) bool {
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 || img.Pix[i+1] != 0 || img.Pix[i+2] != 0 {
			return true
		}
	}
	return false
}

func snapshotHasNear(img *image.RGBA, want color.RGBA) bool {
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if colorNear(p, want, 40) {
				return true
			}
		}
	}
	return false
}

func colorNear(p, want color.RGBA, slop int) bool {
	dr := abs8(int(p.R) - int(want.R))
	dg := abs8(int(p.G) - int(want.G))
	db := abs8(int(p.B) - int(want.B))
	return dr <= slop && dg <= slop && db <= slop && p.A > 128
}

func abs8(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sameOpaquePixels(a, b *image.RGBA) bool {
	if a.Bounds() != b.Bounds() || len(a.Pix) != len(b.Pix) {
		return false
	}
	for i := 0; i < len(a.Pix); i += 4 {
		if a.Pix[i] != b.Pix[i] || a.Pix[i+1] != b.Pix[i+1] || a.Pix[i+2] != b.Pix[i+2] {
			return false
		}
	}
	return true
}
