package fbgrid

import (
	"image/color"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot/theme"
)

func TestVignetteDarkensStageEdgeAndKeepsFocus(t *testing.T) {
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
	vx, vy, ok := VignetteSample(w, h, g.HeaderH, g.FooterH, th)
	if !ok {
		t.Fatal("vignette sample")
	}
	a := VignetteAlphaAt(vx, vy, w, h, g.HeaderH, g.FooterH, th)
	if a == 0 {
		t.Fatal("vignette alpha at edge sample")
	}
	dim := VignetteDim(th.Background, th.Vignette, a)
	assertBGRX(t, dst, cfg, vx, vy, dim.B, dim.G, dim.R, 0)
	if gfxEqualBGRX(dst, cfg, vx, vy, th.Background.B, th.Background.G, th.Background.R, 0) {
		t.Fatal("stage edge stayed the solid theme background")
	}
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
	assertBGRX(t, dst, cfg, 2, 2, th.HeaderBar.B, th.HeaderBar.G, th.HeaderBar.R, 0)
	sx, sy, ok := AtmosphereSample(g)
	if !ok {
		t.Fatal("stage pad")
	}
	assertBGRX(t, dst, cfg, sx, sy, th.Background.B, th.Background.G, th.Background.R, 0)
}

func TestVignetteAlphaZeroSkipsEdgeDarken(t *testing.T) {
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
	th.VignetteA = 0
	th = th.Complete()
	g := New(w, h)
	ApplyTheme(&g, th)
	Paint(d, g)
	d.Present()
	if _, _, ok := VignetteSample(w, h, g.HeaderH, g.FooterH, th); ok {
		t.Fatal("disabled vignette still sampled")
	}
	sx, sy, ok := AtmosphereSample(g)
	if !ok {
		t.Fatal("stage")
	}
	assertBGRX(t, dst, cfg, sx, sy, th.Background.B, th.Background.G, th.Background.R, 0)
}

func TestBezelPaintsThemeFrameOnArcadeAndNight(t *testing.T) {
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
	ApplyTheme(&g, theme.Default())
	Paint(d, g)
	d.Present()
	if _, _, ok := BezelSample(w, h, theme.Default()); ok {
		t.Fatal("classic should omit bezel")
	}
	assertBGRX(t, dst, cfg, 0, 0, theme.Default().HeaderBar.B, theme.Default().HeaderBar.G, theme.Default().HeaderBar.R, 0)

	ApplyTheme(&g, theme.Arcade())
	Paint(d, g)
	d.Present()
	bx, by, ok := BezelSample(w, h, theme.Arcade())
	if !ok {
		t.Fatal("arcade bezel")
	}
	bezel := theme.Arcade().Bezel
	assertBGRX(t, dst, cfg, bx, by, bezel.B, bezel.G, bezel.R, 0)
	assertBGRX(t, dst, cfg, 8, 2, theme.Arcade().HeaderBar.B, theme.Arcade().HeaderBar.G, theme.Arcade().HeaderBar.R, 0)

	ApplyTheme(&g, theme.Night())
	Paint(d, g)
	d.Present()
	night := theme.Night().Bezel
	assertBGRX(t, dst, cfg, 0, 0, night.B, night.G, night.R, 0)
}

func TestSessionChromeDimsBrowseAndKeepsLayout(t *testing.T) {
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
	ix, iy, ok := g.InteriorSample()
	if !ok {
		t.Fatal("interior")
	}
	plain := d.Snapshot().RGBAAt(ix, iy)
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)

	g.Session = SessionChrome{State: "active", Title: "Sonic 2", Hint: SessionKitHint}
	rec := gfx.NewRecorder()
	Paint(rec, g)
	var sawPaused, sawTitle, sawHint bool
	for _, c := range rec.Calls {
		if c.Op != "DrawText" {
			continue
		}
		if c.Text == "Paused" {
			sawPaused = true
		}
		if c.Text == "Sonic 2" {
			sawTitle = true
		}
		if c.Text == SessionKitHint {
			sawHint = true
		}
	}
	if !sawPaused || !sawTitle || !sawHint {
		t.Fatalf("session copy paused=%v title=%v hint=%v ops=%v", sawPaused, sawTitle, sawHint, rec.Ops())
	}

	Paint(d, g)
	d.Present()
	dim := SessionDim(colorToGFX(plain), th)
	assertBGRX(t, dst, cfg, ix, iy, dim.B, dim.G, dim.R, 0)
	if gfxEqualBGRX(dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0) {
		t.Fatal("active session left the focus ring undimmed")
	}
	px, py, ok := SessionBadgeSample(w, h, th)
	if !ok {
		t.Fatal("badge sample")
	}
	assertBGRX(t, dst, cfg, px, py, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)
	ox, oy, ok := g.CellOrigin(0)
	if !ok {
		t.Fatal("origin")
	}
	if ox != 16 || oy != th.HeaderH+th.Pad {
		t.Fatalf("layout moved origin=(%d,%d)", ox, oy)
	}
}

