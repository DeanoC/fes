package fbgrid

import (
	"image"

	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

// DetailFrame is one focused-title pane: large cover, title, meta, optional shot.
type DetailFrame struct {
	Width, Height int
	Header        string
	Title         string
	Meta          string
	Hint          string
	Cover         *image.RGBA
	CoverKind     CoverKind
	Color         gfx.Color
	Shot          *image.RGBA
	ShotCaption   string
	Theme         theme.Theme
	// FadeFromBlack is overlay alpha in [0, 1]. Zero (default) is fully visible.
	FadeFromBlack float64
}

// PaintDetail draws a living-room title pane. It does not Present.
func PaintDetail(d gfx.Device, f DetailFrame) {
	if d == nil || f.Width < 1 || f.Height < 1 {
		return
	}
	th := f.Theme.Complete()
	d.BeginFrame()
	d.Clear(th.Background)
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
	cover, text, shot := DetailLayout(f.Width, f.Height, th, f.Shot != nil || f.ShotCaption != "")
	fill := f.Color
	if fill == (gfx.Color{}) {
		fill = th.SystemColor("")
	}
	inner := cover
	if th.CoverFrameWidth > 0 && inner.W > float32(2*th.CoverFrameWidth) && inner.H > float32(2*th.CoverFrameWidth) {
		d.FillRect(inner, th.CoverFrame)
		fw := float32(th.CoverFrameWidth)
		inner = gfx.Rect{
			X: inner.X + fw,
			Y: inner.Y + fw,
			W: inner.W - 2*fw,
			H: inner.H - 2*fw,
		}
	}
	if f.Cover != nil {
		d.FillRect(inner, letterboxFill(fill, th))
		paintCover(d, f.Cover, inner)
	} else {
		paintPlaceholder(d, inner, f.Title, fill, th, f.CoverKind == CoverLoading)
	}
	title := f.Title
	if title == "" {
		title = "No title"
	}
	titleSize := th.TitlePx()
	title = gfx.FitText(title, titleSize, int(text.W))
	d.DrawText(int(text.X), int(text.Y), title, titleSize, th.Header)
	meta := f.Meta
	if meta != "" {
		metaSize := th.BodyPx()
		meta = gfx.FitText(meta, metaSize, int(text.W))
		metaY := int(text.Y) + gfx.TextHeight(titleSize) + 8
		d.DrawText(int(text.X), metaY, meta, metaSize, th.Label)
	}
	if shot.W > 0 && shot.H > 0 {
		if f.Shot != nil {
			d.FillRect(shot, letterboxFill(fill, th))
			paintCover(d, f.Shot, shot)
		}
		if f.ShotCaption != "" {
			capSize := th.CaptionPx()
			cap := gfx.FitText(f.ShotCaption, capSize, int(shot.W))
			d.DrawText(int(shot.X), int(shot.Y+shot.H)+4, cap, capSize, th.Status)
		}
	}
	header := f.Header
	if header == "" {
		header = "FOGCAST"
	}
	headerSize := th.TitlePx()
	header = gfx.FitText(header, headerSize, f.Width-24)
	d.DrawText(16, chromeTextY(0, headerH, gfx.TextHeight(headerSize), true), header, headerSize, th.Header)
	hint := f.Hint
	if hint == "" {
		hint = "A play | B back"
	}
	statusSize := th.StatusPx()
	footerTop := f.Height - footerH
	if footerTop < 0 {
		footerTop = 0
	}
	hint = gfx.FitText(hint, statusSize, f.Width-16)
	d.DrawText(8, chromeTextY(footerTop, footerH, gfx.TextHeight(statusSize), false), hint, statusSize, th.Status)
	if f.FadeFromBlack > 0 {
		anim.FadeOverlay(d, gfx.Rect{X: 0, Y: 0, W: float32(f.Width), H: float32(f.Height)}, f.FadeFromBlack)
	}
}

// DetailLayout is the cover, title/meta, and optional screenshot rects.
func DetailLayout(width, height int, th theme.Theme, withShot bool) (cover, text, shot gfx.Rect) {
	th = th.Complete()
	pad := th.Pad
	if pad < 8 {
		pad = 8
	}
	headerH := th.HeaderH
	footerH := th.FooterH
	if headerH < 0 {
		headerH = 0
	}
	if footerH < 0 {
		footerH = 0
	}
	stageY := headerH + pad
	stageH := height - headerH - footerH - 2*pad
	if stageH < 1 {
		stageY = headerH
		stageH = height - headerH - footerH
		if stageH < 1 {
			stageH = height
			stageY = 0
		}
	}
	coverW := width * 42 / 100
	if coverW > 280 {
		coverW = 280
	}
	if coverW < 96 {
		coverW = 96
	}
	if coverW > width-2*pad-80 {
		coverW = width - 2*pad - 80
	}
	if coverW < 1 {
		coverW = 1
	}
	cover = gfx.Rect{X: float32(pad), Y: float32(stageY), W: float32(coverW), H: float32(stageH)}
	textX := pad + coverW + pad
	textW := width - textX - pad
	if textW < 1 {
		textW = 1
	}
	titleH := gfx.TextHeight(th.TitlePx()) + 8 + gfx.TextHeight(th.BodyPx())
	textH := titleH + 8
	if textH > stageH {
		textH = stageH
	}
	text = gfx.Rect{X: float32(textX), Y: float32(stageY), W: float32(textW), H: float32(textH)}
	if !withShot {
		return cover, text, gfx.Rect{}
	}
	shotY := stageY + textH + pad
	shotH := height - footerH - pad - shotY
	if shotH < 48 {
		return cover, text, gfx.Rect{}
	}
	if shotH > 180 {
		shotH = 180
	}
	shot = gfx.Rect{X: float32(textX), Y: float32(shotY), W: float32(textW), H: float32(shotH)}
	return cover, text, shot
}

// DetailCoverSample is a pixel inside the large cover cell.
func DetailCoverSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	cover, _, _ := DetailLayout(width, height, th, false)
	if cover.W < 4 || cover.H < 4 {
		return 0, 0, false
	}
	x = int(cover.X + cover.W/2)
	y = int(cover.Y + cover.H/2)
	return x, y, true
}

// DetailTitleOrigin is the top-left of the title DrawText.
func DetailTitleOrigin(width, height int, th theme.Theme) (x, y int, ok bool) {
	_, text, _ := DetailLayout(width, height, th, false)
	if text.W < 1 || text.H < 1 {
		return 0, 0, false
	}
	return int(text.X), int(text.Y), true
}
