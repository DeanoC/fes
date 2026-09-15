package anim

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
)

// Kit-safe scene overlay timings. All stay under 500ms so pad input is
// never held for a living-room beat.
const (
	CurtainDuration = 320 * time.Millisecond
	WipeDuration    = 280 * time.Millisecond
	GlitchDuration  = 240 * time.Millisecond
	MaxDuration     = 400 * time.Millisecond
)

// Style is one kit scene overlay. None is an honest no-op.
type Style int

const (
	StyleNone Style = iota
	StyleCurtain
	StyleWipe
	StyleGlitch
)

// ParseStyle maps a theme token onto a Style. Empty, "none", and unknown
// names are StyleNone.
func ParseStyle(s string) Style {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "curtain":
		return StyleCurtain
	case "wipe":
		return StyleWipe
	case "glitch", "static":
		return StyleGlitch
	default:
		return StyleNone
	}
}

func (s Style) String() string {
	switch s {
	case StyleCurtain:
		return "curtain"
	case StyleWipe:
		return "wipe"
	case StyleGlitch:
		return "glitch"
	default:
		return "none"
	}
}

// Duration is the wall-clock length of this overlay. None is 0.
func (s Style) Duration() time.Duration {
	switch s {
	case StyleCurtain:
		return CurtainDuration
	case StyleWipe:
		return WipeDuration
	case StyleGlitch:
		return GlitchDuration
	default:
		return 0
	}
}

// Progress is 0 at elapsed<=0 and 1 when the overlay has settled.
func Progress(style Style, elapsed time.Duration) float64 {
	if style == StyleNone {
		return 1
	}
	return (Tween{Duration: style.Duration(), Ease: EaseInOut}).Progress(elapsed)
}

// Colors are the overlay fills. Zero-alpha channels fall back to Classic
// chrome so a partial palette still paints.
type Colors struct {
	Fill  gfx.Color
	Edge  gfx.Color
	Flash gfx.Color
}

func (c Colors) complete() Colors {
	if c.Fill.A == 0 {
		c.Fill = gfx.RGB(16, 16, 24)
	}
	if c.Edge.A == 0 {
		c.Edge = gfx.RGB(255, 220, 0)
	}
	if c.Flash.A == 0 {
		c.Flash = gfx.RGB(255, 255, 255)
	}
	return c
}

// PaintTransition draws a scene overlay onto an already-painted frame.
// t is 0 at the cut and 1 when settled. It does not BeginFrame or Present.
func PaintTransition(d gfx.Device, dst gfx.Rect, style Style, t float64, c Colors) {
	if d == nil || style == StyleNone || dst.W < 1 || dst.H < 1 {
		return
	}
	t = Clamp01(t)
	if t >= 1 {
		return
	}
	c = c.complete()
	switch style {
	case StyleCurtain:
		paintCurtain(d, dst, t, c)
	case StyleWipe:
		paintWipe(d, dst, t, c)
	case StyleGlitch:
		paintGlitch(d, dst, t, c)
	}
}

func paintCurtain(d gfx.Device, dst gfx.Rect, t float64, c Colors) {
	remain := 1 - EaseInOut(t)
	panel := dst.W * float32(remain) / 2
	if panel < 1 {
		return
	}
	left := gfx.Rect{X: dst.X, Y: dst.Y, W: panel, H: dst.H}
	right := gfx.Rect{X: dst.X + dst.W - panel, Y: dst.Y, W: panel, H: dst.H}
	d.SetBlend(gfx.BlendNone)
	d.FillRect(left, c.Fill)
	d.FillRect(right, c.Fill)
	hem := float32(3)
	if hem > panel {
		hem = panel
	}
	if hem >= 1 {
		d.FillRect(gfx.Rect{X: left.X + left.W - hem, Y: dst.Y, W: hem, H: dst.H}, c.Edge)
		d.FillRect(gfx.Rect{X: right.X, Y: dst.Y, W: hem, H: dst.H}, c.Edge)
	}
}

func paintWipe(d gfx.Device, dst gfx.Rect, t float64, c Colors) {
	revealed := dst.W * float32(EaseInOut(t))
	cover := dst.W - revealed
	if cover < 1 {
		return
	}
	d.SetBlend(gfx.BlendNone)
	d.FillRect(gfx.Rect{X: dst.X + revealed, Y: dst.Y, W: cover, H: dst.H}, c.Fill)
	bar := float32(8)
	if bar > cover {
		bar = cover
	}
	if bar >= 1 {
		d.FillRect(gfx.Rect{X: dst.X + revealed, Y: dst.Y, W: bar, H: dst.H}, c.Edge)
	}
}

func paintGlitch(d gfx.Device, dst gfx.Rect, t float64, c Colors) {
	remain := 1 - EaseInOut(t)
	pulse := Pulse01(t)
	if remain <= 0 && pulse <= 0 {
		return
	}
	if remain > 0 {
		FadeOverlay(d, dst, remain*0.22)
	}
	if pulse <= 0.04 {
		return
	}
	w := int(dst.W)
	h := int(dst.H)
	if w < 8 || h < 8 {
		return
	}
	seed := uint32(17 + int(t*1000))
	n := 6 + int(pulse*10)
	d.SetBlend(gfx.BlendNone)
	for i := 0; i < n; i++ {
		seed = lcg(seed)
		y := int(dst.Y) + int(seed%uint32(h))
		thick := 1 + int(seed%2)
		col := c.Flash
		if seed&1 == 1 {
			col = c.Edge
		}
		d.FillRect(gfx.Rect{X: dst.X, Y: float32(y), W: dst.W, H: float32(thick)}, col)
	}
	blocks := 2 + int(pulse*3)
	for i := 0; i < blocks; i++ {
		seed = lcg(seed)
		bw := 16 + int(seed%48)
		bh := 4 + int(seed%12)
		if bw > w {
			bw = w
		}
		if bh > h {
			bh = h
		}
		x := int(dst.X)
		if w > bw {
			x += int(seed % uint32(w-bw))
		}
		seed = lcg(seed)
		y := int(dst.Y)
		if h > bh {
			y += int(seed % uint32(h-bh))
		}
		d.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(bw), H: float32(bh)}, c.Flash)
	}
}

func lcg(s uint32) uint32 {
	return s*1664525 + 1013904223
}
