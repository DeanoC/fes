package anim

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

func TestParseStyle(t *testing.T) {
	t.Parallel()
	cases := map[string]Style{
		"":        StyleNone,
		"none":    StyleNone,
		"NONE":    StyleNone,
		"curtain": StyleCurtain,
		"Wipe":    StyleWipe,
		"glitch":  StyleGlitch,
		"static":  StyleGlitch,
		"sparkle": StyleNone,
	}
	for in, want := range cases {
		if got := ParseStyle(in); got != want {
			t.Fatalf("%q: got %s want %s", in, got, want)
		}
	}
	if StyleCurtain.String() != "curtain" || StyleNone.String() != "none" || StyleGlitch.String() != "glitch" {
		t.Fatal("stringer")
	}
}

func TestStyleDurationsStayUnderCap(t *testing.T) {
	t.Parallel()
	for _, s := range []Style{StyleNone, StyleCurtain, StyleWipe, StyleGlitch} {
		if s.Duration() > MaxDuration {
			t.Fatalf("%s duration %s > %s", s, s.Duration(), MaxDuration)
		}
		if s.Duration() > 500*time.Millisecond {
			t.Fatalf("%s duration %s blocks input", s, s.Duration())
		}
	}
	if StyleNone.Duration() != 0 {
		t.Fatal("none duration")
	}
	if Progress(StyleNone, 0) != 1 {
		t.Fatal("none progress")
	}
	if Progress(StyleCurtain, 0) != 0 {
		t.Fatal("curtain t0")
	}
	if Progress(StyleCurtain, CurtainDuration) != 1 {
		t.Fatal("curtain settled")
	}
	mid := Progress(StyleCurtain, CurtainDuration/2)
	if mid <= 0 || mid >= 1 {
		t.Fatalf("curtain mid %v", mid)
	}
}

func TestPaintTransitionNoneIsNoop(t *testing.T) {
	t.Parallel()
	d := softwareDevice(t, 64, 48)
	defer d.Close()
	fillScene(d, gfx.RGB(10, 20, 30))
	before := clonePix(d.Snapshot())
	PaintTransition(d, gfx.Rect{X: 0, Y: 0, W: 64, H: 48}, StyleNone, 0, Colors{})
	if !pixelsEqual(d.Snapshot(), pixImage(before, 64, 48)) {
		t.Fatal("none overlay mutated pixels")
	}
}

func TestPaintCurtainCoversThenReveals(t *testing.T) {
	t.Parallel()
	const w, h = 64, 48
	d := softwareDevice(t, w, h)
	defer d.Close()
	scene := gfx.RGB(40, 80, 120)
	fill := gfx.RGB(8, 8, 16)
	fillScene(d, scene)
	PaintTransition(d, gfx.Rect{X: 0, Y: 0, W: w, H: h}, StyleCurtain, 0, Colors{Fill: fill, Edge: gfx.RGB(255, 0, 0)})
	got := d.Snapshot().RGBAAt(w/4, h/2)
	if got == (color.RGBA{R: scene.R, G: scene.G, B: scene.B, A: 255}) {
		t.Fatal("t0 curtain left panel revealed")
	}
	if got != (color.RGBA{R: fill.R, G: fill.G, B: fill.B, A: 255}) {
		t.Fatalf("t0 panel %+v want fill", got)
	}
	fillScene(d, scene)
	PaintTransition(d, gfx.Rect{X: 0, Y: 0, W: w, H: h}, StyleCurtain, 1, Colors{Fill: fill})
	got = d.Snapshot().RGBAAt(w/2, h/2)
	if got != (color.RGBA{R: scene.R, G: scene.G, B: scene.B, A: 255}) {
		t.Fatalf("settled center %+v", got)
	}
}

func TestPaintWipeRevealsLeftFirst(t *testing.T) {
	t.Parallel()
	const w, h = 64, 48
	d := softwareDevice(t, w, h)
	defer d.Close()
	scene := gfx.RGB(10, 200, 10)
	fill := gfx.RGB(200, 10, 10)
	fillScene(d, scene)
	PaintTransition(d, gfx.Rect{X: 0, Y: 0, W: w, H: h}, StyleWipe, 0.5, Colors{Fill: fill, Edge: gfx.RGB(255, 255, 0)})
	left := d.Snapshot().RGBAAt(w/8, h/2)
	right := d.Snapshot().RGBAAt(7*w/8, h/2)
	if left != (color.RGBA{R: scene.R, G: scene.G, B: scene.B, A: 255}) {
		t.Fatalf("mid wipe left %+v want scene", left)
	}
	if right == (color.RGBA{R: scene.R, G: scene.G, B: scene.B, A: 255}) {
		t.Fatal("mid wipe right already revealed")
	}
}

