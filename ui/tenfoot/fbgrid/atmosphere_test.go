package fbgrid

import (
	"image"
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot/theme"
)

func solidRGBA(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestPaintAtmosphereFanartDimsStageAndKeepsFocus(t *testing.T) {
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
	art := gfx.RGB(40, 180, 80)
	g := New(w, h)
	ApplyTheme(&g, th)
	g.Atmosphere = solidRGBA(32, 16, color.RGBA{R: art.R, G: art.G, B: art.B, A: 255})
	Paint(d, g)
	d.Present()
	sx, sy, ok := AtmosphereSample(g)
	if !ok {
		t.Fatal("stage sample")
	}
	dim := AtmosphereDim(art, th.Background)
	assertBGRX(t, dst, cfg, sx, sy, dim.B, dim.G, dim.R, 0)
	if gfxEqualBGRX(dst, cfg, sx, sy, th.Background.B, th.Background.G, th.Background.R, 0) {
		t.Fatal("fanart stage stayed the solid theme background")
	}
	if gfxEqualBGRX(dst, cfg, sx, sy, art.B, art.G, art.R, 0) {
		t.Fatal("fanart stage was undimmed")
	}
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
	assertBGRX(t, dst, cfg, 2, 2, th.HeaderBar.B, th.HeaderBar.G, th.HeaderBar.R, 0)
}

func TestPaintAtmosphereAbsentKeepsSolidBackground(t *testing.T) {
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
	g := New(w, h)
	ApplyTheme(&g, th)
	Paint(d, g)
	d.Present()
	sx, sy, ok := AtmosphereSample(g)
	if !ok {
		t.Fatal("stage sample")
	}
	assertBGRX(t, dst, cfg, sx, sy, th.Background.B, th.Background.G, th.Background.R, 0)
}

func TestPaintAtmosphereCoverWallWhenFanartMissing(t *testing.T) {
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
	cover := gfx.RGB(200, 40, 40)
	g := NewWithTiles(w, h, []Tile{
		{Name: "A", Color: th.SystemColor("megadrive"), Cover: solidRGBA(8, 12, color.RGBA{R: cover.R, G: cover.G, B: cover.B, A: 255}), CoverKind: CoverPresent},
		{Name: "B", Color: th.SystemColor("snes"), Cover: solidRGBA(8, 12, color.RGBA{R: 32, G: 64, B: 200, A: 255}), CoverKind: CoverPresent},
	})
	ApplyTheme(&g, th)
	Paint(d, g)
	d.Present()
	sx, sy, ok := AtmosphereSample(g)
	if !ok {
		t.Fatal("stage sample")
	}
	if gfxEqualBGRX(dst, cfg, sx, sy, th.Background.B, th.Background.G, th.Background.R, 0) {
		t.Fatal("cover-wall stage stayed the solid theme background")
	}
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
}

func TestPaintWheelAtmosphereDimsStageAndKeepsHero(t *testing.T) {
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
	art := gfx.RGB(255, 32, 160)
	hero := solidRGBA(8, 8, color.RGBA{R: art.R, G: art.G, B: art.B, A: 255})
	PaintWheel(d, WheelFrame{
		Width: w, Height: h, Title: "MEGADRIVE", Stats: "3 games",
		Hero: hero, HeroKind: CoverPresent,
		Color: th.SystemColor("megadrive"),
		Items: []WheelItem{{ID: "megadrive", Label: "MEGADRIVE", Color: th.SystemColor("megadrive")}},
		Theme: th,
	})
	d.Present()
	hx, hy, ok := WheelHeroSample(w, h, th)
	if !ok {
		t.Fatal("hero sample")
	}
	assertBGRX(t, dst, cfg, hx, hy, art.B, art.G, art.R, 0)
	sx, sy, ok := WheelAtmosphereSample(w, h, th)
	if !ok {
		t.Fatal("stage sample")
	}
	dim := AtmosphereDim(art, th.Background)
	assertBGRX(t, dst, cfg, sx, sy, dim.B, dim.G, dim.R, 0)
}

func TestPaintDetailAtmosphereDimsStageAndKeepsCover(t *testing.T) {
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
	art := gfx.RGB(40, 180, 80)
	cover := gfx.RGB(255, 32, 160)
	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Hint:       "A play | B back",
		Cover:      solidRGBA(8, 8, color.RGBA{R: cover.R, G: cover.G, B: cover.B, A: 255}),
		CoverKind:  CoverPresent,
		Atmosphere: solidRGBA(32, 16, color.RGBA{R: art.R, G: art.G, B: art.B, A: 255}),
		Color:      th.SystemColor("megadrive"), Theme: th,
	})
	d.Present()
	cx, cy, ok := DetailCoverSample(w, h, th)
	if !ok {
		t.Fatal("cover sample")
	}
	assertBGRX(t, dst, cfg, cx, cy, cover.B, cover.G, cover.R, 0)
	sx, sy, ok := DetailAtmosphereSample(w, h, th)
	if !ok {
		t.Fatal("stage sample")
	}
	dim := AtmosphereDim(art, th.Background)
	assertBGRX(t, dst, cfg, sx, sy, dim.B, dim.G, dim.R, 0)
}

func TestPaintDetailWithoutArtKeepsSolidBackground(t *testing.T) {
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
	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Title: "Missing", CoverKind: CoverMissing,
		Color: th.SystemColor("snes"), Theme: th,
	})
	d.Present()
	sx, sy, ok := DetailAtmosphereSample(w, h, th)
	if !ok {
		t.Fatal("stage sample")
	}
	assertBGRX(t, dst, cfg, sx, sy, th.Background.B, th.Background.G, th.Background.R, 0)
}
