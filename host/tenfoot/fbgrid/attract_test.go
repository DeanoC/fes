package fbgrid

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestPaintAttractStillAndEmptyPanel(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	th := theme.Default()
	still := image.NewRGBA(image.Rect(0, 0, 40, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 40; x++ {
			still.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	PaintAttract(d, AttractFrame{
		Width: w, Height: h, Title: "Mario", Image: still, Theme: th,
	})
	d.Present()
	dest := AttractStillDest(w, h, still, th)
	sx := int(dest.X + dest.W/2)
	sy := int(dest.Y + dest.H/2)
	assertBGRX(t, dst, cfg, sx, sy, 160, 32, 255, 0)
	lx, ly, ok := AttractLetterboxSample(w, h, th)
	if !ok {
		t.Fatal("letterbox sample")
	}
	assertBGRX(t, dst, cfg, lx, ly, th.AttractBackground.B, th.AttractBackground.G, th.AttractBackground.R, 0)

	PaintAttract(d, AttractFrame{
		Width: w, Height: h, Empty: true, Theme: th,
	})
	d.Present()
	cx, cy, ok := AttractStageSample(w, h, th)
	if !ok {
		t.Fatal("stage sample")
	}
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(dst, cfg, cx, cy)
	if err != nil {
		t.Fatal(err)
	}
	if gotB == 160 && gotG == 32 && gotR == 255 {
		t.Fatal("empty panel kept still pixels")
	}

	next := image.NewRGBA(image.Rect(0, 0, 40, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 40; x++ {
			next.Set(x, y, color.RGBA{R: 40, G: 80, B: 200, A: 255})
		}
	}
	PaintAttract(d, AttractFrame{
		Width: w, Height: h, Title: "Sonic", Image: still, Next: next, FadeT: 0.5, Theme: th,
	})
	d.Present()
	midB, midG, midR, _, err := gfx.SampleBGRX(dst, cfg, sx, sy)
	if err != nil {
		t.Fatal(err)
	}
	if (midB == 160 && midG == 32 && midR == 255) || (midB == 200 && midG == 80 && midR == 40) {
		t.Fatalf("mid-fade still instant bgrx=%d,%d,%d", midB, midG, midR)
	}
}

func TestPaintAttractUsesThemeBackground(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	arcade := theme.Arcade()
	PaintAttract(d, AttractFrame{Width: w, Height: h, Empty: true, Theme: arcade})
	d.Present()
	lx, ly, ok := AttractLetterboxSample(w, h, arcade)
	if !ok {
		t.Fatal("sample")
	}
	assertBGRX(t, dst, cfg, lx, ly, arcade.AttractBackground.B, arcade.AttractBackground.G, arcade.AttractBackground.R, 0)
	if arcade.AttractBackground == theme.Default().AttractBackground {
		t.Fatal("arcade attract background matches default")
	}
}

func TestPaintAttractVideoBadgeAndStillsFallback(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	still := image.NewRGBA(image.Rect(0, 0, 40, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 40; x++ {
			still.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	frame := AttractFrame{
		Width: w, Height: h, Title: "Mario", Image: still,
		VideoBadge: true, Caption: "preview 1 / 2", Theme: th,
	}
	rec := gfx.NewRecorder()
	PaintAttract(rec, frame)
	var sawVideo, sawPreview bool
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if c.Text == "VIDEO" {
			sawVideo = true
		}
		if strings.Contains(c.Text, "preview") {
			sawPreview = true
		}
	}
	if !sawVideo || !sawPreview {
		t.Fatalf("motion paint video=%v preview=%v ops=%v", sawVideo, sawPreview, rec.Ops())
	}

	stillRec := gfx.NewRecorder()
	PaintAttract(stillRec, AttractFrame{Width: w, Height: h, Title: "Mario", Image: still, Theme: th})
	for _, c := range stillRec.Calls {
		if c.Op == "DrawText" && (c.Text == "VIDEO" || strings.Contains(c.Text, "preview")) {
			t.Fatalf("stills-only painted %q", c.Text)
		}
	}

	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	PaintAttract(d, frame)
	d.Present()
	bx, by, ok := AttractVideoBadgeSample(w, h, still, th)
	if !ok {
		t.Fatal("badge sample")
	}
	assertBGRX(t, dst, cfg, bx, by, 0, 220, 255, 0)
}

func TestPaintAttractWallHighlightsCurrentAndHidesNeighborMotion(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	mario := image.NewRGBA(image.Rect(0, 0, 8, 8))
	sonic := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			mario.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
			sonic.Set(x, y, color.RGBA{R: 40, G: 80, B: 200, A: 255})
		}
	}
	frame := AttractFrame{
		Width: w, Height: h, Title: "Mario", VideoBadge: true, Caption: "preview",
		Wall: []AttractWallTile{
			{Image: mario, Video: true},
			{Image: sonic},
			{Image: sonic},
			{Image: sonic},
		},
		Theme: th,
	}
	rec := gfx.NewRecorder()
	PaintAttract(rec, frame)
	var sawVideo bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "VIDEO" {
			sawVideo = true
		}
	}
	if !sawVideo {
		t.Fatalf("wall missing VIDEO ops=%v", rec.Ops())
	}

	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	PaintAttract(d, frame)
	d.Present()
	bx, by, ok := AttractWallBadgeSample(w, h, th)
	if !ok {
		t.Fatal("wall badge sample")
	}
	assertBGRX(t, dst, cfg, bx, by, 0, 220, 255, 0)
	cell := AttractWallCell(w, h, 1, th)
	sx := int(cell.X + cell.W/2)
	sy := int(cell.Y + cell.H/2)
	assertBGRX(t, dst, cfg, sx, sy, 200, 80, 40, 0)
}
