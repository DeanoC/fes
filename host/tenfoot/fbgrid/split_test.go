package fbgrid

import (
	"image"
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestSplitListRowsAreLeftOfHero(t *testing.T) {
	t.Parallel()
	rects := SplitListRects(640, 480, 36, 28, 16, 8, 0, 8)
	if len(rects) != 8 {
		t.Fatalf("rows %d", len(rects))
	}
	list, hero := SplitLayout(640, 480, 36, 28, 16, 8, 0)
	if list.W < 1 || hero.W < 1 || hero.X <= list.X {
		t.Fatalf("columns list=%+v hero=%+v", list, hero)
	}
	if rects[0].X != list.X || rects[0].W != list.W {
		t.Fatalf("row0 %+v list %+v", rects[0], list)
	}
	if rects[1].Y <= rects[0].Y {
		t.Fatalf("rows should stack y0=%v y1=%v", rects[0].Y, rects[1].Y)
	}
	if rects[0].H >= hero.H {
		t.Fatalf("list row %+v should be shorter than hero %+v", rects[0], hero)
	}
	if SplitListRects(640, 480, 36, 28, 16, 8, 0, 0) != nil {
		t.Fatal("empty list")
	}
}

func TestPaintSplitHighlightHeroAndEmpty(t *testing.T) {
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
	cover := image.NewRGBA(image.Rect(0, 0, 16, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 16; x++ {
			cover.SetRGBA(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	tiles := make([]Tile, 8)
	for i := range tiles {
		tiles[i] = Tile{Name: "T", Color: gfx.RGB(40, 90, 200), Meta: "MEGADRIVE | 1991"}
	}
	tiles[2].Name = "SONIC 2"
	tiles[2].Cover = cover
	tiles[2].CoverKind = CoverPresent
	g := NewWithTiles(w, h, tiles)
	g.Kind = BrowseSplit
	g.Focus = 2
	ApplyTheme(&g, th)
	g.Header = "FOGCAST  SPLIT"
	g.Footer = "A play | B platforms | L/R | Y grid"
	Paint(d, g)
	d.Present()
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("split highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
	cx, cy, ok := g.SplitHeroCoverSample()
	if !ok {
		t.Fatal("hero sample")
	}
	row, ok := g.splitListRect(g.Focus)
	if !ok {
		t.Fatal("focus row")
	}
	coverR, ok := g.SplitHeroCover()
	if !ok || coverR.W <= row.W || coverR.H <= row.H {
		t.Fatalf("hero %+v row %+v", coverR, row)
	}
	if cx < int(coverR.X) || cy < int(coverR.Y) {
		t.Fatalf("hero sample %d,%d cover %+v", cx, cy, coverR)
	}
	if _, _, ok := g.CellOrigin(0); !ok {
		t.Fatal("list origin")
	}
	empty := NewWithTiles(w, h, nil)
	empty.Kind = BrowseSplit
	ApplyTheme(&empty, th)
	Paint(d, empty)
	d.Present()
	if _, _, ok := empty.HighlightSample(); ok {
		t.Fatal("empty split highlight")
	}
	if _, ok := empty.SplitHeroCover(); !ok {
		t.Fatal("empty split still has a hero column")
	}
}

func TestSplitTitleUsesThemeTitleRoleAndMetaBody(t *testing.T) {
	t.Parallel()
	g := NewWithTiles(640, 480, []Tile{{
		Name:  "SONIC 2",
		Color: gfx.RGB(40, 90, 200),
		Meta:  "MEGADRIVE | 1991 | USA",
	}})
	g.Kind = BrowseSplit
	ApplyTheme(&g, theme.Default())
	rec := gfx.NewRecorder()
	Paint(rec, g)
	th := theme.Default().Complete()
	var sawTitle, sawMeta bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "SONIC 2" && c.SizePx == th.TitlePx() && c.Weight == th.TitleWeight() {
			sawTitle = true
		}
		if c.Op == "DrawText" && c.Text == "MEGADRIVE | 1991 | USA" && c.SizePx == th.BodyPx() && c.Weight == th.BodyWeight() {
			sawMeta = true
		}
		if c.Op == "DebugText" {
			t.Fatalf("debug text %q", c.Text)
		}
	}
	if !sawTitle || !sawMeta {
		t.Fatalf("title=%v meta=%v ops=%v", sawTitle, sawMeta, rec.Ops())
	}
}

func TestPaintSplitSeriesInHeroAndHideEmpty(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default().Complete()
	tiles := []Tile{{Name: "SONIC", Color: gfx.RGB(40, 90, 200), Meta: "MEGADRIVE | 1991"}}
	g := NewWithTiles(w, h, tiles)
	g.Kind = BrowseSplit
	ApplyTheme(&g, th)
	g.Series = []Tile{
		{Name: "SONIC 2", Color: gfx.RGB(200, 40, 40)},
		{Name: "SONIC 3", Color: gfx.RGB(40, 180, 80)},
	}
	g.SeriesLabel = "Sonic the Hedgehog"
	g.SeriesFocus = 0
	g.SeriesActive = true
	rec := gfx.NewRecorder()
	Paint(rec, g)
	var sawLabel bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Sonic the Hedgehog" && c.SizePx == th.CaptionPx() {
			sawLabel = true
		}
		if c.Op == "DebugText" {
			t.Fatalf("split series DebugText")
		}
	}
	if !sawLabel {
		t.Fatalf("missing split series label ops=%v", rec.Ops())
	}
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	Paint(d, g)
	d.Present()
	sx, sy, ok := g.SplitSeriesHighlightSample()
	if !ok {
		t.Fatal("split series highlight")
	}
	assertBGRX(t, dst, cfg, sx, sy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
	plain := NewWithTiles(w, h, tiles)
	plain.Kind = BrowseSplit
	ApplyTheme(&plain, th)
	hideRec := gfx.NewRecorder()
	Paint(hideRec, plain)
	for _, c := range hideRec.Calls {
		if c.Op == "DrawText" && c.Text == "Sonic the Hedgehog" {
			t.Fatalf("empty split painted series")
		}
	}
}

func TestSplitLogoReplacesListTextKeepsHeroTitle(t *testing.T) {
	t.Parallel()
	logo := image.NewRGBA(image.Rect(0, 0, 24, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 24; x++ {
			logo.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	g := NewWithTiles(640, 480, []Tile{{
		Name:  "SONIC 2",
		Color: gfx.RGB(40, 90, 200),
		Logo:  logo,
		Meta:  "MEGADRIVE",
	}})
	g.Kind = BrowseSplit
	g.Header = "FOGCAST  SPLIT"
	g.Footer = "A play | B detail | L/R | Y grid"
	ApplyTheme(&g, theme.Default())
	rec := gfx.NewRecorder()
	Paint(rec, g)
	th := theme.Default().Complete()
	var heroTitle, listBody bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "SONIC 2" && c.SizePx == th.TitlePx() {
			heroTitle = true
		}
		if c.Op == "DrawText" && c.Text == "SONIC 2" && c.SizePx == th.BodyPx() {
			listBody = true
		}
	}
	if !heroTitle {
		t.Fatalf("hero lost title ops=%v", rec.Ops())
	}
	if listBody {
		t.Fatalf("logo list still drew body title ops=%v", rec.Ops())
	}
}

func TestSplitOmitsEmptyMeta(t *testing.T) {
	t.Parallel()
	g := NewWithTiles(640, 480, []Tile{{Name: "PONG", Color: gfx.RGB(40, 90, 200)}})
	g.Kind = BrowseSplit
	ApplyTheme(&g, theme.Default())
	rec := gfx.NewRecorder()
	Paint(rec, g)
	th := theme.Default().Complete()
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.SizePx == th.BodyPx() && c.Text != "PONG" && c.Text != g.Header && c.Text != g.Footer {
			t.Fatalf("unexpected body copy %q ops=%v", c.Text, rec.Ops())
		}
	}
}
