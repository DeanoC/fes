package fbgrid

import (
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

const (
	coverflowFocusW   = 168
	coverflowFocusH   = 216
	coverflowStep     = 0.56
	coverflowShrink   = 0.22
	coverflowMinScale = 0.46
	coverflowDrop     = 16
)

// CoverflowRects lays out a scaled focus row. The focused tile is largest
// and highest; neighbours shrink and sit lower so the focus stays obvious
// on a 640×480 kit without a 3D rotate. Empty n returns nil.
func CoverflowRects(width, height, headerH, footerH, pad, stripReserve, titleReserve, n, focus int) []gfx.Rect {
	if n < 1 || width < 1 || height < 1 {
		return nil
	}
	if focus < 0 {
		focus = 0
	}
	if focus >= n {
		focus = n - 1
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
	if stripReserve < 0 {
		stripReserve = 0
	}
	if titleReserve < 0 {
		titleReserve = 0
	}
	top := headerH + pad
	bottom := height - footerH - pad - stripReserve - titleReserve
	if bottom <= top {
		bottom = top + 1
	}
	stageH := bottom - top
	cx := float32(width) / 2
	cy := float32(top) + float32(stageH)/2
	fw := float32(coverflowFocusW)
	fh := float32(coverflowFocusH)
	maxW := float32(width) * 0.42
	if maxW >= 48 && fw > maxW {
		fw = maxW
	}
	maxH := float32(stageH) * 0.88
	if maxH >= 48 && fh > maxH {
		fh = maxH
	}
	if fw < 48 {
		fw = 48
	}
	if fh < 48 {
		fh = 48
	}
	out := make([]gfx.Rect, n)
	for i := 0; i < n; i++ {
		d := i - focus
		ad := d
		if ad < 0 {
			ad = -ad
		}
		scale := 1 - coverflowShrink*float32(ad)
		if scale < coverflowMinScale {
			scale = coverflowMinScale
		}
		tw := fw * scale
		th := fh * scale
		if tw < 1 {
			tw = 1
		}
		if th < 1 {
			th = 1
		}
		x := cx - tw/2 + float32(d)*fw*coverflowStep
		y := cy - th/2 + float32(ad)*coverflowDrop
		out[i] = gfx.Rect{X: x, Y: y, W: tw, H: th}
	}
	return out
}

func (g Grid) coverflowTitleReserve() int {
	if g.Kind != BrowseCoverflow {
		return 0
	}
	th := g.Theme.Complete()
	h := gfx.TextHeightWeight(th.TitlePx(), th.TitleWeight()) + 6
	if h < 20 {
		h = 20
	}
	return h
}

func (g Grid) coverflowRects() []gfx.Rect {
	return CoverflowRects(g.Width, g.Height, g.HeaderH, g.FooterH, g.Pad, g.stripReserve(), g.coverflowTitleReserve(), len(g.Tiles), g.Focus)
}

func (g Grid) coverflowRect(i int) (gfx.Rect, bool) {
	if i < 0 || i >= len(g.Tiles) {
		return gfx.Rect{}, false
	}
	rects := g.coverflowRects()
	if i >= len(rects) {
		return gfx.Rect{}, false
	}
	return rects[i], true
}

func (g Grid) tileBaseRect(i int) (gfx.Rect, bool) {
	if g.Kind == BrowseCoverflow {
		return g.coverflowRect(i)
	}
	x, y, ok := g.gridCellOrigin(i)
	if !ok {
		return gfx.Rect{}, false
	}
	return gfx.Rect{X: float32(x), Y: float32(y), W: float32(g.CellW), H: float32(g.CellH)}, true
}

func coverflowPaintOrder(g Grid) []int {
	n := len(g.Tiles)
	order := make([]int, 0, n)
	type dist struct{ i, d int }
	items := make([]dist, 0, n)
	for i := range g.Tiles {
		d := i - g.Focus
		if d < 0 {
			d = -d
		}
		items = append(items, dist{i: i, d: d})
	}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].d > items[i].d || (items[j].d == items[i].d && items[j].i < items[i].i) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	for _, it := range items {
		order = append(order, it.i)
	}
	return order
}
