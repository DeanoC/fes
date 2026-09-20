package fbgrid

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/theme"
)

func TestComposeBadgesHidesEmptyAndKeepsAdmitted(t *testing.T) {
	t.Parallel()
	if got := ComposeBadges("", "", "", false); len(got) != 0 {
		t.Fatalf("empty %+v", got)
	}
	got := ComposeBadges("2", "4.5", "100%", true)
	if len(got) != 4 || got[0].Label != "2P" || got[1].Label != "4.5" || got[2].Label != "100%" || got[3].Label != "PORT" {
		t.Fatalf("full %+v", got)
	}
	if FormatPlayersBadge("1-2") != "1-2" || FormatPlayersBadge("1 player") != "1P" || FormatPlayersBadge("  ") != "" {
		t.Fatalf("players %q %q %q", FormatPlayersBadge("1-2"), FormatPlayersBadge("1 player"), FormatPlayersBadge("  "))
	}
	partial := ComposeBadges("1-2", "", "", false)
	if len(partial) != 1 || partial[0].Kind != BadgePlayers || partial[0].Label != "1-2" {
		t.Fatalf("partial %+v", partial)
	}
}

func TestPaintTileBadgesSampleAndHideWhenAbsent(t *testing.T) {
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
		{Name: "SONIC", Color: gfx.RGB(40, 90, 200), Cover: cover, CoverKind: CoverPresent, Badges: ComposeBadges("2", "4.5", "", false)},
		{Name: "PONG", Color: gfx.RGB(40, 90, 200), Cover: cover, CoverKind: CoverPresent},
	})
	ApplyTheme(&g, th)
	Paint(d, g)
	d.Present()
	bx, by, ok := TileBadgeSample(g, 0, 0)
	if !ok {
		t.Fatal("players badge sample")
	}
	assertBGRX(t, dst, cfg, bx, by, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
	ix, iy, ok := g.InteriorSample()
	if !ok {
		t.Fatal("interior")
	}
	assertBGRX(t, dst, cfg, ix, iy, 160, 32, 255, 0)
	if _, _, ok := TileBadgeSample(g, 1, 0); ok {
		t.Fatal("empty tile painted a badge")
	}

	rec := gfx.NewRecorder()
	Paint(rec, g)
	var sawPlayers, sawRating, sawPort bool
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		switch c.Text {
		case "2P":
			sawPlayers = true
			if c.SizePx != th.CaptionPx() {
				t.Fatalf("players size %d", c.SizePx)
			}
		case "4.5":
			sawRating = true
		case "PORT":
			sawPort = true
		}
	}
	if !sawPlayers || !sawRating || sawPort {
		t.Fatalf("grid chips players=%v rating=%v port=%v ops=%v", sawPlayers, sawRating, sawPort, rec.Ops())
	}
}

func TestPaintTileBadgesReadableOnThemePacks(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	for _, th := range []theme.Theme{theme.Default(), theme.Arcade(), theme.Night()} {
		th = th.Complete()
		dst := make([]byte, cfg.Height*cfg.Stride)
		d, err := gfx.NewLinuxFB(w, h, dst, cfg)
		if err != nil {
			t.Fatal(err)
		}
		g := NewWithTiles(w, h, []Tile{
			{Name: "SONIC", Color: th.SystemColor("megadrive"), Badges: ComposeBadges("1-2", "", "100%", true)},
		})
		ApplyTheme(&g, th)
		Paint(d, g)
		d.Present()
		bx, by, ok := TileBadgeSample(g, 0, 0)
		if !ok {
			d.Close()
			t.Fatalf("%s badge sample", th.Name)
		}
		assertBGRX(t, dst, cfg, bx, by, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
		d.Close()
	}
}

func TestPaintCoverflowAndWallBadgesStayCompact(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	tiles := []Tile{
		{Name: "ONE", Color: gfx.RGB(40, 90, 200), Badges: ComposeBadges("2", "4.5", "100%", true)},
		{Name: "TWO", Color: gfx.RGB(40, 90, 200), Badges: ComposeBadges("2", "", "", false)},
	}
	flow := NewWithTiles(w, h, tiles)
	flow.Kind = BrowseCoverflow
	ApplyTheme(&flow, theme.Default())
	rec := gfx.NewRecorder()
	Paint(rec, flow)
	var saw2P bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "2P" {
			saw2P = true
		}
	}
	if !saw2P {
		t.Fatalf("coverflow missing 2P ops=%v", rec.Ops())
	}
	if _, _, ok := TileBadgeSample(flow, 0, 0); !ok {
		t.Fatal("coverflow badge sample")
	}

	wall := NewWithTiles(w, h, tiles)
	wall.Kind = BrowseWall
	ApplyTheme(&wall, theme.Default())
	inner, ok := wall.tileInner(0)
	if !ok {
		inner, ok = wall.tileRect(0)
	}
	if !ok {
		t.Fatal("wall cell")
	}
	th := theme.Default()
	layouts := layoutBadges(inner, tiles[0].Badges, th, badgeBudget(inner, BrowseWall), tileLabelReserve(BrowseWall, th, false))
	if len(layouts) > badgeMaxWall {
		t.Fatalf("wall chips %d", len(layouts))
	}
	if len(layouts) < 1 {
		t.Fatal("wall hid admitted players")
	}
}

func TestPaintDetailBadgesUnderTitle(t *testing.T) {
	t.Parallel()
	const w, h = 640, 480
	th := theme.Default()
	frame := DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Meta: "MEGADRIVE", Hint: "A play | B back",
		Badges: ComposeBadges("1-2", "", "", true), Theme: th,
	}
	rec := gfx.NewRecorder()
	PaintDetail(rec, frame)
	var sawPlayers, sawPort bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "1-2" {
			sawPlayers = true
		}
		if c.Op == "DrawText" && c.Text == "PORT" {
			sawPort = true
		}
	}
	if !sawPlayers || !sawPort {
		t.Fatalf("detail chips players=%v port=%v ops=%v", sawPlayers, sawPort, rec.Ops())
	}
	empty := DetailFrame{Width: w, Height: h, Header: "FOGCAST", Title: "Pong", Hint: "A play | B back", Theme: th}
	emptyRec := gfx.NewRecorder()
	PaintDetail(emptyRec, empty)
	for _, c := range emptyRec.Calls {
		if c.Op == "DrawText" && (c.Text == "PORT" || c.Text == "2P" || c.Text == "1-2") {
			t.Fatalf("empty detail painted %q", c.Text)
		}
	}

	cfg := gfx.FBConfig{Width: w, Height: h, Stride: 2560, BPP: 32}
	dst := make([]byte, cfg.Height*cfg.Stride)
	d, err := gfx.NewLinuxFB(w, h, dst, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	PaintDetail(d, frame)
	d.Present()
	bx, by, ok := DetailBadgeSample(w, h, th, frame)
	if !ok {
		t.Fatal("detail badge sample")
	}
	assertBGRX(t, dst, cfg, bx, by, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
}

func TestBadgeBudgetSkipsTinyCells(t *testing.T) {
	t.Parallel()
	tiny := gfx.Rect{X: 0, Y: 0, W: 20, H: 16}
	if n := badgeBudget(tiny, BrowseGrid); n != 0 {
		t.Fatalf("tiny budget %d", n)
	}
	if layouts := layoutBadges(tiny, ComposeBadges("2", "", "", false), theme.Default(), 3, 0); len(layouts) != 0 {
		t.Fatalf("tiny layouts %+v", layouts)
	}
	if !strings.HasPrefix(clipBadgeLabel("completed"), "C") {
		t.Fatal("clip")
	}
}
