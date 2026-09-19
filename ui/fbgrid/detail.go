package fbgrid

import (
	"image"
	"strings"

	"github.com/DeanoC/FogCast/ui/audioreact"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/shared"
	"github.com/DeanoC/FogCast/ui/theme"
)

// DetailFrame is one focused-title pane: large cover, title, meta, optional shot.
type DetailFrame struct {
	Width, Height int
	Header        string
	Title         string
	Meta          string
	Description   string
	Hint          string
	FooterLines   []string // Optional wrapped footer; nil preserves legacy chrome.
	Cover         *image.RGBA
	CoverKind     CoverKind
	// Box is optional 3D box/cart art for the hero. Paint prefers it over Cover.
	Box         *image.RGBA
	Logo        *image.RGBA
	Color       gfx.Color
	Shot        *image.RGBA
	ShotCaption string
	VideoBadge  bool
	Badges      []Badge
	// Marquee is optional banner/marquee strip art. Nil hides the strip.
	Marquee *image.RGBA
	// Atmosphere is optional fanart behind chrome. When nil, PaintDetail
	// dims the title cover across the stage if one is present.
	Atmosphere   *image.RGBA
	Theme        theme.Theme
	Series       []Tile
	SeriesLabel  string
	SeriesFocus  int
	SeriesActive bool
	// Session is optional pause overlay chrome when the host reports a live
	// session. Empty State leaves the pane undimmed.
	Session SessionChrome
	// Audio is optional edge chrome. Zero skips paint (default).
	Audio audioreact.Sample
}

