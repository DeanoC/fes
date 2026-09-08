package fbgrid

import (
	"bytes"
	"image"
	"image/color"
	"strings"
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

	c0 := g.Tiles[0].Color
	assertPanel(t, dst, cfg, g, 0, c0)

	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionRight, Active: false})
	Paint(d, g)
	d.Present()
	hx, hy, ok = g.HighlightSample()
	if !ok {
		t.Fatal("moved highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
	c1 := g.Tiles[1].Color
	assertPanel(t, dst, cfg, g, 1, c1)
	assertPanel(t, dst, cfg, g, 0, c0)

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
	c2 := g.Tiles[2].Color
	assertPanel(t, dst, cfg, g, 2, c2)
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
	assertPanel(t, dst, cfg, g, 1, fallback)
	px, py, ok := g.PanelSample(1)
	if !ok {
		t.Fatal("fallback panel")
	}
	if gfxEqualBGRX(dst, cfg, px, py, fallback.B, fallback.G, fallback.R, 0) {
		t.Fatal("missing-art tile stayed a flat system fill")
	}

	g.Focus = 1
	g.ConfirmIndex = 0
	g.ConfirmLeft = ConfirmFrames
	Paint(d, g)
	d.Present()
	sx, sy, _ := g.CellOrigin(0)
	assertBGRX(t, dst, cfg, sx+g.CellW/2, sy+g.CellH/2, 255, 255, 255, 0)
}

func TestPaintLogoReplacesLabelTextAndFallsBack(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	logo := image.NewRGBA(image.Rect(0, 0, 24, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 24; x++ {
			logo.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	th := theme.Default()
	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 40, G: 90, B: 200, A: 255})
		}
	}
	g := NewWithTiles(w, h, []Tile{
		{Name: "SONIC", Color: th.SystemColor("megadrive"), Cover: cover, CoverKind: CoverPresent, Logo: logo},
		{Name: "PONG", Color: th.SystemColor("pong")},
	})
	if g.Tiles[0].Logo == nil {
		t.Fatal("logo missing from grid tile")
	}
	Paint(d, g)
	d.Present()
	lx, ly, ok := g.LabelBarSample(0)
	if !ok {
		t.Fatal("logo bar sample")
	}
	assertBGRX(t, dst, cfg, lx, ly, 160, 32, 255, 0)

	rec := gfx.NewRecorder()
	Paint(rec, g)
	labelPx := th.BodyPx()
	var sawSonic, sawPong bool
	var texts []string
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		texts = append(texts, c.Text)
		if c.Text == "SONIC" && c.SizePx == labelPx {
			sawSonic = true
		}
		if c.Text == "PONG" && c.SizePx == labelPx {
			sawPong = true
		}
	}
	if sawSonic {
		t.Fatalf("logo tile still drew truncated title text ops=%v", texts)
	}
	if !sawPong {
		t.Fatalf("neighbour without logo dropped text fallback ops=%v", texts)
	}
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
	if got := chromeTextY(0, 36, 16, true); got != 10 {
		t.Fatalf("default header y=%d want 10", got)
	}
	if got := chromeTextY(480-28, 28, 16, false); got != 480-22 {
		t.Fatalf("default footer y=%d want %d", got, 480-22)
	}
	if got := chromeTextY(0, 80, 16, true); got != 32 {
		t.Fatalf("tall header y=%d want 32", got)
	}
	if got := chromeTextY(480-64, 64, 16, false); got != 440 {
		t.Fatalf("tall footer y=%d want 440", got)
	}
	if got := chromeTextY(0, 36, 64, true); got != 36-64 {
		t.Fatalf("scaled header overflow y=%d want %d", got, 36-64)
	}
	if got := chromeTextY(480-28, 28, 64, false); got != 480-28 {
		t.Fatalf("scaled footer overflow y=%d want %d", got, 480-28)
	}
}

