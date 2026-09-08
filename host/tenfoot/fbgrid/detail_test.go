package fbgrid

import (
	"image"
	"image/color"
	"testing"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestPaintDetailDrawsTitleCoverAndHint(t *testing.T) {
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
	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST  SNES 1/1",
		Title: "Mario", Meta: "SNES  ·  1985  ·  Platform",
		Hint: "A play | B back", Cover: cover, CoverKind: CoverPresent,
		Color: th.SystemColor("snes"), Theme: th,
	})
	d.Present()
	cx, cy, ok := DetailCoverSample(w, h, th)
	if !ok {
		t.Fatal("cover sample")
	}
	assertBGRX(t, dst, cfg, cx, cy, 160, 32, 255, 0)

	rec := gfx.NewRecorder()
	PaintDetail(rec, DetailFrame{
		Width: w, Height: h, Header: "FOGCAST", Title: "Mario",
		Meta: "SNES  ·  1985", Hint: "A play | B back", Theme: th,
	})
	var sawTitle bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "Mario" && c.SizePx == th.TitlePx() {
			sawTitle = true
		}
		if c.Op == "DebugText" {
			t.Fatalf("detail used DebugText: %+v", rec.Ops())
		}
	}
	if !sawTitle {
		t.Fatalf("missing title DrawText ops=%v", rec.Ops())
	}

	PaintDetail(d, DetailFrame{
		Width: w, Height: h, Title: "Missing", CoverKind: CoverMissing,
		Color: th.SystemColor("snes"), Theme: th,
	})
	d.Present()
	px, py, ok := DetailCoverSample(w, h, th)
	if !ok {
		t.Fatal("placeholder sample")
	}
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(dst, cfg, px, py)
	if err != nil {
		t.Fatal(err)
	}
	sys := th.SystemColor("snes")
	if gotB == sys.B && gotG == sys.G && gotR == sys.R {
		t.Fatal("missing cover stayed a flat system fill")
	}
	panel := PlaceholderPanel(sys, th, false)
	if gotB != panel.B || gotG != panel.G || gotR != panel.R {
		t.Fatalf("placeholder bgrx %d,%d,%d want %d,%d,%d", gotB, gotG, gotR, panel.B, panel.G, panel.R)
	}
}
