package fbgrid

import (
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot/theme"
)

func TestPaintFocusPrefersBoxThenFauxCoverAndHidesMissing(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	cover := solidRGBA(16, 20, color.RGBA{R: 255, G: 32, B: 160, A: 255})
	box := solidRGBA(20, 24, color.RGBA{R: 16, G: 200, B: 48, A: 255})
	fill := theme.Default().SystemColor("megadrive")
	g := NewWithTiles(w, h, []Tile{
		{Name: "SONIC", Color: fill, Cover: cover, CoverKind: CoverPresent, Box: box},
		{Name: "STREETS", Color: fill, Cover: cover, CoverKind: CoverPresent, Box: box},
		{Name: "PONG", Color: fill, CoverKind: CoverMissing},
	})
	Paint(d, g)
	d.Present()
	ix, iy, ok := g.InteriorSample()
	if !ok {
		t.Fatal("interior")
	}
	assertBGRX(t, dst, cfg, ix, iy, 48, 200, 16, 0)
	ox, oy, ok := g.CellOrigin(1)
	if !ok {
		t.Fatal("unfocused")
	}
	assertBGRX(t, dst, cfg, ox+g.CellW/2, oy+g.CellH/2, 160, 32, 255, 0)

	g.Focus = 2
	Paint(d, g)
	d.Present()
	mx, my, ok := g.InteriorSample()
	if !ok {
		t.Fatal("missing")
	}
	if gfxEqualBGRX(dst, cfg, mx, my, 48, 200, 16, 0) || gfxEqualBGRX(dst, cfg, mx, my, 160, 32, 255, 0) {
		t.Fatal("missing art invented box pixels")
	}

	g.Focus = 1
	g.Tiles[1].Box = nil
	Paint(d, g)
	d.Present()
	fx, fy, ok := g.InteriorSample()
	if !ok {
		t.Fatal("faux")
	}
	assertBGRX(t, dst, cfg, fx, fy, 160, 32, 255, 0)
}

func TestPaintArcadeCabinetAroundFocus(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	th := theme.Arcade().Complete()
	cover := solidRGBA(16, 20, color.RGBA{R: 255, G: 32, B: 160, A: 255})
	g := NewWithTiles(w, h, []Tile{{Name: "SONIC", Color: th.SystemColor("megadrive"), Cover: cover, CoverKind: CoverPresent}})
	g.Theme = th
	Paint(d, g)
	d.Present()
	cell, ok := g.tileRect(0)
	if !ok {
		t.Fatal("cell")
	}
	x, y, ok := CabinetSample(cell, th)
	if !ok {
		t.Fatal("cabinet")
	}
	assertBGRX(t, dst, cfg, x, y, th.CoverFrame.B, th.CoverFrame.G, th.CoverFrame.R, 0)
	if _, _, on := CabinetSample(cell, theme.Default()); on {
		t.Fatal("classic cabinet")
	}
}