// PaintDetail draws a living-room title pane. It does not Present.
func PaintDetail(d gfx.Device, f DetailFrame) {
	if d == nil || f.Width < 1 || f.Height < 1 {
		return
	}
	th := f.Theme.Complete()
	if len(f.FooterLines) > 0 {
		th.FooterH = readingFooterHeight(f.FooterLines, th)
	}
	d.BeginFrame()
	d.Clear(th.Background)
	d.SetBlend(gfx.BlendNone)
	fanart := f.Atmosphere
	if fanart == nil {
		fanart = f.Cover
	}
	paintAtmosphere(d, f.Width, f.Height, th, fanart, nil)
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
	marqueeH, seriesReserve := detailReserves(f, th)
	_, text0, _ := DetailLayout(f.Width, f.Height, th, withShot, 0, 0, marqueeH, seriesReserve)
	logoH := 0
	if f.Logo != nil {
		logoH = logoFitHeight(f.Logo, int(text0.W), detailLogoMaxH)
	}
	metaLines, descLines, bodyH := wrapDetailCopy(f, th, int(text0.W), logoH, withShot, f.Height, marqueeH, seriesReserve)
	cover, text, shot := DetailLayout(f.Width, f.Height, th, withShot, logoH, bodyH, marqueeH, seriesReserve)
	if marqueeH > 0 {
		if band := DetailMarqueeRect(f.Width, f.Height, th, f.Marquee); band.H >= 1 {
			d.FillRect(band, letterboxFill(th.HeaderBar, th))
			paintCover(d, f.Marquee, band)
		}
	}
	fill := f.Color
	if fill == (gfx.Color{}) {
		fill = th.SystemColor("")
	}
	inner := cover
	paintFocusCabinet(d, cover, th)
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
	if art := heroBoxArt(f.Box, f.Cover); art != nil {
		d.FillRect(inner, letterboxFill(fill, th))
		paintCover(d, art, inner)
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
	if len(f.Badges) > 0 {
		rowH := badgeRowHeight(th)
		copyY += detailCopyGap
		paintDetailBadges(d, gfx.Rect{X: text.X, Y: float32(copyY), W: text.W, H: float32(rowH)}, f.Badges, th)
		copyY += rowH
	}
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
	if len(f.FooterLines) > 0 {
		paintReadingFooter(d, f.FooterLines, f.Width, f.Height, th)
	} else {
		d.DrawTextWeight(8, chromeTextY(footerTop, footerH, gfx.TextHeightWeight(statusSize, statusW), false), hint, statusSize, statusW, th.Status)
	}
	paintDetailSeries(d, f, th)
	paintLivingRoomChrome(d, f.Width, f.Height, headerH, footerH, th, f.Session)
	PaintAudioChrome(d, f.Width, f.Height, th, f.Audio)
}

const (
	detailLogoMaxH     = 56
	detailMetaMaxLines = 2
	detailDescMaxLines = 8
	detailShotMinH     = 48
	detailCopyGap      = 8
	detailMarqueeMaxH  = 72
)

func wrapDetailCopy(f DetailFrame, th theme.Theme, textW, logoH int, withShot bool, height, marqueeH, seriesReserve int) (metaLines, descLines []string, bodyH int) {
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
	if seriesReserve < 0 {
		seriesReserve = 0
	}
	stageH := height - headerH - footerH - 2*pad - seriesReserve
	if marqueeH > 0 {
		stageH -= marqueeH + detailCopyGap
	}
	if stageH < 1 {
		stageH = height - headerH - footerH - seriesReserve
		if marqueeH > 0 {
			stageH -= marqueeH + detailCopyGap
		}
		if stageH < 1 {
			stageH = height
		}
	}

	metaLines = gfx.WrapTextWeight(strings.TrimSpace(f.Meta), metaSize, textW, detailMetaMaxLines, metaWeight)
	used := titleBlock
	if len(f.Badges) > 0 {
		used += detailCopyGap + badgeRowHeight(th)
	}
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
// marqueeH reserves a banner strip under the header; seriesReserve reserves
// the series row above the footer. 0,0 keeps today's layout.
func DetailLayout(width, height int, th theme.Theme, withShot bool, logoH, bodyH, marqueeH, seriesReserve int) (cover, text, shot gfx.Rect) {
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
	if seriesReserve < 0 {
		seriesReserve = 0
	}
	stageY := headerH + pad
	stageH := height - headerH - footerH - 2*pad - seriesReserve
	if marqueeH > 0 {
		stageY += marqueeH + detailCopyGap
		stageH -= marqueeH + detailCopyGap
	}
	if stageH < 1 {
		stageY = headerH
		if marqueeH > 0 {
			stageY += marqueeH + detailCopyGap
		}
		stageH = height - headerH - footerH - seriesReserve
		if marqueeH > 0 {
			stageH -= marqueeH + detailCopyGap
		}
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
	shotH := height - footerH - seriesReserve - pad - shotY
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
	cover, _, _ := DetailLayout(width, height, th, false, 0, 0, 0, 0)
	if cover.W < 4 || cover.H < 4 {
		return 0, 0, false
	}
	x = int(cover.X + cover.W/2)
	y = int(cover.Y + cover.H/2)
	return x, y, true
}

// DetailTitleOrigin is the top-left of the title DrawText or logo slot.
func DetailTitleOrigin(width, height int, th theme.Theme) (x, y int, ok bool) {
	_, text, _ := DetailLayout(width, height, th, false, 0, 0, 0, 0)
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
	_, text, _ := DetailLayout(width, height, th, false, 0, 0, 0, 0)
	logoH := logoFitHeight(logo, int(text.W), detailLogoMaxH)
	_, text, _ = DetailLayout(width, height, th, false, logoH, 0, 0, 0)
	dx, dy, dw, dh := shared.CoverDestRect(int(text.X), int(text.Y), int(text.W), logoH, b.Dx(), b.Dy())
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
	marqueeH, seriesReserve := detailReserves(f, th)
	_, text0, _ := DetailLayout(width, height, th, withShot, 0, 0, marqueeH, seriesReserve)
	logoH := 0
	if f.Logo != nil {
		logoH = logoFitHeight(f.Logo, int(text0.W), detailLogoMaxH)
	}
	_, _, bodyH := wrapDetailCopy(f, th, int(text0.W), logoH, withShot, height, marqueeH, seriesReserve)
	return DetailLayout(width, height, th, withShot, logoH, bodyH, marqueeH, seriesReserve)
}

func detailReserves(f DetailFrame, th theme.Theme) (marqueeH, seriesReserve int) {
	seriesReserve = f.seriesReserve(th)
	marqueeH = detailMarqueeHeight(f, th)
	return marqueeH, seriesReserve
}

func detailMarqueeHeight(f DetailFrame, th theme.Theme) int {
	if f.Marquee == nil {
		return 0
	}
	th = th.Complete()
	pad := th.Pad
	if pad < 8 {
		pad = 8
	}
	maxW := f.Width - 2*pad
	if maxW < 8 {
		maxW = f.Width
	}
	h := logoFitHeight(f.Marquee, maxW, detailMarqueeMaxH)
	if h < 1 {
		return 0
	}
	headerH := th.HeaderH
	footerH := th.FooterH
	if headerH < 0 {
		headerH = 0
	}
	if footerH < 0 {
		footerH = 0
	}
	seriesReserve := f.seriesReserve(th)
	stageH := f.Height - headerH - footerH - 2*pad - seriesReserve
	if h+detailCopyGap+detailShotMinH > stageH {
		return 0
	}
	return h
}

// DetailMarqueeRect is the inset banner strip under the header, or empty.
func DetailMarqueeRect(width, height int, th theme.Theme, img *image.RGBA) gfx.Rect {
	f := DetailFrame{Width: width, Height: height, Theme: th, Marquee: img}
	h := detailMarqueeHeight(f, th)
	if h < 1 {
		return gfx.Rect{}
	}
	th = th.Complete()
	pad := th.Pad
	if pad < 8 {
		pad = 8
	}
	headerH := th.HeaderH
	if headerH < 0 {
		headerH = 0
	}
	maxW := width - 2*pad
	if maxW < 1 {
		maxW = width
		pad = 0
	}
	return gfx.Rect{X: float32(pad), Y: float32(headerH + pad), W: float32(maxW), H: float32(h)}
}

// DetailMarqueeSample is a pixel inside the banner strip.
func DetailMarqueeSample(width, height int, th theme.Theme, img *image.RGBA) (x, y int, ok bool) {
	r := DetailMarqueeRect(width, height, th, img)
	if r.W < 4 || r.H < 4 {
		return 0, 0, false
	}
	return int(r.X + r.W/2), int(r.Y + r.H/2), true
}

// DetailCoverSampleFor is a cover-cell pixel for a full detail frame.
func DetailCoverSampleFor(width, height int, th theme.Theme, f DetailFrame) (x, y int, ok bool) {
	cover, _, _ := detailShotRect(width, height, th, f)
	if cover.W < 4 || cover.H < 4 {
		return 0, 0, false
	}
	return int(cover.X + cover.W/2), int(cover.Y + cover.H/2), true
}

func (f DetailFrame) seriesVisible() int {
	n := len(f.Series)
	if n > StripMaxTiles {
		n = StripMaxTiles
	}
	return n
}

func (f DetailFrame) seriesReserve(th theme.Theme) int {
	if f.seriesVisible() == 0 {
		return 0
	}
	th = th.Complete()
	h := gfx.TextHeightWeight(th.CaptionPx(), th.CaptionWeight()) + 2
	if h < 12 {
		h = 12
	}
	return h + stripCellH + 8
}

func (f DetailFrame) seriesOrigin(i int, th theme.Theme) (x, y int, ok bool) {
	n := f.seriesVisible()
	if i < 0 || i >= n {
		return 0, 0, false
	}
	th = th.Complete()
	pad := th.Pad
	if pad < 8 {
		pad = 8
	}
	footerH := th.FooterH
	if footerH < 0 {
		footerH = 0
	}
	cellW := stripCellWMax
	innerW := f.Width - 2*pad - (n-1)*8
	if n > 0 && innerW > 0 {
		w := innerW / n
		if w < cellW {
			cellW = w
		}
	}
	if cellW < 1 {
		cellW = 1
	}
	x = pad + i*(cellW+8)
	y = f.Height - footerH - pad - stripCellH
	if y < th.HeaderH {
		y = th.HeaderH
	}
	return x, y, true
}

func (f DetailFrame) seriesTileRect(i int, th theme.Theme) (gfx.Rect, bool) {
	x, y, ok := f.seriesOrigin(i, th)
	if !ok {
		return gfx.Rect{}, false
	}
	n := f.seriesVisible()
	pad := th.Complete().Pad
	if pad < 8 {
		pad = 8
	}
	cellW := stripCellWMax
	innerW := f.Width - 2*pad - (n-1)*8
	if n > 0 && innerW > 0 {
		w := innerW / n
		if w < cellW {
			cellW = w
		}
	}
	if cellW < 1 {
		cellW = 1
	}
	return gfx.Rect{X: float32(x), Y: float32(y), W: float32(cellW), H: float32(stripCellH)}, true
}

// DetailSeriesHighlightSample is a pixel on the focused series tile border.
func DetailSeriesHighlightSample(f DetailFrame) (x, y int, ok bool) {
	if !f.SeriesActive {
		return 0, 0, false
	}
	r, ok := f.seriesTileRect(f.SeriesFocus, f.Theme)
	if !ok || r.W < 2 || r.H < 2 {
		return 0, 0, false
	}
	return int(r.X + 1), int(r.Y + 1), true
}

func paintDetailSeries(d gfx.Device, f DetailFrame, th theme.Theme) {
	n := f.seriesVisible()
	if d == nil || n == 0 {
		return
	}
	th = th.Complete()
	if r, ok := f.seriesTileRect(0, th); ok {
		label := f.SeriesLabel
		if label == "" {
			label = "Series"
		}
		size := th.CaptionPx()
		weight := th.CaptionWeight()
		maxW := f.Width - 16
		if maxW < 1 {
			maxW = 1
		}
		label = gfx.FitTextWeight(label, size, maxW, weight)
		textH := gfx.TextHeightWeight(size, weight)
		labelH := textH + 2
		if labelH < 12 {
			labelH = 12
		}
		ly := int(r.Y) - labelH
		if ly < 0 {
			ly = 0
		}
		d.DrawTextWeight(int(r.X), chromeTextY(ly, labelH, textH, true), label, size, weight, th.Header)
	}
	for i := 0; i < n; i++ {
		r, ok := f.seriesTileRect(i, th)
		if !ok {
			continue
		}
		tile := f.Series[i]
		fill := tile.Color
		inner := r
		focused := f.SeriesActive && i == f.SeriesFocus
		if focused {
			d.FillRect(r, th.Highlight)
			inner = gfx.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}
			if inner.W < 1 {
				inner.W = 1
			}
			if inner.H < 1 {
				inner.H = 1
			}
			d.FillRect(inner, fill)
		} else {
			d.FillRect(r, fill)
		}
		if tile.Cover != nil {
			d.FillRect(inner, letterboxFill(fill, th))
			paintCover(d, tile.Cover, inner)
		} else {
			paintPlaceholder(d, inner, tile.Name, fill, th, tile.CoverKind == CoverLoading)
		}
		if focused {
			paintRectOutline(d, inner, 1, th.Highlight)
		}
	}
}

func logoFitHeight(img *image.RGBA, maxW, maxH int) int {
	if img == nil || maxW < 1 || maxH < 1 {
		return 0
	}
	b := img.Bounds()
	_, _, _, h := shared.CoverDestRect(0, 0, maxW, maxH, b.Dx(), b.Dy())
	if h < 1 {
		return 0
	}
	return int(h)
}
