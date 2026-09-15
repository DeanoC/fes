package fbgrid

import (
	"github.com/DeanoC/FogCast/ui/tenfoot/gfx"
	"github.com/DeanoC/FogCast/ui/tenfoot/theme"
)

const (
	splitListFrac         = 38
	splitListMinW         = 140
	splitHeroMinW         = 176
	splitRowMinH          = 28
	splitRowMaxH          = 40
	splitRowGap           = 2
	splitHeroMetaMaxLines = 2
	splitHeroCopyGap      = 6
	splitSeriesCellH      = 40
	splitSeriesCellWMax   = 72
)

// SplitLayout is the left list column and right hero column for a split
// browse stage. Empty geometry returns zero rects.
func SplitLayout(width, height, headerH, footerH, pad, gap, stripReserve int) (list, hero gfx.Rect) {
	if width < 1 || height < 1 {
		return gfx.Rect{}, gfx.Rect{}
	}
	if headerH < 0 {
		headerH = 0
	}
	if footerH < 0 {
		footerH = 0
	}
	if pad < 0 {
		pad = 0
	}
	if gap < 0 {
		gap = 0
	}
	if stripReserve < 0 {
		stripReserve = 0
	}
	stageY := headerH + pad
	stageH := height - headerH - footerH - 2*pad - stripReserve
	if stageH < 1 {
		stageY = headerH
		stageH = height - headerH - footerH - stripReserve
		if stageH < 1 {
			stageH = height - headerH - footerH
			if stageH < 1 {
				stageH = height
				stageY = 0
			}
		}
	}
	innerW := width - 2*pad
	if innerW < 1 {
		innerW = width
		pad = 0
	}
	listW := innerW * splitListFrac / 100
	if listW < splitListMinW {
		listW = splitListMinW
	}
	heroW := innerW - listW - gap
	if heroW < splitHeroMinW && innerW > splitHeroMinW+gap {
		heroW = splitHeroMinW
		listW = innerW - heroW - gap
	}
	if listW < 1 {
		listW = 1
	}
	if heroW < 1 {
		heroW = 1
	}
	list = gfx.Rect{X: float32(pad), Y: float32(stageY), W: float32(listW), H: float32(stageH)}
	hero = gfx.Rect{X: float32(pad + listW + gap), Y: float32(stageY), W: float32(heroW), H: float32(stageH)}
	return list, hero
}

// SplitListRects lays out n vertical list rows in the left column.
// Empty n returns nil.
func SplitListRects(width, height, headerH, footerH, pad, gap, stripReserve, n int) []gfx.Rect {
	if n < 1 {
		return nil
	}
	list, _ := SplitLayout(width, height, headerH, footerH, pad, gap, stripReserve)
	if list.W < 1 || list.H < 1 {
		return nil
	}
	rowGap := splitRowGap
	if rowGap < 0 {
		rowGap = 0
	}
	rowH := int(list.H)
	if n > 1 {
		avail := int(list.H) - (n-1)*rowGap
		if avail < n {
			avail = int(list.H)
			rowGap = 0
		}
		rowH = avail / n
	}
	if rowH > splitRowMaxH {
		rowH = splitRowMaxH
	}
	if rowH < splitRowMinH {
		avail := int(list.H) - (n-1)*rowGap
		if n > 0 && avail/n >= 1 {
			rowH = avail / n
		}
		if rowH < 1 {
			rowH = 1
		}
	}
	out := make([]gfx.Rect, n)
	y := list.Y
	for i := 0; i < n; i++ {
		out[i] = gfx.Rect{X: list.X, Y: y, W: list.W, H: float32(rowH)}
		y += float32(rowH + rowGap)
	}
	return out
}

func (g Grid) splitListRects() []gfx.Rect {
	return SplitListRects(g.Width, g.Height, g.HeaderH, g.FooterH, g.Pad, g.Gap, g.stripReserve(), len(g.Tiles))
}

func (g Grid) splitListRect(i int) (gfx.Rect, bool) {
	if i < 0 || i >= len(g.Tiles) {
		return gfx.Rect{}, false
	}
	rects := g.splitListRects()
	if i >= len(rects) {
		return gfx.Rect{}, false
	}
	return rects[i], true
}

func (g Grid) splitColumns() (list, hero gfx.Rect) {
	return SplitLayout(g.Width, g.Height, g.HeaderH, g.FooterH, g.Pad, g.Gap, g.stripReserve())
}

func (g Grid) splitHeroSlots() (cover, title, meta gfx.Rect) {
	_, hero := g.splitColumns()
	if hero.W < 1 || hero.H < 1 {
		return gfx.Rect{}, gfx.Rect{}, gfx.Rect{}
	}
	th := g.Theme.Complete()
	titleH := gfx.TextHeightWeight(th.TitlePx(), th.TitleWeight()) + 4
	if titleH < 20 {
		titleH = 20
	}
	metaH := 0
	if g.Focus >= 0 && g.Focus < len(g.Tiles) {
		if lines := splitMetaLines(g.Tiles[g.Focus].Meta, th, int(hero.W)); len(lines) > 0 {
			metaH = len(lines)*gfx.TextHeightWeight(th.BodyPx(), th.BodyWeight()) + splitHeroCopyGap
		}
	}
	seriesH := g.splitSeriesReserve()
	coverH := int(hero.H) - titleH - metaH - splitHeroCopyGap - seriesH
	if coverH < 48 {
		coverH = int(hero.H) * 62 / 100
		if coverH < 48 {
			coverH = 48
		}
		remain := int(hero.H) - coverH - splitHeroCopyGap - seriesH
		if remain < titleH {
			titleH = remain
			metaH = 0
		} else {
			metaH = remain - titleH
		}
		if titleH < 1 {
			titleH = 1
		}
	}
	if coverH > int(hero.H) {
		coverH = int(hero.H)
	}
	if coverH < 1 {
		coverH = 1
	}
	cover = gfx.Rect{X: hero.X, Y: hero.Y, W: hero.W, H: float32(coverH)}
	titleY := hero.Y + float32(coverH) + float32(splitHeroCopyGap)
	title = gfx.Rect{X: hero.X, Y: titleY, W: hero.W, H: float32(titleH)}
	if metaH > 0 {
		meta = gfx.Rect{X: hero.X, Y: title.Y + title.H, W: hero.W, H: float32(metaH)}
	}
	return cover, title, meta
}

