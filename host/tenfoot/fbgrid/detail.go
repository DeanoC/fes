package fbgrid

import (
	"image"
	"strings"

	"github.com/DeanoC/FogCast/host/tenfoot"
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
	Description   string
	Hint          string
	Cover         *image.RGBA
	CoverKind     CoverKind
	Logo          *image.RGBA
	Color         gfx.Color
	Shot          *image.RGBA
	ShotCaption   string
	VideoBadge    bool
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
	withShot := f.Shot != nil || f.ShotCaption != "" || f.VideoBadge
	_, text0, _ := DetailLayout(f.Width, f.Height, th, withShot, 0, 0)
	logoH := 0
	if f.Logo != nil {
		logoH = logoFitHeight(f.Logo, int(text0.W), detailLogoMaxH)
	}
	metaLines, descLines, bodyH := wrapDetailCopy(f, th, int(text0.W), logoH, withShot, f.Height)
	cover, text, shot := DetailLayout(f.Width, f.Height, th, withShot, logoH, bodyH)
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
	titleW := th.TitleWeight()
	titleBlockH := gfx.TextHeightWeight(titleSize, titleW)
	if f.Logo != nil && logoH > 0 {
		logoCell := gfx.Rect{X: text.X, Y: text.Y, W: text.W, H: float32(logoH)}
		paintCover(d, f.Logo, logoCell)
		titleBlockH = logoH
	} else {
		title = gfx.FitTextWeight(title, titleSize, int(text.W), titleW)
		d.DrawTextWeight(int(text.X), int(text.Y), title, titleSize, titleW, th.Header)
	}
	copyY := int(text.Y) + titleBlockH
	if len(metaLines) > 0 {
		metaSize := th.BodyPx()
		metaW := th.BodyWeight()
		metaLineH := gfx.TextHeightWeight(metaSize, metaW)
		copyY += detailCopyGap
		for _, line := range metaLines {
			d.DrawTextWeight(int(text.X), copyY, line, metaSize, metaW, th.Label)
			copyY += metaLineH
		}
	}
	if len(descLines) > 0 {
		descSize := th.CaptionPx()
		descW := th.CaptionWeight()
		descLineH := gfx.TextHeightWeight(descSize, descW)
		copyY += detailCopyGap
		for _, line := range descLines {
			d.DrawTextWeight(int(text.X), copyY, line, descSize, descW, th.Label)
			copyY += descLineH
		}
	}
	if shot.W > 0 && shot.H > 0 {
		if f.Shot != nil {
			d.FillRect(shot, letterboxFill(fill, th))
			paintCover(d, f.Shot, shot)
		}
		if f.VideoBadge {
			paintVideoBadge(d, shot, th)
		}
		if f.ShotCaption != "" {
			capSize := th.CaptionPx()
			capW := th.CaptionWeight()
			cap := gfx.FitTextWeight(f.ShotCaption, capSize, int(shot.W), capW)
			d.DrawTextWeight(int(shot.X), int(shot.Y+shot.H)+4, cap, capSize, capW, th.Status)
		}
	}
	header := f.Header
	if header == "" {
		header = "FOGCAST"
	}
	headerSize := th.TitlePx()
	headerW := th.HeaderWeight()
	header = gfx.FitTextWeight(header, headerSize, f.Width-24, headerW)
	d.DrawTextWeight(16, chromeTextY(0, headerH, gfx.TextHeightWeight(headerSize, headerW), true), header, headerSize, headerW, th.Header)
	hint := f.Hint
	if hint == "" {
		hint = "A play | B back"
	}
	statusSize := th.StatusPx()
	statusW := th.StatusWeight()
	footerTop := f.Height - footerH
	if footerTop < 0 {
		footerTop = 0
	}
	hint = gfx.FitTextWeight(hint, statusSize, f.Width-16, statusW)
	d.DrawTextWeight(8, chromeTextY(footerTop, footerH, gfx.TextHeightWeight(statusSize, statusW), false), hint, statusSize, statusW, th.Status)
	if f.FadeFromBlack > 0 {
		anim.FadeOverlay(d, gfx.Rect{X: 0, Y: 0, W: float32(f.Width), H: float32(f.Height)}, f.FadeFromBlack)
	}
}

