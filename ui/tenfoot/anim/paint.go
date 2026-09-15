package anim

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
)

const (
	ProofWidth  = 64
	ProofHeight = 48
)

var (
	// StillA and StillB are the attract still stand-ins.
	StillA = gfx.RGB(200, 40, 40)
	StillB = gfx.RGB(40, 80, 200)
	// SpriteColor is the moving tile.
	SpriteColor = gfx.RGB(255, 220, 0)
	// AttractBG is the proof clear colour.
	AttractBG = gfx.RGB(12, 14, 20)
)

// CoverSlot is the still rectangle inside a w×h frame.
func CoverSlot(w, h int) gfx.Rect {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	y := h / 8
	if y < 10 {
		y = 10
	}
	return gfx.Rect{
		X: float32(w / 4),
		Y: float32(y),
		W: float32(w / 2),
		H: float32(h / 3),
	}
}

// SpritePath is the left-to-right travel of the proof sprite.
func SpritePath(w, h int) (from, to gfx.Rect) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	size := 8
	if h >= 48 {
		size = 12
	}
	slot := CoverSlot(w, h)
	slotBottom := int(slot.Y + slot.H)
	y := slotBottom + (h-slotBottom-size)/2
	if y < slotBottom {
		y = h - size
		if y < 0 {
			y = 0
		}
	}
	margin := float32(4)
	from = gfx.Rect{X: margin, Y: float32(y), W: float32(size), H: float32(size)}
	to = gfx.Rect{X: float32(w) - float32(size) - margin, Y: float32(y), W: float32(size), H: float32(size)}
	return from, to
}

// PaintStillCycle draws one attract still/crossfade plus a moving sprite.
// It does not Present. elapsed selects the still pair and sprite position.
func PaintStillCycle(d gfx.Device, w, h int, elapsed time.Duration) {
	if d == nil || w < 1 || h < 1 {
		return
	}
	idx, fadeT, moveT := CyclePhase(elapsed, 2)
	from, to := StillA, StillB
	if idx%2 == 1 {
		from, to = to, from
	}
	d.BeginFrame()
	d.Clear(AttractBG)
	d.SetBlend(gfx.BlendNone)
	CrossfadeFill(d, CoverSlot(w, h), from, to, fadeT)

	sf, st := SpritePath(w, h)
	dst := Move(sf, st, EaseInOut(moveT))
	size := int(sf.W)
	if size < 1 {
		size = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fill := color.RGBA{SpriteColor.R, SpriteColor.G, SpriteColor.B, 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, fill)
		}
	}
	if tex, err := d.CreateRGBA(img); err == nil {
		d.Draw(tex, nil, dst)
		d.Destroy(tex)
	} else {
		d.FillRect(dst, SpriteColor)
	}
	d.DebugText(2, 2, "FC2D SW-REPLAY", 1)
}