func TestPaintGlitchMidDiffersAndSettles(t *testing.T) {
	t.Parallel()
	const w, h = 64, 48
	d := softwareDevice(t, w, h)
	defer d.Close()
	scene := gfx.RGB(12, 14, 20)
	fillScene(d, scene)
	settled := clonePix(d.Snapshot())
	PaintTransition(d, gfx.Rect{X: 0, Y: 0, W: w, H: h}, StyleGlitch, 0.5, Colors{
		Fill: gfx.RGB(0, 0, 0), Edge: gfx.RGB(255, 0, 170), Flash: gfx.RGB(0, 255, 240),
	})
	mid := d.Snapshot()
	if pixelsEqual(mid, pixImage(settled, w, h)) {
		t.Fatal("mid glitch matched settled")
	}
	fillScene(d, scene)
	PaintTransition(d, gfx.Rect{X: 0, Y: 0, W: w, H: h}, StyleGlitch, 1, Colors{
		Fill: gfx.RGB(0, 0, 0), Edge: gfx.RGB(255, 0, 170), Flash: gfx.RGB(0, 255, 240),
	})
	if !pixelsEqual(d.Snapshot(), pixImage(settled, w, h)) {
		t.Fatal("settled glitch mutated pixels")
	}
}

func TestPaintStylesDifferAtMid(t *testing.T) {
	t.Parallel()
	const w, h = 64, 48
	dst := gfx.Rect{X: 0, Y: 0, W: w, H: h}
	c := Colors{Fill: gfx.RGB(8, 8, 16), Edge: gfx.RGB(255, 220, 0), Flash: gfx.RGB(255, 255, 255)}
	pix := map[Style][]byte{}
	for _, s := range []Style{StyleCurtain, StyleWipe, StyleGlitch} {
		d := softwareDevice(t, w, h)
		fillScene(d, gfx.RGB(80, 40, 20))
		PaintTransition(d, dst, s, 0.4, c)
		pix[s] = clonePix(d.Snapshot())
		d.Close()
	}
	if pixelsEqual(pixImage(pix[StyleCurtain], w, h), pixImage(pix[StyleWipe], w, h)) {
		t.Fatal("curtain matched wipe")
	}
	if pixelsEqual(pixImage(pix[StyleCurtain], w, h), pixImage(pix[StyleGlitch], w, h)) {
		t.Fatal("curtain matched glitch")
	}
	if pixelsEqual(pixImage(pix[StyleWipe], w, h), pixImage(pix[StyleGlitch], w, h)) {
		t.Fatal("wipe matched glitch")
	}
}

func TestPaintGlitchIsDeterministic(t *testing.T) {
	t.Parallel()
	const w, h = 64, 48
	dst := gfx.Rect{X: 0, Y: 0, W: w, H: h}
	c := Colors{Fill: gfx.RGB(0, 0, 0), Edge: gfx.RGB(255, 0, 0), Flash: gfx.RGB(0, 255, 0)}
	snap := func() []byte {
		d := softwareDevice(t, w, h)
		defer d.Close()
		fillScene(d, gfx.RGB(1, 2, 3))
		PaintTransition(d, dst, StyleGlitch, 0.37, c)
		return clonePix(d.Snapshot())
	}
	a, b := snap(), snap()
	if !pixelsEqual(pixImage(a, w, h), pixImage(b, w, h)) {
		t.Fatal("glitch paint is not deterministic")
	}
}

func softwareDevice(t *testing.T, w, h int) *gfx.Software {
	t.Helper()
	d, err := gfx.NewSoftware(w, h)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fillScene(d gfx.Device, c gfx.Color) {
	d.BeginFrame()
	d.Clear(c)
	d.SetBlend(gfx.BlendNone)
}

func clonePix(img *image.RGBA) []byte {
	out := make([]byte, len(img.Pix))
	copy(out, img.Pix)
	return out
}

func pixImage(pix []byte, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, pix)
	return img
}
