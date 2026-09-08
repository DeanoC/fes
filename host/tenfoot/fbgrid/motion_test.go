package fbgrid

import (
	"image/color"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/linuxinput"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

func TestFocusPopAdvancesWithTickAndNow(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	if g.FocusPopAmount() != 0 {
		t.Fatalf("rest amount %v", g.FocusPopAmount())
	}
	g.Move(1, 0)
	if g.Focus != 1 {
		t.Fatalf("focus %d", g.Focus)
	}
	if g.FocusPopT() != 0 || g.FocusPopAmount() != 0 {
		t.Fatalf("t0 pop t=%v amount=%v", g.FocusPopT(), g.FocusPopAmount())
	}
	if g.FocusScale() != 1 {
		t.Fatalf("t0 scale %v", g.FocusScale())
	}
	mid := int(FocusPopDuration / TickPeriod / 2)
	if mid < 1 {
		t.Fatal("mid ticks")
	}
	for i := 0; i < mid; i++ {
		g.Tick()
	}
	if g.FocusPopAmount() <= 0.5 {
		t.Fatalf("mid-pop amount %v", g.FocusPopAmount())
	}
	if g.FocusScale() <= 1.03 {
		t.Fatalf("mid-pop scale %v", g.FocusScale())
	}
	if !g.MotionActive() {
		t.Fatal("mid-pop should be active")
	}
	origin0x, origin0y, ok := g.CellOrigin(0)
	if !ok {
		t.Fatal("origin0")
	}
	for i := 0; i < mid+4; i++ {
		g.Tick()
	}
	if g.FocusPopAmount() != 0 || g.FocusScale() != 1 {
		t.Fatalf("settled amount=%v scale=%v", g.FocusPopAmount(), g.FocusScale())
	}
	x, y, ok := g.CellOrigin(0)
	if !ok || x != origin0x || y != origin0y {
		t.Fatalf("unfocused origin moved %d,%d -> %d,%d", origin0x, origin0y, x, y)
	}

	t0 := time.Unix(10, 0)
	g.Now = t0
	g.Move(1, 0)
	if g.Focus != 2 || g.FocusPopAmount() != 0 {
		t.Fatalf("wall t0 focus=%d amount=%v", g.Focus, g.FocusPopAmount())
	}
	g.Now = t0.Add(FocusPopDuration / 2)
	if g.FocusPopAmount() <= 0.5 {
		t.Fatalf("wall mid amount %v", g.FocusPopAmount())
	}
	g.Now = t0.Add(FocusPopDuration)
	if g.FocusPopAmount() != 0 {
		t.Fatalf("wall settled %v", g.FocusPopAmount())
	}
}

func TestConfirmPulseEasesOutOverConfirmFrames(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: true})
	if g.ConfirmLeft != ConfirmFrames {
		t.Fatalf("left %d", g.ConfirmLeft)
	}
	if g.ConfirmPulseAmount() != 1 {
		t.Fatalf("t0 pulse %v", g.ConfirmPulseAmount())
	}
	if g.confirmFill(gfx.RGB(40, 90, 200), Flash) != Flash {
		t.Fatal("t0 fill should be flash")
	}
	mid := ConfirmFrames / 2
	for i := 0; i < mid; i++ {
		g.Tick()
	}
	a := g.ConfirmPulseAmount()
	if a <= 0.2 || a >= 0.95 {
		t.Fatalf("mid pulse %v", a)
	}
	fill := g.confirmFill(gfx.RGB(40, 90, 200), Flash)
	if fill == Flash || fill == gfx.RGB(40, 90, 200) {
		t.Fatalf("mid fill %+v", fill)
	}
	for i := 0; i < ConfirmFrames-mid; i++ {
		g.Tick()
	}
	if g.ConfirmLeft != 0 || g.ConfirmPulseAmount() != 0 {
		t.Fatalf("done left=%d amount=%v", g.ConfirmLeft, g.ConfirmPulseAmount())
	}
}

func TestArmPopUsesWallClock(t *testing.T) {
	t.Parallel()
	g := New(640, 480)
	g.Focus = 2
	t0 := time.Unix(1, 0)
	ArmPop(&g, t0, t0)
	if g.FocusPopAmount() != 0 {
		t.Fatalf("armed t0 %v", g.FocusPopAmount())
	}
	ArmPop(&g, t0, t0.Add(FocusPopDuration/2))
	if g.FocusPopAmount() <= 0.5 {
		t.Fatalf("armed mid %v", g.FocusPopAmount())
	}
	ArmPop(&g, time.Time{}, t0.Add(time.Second))
	if g.FocusPopAmount() != 0 {
		t.Fatalf("zero popAt should rest %v", g.FocusPopAmount())
	}
}