func TestPaintChromeTextUsesThemeMetrics(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	g := New(w, h)
	g.Header = "TITLE"
	g.Footer = "STATUS"
	def := theme.Default().Complete()

	rec := gfx.NewRecorder()
	Paint(rec, g)
	headerSize := def.TitlePx()
	statusSize := def.StatusPx()
	headerW := def.HeaderWeight()
	statusW := def.StatusWeight()
	assertChromeText(t, rec, "TITLE", 16, chromeTextY(0, g.HeaderH, gfx.TextHeightWeight(headerSize, headerW), true), headerSize, def.Header, headerW)
	assertChromeText(t, rec, "STATUS", 8, chromeTextY(h-g.FooterH, g.FooterH, gfx.TextHeightWeight(statusSize, statusW), false), statusSize, def.Status, statusW)
	assertNoDebugText(t, rec)

	th := theme.Default()
	th.HeaderH = 80
	th.FooterH = 64
	th.TitleSize = 0
	th.StatusSize = 0
	th.HeaderScale = 3
	th.StatusScale = 4
	ApplyTheme(&g, th)
	rec = gfx.NewRecorder()
	Paint(rec, g)
	headerSize = gfx.ScalePx(3)
	statusSize = gfx.ScalePx(4)
	headerW = th.Complete().HeaderWeight()
	statusW = th.Complete().StatusWeight()
	headerY := chromeTextY(0, g.HeaderH, gfx.TextHeightWeight(headerSize, headerW), true)
	footerY := chromeTextY(h-g.FooterH, g.FooterH, gfx.TextHeightWeight(statusSize, statusW), false)
	assertChromeText(t, rec, "TITLE", 16, headerY, headerSize, th.Complete().Header, headerW)
	assertChromeText(t, rec, "STATUS", 8, footerY, statusSize, th.Complete().Status, statusW)
	if headerY+gfx.TextHeightWeight(headerSize, headerW) > g.HeaderH {
		t.Fatalf("header glyph [%d,%d) crosses HeaderH=%d", headerY, headerY+gfx.TextHeightWeight(headerSize, headerW), g.HeaderH)
	}
	if footerY < h-g.FooterH {
		t.Fatalf("footer glyph y=%d is above footer top %d", footerY, h-g.FooterH)
	}

	th.HeaderH = 36
	th.FooterH = 28
	th.TitleSize = 0
	th.StatusSize = 0
	th.HeaderScale = 8
	th.StatusScale = 8
	ApplyTheme(&g, th)
	rec = gfx.NewRecorder()
	Paint(rec, g)
	headerSize = gfx.ScalePx(8)
	statusSize = gfx.ScalePx(8)
	headerW = th.Complete().HeaderWeight()
	statusW = th.Complete().StatusWeight()
	headerY = chromeTextY(0, g.HeaderH, gfx.TextHeightWeight(headerSize, headerW), true)
	footerY = chromeTextY(h-g.FooterH, g.FooterH, gfx.TextHeightWeight(statusSize, statusW), false)
	assertChromeText(t, rec, "TITLE", 16, headerY, headerSize, th.Complete().Header, headerW)
	assertChromeText(t, rec, "STATUS", 8, footerY, statusSize, th.Complete().Status, statusW)
	if headerY+gfx.TextHeightWeight(headerSize, headerW) > g.HeaderH && headerY >= 0 {
		t.Fatalf("oversized header should overflow upward, y=%d height=%d", headerY, gfx.TextHeightWeight(headerSize, headerW))
	}
	if footerY < h-g.FooterH {
		t.Fatalf("oversized footer glyph y=%d is above footer top %d", footerY, h-g.FooterH)
	}
}

func TestPaintChromeTextUsesPixelRolesOverScale(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	g := New(w, h)
	g.Header = "TITLE"
	g.Footer = "STATUS"
	th := theme.Default()
	th.HeaderScale = 8
	th.StatusScale = 8
	th.TitleSize = 20
	th.StatusSize = 14
	ApplyTheme(&g, th)
	rec := gfx.NewRecorder()
	Paint(rec, g)
	assertChromeText(t, rec, "TITLE", 16, chromeTextY(0, g.HeaderH, gfx.TextHeightWeight(20, th.Complete().HeaderWeight()), true), 20, th.Complete().Header, th.Complete().HeaderWeight())
	assertChromeText(t, rec, "STATUS", 8, chromeTextY(h-g.FooterH, g.FooterH, gfx.TextHeightWeight(14, th.Complete().StatusWeight()), false), 14, th.Complete().Status, th.Complete().StatusWeight())
	if recSize(rec, "TITLE") == gfx.ScalePx(8) {
		t.Fatal("header still used header_scale instead of title_px")
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
	assertPanel(t, dst, cfg, g, 0, g.Tiles[0].Color)
}

func assertChromeText(t *testing.T, rec *gfx.Recorder, text string, x, y, sizePx int, col gfx.Color, w gfx.Weight) {
	t.Helper()
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == text {
			if c.X != x || c.Y != y || c.SizePx != sizePx {
				t.Fatalf("%q DrawText x,y,size=%d,%d,%d want %d,%d,%d", text, c.X, c.Y, c.SizePx, x, y, sizePx)
			}
			if c.Color != col {
				t.Fatalf("%q DrawText color %+v want %+v", text, c.Color, col)
			}
			if c.Weight != w {
				t.Fatalf("%q DrawText weight %s want %s", text, c.Weight, w)
			}
			return
		}
	}
	t.Fatalf("missing DrawText %q in %v", text, rec.Ops())
}