const (
	detailLogoMaxH     = 56
	detailMetaMaxLines = 2
	detailDescMaxLines = 8
	detailShotMinH     = 48
	detailCopyGap      = 8
)

func wrapDetailCopy(f DetailFrame, th theme.Theme, textW, logoH int, withShot bool, height int) (metaLines, descLines []string, bodyH int) {
	th = th.Complete()
	titleBlock := gfx.TextHeightWeight(th.TitlePx(), th.TitleWeight())
	if logoH > titleBlock {
		titleBlock = logoH
	}
	metaSize := th.BodyPx()
	metaWeight := th.BodyWeight()
	metaLineH := gfx.TextHeightWeight(metaSize, metaWeight)
	descSize := th.CaptionPx()
	descWeight := th.CaptionWeight()
	descLineH := gfx.TextHeightWeight(descSize, descWeight)

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
	stageH := height - headerH - footerH - 2*pad
	if stageH < 1 {
		stageH = height - headerH - footerH
		if stageH < 1 {
			stageH = height
		}
	}

	metaLines = gfx.WrapTextWeight(strings.TrimSpace(f.Meta), metaSize, textW, detailMetaMaxLines, metaWeight)
	used := titleBlock
	if len(metaLines) > 0 {
		used += detailCopyGap + len(metaLines)*metaLineH
	}

	desc := strings.TrimSpace(f.Description)
	if desc != "" && descLineH > 0 {
		remain := stageH - used - detailCopyGap
		if withShot {
			remain -= pad + detailShotMinH
		}
		maxLines := 0
		if remain >= descLineH {
			maxLines = remain / descLineH
		}
		if maxLines > detailDescMaxLines {
			maxLines = detailDescMaxLines
		}
		if maxLines > 0 {
			descLines = gfx.WrapTextWeight(desc, descSize, textW, maxLines, descWeight)
			if len(descLines) > 0 {
				used += detailCopyGap + len(descLines)*descLineH
			}
		}
	}
	bodyH = used + detailCopyGap
	if bodyH > stageH {
		bodyH = stageH
	}
	if bodyH < 1 {
		bodyH = 1
	}
	return metaLines, descLines, bodyH
}