func TestDetailFadeFromBlackSettles(t *testing.T) {
	t.Parallel()
	if got := DetailFadeFromBlack(0); got != DetailFadePeak {
		t.Fatalf("open %v want %v", got, DetailFadePeak)
	}
	mid := DetailFadeFromBlack(DetailFadeDuration / 2)
	if mid <= 0 || mid >= DetailFadePeak {
		t.Fatalf("mid %v", mid)
	}
	if got := DetailFadeFromBlack(DetailFadeDuration); got != 0 {
		t.Fatalf("settled %v", got)
	}
}

func TestPaintFocusPopChangesPixels(t *testing.T) {
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
	g.Move(1, 0)
	Paint(d, g)
	d.Present()
	rest := append([]byte(nil), dst...)
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
	ox, oy, ok := g.CellOrigin(g.Focus)
	if !ok {
		t.Fatal("origin")
	}
	gapX, gapY := ox-2, oy+g.CellH/2
	restGapB, restGapG, restGapR, _, err := gfx.SampleBGRX(dst, cfg, gapX, gapY)
	if err != nil {
		t.Fatal(err)
	}

	mid := int(FocusPopDuration / TickPeriod / 2)
	for i := 0; i < mid; i++ {
		g.Tick()
	}
	if g.FocusPopAmount() <= 0 {
		t.Fatal("expected mid pop")
	}
	Paint(d, g)
	d.Present()
	if bytesEqual(rest, dst) {
		t.Fatal("mid-pop paint matched rest")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
	popB, popG, popR, _, err := gfx.SampleBGRX(dst, cfg, gapX, gapY)
	if err != nil {
		t.Fatal(err)
	}
	if popB == restGapB && popG == restGapG && popR == restGapR {
		t.Fatalf("gap pixel (%d,%d) did not change during pop", gapX, gapY)
	}
	x0, y0, _ := g.CellOrigin(0)
	c0 := g.Tiles[0].Color
	assertPanel(t, dst, cfg, g, 0, c0)
	if x0 != g.Pad {
		t.Fatalf("unfocused origin %d", x0)
	}
	_ = y0

	for i := 0; i < mid+4; i++ {
		g.Tick()
	}
	Paint(d, g)
	d.Present()
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
	assertPanel(t, dst, cfg, g, 0, c0)
}

func TestPaintConfirmPulseDiffersMid(t *testing.T) {
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
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: true})
	g.Apply(linuxinput.Mapped{Action: linuxinput.ActionConfirm, Active: false})
	Paint(d, g)
	d.Present()
	sx, sy, ok := g.CellOrigin(0)
	if !ok {
		t.Fatal("origin")
	}
	ix, iy := sx+g.CellW/2, sy+g.CellH/2
	assertBGRX(t, dst, cfg, ix, iy, 255, 255, 255, 0)
	start := append([]byte(nil), dst...)

	mid := ConfirmFrames / 2
	for i := 0; i < mid; i++ {
		g.Tick()
	}
	Paint(d, g)
	d.Present()
	if bytesEqual(start, dst) {
		t.Fatal("mid-pulse matched start flash")
	}
	gotB, gotG, gotR, _, err := gfx.SampleBGRX(dst, cfg, ix, iy)
	if err != nil {
		t.Fatal(err)
	}
	if gotB == 255 && gotG == 255 && gotR == 255 {
		t.Fatal("mid-pulse interior still full flash")
	}
	tile := g.Tiles[0].Color
	if gotB == tile.B && gotG == tile.G && gotR == tile.R {
		t.Fatal("mid-pulse interior already tile fill")
	}
	hx, hy, ok := g.HighlightSample()
	if !ok {
		t.Fatal("highlight")
	}
	assertBGRX(t, dst, cfg, hx, hy, 0, 220, 255, 0)
}

func TestPaintDetailFadeFromBlackDiffers(t *testing.T) {
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
	frame := DetailFrame{
		Width: w, Height: h, Title: "Mario", Theme: th,
		Color: th.SystemColor("snes"),
	}
	PaintDetail(d, frame)
	d.Present()
	settled := d.Snapshot()
	cx, cy, ok := DetailCoverSample(w, h, th)
	if !ok {
		t.Fatal("cover sample")
	}
	panel := PlaceholderPanel(frame.Color, th.Complete(), false)
	assertBGRX(t, dst, cfg, cx, cy, panel.B, panel.G, panel.R, 0)

	frame.FadeFromBlack = DetailFadeFromBlack(0)
	PaintDetail(d, frame)
	d.Present()
	faded := d.Snapshot()
	if bytesEqual(settled.Pix, faded.Pix) {
		t.Fatal("open fade matched settled pane")
	}
	p := faded.RGBAAt(cx, cy)
	if p == (color.RGBA{R: panel.R, G: panel.G, B: panel.B, A: 255}) {
		t.Fatal("open fade left cover sample unchanged")
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
