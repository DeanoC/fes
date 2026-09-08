package fbgrid

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/linuxinput"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestPaintFocusAndConfirm(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	g := New(w, h)
	Paint(d, g)
	d.Present()

	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight sample")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)

	ix, iy, ok := g.InteriorSample()
	if !ok {
		t.Fatal("interior sample")
	}
	c0 := g.Tiles[0].Color
	assertBGRX(t, dst, cfg, ix, iy, c0.B, c0.G, c0.R, 0)

	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: false})
	Paint(d, g)
	d.Present()
	hx, hy, ok = g.HighlightSample()
	if !ok {
		t.Fatal("moved highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
	ix, iy, _ = g.InteriorSample()
	c1 := g.Tiles[1].Color
	assertBGRX(t, dst, cfg, ix, iy, c1.B, c1.G, c1.R, 0)

	ox, oy, _ := g.CellOrigin(0)
	assertBGRX(t, dst, cfg, ox+1, oy+1, c0.B, c0.G, c0.R, 0)

	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: false})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: false})
	Paint(d, g)
	d.Present()
	if g.Focus != 2 || g.ConfirmIndex != 1 {
		t.Fatalf("focus=%d confirm=%d", g.Focus, g.ConfirmIndex)
	}
	sx, sy, ok := g.CellOrigin(1)
	if !ok {
		t.Fatal("confirmed origin")
	}
	assertBGRX(t, dst, cfg, sx+g.CellW/2, sy+g.CellH/2, 255, 255, 255, 0)
	hx, hy, ok = g.HighlightSample()
	if !ok {
		t.Fatal("new highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
	ix, iy, _ = g.InteriorSample()
	c2 := g.Tiles[2].Color
	assertBGRX(t, dst, cfg, ix, iy, c2.B, c2.G, c2.R, 0)
	snap := d.Snapshot()
	p := snap.RGBAAt(sx+g.CellW/2, sy+g.CellH/2)
	if p != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("flash rgba %+v", p)
	}
}

func TestPaintCoverDrawsRGBAAndFallsBack(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 0, B: 200, A: 255})
		}
	}
	fallback := gfx.RGB(44, 96, 156)
	g := NewWithTiles(w, h, []Tile{
		{Name: "ART", Color: fallback, Cover: cover},
		{Name: "FLAT", Color: fallback},
	})
	Paint(d, g)
	d.Present()
	ix, iy, ok := g.InteriorSample()
	if !ok {
		t.Fatal("interior")
	}
	assertBGRX(t, dst, cfg, ix, iy, 200, 0, 255, 0)
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
	ox, oy, ok := g.CellOrigin(1)
	if !ok {
		t.Fatal("fallback origin")
	}
	assertBGRX(t, dst, cfg, ox+g.CellW/2, oy+g.CellH/2, fallback.B, fallback.G, fallback.R, 0)

	g.Focus = 1
	g.ConfirmIndex = 0
	g.ConfirmLeft = ConfirmFrames
	Paint(d, g)
	d.Present()
	sx, sy, _ := g.CellOrigin(0)
	assertBGRX(t, dst, cfg, sx+g.CellW/2, sy+g.CellH/2, 255, 255, 255, 0)
}

func TestPaintUsesOptionalHeaderAndFooter(t *testing.T) {
	t.Parallel()
	const w, h = 320, 240
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 1280, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	g := NewWithTiles(w, h, []Tile{{Name: "ONE", Color: gfx.RGB(20, 30, 40)}})
	Paint(d, g)
	d.Present()
	defaultPixels := d.Snapshot().Pix
	g.Header = "HEADER"
	g.Footer = "FOOTER"
	Paint(d, g)
	d.Present()
	if bytes.Equal(defaultPixels, d.Snapshot().Pix) {
		t.Fatal("optional header/footer did not affect paint")
	}
}

func TestChromeTextYCentersAndOverflowsAwayFromTiles(t *testing.T) {
	t.Parallel()
	if got := chromeTextY(0, 36, 2, true); got != 10 {
		t.Fatalf("default header y=%d want 10", got)
	}
	if got := chromeTextY(480-28, 28, 2, false); got != 480-22 {
		t.Fatalf("default footer y=%d want %d", got, 480-22)
	}
	if got := chromeTextY(0, 80, 2, true); got != 32 {
		t.Fatalf("tall header y=%d want 32", got)
	}
	if got := chromeTextY(480-64, 64, 2, false); got != 440 {
		t.Fatalf("tall footer y=%d want 440", got)
	}
	if got := chromeTextY(0, 36, 8, true); got != 36-64 {
		t.Fatalf("scaled header overflow y=%d want %d", got, 36-64)
	}
	if got := chromeTextY(480-28, 28, 8, false); got != 480-28 {
		t.Fatalf("scaled footer overflow y=%d want %d", got, 480-28)
	}
}