// RunFPGAProof paints timed stills on gfx.NewFPGA, checks software replay
// goldens, and reports backend/stub/HW=not-yet. CGO-free.
func RunFPGAProof() (string, error) {
	var b strings.Builder
	slot := CoverSlot(ProofWidth, ProofHeight)
	sx := int(slot.X + slot.W/2)
	sy := int(slot.Y + slot.H/2)
	sf, st := SpritePath(ProofWidth, ProofHeight)
	spx := int(sf.X + sf.W/2)
	spy := int(sf.Y + sf.H/2)

	type sample struct {
		name    string
		elapsed time.Duration
		want    gfx.Color
	}
	samples := []sample{
		{name: "t0", elapsed: 0, want: StillA},
		{name: "hold-end", elapsed: StillHold, want: StillA},
		{name: "next-still", elapsed: StillHold + StillFade, want: StillB},
	}
	var backend string
	var stub bool
	var raster string
	for _, s := range samples {
		dev, err := gfx.NewFPGA(ProofWidth, ProofHeight)
		if err != nil {
			return b.String(), err
		}
		PaintStillCycle(dev, ProofWidth, ProofHeight, s.elapsed)
		dev.Present()
		snap := dev.Snapshot()
		got := snap.RGBAAt(sx, sy)
		want := color.RGBA{s.want.R, s.want.G, s.want.B, 255}
		backend, stub, raster = dev.BackendName(), dev.IsStub(), dev.RasterMode()
		fmt.Fprintf(&b, "%s elapsed=%s cover=(%d,%d) rgba=%d,%d,%d,%d want=%d,%d,%d backend=%s stub=%v raster=%s HW=not-yet\n",
			s.name, s.elapsed, sx, sy, got.R, got.G, got.B, got.A, want.R, want.G, want.B,
			backend, stub, raster)
		if got != want {
			dev.Close()
			return b.String(), fmt.Errorf("%s cover %+v want %+v", s.name, got, want)
		}
		if s.name == "t0" {
			sp := snap.RGBAAt(spx, spy)
			if sp != (color.RGBA{SpriteColor.R, SpriteColor.G, SpriteColor.B, 255}) {
				dev.Close()
				return b.String(), fmt.Errorf("sprite origin %+v", sp)
			}
		}
		raw, err := dev.Bytes()
		if err != nil {
			dev.Close()
			return b.String(), err
		}
		sw, err := gfx.NewSoftware(ProofWidth, ProofHeight)
		if err != nil {
			dev.Close()
			return b.String(), err
		}
		if _, err := gfx.ReplayBytes(sw, raw); err != nil {
			dev.Close()
			return b.String(), err
		}
		if !pixelsEqual(sw.Snapshot(), snap) {
			dev.Close()
			return b.String(), fmt.Errorf("%s replay pixels differ", s.name)
		}
		fmt.Fprintf(&b, "%s cmds=%d bytes=%d replay=ok\n", s.name, len(dev.Commands()), len(raw))
		dev.Close()
	}

	mid, err := gfx.NewFPGA(ProofWidth, ProofHeight)
	if err != nil {
		return b.String(), err
	}
	defer mid.Close()
	PaintStillCycle(mid, ProofWidth, ProofHeight, StillHold+StillFade/2)
	mid.Present()
	blend := mid.Snapshot().RGBAAt(sx, sy)
	if blend == (color.RGBA{StillA.R, StillA.G, StillA.B, 255}) || blend == (color.RGBA{StillB.R, StillB.G, StillB.B, 255}) {
		return b.String(), fmt.Errorf("mid-fade still instant: %+v", blend)
	}
	fmt.Fprintf(&b, "mid-fade cover=(%d,%d) rgba=%d,%d,%d,%d (timed, not a swap)\n", sx, sy, blend.R, blend.G, blend.B, blend.A)

	travel, err := gfx.NewFPGA(ProofWidth, ProofHeight)
	if err != nil {
		return b.String(), err
	}
	defer travel.Close()
	PaintStillCycle(travel, ProofWidth, ProofHeight, time.Second)
	travel.Present()
	_, _, moveT := CyclePhase(time.Second, 2)
	tpx := int(Move(sf, st, EaseInOut(moveT)).X + sf.W/2)
	tp := travel.Snapshot().RGBAAt(tpx, spy)
	origin := travel.Snapshot().RGBAAt(spx, spy)
	if tp != (color.RGBA{SpriteColor.R, SpriteColor.G, SpriteColor.B, 255}) {
		return b.String(), fmt.Errorf("sprite mid-travel %+v at x=%d", tp, tpx)
	}
	if origin == (color.RGBA{SpriteColor.R, SpriteColor.G, SpriteColor.B, 255}) && tpx != spx {
		return b.String(), fmt.Errorf("sprite did not leave origin")
	}
	fmt.Fprintf(&b, "sprite origin-x=%d mid-x=%d moved=%v HW=not-yet\n", spx, tpx, tpx != spx)
	fmt.Fprintf(&b, "backend=%s stub=%v raster=%s protocol=FC2D/%d HW=not-yet\n",
		backend, stub, raster, gfx.ProtocolVersion)
	fmt.Fprintf(&b, "selftest-fpga PASS\n")
	return b.String(), nil
}

func pixelsEqual(a, b *image.RGBA) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Bounds() != b.Bounds() || len(a.Pix) != len(b.Pix) {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}
