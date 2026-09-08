package fbgrid

import (
	"image"
	"image/color"
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