// DetailLayout is the cover, title/meta, and optional screenshot rects.
// bodyH is the title-column height in pixels; 0 uses title plus one body line.
func DetailLayout(width, height int, th theme.Theme, withShot bool, logoH, bodyH int) (cover, text, shot gfx.Rect) {
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
	titleBlock := gfx.TextHeightWeight(th.TitlePx(), th.TitleWeight())
	if logoH > titleBlock {
		titleBlock = logoH
	}
	titleH := titleBlock + detailCopyGap + gfx.TextHeightWeight(th.BodyPx(), th.BodyWeight())
	textH := titleH + detailCopyGap
	if bodyH > 0 {
		textH = bodyH
	}
	if textH > stageH {
		textH = stageH
	}
	text = gfx.Rect{X: float32(textX), Y: float32(stageY), W: float32(textW), H: float32(textH)}
	if !withShot {
		return cover, text, gfx.Rect{}
	}
	shotY := stageY + textH + pad
	shotH := height - footerH - pad - shotY
	if shotH < detailShotMinH {
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
	cover, _, _ := DetailLayout(width, height, th, false, 0, 0)
	if cover.W < 4 || cover.H < 4 {
		return 0, 0, false
	}
	x = int(cover.X + cover.W/2)
	y = int(cover.Y + cover.H/2)
	return x, y, true
}

// DetailTitleOrigin is the top-left of the title DrawText or logo slot.
func DetailTitleOrigin(width, height int, th theme.Theme) (x, y int, ok bool) {
	_, text, _ := DetailLayout(width, height, th, false, 0, 0)
	if text.W < 1 || text.H < 1 {
		return 0, 0, false
	}
	return int(text.X), int(text.Y), true
}

// DetailLogoSample is a pixel inside the title-slot logo.
func DetailLogoSample(width, height int, th theme.Theme, logo *image.RGBA) (x, y int, ok bool) {
	if logo == nil {
		return 0, 0, false
	}
	b := logo.Bounds()
	_, text, _ := DetailLayout(width, height, th, false, 0, 0)
	logoH := logoFitHeight(logo, int(text.W), detailLogoMaxH)
	_, text, _ = DetailLayout(width, height, th, false, logoH, 0)
	dx, dy, dw, dh := tenfoot.CoverDestRect(int(text.X), int(text.Y), int(text.W), logoH, b.Dx(), b.Dy())
	if dw < 2 || dh < 2 {
		return 0, 0, false
	}
	return int(dx + dw/2), int(dy + dh/2), true
}

func paintVideoBadge(d gfx.Device, shot gfx.Rect, th theme.Theme) {
	if d == nil || shot.W < 24 || shot.H < 12 {
		return
	}
	label := "VIDEO"
	size := th.CaptionPx()
	weight := th.CaptionWeight()
	if size < 1 {
		size = 8
	}
	tw := gfx.MeasureTextWeight(label, size, weight)
	thgt := gfx.TextHeightWeight(size, weight)
	padX := 4
	padY := 2
	bw := float32(tw + 2*padX)
	bh := float32(thgt + 2*padY)
	if bw > shot.W-4 {
		bw = shot.W - 4
	}
	if bh > shot.H-4 {
		bh = shot.H - 4
	}
	if bw < 8 || bh < 8 {
		return
	}
	r := gfx.Rect{X: shot.X + 4, Y: shot.Y + 4, W: bw, H: bh}
	d.FillRect(r, th.Highlight)
	text := gfx.FitTextWeight(label, size, int(r.W)-2*padX, weight)
	if text == "" {
		return
	}
	d.DrawTextWeight(int(r.X)+padX, int(r.Y)+padY, text, size, weight, th.Background)
}

// DetailShotSample is a pixel inside the screenshot / preview cell.
func DetailShotSample(width, height int, th theme.Theme, f DetailFrame) (x, y int, ok bool) {
	_, _, shot := detailShotRect(width, height, th, f)
	if shot.W < 4 || shot.H < 4 {
		return 0, 0, false
	}
	return int(shot.X + shot.W/2), int(shot.Y + shot.H/2), true
}

// DetailVideoBadgeSample is a pixel inside the VIDEO badge fill.
func DetailVideoBadgeSample(width, height int, th theme.Theme, f DetailFrame) (x, y int, ok bool) {
	_, _, shot := detailShotRect(width, height, th, f)
	if shot.W < 24 || shot.H < 12 {
		return 0, 0, false
	}
	return int(shot.X + 5), int(shot.Y + 5), true
}

func detailShotRect(width, height int, th theme.Theme, f DetailFrame) (cover, text, shot gfx.Rect) {
	f.Width = width
	f.Height = height
	f.Theme = th
	withShot := f.Shot != nil || f.ShotCaption != "" || f.VideoBadge
	_, text0, _ := DetailLayout(width, height, th, withShot, 0, 0)
	logoH := 0
	if f.Logo != nil {
		logoH = logoFitHeight(f.Logo, int(text0.W), detailLogoMaxH)
	}
	_, _, bodyH := wrapDetailCopy(f, th, int(text0.W), logoH, withShot, height)
	return DetailLayout(width, height, th, withShot, logoH, bodyH)
}

func logoFitHeight(img *image.RGBA, maxW, maxH int) int {
	if img == nil || maxW < 1 || maxH < 1 {
		return 0
	}
	b := img.Bounds()
	_, _, _, h := tenfoot.CoverDestRect(0, 0, maxW, maxH, b.Dx(), b.Dy())
	if h < 1 {
		return 0
	}
	return int(h)
}
