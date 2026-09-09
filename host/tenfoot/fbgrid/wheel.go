package fbgrid

import (
	"image"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/audioreact"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

const (
	wheelCellW   = 120
	wheelStripH  = 88
	wheelHeroBar = 72
)

// WheelItem is one platform cell on the living-room wheel.
type WheelItem struct {
	ID    string
	Label string
	Logo  *image.RGBA
	Color gfx.Color
}

// WheelFrame is the living-room platform browser: a hero for the focused
// system and a horizontal clear-logo / wordmark strip.
type WheelFrame struct {
	Width, Height int
	Header        string
	Footer        string
	Title         string
	Stats         string
	Featured      string
	Hero          *image.RGBA
	HeroKind      CoverKind
	// Atmosphere is optional fanart behind chrome. When nil, PaintWheel
	// dims the hero across the stage so the bright hero sits on the same art.
	Atmosphere *image.RGBA
	Logo       *image.RGBA
	Color      gfx.Color
	Items      []WheelItem
	Focus      int
	Theme      theme.Theme
	Now        time.Time
	PopAt      time.Time
	// Session is optional pause overlay chrome when the host reports a live
	// session. Empty State leaves the wheel undimmed.
	Session SessionChrome
	// Audio is optional edge chrome. Zero skips paint (default).
	Audio audioreact.Sample
}

// PaintWheel draws the platform wheel. It does not Present.
func PaintWheel(d gfx.Device, f WheelFrame) {
	if d == nil || f.Width < 1 || f.Height < 1 {
		return
	}
	th := f.Theme.Complete()
	d.BeginFrame()
	d.Clear(th.Background)
	d.SetBlend(gfx.BlendNone)
	fanart := f.Atmosphere
	if fanart == nil {
		fanart = f.Hero
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
	hero, strip := WheelLayout(f.Width, f.Height, th)
	fill := f.Color
	if fill == (gfx.Color{}) {
		fill = th.SystemColor("")
	}
	inner := hero
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
	if f.Hero != nil {
		d.FillRect(inner, letterboxFill(fill, th))
		paintCover(d, f.Hero, inner)
	} else {
		paintPlaceholder(d, inner, f.Title, fill, th, f.HeroKind == CoverLoading)
	}
	barH := float32(wheelHeroBar)
	if barH > inner.H {
		barH = inner.H
	}
	bar := gfx.Rect{X: inner.X, Y: inner.Y + inner.H - barH, W: inner.W, H: barH}
	d.FillRect(bar, th.LabelBar)
	title := f.Title
	if title == "" {
		title = "FOGCAST"
	}
	textX := int(bar.X) + 12
	maxW := int(bar.W) - 24
	if maxW < 1 {
		maxW = 1
	}
	titleSize := th.TitlePx()
	titleW := th.TitleWeight()
	if f.Logo != nil {
		logoH := logoFitHeight(f.Logo, maxW, int(bar.H)-28)
		if logoH < 16 {
			logoH = 16
		}
		logoCell := gfx.Rect{X: bar.X + 12, Y: bar.Y + 6, W: float32(maxW), H: float32(logoH)}
		paintCover(d, f.Logo, logoCell)
	} else {
		label := gfx.FitTextWeight(title, titleSize, maxW, titleW)
		d.DrawTextWeight(textX, int(bar.Y)+8, label, titleSize, titleW, th.Header)
	}
	meta := f.Stats
	if f.Featured != "" {
		if meta != "" {
			meta = meta + "  |  " + f.Featured
		} else {
			meta = f.Featured
		}
	}
	if meta != "" {
		metaSize := th.BodyPx()
		metaW := th.BodyWeight()
		meta = gfx.FitTextWeight(meta, metaSize, maxW, metaW)
		d.DrawTextWeight(textX, int(bar.Y+bar.H)-gfx.TextHeightWeight(metaSize, metaW)-8, meta, metaSize, metaW, th.Label)
	}
	paintWheelStrip(d, f, th, strip)
	header := f.Header
	if header == "" {
		header = "FOGCAST"
	}
	headerSize := th.TitlePx()
	headerWeight := th.HeaderWeight()
	header = gfx.FitTextWeight(header, headerSize, f.Width-24, headerWeight)
	d.DrawTextWeight(16, chromeTextY(0, headerH, gfx.TextHeightWeight(headerSize, headerWeight), true), header, headerSize, headerWeight, th.Header)
	status := f.Footer
	if status == "" {
		status = "A open | L/R platform"
	}
	statusSize := th.StatusPx()
	statusW := th.StatusWeight()
	footerTop := f.Height - footerH
	if footerTop < 0 {
		footerTop = 0
	}
	status = gfx.FitTextWeight(status, statusSize, f.Width-16, statusW)
	d.DrawTextWeight(8, chromeTextY(footerTop, footerH, gfx.TextHeightWeight(statusSize, statusW), false), status, statusSize, statusW, th.Status)
	paintLivingRoomChrome(d, f.Width, f.Height, headerH, footerH, th, f.Session)
	PaintAudioChrome(d, f.Width, f.Height, th, f.Audio)
}

func paintWheelStrip(d gfx.Device, f WheelFrame, th theme.Theme, strip gfx.Rect) {
	n := len(f.Items)
	if n == 0 {
		d.FillRect(strip, th.LabelBar)
		return
	}
	cells := WheelCellRects(f.Width, f.Height, th, n, f.Focus)
	scale := f.focusScale()
	for i, item := range f.Items {
		if i < 0 || i >= len(cells) {
			continue
		}
		r := cells[i]
		if r.W < 1 || r.H < 1 {
			continue
		}
		if i == f.Focus && scale != 1 {
			r = anim.Scale(r, scale)
		}
		fill := item.Color
		if fill == (gfx.Color{}) {
			fill = th.SystemColor(item.ID)
		}
		if i == f.Focus {
			d.FillRect(r, th.Highlight)
			inset := float32(th.Border)
			if inset < 1 {
				inset = 1
			}
			inner := gfx.Rect{X: r.X + inset, Y: r.Y + inset, W: r.W - 2*inset, H: r.H - 2*inset}
			if inner.W < 1 {
				inner.W = 1
			}
			if inner.H < 1 {
				inner.H = 1
			}
			d.FillRect(inner, fill)
			paintWheelCellContent(d, inner, item, th)
			paintRectOutline(d, inner, 1, th.Highlight)
			continue
		}
		d.FillRect(r, fill)
		paintWheelCellContent(d, r, item, th)
	}
}

func paintWheelCellContent(d gfx.Device, r gfx.Rect, item WheelItem, th theme.Theme) {
	padX := float32(6)
	padY := float32(8)
	cell := gfx.Rect{X: r.X + padX, Y: r.Y + padY, W: r.W - 2*padX, H: r.H - 2*padY}
	if cell.W < 1 {
		cell.W = 1
	}
	if cell.H < 1 {
		cell.H = 1
	}
	if item.Logo != nil {
		paintCover(d, item.Logo, cell)
		return
	}
	labelSize := th.BodyPx()
	labelW := th.TitleWeight()
	name := gfx.FitTextWeight(item.Label, labelSize, int(cell.W), labelW)
	textH := gfx.TextHeightWeight(labelSize, labelW)
	x := int(cell.X) + (int(cell.W)-gfx.MeasureTextWeight(name, labelSize, labelW))/2
	y := int(cell.Y) + (int(cell.H)-textH)/2
	d.DrawTextWeight(x, y, name, labelSize, labelW, th.Label)
}

func (f WheelFrame) focusScale() float64 {
	if f.PopAt.IsZero() || f.Now.IsZero() {
		return 1
	}
	t := (anim.Tween{Duration: FocusPopDuration}).Progress(f.Now.Sub(f.PopAt))
	if t <= 0 || t >= 1 {
		return 1
	}
	return 1 + (FocusPopPeak-1)*anim.Pulse01(t)
}

// MotionActive is true while the focused wheel cell is still popping.
func (f WheelFrame) MotionActive() bool {
	if f.PopAt.IsZero() || f.Now.IsZero() {
		return false
	}
	elapsed := f.Now.Sub(f.PopAt)
	return elapsed >= 0 && elapsed < FocusPopDuration
}

// WheelLayout is the hero stage and the logo strip.
func WheelLayout(width, height int, th theme.Theme) (hero, strip gfx.Rect) {
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
	stripH := wheelStripH
	avail := height - headerH - footerH - 2*pad
	if stripH > avail/2 && avail > 0 {
		stripH = avail / 2
	}
	if stripH < 48 {
		stripH = 48
	}
	stripY := height - footerH - pad - stripH
	if stripY < headerH+pad {
		stripY = headerH + pad
	}
	strip = gfx.Rect{X: float32(pad), Y: float32(stripY), W: float32(width - 2*pad), H: float32(stripH)}
	heroH := stripY - headerH - 2*pad
	if heroH < 1 {
		heroH = 1
	}
	hero = gfx.Rect{X: float32(pad), Y: float32(headerH + pad), W: float32(width - 2*pad), H: float32(heroH)}
	return hero, strip
}

// WheelCellRects is the visible strip cells, scrolled so focus stays in view.
func WheelCellRects(width, height int, th theme.Theme, n, focus int) []gfx.Rect {
	_, strip := WheelLayout(width, height, th)
	if n < 1 {
		return nil
	}
	gap := float32(th.Gap)
	if gap < 4 {
		gap = 4
	}
	cellW := float32(wheelCellW)
	if cellW > strip.W {
		cellW = strip.W
	}
	visible := int((strip.W + gap) / (cellW + gap))
	if visible < 1 {
		visible = 1
	}
	if visible > n {
		visible = n
	}
	if focus < 0 {
		focus = 0
	}
	if focus >= n {
		focus = n - 1
	}
	start := focus - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > n {
		start = n - visible
	}
	used := float32(visible)*cellW + float32(visible-1)*gap
	x0 := strip.X + (strip.W-used)/2
	out := make([]gfx.Rect, n)
	for i := 0; i < n; i++ {
		if i < start || i >= start+visible {
			continue
		}
		col := i - start
		out[i] = gfx.Rect{
			X: x0 + float32(col)*(cellW+gap),
			Y: strip.Y,
			W: cellW,
			H: strip.H,
		}
	}
	return out
}

// WheelHeroSample is a pixel inside the hero, above the stats bar.
func WheelHeroSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	hero, _ := WheelLayout(width, height, th)
	if hero.W < 8 || hero.H < 8 {
		return 0, 0, false
	}
	x = int(hero.X + hero.W/2)
	y = int(hero.Y + hero.H/4)
	return x, y, true
}

// WheelFocusSample is a pixel on the focused cell's highlight ring.
func WheelFocusSample(width, height int, th theme.Theme, n, focus int) (x, y int, ok bool) {
	cells := WheelCellRects(width, height, th, n, focus)
	if focus < 0 || focus >= len(cells) {
		return 0, 0, false
	}
	r := cells[focus]
	if r.W < 4 || r.H < 4 {
		return 0, 0, false
	}
	return int(r.X) + 1, int(r.Y) + 1, true
}