func assertNoDebugText(t *testing.T, rec *gfx.Recorder) {
	t.Helper()
	for _, c := range rec.Calls {
		if c.Op == "DebugText" {
			t.Fatalf("fbgrid chrome still used DebugText: %v", rec.Ops())
		}
	}
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

func TestPaintTileLabelsUseThemeColorAndTruncate(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	g := NewWithTiles(w, h, []Tile{
		{Name: "SONIC THE HEDGEHOG 2 EXTRA LONG TITLE", Color: gfx.RGB(44, 96, 156)},
	})
	th := theme.Arcade()
	ApplyTheme(&g, th)
	rec := gfx.NewRecorder()
	Paint(rec, g)
	assertNoDebugText(t, rec)
	var label gfx.Call
	found := false
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Color == th.Label && c.SizePx == th.BodyPx() {
			label = c
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing tile DrawText in %v", rec.Ops())
	}
	if label.Text == "SONIC THE HEDGEHOG 2 EXTRA LONG TITLE" || !strings.HasSuffix(label.Text, "...") {
		t.Fatalf("expected truncated tile label, got %q", label.Text)
	}
	maxW := g.CellW - 8
	if gfx.MeasureTextWeight(label.Text, label.SizePx, th.BodyWeight()) > maxW {
		t.Fatalf("label %q still wider than cell", label.Text)
	}
	if label.Weight != th.BodyWeight() {
		t.Fatalf("tile label weight %s want %s", label.Weight, th.BodyWeight())
	}
}

func TestPaintHeaderUsesBoldAndFooterStaysRegular(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	g := New(w, h)
	g.Header = "TITLE"
	g.Footer = "STATUS"
	th := theme.Default()
	ApplyTheme(&g, th)
	rec := gfx.NewRecorder()
	Paint(rec, g)
	assertChromeText(t, rec, "TITLE", 16, chromeTextY(0, g.HeaderH, gfx.TextHeightWeight(th.TitlePx(), th.HeaderWeight()), true), th.TitlePx(), th.Complete().Header, gfx.WeightBold)
	assertChromeText(t, rec, "STATUS", 8, chromeTextY(h-g.FooterH, g.FooterH, gfx.TextHeightWeight(th.StatusPx(), th.StatusWeight()), false), th.StatusPx(), th.Complete().Status, gfx.WeightRegular)

	regular := theme.Default()
	regular.TitleBold = false
	regular.HeaderBold = false
	ApplyTheme(&g, regular)
	regRec := gfx.NewRecorder()
	Paint(regRec, g)
	var header gfx.Call
	for _, c := range regRec.Calls {
		if c.Op == "DrawText" && c.Text == "TITLE" {
			header = c
			break
		}
	}
	if header.Weight != gfx.WeightRegular {
		t.Fatalf("title_bold false still painted %s", header.Weight)
	}

	ui, err := gfx.NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	ApplyTheme(&g, th)
	Paint(ui, g)
	boldSnap := ui.Snapshot()
	reg, err := gfx.NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	ApplyTheme(&g, regular)
	Paint(reg, g)
	if bytes.Equal(headerStrip(boldSnap, g.HeaderH), headerStrip(reg.Snapshot(), g.HeaderH)) {
		t.Fatal("bold header matched regular header pixels")
	}
}

func TestPaintUIFaceDiffersFromDebugTextAndTintsHeader(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	g := New(w, h)
	g.Header = "FOGCAST  MEGADRIVE 5/15"
	g.Footer = "A play | L/R shelf"
	th := theme.Arcade()
	ApplyTheme(&g, th)

	ui, err := gfx.NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	Paint(ui, g)
	uiSnap := ui.Snapshot()

	debug, err := gfx.NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	debug.BeginFrame()
	debug.Clear(th.Background)
	debug.FillRect(gfx.Rect{X: 0, Y: 0, W: float32(w), H: float32(g.HeaderH)}, th.HeaderBar)
	debug.DebugText(16, chromeTextY(0, g.HeaderH, gfx.ScalePx(th.HeaderScale), true), g.Header, th.HeaderScale)
	debugSnap := debug.Snapshot()
	if bytes.Equal(headerStrip(uiSnap, g.HeaderH), headerStrip(debugSnap, g.HeaderH)) {
		t.Fatal("header still matches DebugText 8x8 HUD")
	}
	if !headerHasNear(uiSnap, g.HeaderH, color.RGBA{th.Header.R, th.Header.G, th.Header.B, 255}) {
		t.Fatal("arcade header missing themed glyph ink")
	}
}

func headerStrip(img *image.RGBA, headerH int) []byte {
	if headerH < 1 {
		headerH = 1
	}
	w := img.Bounds().Dx()
	out := make([]byte, w*headerH*4)
	for y := 0; y < headerH; y++ {
		off := img.PixOffset(0, y)
		copy(out[y*w*4:(y+1)*w*4], img.Pix[off:off+w*4])
	}
	return out
}

func headerHasNear(img *image.RGBA, headerH int, want color.RGBA) bool {
	for y := 0; y < headerH && y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			p := img.RGBAAt(x, y)
			dr := absInt(int(p.R) - int(want.R))
			dg := absInt(int(p.G) - int(want.G))
			db := absInt(int(p.B) - int(want.B))
			if dr <= 40 && dg <= 40 && db <= 40 && p.A > 128 {
				return true
			}
		}
	}
	return false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestPaintPlaceholderDiffersFromFlatFillAndLoading(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	fallback := gfx.RGB(44, 96, 156)
	g := NewWithTiles(w, h, []Tile{
		{Name: "SONIC", Color: fallback, CoverKind: CoverMissing},
		{Name: "WAIT", Color: fallback, CoverKind: CoverLoading},
	})
	Paint(d, g)
	d.Present()
	th := g.Theme.Complete()
	missing := placeholderPanel(fallback, th, false)
	loading := placeholderPanel(fallback, th, true)
	if missing == fallback || loading == fallback || missing == loading {
		t.Fatalf("placeholder panels must differ from fill and each other: miss=%+v load=%+v fill=%+v", missing, loading, fallback)
	}
	assertPanel(t, dst, cfg, g, 0, fallback)
	g.Focus = 1
	Paint(d, g)
	d.Present()
	assertPanel(t, dst, cfg, g, 1, fallback)
	mx, my, _ := g.PanelSample(0)
	lx, ly, _ := g.PanelSample(1)
	assertBGRX(t, dst, cfg, mx, my, missing.B, missing.G, missing.R, 0)
	assertBGRX(t, dst, cfg, lx, ly, loading.B, loading.G, loading.R, 0)

	rec := gfx.NewRecorder()
	g.Focus = 0
	Paint(rec, g)
	sawLetter := false
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "S" {
			sawLetter = true
			break
		}
	}
	if !sawLetter {
		t.Fatalf("missing-art placeholder omitted lettermark in %v", rec.Ops())
	}
	if recSize(rec, "S") != th.CaptionPx() {
		t.Fatalf("lettermark size %d want caption %d", recSize(rec, "S"), th.CaptionPx())
	}
}

