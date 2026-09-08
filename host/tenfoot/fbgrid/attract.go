package fbgrid

import (
	"image"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

// AttractFrame is one living-room stills (or empty idle) paint.
type AttractFrame struct {
	Width, Height int
	Title         string
	Hint          string
	Image         *image.RGBA
	Next          *image.RGBA
	FadeT         float64
	Empty         bool
	Theme         theme.Theme
}

// PaintAttract draws title chrome and a still (or empty idle panel) using
// theme AttractBackground and typography roles. It does not Present.
func PaintAttract(d gfx.Device, f AttractFrame) {
	if d == nil || f.Width < 1 || f.Height < 1 {
		return
	}
	th := f.Theme.Complete()
	d.BeginFrame()
	d.Clear(th.AttractBackground)
	d.SetBlend(gfx.BlendNone)
	headerH := th.HeaderH
	footerH := th.FooterH
	if headerH < 0 {
		headerH = 0
	}
	if footerH < 0 {
		footerH = 0
	}
	if headerH > 0 {
		d.FillRect(gfx.Rect{X: 0, Y: 0, W: float32(f.Width), H: float32(headerH)}, th.HeaderBar)
	}
	if footerH > 0 {
		fy := f.Height - footerH
		if fy < 0 {
			fy = 0
		}
		d.FillRect(gfx.Rect{X: 0, Y: float32(fy), W: float32(f.Width), H: float32(f.Height - fy)}, th.FooterBar)
	}
	stageY := headerH
	stageH := f.Height - headerH - footerH
	if stageH < 1 {
		stageH = f.Height
		stageY = 0
	}
	stage := gfx.Rect{X: 0, Y: float32(stageY), W: float32(f.Width), H: float32(stageH)}
	if f.Empty || (f.Image == nil && f.Next == nil) {
		paintAttractIdlePanel(d, stage, th)
	} else {
		paintAttractStill(d, stage, f.Image, f.Next, f.FadeT)
	}
	title := f.Title
	if title == "" {
		title = "FOGCAST"
	}
	titleSize := th.TitlePx()
	title = gfx.FitText(title, titleSize, f.Width-24)
	d.DrawText(16, chromeTextY(0, headerH, gfx.TextHeight(titleSize), true), title, titleSize, th.Header)
	hint := f.Hint
	if hint == "" {
		if f.Empty {
			hint = "any back"
		} else {
			hint = "A play | any back"
		}
	}
	statusSize := th.StatusPx()
	footerTop := f.Height - footerH
	if footerTop < 0 {
		footerTop = 0
	}
	hint = gfx.FitText(hint, statusSize, f.Width-16)
	d.DrawText(8, chromeTextY(footerTop, footerH, gfx.TextHeight(statusSize), false), hint, statusSize, th.Status)
}

func paintAttractIdlePanel(d gfx.Device, stage gfx.Rect, th theme.Theme) {
	title := "Idle"
	caption := "No attract stills"
	titleSize := th.TitlePx()
	captionSize := th.BodyPx()
	titleH := gfx.TextHeight(titleSize)
	captionH := gfx.TextHeight(captionSize)
	gap := 8
	blockH := titleH + gap + captionH
	x0 := int(stage.X)
	y0 := int(stage.Y) + (int(stage.H)-blockH)/2
	if y0 < int(stage.Y)+8 {
		y0 = int(stage.Y) + 8
	}
	title = gfx.FitText(title, titleSize, int(stage.W)-32)
	tw := gfx.MeasureText(title, titleSize)
	d.DrawText(x0+(int(stage.W)-tw)/2, y0, title, titleSize, th.Header)
	caption = gfx.FitText(caption, captionSize, int(stage.W)-32)
	cw := gfx.MeasureText(caption, captionSize)
	d.DrawText(x0+(int(stage.W)-cw)/2, y0+titleH+gap, caption, captionSize, th.Label)
}

func paintAttractStill(d gfx.Device, stage gfx.Rect, img, next *image.RGBA, fadeT float64) {
	fadeT = anim.Clamp01(fadeT)
	if fadeT <= 0 || next == nil {
		paintCover(d, img, stage)
		return
	}
	if fadeT >= 1 {
		paintCover(d, next, stage)
		return
	}
	if fadeT < 0.5 {
		paintCover(d, img, stage)
		anim.FadeOverlay(d, stage, fadeT*2)
		return
	}
	paintCover(d, next, stage)
	anim.FadeOverlay(d, stage, (1-fadeT)*2)
}

// AttractStageSample is a pixel inside the still stage, below the header.
func AttractStageSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	if width < 1 || height < 1 {
		return 0, 0, false
	}
	th = th.Complete()
	y = th.HeaderH + (height-th.HeaderH-th.FooterH)/2
	if y < th.HeaderH+1 {
		y = th.HeaderH + 1
	}
	if y >= height-th.FooterH {
		y = height / 2
	}
	x = width / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= width {
		x = width - 1
	}
	if y >= height {
		y = height - 1
	}
	return x, y, true
}

// AttractLetterboxSample is a pixel in the stage that stays AttractBackground
// around a wide still (top of the stage, just below the header).
func AttractLetterboxSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	if width < 1 || height < 1 {
		return 0, 0, false
	}
	th = th.Complete()
	x = 2
	y = th.HeaderH + 2
	if y >= height-th.FooterH {
		y = th.HeaderH
	}
	if y < 0 {
		y = 0
	}
	if y >= height {
		y = height - 1
	}
	return x, y, true
}

// AttractStillDest is the aspect-fit rectangle used for a still inside the stage.
func AttractStillDest(width, height int, img *image.RGBA, th theme.Theme) gfx.Rect {
	th = th.Complete()
	stageY := th.HeaderH
	stageH := height - th.HeaderH - th.FooterH
	if stageH < 1 {
		stageY = 0
		stageH = height
	}
	if img == nil {
		return gfx.Rect{X: 0, Y: float32(stageY), W: float32(width), H: float32(stageH)}
	}
	b := img.Bounds()
	dx, dy, dw, dh := tenfoot.CoverDestRect(0, stageY, width, stageH, b.Dx(), b.Dy())
	return gfx.Rect{X: dx, Y: dy, W: dw, H: dh}
}