func TestSessionChromeIdleOmitsOverlay(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Session = SessionChrome{State: "idle", Title: "Sonic 2"}
	rec := gfx.NewRecorder()
	Paint(rec, g)
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && (c.Text == "Paused" || strings.Contains(c.Text, "Select+Start")) {
			t.Fatalf("idle painted session copy %q", c.Text)
		}
	}
}

func TestSessionChromeSplitAndDetailKeepCoverLayout(t *testing.T) {
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
	cover := solidRGBA(8, 8, color.RGBA{R: 255, G: 32, B: 160, A: 255})
	g := NewWithTiles(w, h, []Tile{
		{Name: "SONIC", Color: th.SystemColor("megadrive"), Cover: cover, CoverKind: CoverPresent, Meta: "Mega Drive  1992"},
		{Name: "STREETS", Color: th.SystemColor("megadrive"), Cover: cover, CoverKind: CoverPresent},
	})
	g.Kind = BrowseSplit
	ApplyTheme(&g, th)
	g.layout()
	Paint(d, g)
	d.Present()
	plain := d.Snapshot()
	cx, cy, ok := g.PanelSample(0)
	if !ok {
		t.Fatal("split panel")
	}

	g.Session = SessionChrome{State: "active", Title: "SONIC"}
	Paint(d, g)
	d.Present()
	before := plain.RGBAAt(cx, cy)
	after := d.Snapshot().RGBAAt(cx, cy)
	if after == before {
		t.Fatal("split session did not dim")
	}
	dim := SessionDim(colorToGFX(before), th)
	assertBGRX(t, dst, cfg, cx, cy, dim.B, dim.G, dim.R, 0)

	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Sonic",
		Hint: "A play | B back", Cover: cover, CoverKind: CoverPresent,
		Color: th.SystemColor("megadrive"), Theme: th,
		Session: SessionChrome{State: "failed", Title: "Sonic", Hint: SessionKitRetryHint},
	})
	d.Present()
	coverRect, _, _ := DetailLayout(w, h, th, false, 0, 0, 0, 0)
	dx, dy := int(coverRect.X)+4, int(coverRect.Y)+4
	if dx < 0 || dy < 0 {
		t.Fatal("detail cover origin")
	}
	box := letterboxFill(th.SystemColor("megadrive"), th)
	want := SessionDim(box, th)
	assertBGRX(t, dst, cfg, dx, dy, want.B, want.G, want.R, 0)
	rec := gfx.NewRecorder()
	PaintDetail(rec, DetailFrame{
		Width: w, Height: h, Title: "Sonic", Cover: cover, CoverKind: CoverPresent,
		Theme: th, Session: SessionChrome{State: "failed", Title: "Sonic"},
	})
	var sawRetry bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Retry Stop" {
			sawRetry = true
		}
	}
	if !sawRetry {
		t.Fatalf("failed session omitted badge ops=%v", rec.Ops())
	}
}

func TestSessionBadgeAndLive(t *testing.T) {
	t.Parallel()
	if SessionBadge("active") != "Paused" || SessionBadge("launching") != "Starting" {
		t.Fatal("active/launching")
	}
	if SessionBadge("stopping") != "Stopping" || SessionBadge("failed") != "Retry Stop" {
		t.Fatal("stop/fail")
	}
	if SessionLive(SessionChrome{State: "idle"}) || SessionLive(SessionChrome{}) {
		t.Fatal("idle should hide overlay")
	}
	if !SessionLive(SessionChrome{State: "active"}) {
		t.Fatal("active should show overlay")
	}
	r := SessionPanelRect(640, 480, theme.Default())
	if r.W < 160 || r.H < 48 {
		t.Fatalf("panel %+v", r)
	}
	if r.X < 16 || r.Y < 36 {
		t.Fatalf("panel should sit in the stage %+v", r)
	}
}

func colorToGFX(c color.RGBA) gfx.Color {
	return gfx.RGB(c.R, c.G, c.B)
}