func recSize(rec *gfx.Recorder, text string) int {
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == text {
			return c.SizePx
		}
	}
	return 0
}

func TestPaintCoverLetterboxUsesThemeMix(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	cover := image.NewRGBA(image.Rect(0, 0, 8, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 0, B: 200, A: 255})
		}
	}
	fallback := gfx.RGB(44, 96, 156)
	g := NewWithTiles(w, h, []Tile{
		{Name: "ART", Color: fallback, Cover: cover, CoverKind: CoverPresent},
	})
	Paint(d, g)
	d.Present()
	th := g.Theme.Complete()
	box := letterboxFill(fallback, th)
	px, py, ok := g.PanelSample(0)
	if !ok {
		t.Fatal("letterbox sample")
	}
	assertBGRX(t, dst, cfg, px, py, box.B, box.G, box.R, 0)
	ix, iy, ok := g.InteriorSample()
	if !ok {
		t.Fatal("cover interior")
	}
	assertBGRX(t, dst, cfg, ix, iy, 200, 0, 255, 0)
	if gfxEqualBGRX(dst, cfg, px, py, fallback.B, fallback.G, fallback.R, 0) {
		t.Fatal("letterbox stayed the raw system fill")
	}
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
}

func TestPlaceholderLetterUsesFirstAlnum(t *testing.T) {
	t.Parallel()
	if got := placeholderLetter("sonic"); got != "S" {
		t.Fatalf("letter %q", got)
	}
	if got := placeholderLetter("  2fast"); got != "2" {
		t.Fatalf("digit %q", got)
	}
	if got := placeholderLetter("..."); got != "" {
		t.Fatalf("punct %q", got)
	}
}

func assertPanel(t *testing.T, dst []byte, cfg gfx.FBConfig, g Grid, i int, fill gfx.Color) {
	t.Helper()
	th := g.Theme.Complete()
	x, y, ok := g.PanelSample(i)
	if !ok {
		t.Fatal("panel sample")
	}
	loading := i >= 0 && i < len(g.Tiles) && g.Tiles[i].CoverKind == CoverLoading
	p := placeholderPanel(fill, th, loading)
	assertBGRX(t, dst, cfg, x, y, p.B, p.G, p.R, 0)
}

func gfxEqualBGRX(dst []byte, cfg gfx.FBConfig, x, y int, b, g, r, xx byte) bool {
	gotB, gotG, gotR, gotX, err := gfx.SampleBGRX(dst, cfg, x, y)
	if err != nil {
		return false
	}
	return gotB == b && gotG == g && gotR == r && gotX == xx
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
