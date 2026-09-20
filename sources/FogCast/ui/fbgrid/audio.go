package fbgrid

import (
	"math"

	"github.com/DeanoC/FogCast/ui/audioreact"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/theme"
)

const (
	audioBarMin = 2
	audioBarMax = 6
)

// PaintAudioChrome draws theme-highlight edge bars scaled by sample.Level.
// KindNone or a zero level is a no-op. It restores BlendNone.
func PaintAudioChrome(d gfx.Device, w, h int, th theme.Theme, sample audioreact.Sample) {
	if d == nil || w < 1 || h < 1 || !sample.Visible() {
		return
	}
	th = th.Complete()
	level := audioreact.Clamp(sample.Level)
	thickness := AudioBarThickness(level)
	if thickness < 1 {
		return
	}
	a := AudioBarAlpha(level)
	if a == 0 {
		return
	}
	c := th.Highlight
	c.A = a
	d.SetBlend(gfx.BlendAlpha)
	fw, fh := float32(w), float32(h)
	t := float32(thickness)
	d.FillRect(gfx.Rect{X: 0, Y: 0, W: t, H: fh}, c)
	d.FillRect(gfx.Rect{X: fw - t, Y: 0, W: t, H: fh}, c)
	if fw > 2*t {
		d.FillRect(gfx.Rect{X: t, Y: 0, W: fw - 2*t, H: t}, c)
		d.FillRect(gfx.Rect{X: t, Y: fh - t, W: fw - 2*t, H: t}, c)
	}
	d.SetBlend(gfx.BlendNone)
}

// AudioBarAlpha is the highlight opacity for a 0..1 level.
func AudioBarAlpha(level float64) uint8 {
	level = audioreact.Clamp(level)
	if level <= 0 {
		return 0
	}
	return uint8(36 + math.Round(level*140))
}

// AudioBarThickness is the edge width in pixels for a 0..1 level.
func AudioBarThickness(level float64) int {
	level = audioreact.Clamp(level)
	if level <= 0 {
		return 0
	}
	n := audioBarMin + int(math.Round(level*float64(audioBarMax-audioBarMin)))
	if n < 1 {
		return 1
	}
	return n
}

// AudioEdgeSample is a left-edge pixel that sits in the bar when chrome paints.
func AudioEdgeSample(width, height int) (x, y int, ok bool) {
	if width < 1 || height < 1 {
		return 0, 0, false
	}
	return 0, height / 2, true
}