func TestPaintChromeTextUsesThemeMetrics(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	g := New(w, h)
	g.Header = "TITLE"
	g.Footer = "STATUS"

	rec := gfx.NewRecorder()
	Paint(rec, g)
	assertChromeText(t, rec, "TITLE", 16, 10, 2)
	assertChromeText(t, rec, "STATUS", 8, h-22, 2)

	th := theme.Default()
	th.HeaderH = 80
	th.FooterH = 64
	th.HeaderScale = 3
	th.StatusScale = 4
	ApplyTheme(&g, th)
	rec = gfx.NewRecorder()
	Paint(rec, g)
	headerY := chromeTextY(0, g.HeaderH, 3, true)
	footerY := chromeTextY(h-g.FooterH, g.FooterH, 4, false)
	if headerY != 28 {
		t.Fatalf("expected centered tall header y=28 got %d", headerY)
	}
	if footerY != 432 {
		t.Fatalf("expected centered tall footer y=432 got %d", footerY)
	}
	assertChromeText(t, rec, "TITLE", 16, headerY, 3)
	assertChromeText(t, rec, "STATUS", 8, footerY, 4)
	if headerY+debugGlyphPx*3 > g.HeaderH {
		t.Fatalf("header glyph [%d,%d) crosses HeaderH=%d", headerY, headerY+debugGlyphPx*3, g.HeaderH)
	}
	if footerY < h-g.FooterH {
		t.Fatalf("footer glyph y=%d is above footer top %d", footerY, h-g.FooterH)
	}

	th.HeaderH = 36
	th.FooterH = 28
	th.HeaderScale = 8
	th.StatusScale = 8
	ApplyTheme(&g, th)
	rec = gfx.NewRecorder()
	Paint(rec, g)
	headerY = chromeTextY(0, g.HeaderH, 8, true)
	footerY = chromeTextY(h-g.FooterH, g.FooterH, 8, false)
	assertChromeText(t, rec, "TITLE", 16, headerY, 8)
	assertChromeText(t, rec, "STATUS", 8, footerY, 8)
	if headerY+debugGlyphPx*8 > g.HeaderH {
		t.Fatalf("oversized header glyph [%d,%d) still crosses HeaderH=%d", headerY, headerY+debugGlyphPx*8, g.HeaderH)
	}
	if footerY < h-g.FooterH {
		t.Fatalf("oversized footer glyph y=%d is above footer top %d", footerY, h-g.FooterH)
	}
}

func TestPaintChromeTextLeavesTilesWhenHeaderIsTall(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	g := New(w, h)
	g.Header = "TITLE"
	g.Footer = "STATUS"
	th := theme.Default()
	th.HeaderH = 80
	th.FooterH = 64
	ApplyTheme(&g, th)
	Paint(d, g)
	d.Present()
	chrome := th.Complete().HeaderBar
	assertBGRX(t, dst, cfg, 18, 12, chrome.B, chrome.G, chrome.R, 0)
	ix, iy, ok := g.InteriorSample()
	if !ok {
		t.Fatal("tile interior")
	}
	c0 := g.Tiles[0].Color
	assertBGRX(t, dst, cfg, ix, iy, c0.B, c0.G, c0.R, 0)
}

func assertChromeText(t *testing.T, rec *gfx.Recorder, text string, x, y, scale int) {
	t.Helper()
	for _, c := range rec.Calls {
		if c.Op == "DebugText" && c.Text == text {
			if c.X != x || c.Y != y || c.Scale != scale {
				t.Fatalf("%q DebugText x,y,scale=%d,%d,%d want %d,%d,%d", text, c.X, c.Y, c.Scale, x, y, scale)
			}
			return
		}
	}
	t.Fatalf("missing DebugText %q in %v", text, rec.Ops())
}

func TestPaintAppliesThemeTokens(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	g := New(w, h)
	Paint(d, g)
	d.Present()
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
	assertBGRX(t, dst, cfg, 2, 2, 24, 16, 16, 0)

	ApplyTheme(&g, theme.Arcade())
	Paint(d, g)
	d.Present()
	hx, hy, ok = g.HighlightSample()
	if !ok {
		t.Fatal("arcade highlight")
	}
	hl := theme.Arcade().Highlight
	assertBGRX(t, dst, cfg, hx, hy, hl.B, hl.G, hl.R, 0)
	bg := theme.Arcade().Background
	assertBGRX(t, dst, cfg, 2, g.HeaderH+2, bg.B, bg.G, bg.R, 0)
	chrome := theme.Arcade().HeaderBar
	assertBGRX(t, dst, cfg, 8, 2, chrome.B, chrome.G, chrome.R, 0)
	if theme.Arcade().Highlight == theme.Default().Highlight {
		t.Fatal("arcade highlight must differ")
	}
}

func assertBGRX(t *testing.T, dst []byte, cfg gfx.FBConfig, x, y int, b, g, r, xx byte) {
	t.Helper()
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(dst, cfg, x, y)
	if err != nil {
		t.Fatal(err)
	}
	if gotB != b || gotG != g || gotR != r || gotX != xx {
		t.Fatalf("(%d,%d) bgrx=%d,%d,%d,%d want %d,%d,%d,%d", x, y, gotB, gotG, gotR, gotX, b, g, r, xx)
	}
}
