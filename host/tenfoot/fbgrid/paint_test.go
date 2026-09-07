package fbgrid

import (
	"bytes"
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/linuxinput"
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