func splitMetaLines(meta string, th theme.Theme, maxW int) []string {
	th = th.Complete()
	if maxW < 1 {
		maxW = 1
	}
	return gfx.WrapTextWeight(meta, th.BodyPx(), maxW, splitHeroMetaMaxLines, th.BodyWeight())
}

// SplitHeroCover is the large art cell for the focused split title.
func (g Grid) SplitHeroCover() (gfx.Rect, bool) {
	if g.Kind != BrowseSplit {
		return gfx.Rect{}, false
	}
	cover, _, _ := g.splitHeroSlots()
	if cover.W < 1 || cover.H < 1 {
		return gfx.Rect{}, false
	}
	return cover, true
}

// SplitHeroCoverSample is a pixel inside the split hero cover.
func (g Grid) SplitHeroCoverSample() (x, y int, ok bool) {
	cover, ok := g.SplitHeroCover()
	if !ok || cover.W < 4 || cover.H < 4 {
		return 0, 0, false
	}
	return int(cover.X + cover.W/2), int(cover.Y + cover.H/2), true
}

// SplitHeroTitleOrigin is the top-left of the split hero title or logo.
func (g Grid) SplitHeroTitleOrigin() (x, y int, ok bool) {
	if g.Kind != BrowseSplit {
		return 0, 0, false
	}
	_, title, _ := g.splitHeroSlots()
	if title.W < 1 || title.H < 1 {
		return 0, 0, false
	}
	return int(title.X), int(title.Y), true
}

func (g Grid) splitSeriesVisible() int {
	n := len(g.Series)
	if n > StripMaxTiles {
		n = StripMaxTiles
	}
	return n
}

func (g Grid) splitSeriesReserve() int {
	if g.Kind != BrowseSplit || g.splitSeriesVisible() == 0 {
		return 0
	}
	th := g.Theme.Complete()
	labelH := gfx.TextHeightWeight(th.CaptionPx(), th.CaptionWeight()) + 2
	if labelH < 12 {
		labelH = 12
	}
	return labelH + splitSeriesCellH + splitHeroCopyGap
}

func (g Grid) splitSeriesOrigin(i int) (x, y int, ok bool) {
	n := g.splitSeriesVisible()
	if i < 0 || i >= n {
		return 0, 0, false
	}
	_, hero := g.splitColumns()
	if hero.W < 1 || hero.H < 1 {
		return 0, 0, false
	}
	reserve := g.splitSeriesReserve()
	if reserve < 1 {
		return 0, 0, false
	}
	cellW := splitSeriesCellWMax
	innerW := int(hero.W) - (n-1)*g.Gap
	if n > 0 && innerW > 0 {
		w := innerW / n
		if w < cellW {
			cellW = w
		}
	}
	if cellW < 1 {
		cellW = 1
	}
	x = int(hero.X) + i*(cellW+g.Gap)
	y = int(hero.Y+hero.H) - splitSeriesCellH
	return x, y, true
}

func (g Grid) splitSeriesTileRect(i int) (gfx.Rect, bool) {
	x, y, ok := g.splitSeriesOrigin(i)
	if !ok {
		return gfx.Rect{}, false
	}
	n := g.splitSeriesVisible()
	cellW := splitSeriesCellWMax
	_, hero := g.splitColumns()
	innerW := int(hero.W) - (n-1)*g.Gap
	if n > 0 && innerW > 0 {
		w := innerW / n
		if w < cellW {
			cellW = w
		}
	}
	if cellW < 1 {
		cellW = 1
	}
	return gfx.Rect{X: float32(x), Y: float32(y), W: float32(cellW), H: float32(splitSeriesCellH)}, true
}

// SplitSeriesHighlightSample is a pixel on the focused split-hero series tile.
func (g Grid) SplitSeriesHighlightSample() (x, y int, ok bool) {
	if !g.SeriesActive {
		return 0, 0, false
	}
	r, ok := g.splitSeriesTileRect(g.SeriesFocus)
	if !ok || r.W < 2 || r.H < 2 {
		return 0, 0, false
	}
	return int(r.X + 1), int(r.Y + 1), true
}

func (g Grid) splitSeriesLabelOrigin() (x, y int, ok bool) {
	r, ok := g.splitSeriesTileRect(0)
	if !ok {
		return 0, 0, false
	}
	th := g.Theme.Complete()
	h := gfx.TextHeightWeight(th.CaptionPx(), th.CaptionWeight()) + 2
	if h < 12 {
		h = 12
	}
	y = int(r.Y) - h
	if y < 0 {
		y = 0
	}
	return int(r.X), y, true
}
