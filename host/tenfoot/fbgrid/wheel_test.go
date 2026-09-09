package fbgrid

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestPaintWheelHeroPlaceholderAndFocus(t *testing.T) {
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
	frame := WheelFrame{
		Width: w, Height: h,
		Header: "FOGCAST  MEGADRIVE 3/6",
		Footer: "A open | L/R platform",
		Title:  "MEGADRIVE",
		Stats:  "3 games",
		Color:  th.SystemColor("megadrive"),
		Items: []WheelItem{
			{ID: "all", Label: "ALL", Color: th.SystemColor("")},
			{ID: "pong", Label: "PONG", Color: th.SystemColor("pong")},
			{ID: "megadrive", Label: "MEGADRIVE", Color: th.SystemColor("megadrive")},
			{ID: "snes", Label: "SNES", Color: th.SystemColor("snes")},
		},
		Focus: 2,
		Theme: th,
	}
	PaintWheel(d, frame)
	d.Present()
	hx, hy, ok := WheelFocusSample(w, h, th, len(frame.Items), frame.Focus)
	if !ok {
		t.Fatal("focus sample")
	}
	assertBGRX(t, dst, cfg, hx, hy, th.Highlight.B, th.Highlight.G, th.Highlight.R, 0)

	rec := gfx.NewRecorder()
	PaintWheel(rec, frame)
	var sawTitle, sawStats, sawBold bool
	for _, c := range rec.Calls {
		if c.Op == "DrawText" && c.Text == "MEGADRIVE" && c.SizePx == th.TitlePx() {
			sawTitle = true
			if c.Weight != th.TitleWeight() {
				t.Fatalf("title weight %s", c.Weight)
			}
			sawBold = true
		}
		if c.Op == "DrawText" && c.Text == "3 games" {
			sawStats = true
		}
		if c.Op == "DebugText" {
			t.Fatalf("wheel used DebugText")
		}
	}
	if !sawTitle || !sawStats || !sawBold {
		t.Fatalf("title=%v stats=%v bold=%v ops=%v", sawTitle, sawStats, sawBold, rec.Ops())
	}
}

func TestPaintWheelHeroArtAndLogo(t *testing.T) {
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
	hero := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			hero.Set(x, y, color.RGBA{R: 255, G: 32, B: 160, A: 255})
		}
	}
	logo := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 16; x++ {
			logo.Set(x, y, color.RGBA{R: 32, G: 220, B: 80, A: 255})
		}
	}
	PaintWheel(d, WheelFrame{
		Width: w, Height: h, Title: "MEGADRIVE", Stats: "3 games",
		Hero: hero, HeroKind: CoverPresent, Logo: logo,
		Color: th.SystemColor("megadrive"),
		Items: []WheelItem{{ID: "megadrive", Label: "MEGADRIVE", Logo: logo, Color: th.SystemColor("megadrive")}},
		Theme: th,
	})
	d.Present()
	hx, hy, ok := WheelHeroSample(w, h, th)
	if !ok {
		t.Fatal("hero sample")
	}
	assertBGRX(t, dst, cfg, hx, hy, 160, 32, 255, 0)
}

func TestWheelFocusPopIsMotionActive(t *testing.T) {
	t.Parallel()
	now := time.Unix(10, 0)
	f := WheelFrame{Now: now, PopAt: now}
	if !f.MotionActive() || f.focusScale() != 1 {
		t.Fatalf("start scale=%v active=%v", f.focusScale(), f.MotionActive())
	}
	f.Now = now.Add(FocusPopDuration / 2)
	if !f.MotionActive() || f.focusScale() <= 1 {
		t.Fatalf("mid scale=%v", f.focusScale())
	}
	f.Now = now.Add(FocusPopDuration)
	if f.MotionActive() || f.focusScale() != 1 {
		t.Fatalf("end scale=%v active=%v", f.focusScale(), f.MotionActive())
	}
}
