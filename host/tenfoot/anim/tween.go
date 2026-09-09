// Package anim is CGO-free timed 2D helpers for tenfoot FPGA/software
// backends: eases, rect moves, colour fades, kit scene overlays, and a
// still-cycle attract proof. It talks only to gfx.Device.
package anim

import (
	"math"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

const (
	// StillHold is how long an attract still stays fully visible.
	StillHold = 800 * time.Millisecond
	// StillFade is the crossfade onto the next still.
	StillFade = 400 * time.Millisecond
)

// Clamp01 clamps t into [0, 1].
func Clamp01(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

// EaseLinear is t.
func EaseLinear(t float64) float64 { return Clamp01(t) }

// EaseInOut is a smoothstep curve, 0 at the ends and 0.5 at mid.
func EaseInOut(t float64) float64 {
	t = Clamp01(t)
	return t * t * (3 - 2*t)
}

// Pulse01 is 0 at the ends of [0, 1] and 1 at mid, with EaseInOut on the
// triangle envelope. A focus pop or confirm pulse uses this over a Tween.
func Pulse01(t float64) float64 {
	t = Clamp01(t)
	tri := 1 - math.Abs(2*t-1)
	return EaseInOut(tri)
}

// Lerp interpolates a to b by t.
func Lerp(a, b, t float64) float64 {
	t = Clamp01(t)
	return a + (b-a)*t
}

// LerpF32 interpolates float32 endpoints.
func LerpF32(a, b float32, t float64) float32 {
	return float32(Lerp(float64(a), float64(b), t))
}

// LerpColor interpolates 8-bit RGBA.
func LerpColor(a, b gfx.Color, t float64) gfx.Color {
	t = Clamp01(t)
	return gfx.Color{
		R: lerpU8(a.R, b.R, t),
		G: lerpU8(a.G, b.G, t),
		B: lerpU8(a.B, b.B, t),
		A: lerpU8(a.A, b.A, t),
	}
}

func lerpU8(a, b uint8, t float64) uint8 {
	return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

// Tween maps elapsed time onto [0, 1] with an optional ease.
type Tween struct {
	Duration time.Duration
	Ease     func(float64) float64
}

// Progress is 0 at elapsed<=0 and 1 at elapsed>=Duration.
func (tw Tween) Progress(elapsed time.Duration) float64 {
	if tw.Duration <= 0 {
		return 1
	}
	t := Clamp01(float64(elapsed) / float64(tw.Duration))
	if tw.Ease != nil {
		return tw.Ease(t)
	}
	return t
}

// Move interpolates a rectangle.
func Move(from, to gfx.Rect, t float64) gfx.Rect {
	return gfx.Rect{
		X: LerpF32(from.X, to.X, t),
		Y: LerpF32(from.Y, to.Y, t),
		W: LerpF32(from.W, to.W, t),
		H: LerpF32(from.H, to.H, t),
	}
}

// Scale grows or shrinks r about its centre. factor 1 leaves r unchanged.
func Scale(r gfx.Rect, factor float64) gfx.Rect {
	if factor == 1 {
		return r
	}
	cx := r.X + r.W/2
	cy := r.Y + r.H/2
	w := float32(float64(r.W) * factor)
	h := float32(float64(r.H) * factor)
	return gfx.Rect{X: cx - w/2, Y: cy - h/2, W: w, H: h}
}

// Sprite is a textured quad that travels from From to To.
type Sprite struct {
	Tex      gfx.Texture
	From, To gfx.Rect
}

// DrawAt draws the sprite at eased progress t.
func (s Sprite) DrawAt(d gfx.Device, t float64) {
	if d == nil || !s.Tex.Valid() {
		return
	}
	d.Draw(s.Tex, nil, Move(s.From, s.To, EaseInOut(t)))
}

// CrossfadeFill paints from, then to with alpha t, using SetBlend.
func CrossfadeFill(d gfx.Device, dst gfx.Rect, from, to gfx.Color, t float64) {
	if d == nil {
		return
	}
	t = Clamp01(t)
	d.SetBlend(gfx.BlendNone)
	d.FillRect(dst, from)
	if t <= 0 {
		return
	}
	a := uint8(math.Round(t * 255))
	if a == 0 {
		return
	}
	d.SetBlend(gfx.BlendAlpha)
	d.FillRect(dst, gfx.RGBA(to.R, to.G, to.B, a))
	d.SetBlend(gfx.BlendNone)
}

// FadeOverlay darkens dst with black alpha t (fade-through-black).
func FadeOverlay(d gfx.Device, dst gfx.Rect, t float64) {
	if d == nil {
		return
	}
	t = Clamp01(t)
	if t <= 0 {
		return
	}
	a := uint8(math.Round(t * 255))
	if a == 0 {
		return
	}
	d.SetBlend(gfx.BlendAlpha)
	d.FillRect(dst, gfx.RGBA(0, 0, 0, a))
	d.SetBlend(gfx.BlendNone)
}

// CyclePhase returns the current still index, crossfade 0..1, and a
// ping-pong 0..1 used for sprite travel.
func CyclePhase(elapsed time.Duration, nStills int) (index int, fadeT, moveT float64) {
	if nStills < 1 {
		nStills = 1
	}
	if elapsed < 0 {
		elapsed = 0
	}
	period := StillHold + StillFade
	idx := int(elapsed / period)
	rem := elapsed % period
	index = idx % nStills
	if rem >= StillHold && StillFade > 0 {
		fadeT = float64(rem-StillHold) / float64(StillFade)
	}
	moveT = pingPong(float64(elapsed) / float64(time.Second))
	return index, Clamp01(fadeT), moveT
}

func pingPong(x float64) float64 {
	if x < 0 {
		x = -x
	}
	cycle := math.Mod(x, 2)
	if cycle < 0 {
		cycle += 2
	}
	if cycle <= 1 {
		return cycle
	}
	return 2 - cycle
}
