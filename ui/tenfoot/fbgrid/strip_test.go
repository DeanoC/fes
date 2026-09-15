package fbgrid

import (
	"image"
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot/theme"
)

func TestLayoutUnchangedWhenStripEmpty(t *testing.T) {
	t.Parallel()
	plain := New(640, 480)
	g := New(640, 480)
	g.SetStrip(nil, "Recent", 0, true)
	if g.CellW != plain.CellW || g.CellH != plain.CellH {
		t.Fatalf("empty strip relayout cell=%dx%d want %dx%d", g.CellW, g.CellH, plain.CellW, plain.CellH)
	}
	if g.StripActive || g.stripReserve() != 0 {
		t.Fatalf("empty strip active=%v reserve=%d", g.StripActive, g.stripReserve())
	}
	if _, _, ok := g.StripOrigin(0); ok {
		t.Fatal("empty strip origin")
	}
}

func TestLayoutShrinksForStripAndHidesWhenCleared(t *testing.T) {
	t.Parallel()
	plain := New(640, 480)
	g := New(640, 480)
	g.SetStrip([]Tile{{Name: "SONIC", Color: gfx.RGB(40, 90, 160)}}, "Recent", 0, false)
	if g.CellH >= plain.CellH {
		t.Fatalf("strip did not shrink grid cell %d vs %d", g.CellH, plain.CellH)
	}
	if g.CellH < 60 {
		t.Fatalf("strip crushed grid cell %d", g.CellH)
	}
	ox, oy, ok := g.StripOrigin(0)
	if !ok || ox != g.Pad {
		t.Fatalf("strip origin %d,%d ok=%v", ox, oy, ok)
	}
	_, gy, _ := g.CellOrigin(8)
	if oy <= gy+g.CellH {
		t.Fatalf("strip overlaps grid gy=%d gh=%d stripy=%d", gy, g.CellH, oy)
	}
	if oy+g.StripCellH > g.Height-g.FooterH {
		t.Fatalf("strip overlaps footer y=%d h=%d footer=%d", oy, g.StripCellH, g.FooterH)
	}
	g.SetStrip(nil, "", 0, true)
	if g.CellH != plain.CellH || g.stripReserve() != 0 {
		t.Fatalf("cleared strip cell=%d reserve=%d want %d", g.CellH, g.stripReserve(), plain.CellH)
	}
}

func TestPaintStripPresenceAbsenceAndFocusHandoff(t *testing.T) {
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
	cover := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			cover.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	g := NewWithTiles(w, h, []Tile{
		{Name: "GRID", Color: th.SystemColor("snes")},
		{Name: "NEXT", Color: th.SystemColor("megadrive")},
	})
	Paint(d, g)
	d.Present()
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("grid highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)

	g.SetStrip([]Tile{
		{Name: "SONIC", Color: th.SystemColor("megadrive"), Cover: cover, CoverKind: CoverPresent},
		{Name: "MARIO", Color: th.SystemColor("snes")},
	}, "Recent", 0, true)
	Paint(d, g)
	d.Present()
	sx, sy, ok := g.StripHighlightSample()
	if !ok {
		t.Fatal("strip highlight")
	}
	assertBGRX(t, dst, cfg, sx, sy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
	if gfxEqualBGRX(dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0) {
		t.Fatal("grid kept highlight while strip focused")
	}
	ix, iy, ok := g.StripOrigin(0)
	if !ok {
		t.Fatal("strip tile")
	}
	assertBGRX(t, dst, cfg, ix+g.StripCellW/2, iy+g.StripCellH/3, 160, 32, 255, 0)

	rec := gfx.NewRecorder()
	Paint(rec, g)
	var sawLabel, sawCaption bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Recent" {
			sawLabel = true
			if c.SizePx != th.CaptionPx() {
				t.Fatalf("strip label size %d want %d", c.SizePx, th.CaptionPx())
			}
			if c.Weight != th.CaptionWeight() {
				t.Fatalf("strip label weight %s", c.Weight)
			}
		}
		if c.Op == "DrawText" && c.SizePx == th.CaptionPx() {
			sawCaption = true
		}
		if c.Op == "DebugText" {
			t.Fatal("strip DebugText")
		}
	}
	if !sawLabel || !sawCaption {
		t.Fatalf("strip text label=%v caption=%v ops=%v", sawLabel, sawCaption, rec.Ops())
	}

	g.SetStrip(nil, "Recent", 0, true)
	emptyRec := gfx.NewRecorder()
	Paint(emptyRec, g)
	for _, c := range emptyRec.Calls {
		if c.Op == "DrawText" && c.Text == "Recent" {
			t.Fatal("empty strip still painted Recent")
		}
	}
}
