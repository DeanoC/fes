package fbgrid

import (
	"image"
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestCoverflowFocusRectLargerThanNeighbors(t *testing.T) {
	t.Parallel()
	rects := CoverflowRects(640, 480, 36, 28, 16, 0, 24, 5, 2)
	if len(rects) != 5 {
		t.Fatalf("rects %d", len(rects))
	}
	focus := rects[2]
	if focus.W <= rects[1].W || focus.W <= rects[3].W || focus.H <= rects[0].H {
		t.Fatalf("focus %+v neighbors %+v %+v %+v", focus, rects[0], rects[1], rects[3])
	}
	if focus.Y >= rects[0].Y || focus.Y >= rects[4].Y {
		t.Fatalf("focus should sit higher y=%v far=%v %v", focus.Y, rects[0].Y, rects[4].Y)
	}
	if CoverflowRects(640, 480, 36, 28, 16, 0, 24, 0, 0) != nil {
		t.Fatal("empty window")
	}
}

func TestPaintCoverflowHighlightAndEmpty(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	th := theme.Default().Complete()
	g := NewWithTiles(w, h, FakeTiles()[:5])
	g.Kind = BrowseCoverflow
	g.Focus = 2
	ApplyTheme(&g, th)
	g.Header = "FOGCAST  FLOW"
	g.Footer = "A play | B platforms | L/R | Y wall"
	Paint(d, g)
	d.Present()
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("coverflow highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
	fr, ok := g.coverflowRect(g.Focus)
	if !ok {
		t.Fatal("focus rect")
	}
	nr, ok := g.coverflowRect(0)
	if !ok {
		t.Fatal("neighbor rect")
	}
	if fr.W <= nr.W {
		t.Fatalf("painted focus %+v neighbor %+v", fr, nr)
	}
	empty := NewWithTiles(w, h, nil)
	empty.Kind = BrowseCoverflow
	ApplyTheme(&empty, th)
	Paint(d, empty)
	d.Present()
	if _, _, ok := empty.HighlightSample(); ok {
		t.Fatal("empty coverflow highlight")
	}
}

func TestPaintWallDenserThanGrid(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	tiles := make([]Tile, 18)
	for i := range tiles {
		tiles[i] = Tile{Name: "T", Color: gfx.RGB(40, 90, 200)}
	}
	grid := NewWithTiles(w, h, tiles[:12])
	ApplyTheme(&grid, theme.Default())
	wall := NewWithTiles(w, h, tiles)
	wall.Kind = BrowseWall
	ApplyTheme(&wall, theme.Default())
	if wall.Columns != 6 {
		t.Fatalf("wall cols %d", wall.Columns)
	}
	if wall.CellW >= grid.CellW {
		t.Fatalf("wall cell %d not denser than grid %d", wall.CellW, grid.CellW)
	}
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	wall.Focus = 1
	Paint(d, wall)
	d.Present()
	hx, hy, ok := wall.HighlightSample()
	if !ok {
		t.Fatal("wall highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
}

func TestCoverflowTitleUsesThemeTitleRole(t *testing.T) {
	t.Parallel()
	g := NewWithTiles(640, 480, []Tile{{Name: "SONIC 2", Color: gfx.RGB(40, 90, 200)}})
	g.Kind = BrowseCoverflow
	ApplyTheme(&g, theme.Default())
	rec := gfx.NewRecorder()
	Paint(rec, g)
	th := theme.Default().Complete()
	var sawTitle bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "SONIC 2" && c.SizePx == th.TitlePx() && c.Weight == th.TitleWeight() {
			sawTitle = true
		}
		if c.Op == "DebugText" {
			t.Fatalf("debug text %q", c.Text)
		}
	}
	if !sawTitle {
		t.Fatalf("missing title role ops=%v", rec.Ops())
	}
}

func TestCoverflowLogoReplacesTitleText(t *testing.T) {
	t.Parallel()
	logo := image.NewRGBA(image.Rect(0, 0, 24, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 24; x++ {
			logo.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	g := NewWithTiles(640, 480, []Tile{{Name: "SONIC 2", Color: gfx.RGB(40, 90, 200), Logo: logo}})
	g.Kind = BrowseCoverflow
	g.Header = "FOGCAST  FLOW"
	g.Footer = "A play | B platforms | L/R | Y wall"
	ApplyTheme(&g, theme.Default())
	rec := gfx.NewRecorder()
	Paint(rec, g)
	th := theme.Default().Complete()
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "SONIC 2" && c.SizePx == th.TitlePx() {
			t.Fatalf("logo still drew title %v", rec.Ops())
		}
	}
}
