package fbgrid

import (
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

const (
	// FocusPopDuration is the wall-clock length of the focus scale/border pop.
	FocusPopDuration = 160 * time.Millisecond
	// FocusPopPeak is the peak scale of a focused tile during the pop.
	FocusPopPeak = 1.06
	// TickPeriod is the kit/linuxfb present interval used to map ConfirmLeft
	// and Tick onto wall-clock tweens.
	TickPeriod = 16 * time.Millisecond
	// ConfirmPulseDuration matches ConfirmFrames ticks at TickPeriod.
	ConfirmPulseDuration = time.Duration(ConfirmFrames) * TickPeriod
	// DetailFadeDuration is the title-pane open fade from a light overlay.
	DetailFadeDuration = 140 * time.Millisecond
	// DetailFadePeak is the opening overlay alpha (subtle, not a black frame).
	DetailFadePeak = 0.35
)

func (g *Grid) startFocusPop() {
	g.popLive = true
	g.popIndex = g.Focus
	g.popElapsed = 0
	if !g.Now.IsZero() {
		g.popAt = g.Now
		return
	}
	g.popAt = time.Time{}
}

func (g *Grid) startConfirmPulse() {
	g.confirmElapsed = 0
	if !g.Now.IsZero() {
		g.confirmAt = g.Now
		return
	}
	g.confirmAt = time.Time{}
}

func (g *Grid) advanceMotion() {
	if g.popLive && g.popElapsed < FocusPopDuration {
		g.popElapsed += TickPeriod
	}
	if g.confirmElapsed < ConfirmPulseDuration {
		g.confirmElapsed += TickPeriod
	}
}

// ArmPop copies wall-clock pop state onto a reconstructed grid. A zero popAt
// leaves the focused tile at rest.
func ArmPop(g *Grid, popAt, now time.Time) {
	if g == nil {
		return
	}
	g.Now = now
	g.popAt = popAt
	g.popIndex = g.Focus
	g.popLive = !popAt.IsZero()
}

func (g Grid) popElapsedNow() time.Duration {
	if !g.Now.IsZero() && !g.popAt.IsZero() {
		return g.Now.Sub(g.popAt)
	}
	return g.popElapsed
}

func (g Grid) confirmElapsedNow() time.Duration {
	if !g.Now.IsZero() && !g.confirmAt.IsZero() {
		return g.Now.Sub(g.confirmAt)
	}
	if g.ConfirmLeft > 0 && g.ConfirmLeft <= ConfirmFrames {
		return time.Duration(ConfirmFrames-g.ConfirmLeft) * TickPeriod
	}
	return g.confirmElapsed
}

// FocusPopT is 0 at pop start and 1 when the pop has settled. Idle grids
// (no startFocusPop / ArmPop) stay at 1 so Tick cannot start a pop.
func (g Grid) FocusPopT() float64 {
	if !g.popLive || g.popIndex != g.Focus {
		return 1
	}
	return (anim.Tween{Duration: FocusPopDuration}).Progress(g.popElapsedNow())
}

// FocusPopAmount is 0 at rest, 1 at the mid-pop peak.
func (g Grid) FocusPopAmount() float64 {
	t := g.FocusPopT()
	if t <= 0 || t >= 1 {
		return 0
	}
	return anim.Pulse01(t)
}

// FocusScale is 1 at rest and FocusPopPeak at the mid-pop peak.
func (g Grid) FocusScale() float64 {
	return 1 + (FocusPopPeak-1)*g.FocusPopAmount()
}

// ConfirmPulseT is 0 at confirm start and 1 when ConfirmFrames have elapsed.
func (g Grid) ConfirmPulseT() float64 {
	if g.ConfirmLeft <= 0 {
		return 1
	}
	return (anim.Tween{Duration: ConfirmPulseDuration}).Progress(g.confirmElapsedNow())
}

// ConfirmPulseAmount is 1 on the first confirm frame and eases to 0.
func (g Grid) ConfirmPulseAmount() float64 {
	if g.ConfirmLeft <= 0 {
		return 0
	}
	t := g.ConfirmPulseT()
	if t >= 1 {
		return 0
	}
	return 1 - anim.EaseInOut(t)
}

// ConfirmScale is 1 at rest and FocusPopPeak at confirm start.
func (g Grid) ConfirmScale() float64 {
	return 1 + (FocusPopPeak-1)*g.ConfirmPulseAmount()
}

// MotionActive is true while a pop or confirm pulse still changes pixels.
func (g Grid) MotionActive() bool {
	return g.FocusPopAmount() > 0 || g.ConfirmPulseAmount() > 0
}

func (g Grid) tileScale(i int) float64 {
	scale := 1.0
	if i == g.Focus {
		if s := g.FocusScale(); s > scale {
			scale = s
		}
	}
	if g.ConfirmLeft > 0 && i == g.ConfirmIndex {
		if s := g.ConfirmScale(); s > scale {
			scale = s
		}
	}
	return scale
}

func (g Grid) tileRect(i int) (gfx.Rect, bool) {
	x, y, ok := g.CellOrigin(i)
	if !ok {
		return gfx.Rect{}, false
	}
	r := gfx.Rect{X: float32(x), Y: float32(y), W: float32(g.CellW), H: float32(g.CellH)}
	if s := g.tileScale(i); s != 1 {
		r = anim.Scale(r, s)
	}
	return r, true
}

func (g Grid) tileInner(i int) (gfx.Rect, bool) {
	x, y, ok := g.CellOrigin(i)
	if !ok {
		return gfx.Rect{}, false
	}
	inset := float32(g.Border)
	if inset < 1 {
		inset = 1
	}
	w := float32(g.CellW) - 2*inset
	h := float32(g.CellH) - 2*inset
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return gfx.Rect{X: float32(x) + inset, Y: float32(y) + inset, W: w, H: h}, true
}

func (g Grid) confirmFill(base, flash gfx.Color) gfx.Color {
	a := g.ConfirmPulseAmount()
	if a <= 0 {
		return base
	}
	if a >= 1 {
		return flash
	}
	return anim.LerpColor(base, flash, a)
}

// DetailFadeFromBlack is overlay alpha for an opening title pane. elapsed 0
// is DetailFadePeak; elapsed >= DetailFadeDuration is 0 (fully visible).
func DetailFadeFromBlack(elapsed time.Duration) float64 {
	t := (anim.Tween{Duration: DetailFadeDuration, Ease: anim.EaseInOut}).Progress(elapsed)
	return (1 - t) * DetailFadePeak
}
