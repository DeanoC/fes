package anim

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestCrossfadeFillUsesBlend(t *testing.T) {
	s, err := gfx.NewSoftware(4, 2)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(gfx.RGB(0, 0, 0))
	CrossfadeFill(s, gfx.Rect{X: 0, Y: 0, W: 4, H: 2}, StillA, StillB, 0)
	got := s.Snapshot().RGBAAt(1, 1)
	if got != (color.RGBA{StillA.R, StillA.G, StillA.B, 255}) {
		t.Fatalf("t0 %+v", got)
	}
	CrossfadeFill(s, gfx.Rect{X: 0, Y: 0, W: 4, H: 2}, StillA, StillB, 1)
	got = s.Snapshot().RGBAAt(1, 1)
	if got != (color.RGBA{StillB.R, StillB.G, StillB.B, 255}) {
		t.Fatalf("t1 %+v", got)
	}
	s.Clear(gfx.RGB(0, 0, 0))
	CrossfadeFill(s, gfx.Rect{X: 0, Y: 0, W: 4, H: 2}, StillA, StillB, 0.5)
	got = s.Snapshot().RGBAAt(1, 1)
	if got == (color.RGBA{StillA.R, StillA.G, StillA.B, 255}) || got == (color.RGBA{StillB.R, StillB.G, StillB.B, 255}) {
		t.Fatalf("mid should blend, got %+v", got)
	}
}

func TestSpriteDrawAtMoves(t *testing.T) {
	s, err := gfx.NewSoftware(32, 8)
	if err != nil {
		t.Fatal(err)
	}
	img := yellowRGBA(4)
	tex, err := s.CreateRGBA(img)
	if err != nil {
		t.Fatal(err)
	}
	sp := Sprite{
		Tex:  tex,
		From: gfx.Rect{X: 0, Y: 0, W: 4, H: 4},
		To:   gfx.Rect{X: 16, Y: 0, W: 4, H: 4},
	}
	s.Clear(gfx.RGB(0, 0, 0))
	sp.DrawAt(s, 0)
	if s.Snapshot().RGBAAt(1, 1) != (color.RGBA{SpriteColor.R, SpriteColor.G, SpriteColor.B, 255}) {
		t.Fatal("sprite at 0")
	}
	s.Clear(gfx.RGB(0, 0, 0))
	sp.DrawAt(s, 1)
	if s.Snapshot().RGBAAt(17, 1) != (color.RGBA{SpriteColor.R, SpriteColor.G, SpriteColor.B, 255}) {
		t.Fatal("sprite at 1")
	}
	if s.Snapshot().RGBAAt(1, 1) == (color.RGBA{SpriteColor.R, SpriteColor.G, SpriteColor.B, 255}) {
		t.Fatal("sprite should have left origin")
	}
}

func TestPaintStillCycleOnFPGA(t *testing.T) {
	f, err := gfx.NewFPGA(ProofWidth, ProofHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	PaintStillCycle(f, ProofWidth, ProofHeight, 0)
	f.Present()
	slot := CoverSlot(ProofWidth, ProofHeight)
	got := f.Snapshot().RGBAAt(int(slot.X+slot.W/2), int(slot.Y+slot.H/2))
	if got != (color.RGBA{StillA.R, StillA.G, StillA.B, 255}) {
		t.Fatalf("cover %+v", got)
	}
	raw, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	_, cmds, err := gfx.DecodeStream(raw)
	if err != nil {
		t.Fatal(err)
	}
	var begin, fill, draw, create bool
	for _, c := range cmds {
		switch c.Op {
		case gfx.OpBeginFrame:
			begin = true
		case gfx.OpFillRect:
			fill = true
		case gfx.OpDraw:
			draw = true
		case gfx.OpCreateTexture:
			create = true
		}
	}
	if !begin || !fill || !draw || !create {
		t.Fatalf("missing ops begin=%v fill=%v draw=%v create=%v n=%d", begin, fill, draw, create, len(cmds))
	}
}

func TestRunFPGAProof(t *testing.T) {
	report, err := RunFPGAProof()
	if err != nil {
		t.Fatalf("%v\n%s", err, report)
	}
	for _, need := range []string{
		"selftest-fpga PASS",
		"backend=fpga",
		"stub=true",
		"raster=software-replay",
		"HW=not-yet",
		"replay=ok",
		"timed, not a swap",
		"moved=true",
	} {
		if !strings.Contains(report, need) {
			t.Fatalf("missing %q in\n%s", need, report)
		}
	}
}

func TestFadeOverlayDarkens(t *testing.T) {
	s, err := gfx.NewSoftware(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.Clear(gfx.RGB(255, 255, 255))
	FadeOverlay(s, gfx.Rect{X: 0, Y: 0, W: 1, H: 1}, 0.5)
	got := s.Snapshot().RGBAAt(0, 0)
	if got.R >= 255 || got.G >= 255 {
		t.Fatalf("expected darken %+v", got)
	}
	if s.Snapshot().RGBAAt(1, 0) != (color.RGBA{255, 255, 255, 255}) {
		t.Fatal("overlay should clip to dst")
	}
}

func TestPaintStillCycleAdvancesCover(t *testing.T) {
	a, err := gfx.NewFPGA(ProofWidth, ProofHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	PaintStillCycle(a, ProofWidth, ProofHeight, 0)
	slot := CoverSlot(ProofWidth, ProofHeight)
	cx, cy := int(slot.X+slot.W/2), int(slot.Y+slot.H/2)
	if a.Snapshot().RGBAAt(cx, cy) != (color.RGBA{StillA.R, StillA.G, StillA.B, 255}) {
		t.Fatal("still A")
	}
	b, err := gfx.NewFPGA(ProofWidth, ProofHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	PaintStillCycle(b, ProofWidth, ProofHeight, StillHold+StillFade)
	if b.Snapshot().RGBAAt(cx, cy) != (color.RGBA{StillB.R, StillB.G, StillB.B, 255}) {
		t.Fatal("still B")
	}
	mid, err := gfx.NewFPGA(ProofWidth, ProofHeight)
	if err != nil {
		t.Fatal(err)
	}
	defer mid.Close()
	PaintStillCycle(mid, ProofWidth, ProofHeight, StillHold+StillFade/2)
	got := mid.Snapshot().RGBAAt(cx, cy)
	if got == a.Snapshot().RGBAAt(cx, cy) || got == b.Snapshot().RGBAAt(cx, cy) {
		t.Fatalf("fade should differ from both stills %+v", got)
	}
}

func yellowRGBA(n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.SetRGBA(x, y, color.RGBA{SpriteColor.R, SpriteColor.G, SpriteColor.B, 255})
		}
	}
	return img
}
