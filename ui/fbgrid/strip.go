package fbgrid

import (
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/theme"
)

const (
	// StripMaxTiles is the living-room recent/favorites row width.
	StripMaxTiles = 6
	stripCellH    = 56
	stripCellWMax = 112
)

// SetStrip copies the recent/favorites row and relayouts. Empty tiles hide
// the row and restore the full grid height.
func (g *Grid) SetStrip(tiles []Tile, label string, focus int, active bool) {
	if g == nil {
		return
	}
	g.Strip = append([]Tile(nil), tiles...)
	g.StripLabel = label
	if len(g.Strip) == 0 {
		g.StripFocus = 0
		g.StripActive = false
		g.StripCellW = 0
		g.StripCellH = 0
		g.layout()
		return
	}
	if focus < 0 {
		focus = 0
	}
	if focus >= len(g.Strip) {
		focus = len(g.Strip) - 1
	}
	g.StripFocus = focus
	g.StripActive = active
	g.layout()
}

func (g Grid) stripVisible() int {
	n := len(g.Strip)
	if n > StripMaxTiles {
		n = StripMaxTiles
	}
	return n
}

func (g Grid) stripLabelH() int {
	if g.stripVisible() == 0 {
		return 0
	}
	th := g.Theme.Complete()
	h := gfx.TextHeightWeight(th.CaptionPx(), th.CaptionWeight()) + 2
	if h < 12 {
		h = 12
	}
	return h
}

func (g Grid) stripReserve() int {
	if g.stripVisible() == 0 {
		return 0
	}
	return g.stripLabelH() + stripCellH + g.Gap
}

func (g *Grid) layoutStrip() {
	n := g.stripVisible()
	if n == 0 {
		g.StripCellW = 0
		g.StripCellH = 0
		return
	}
	g.StripCellH = stripCellH
	innerW := g.Width - 2*g.Pad - (n-1)*g.Gap
	w := 1
	if n > 0 && innerW > 0 {
		w = innerW / n
	}
	if w > stripCellWMax {
		w = stripCellWMax
	}
	if w < 1 {
		w = 1
	}
	g.StripCellW = w
}

// StripOrigin is the top-left pixel of visible strip tile i.
func (g Grid) StripOrigin(i int) (x, y int, ok bool) {
	n := g.stripVisible()
	if i < 0 || i >= n || g.StripCellW < 1 || g.StripCellH < 1 {
		return 0, 0, false
	}
	x = g.Pad + i*(g.StripCellW+g.Gap)
	y = g.Height - g.FooterH - g.Pad - g.StripCellH
	if y < g.HeaderH {
		y = g.HeaderH
	}
	return x, y, true
}

func (g Grid) stripTileRect(i int) (gfx.Rect, bool) {
	x, y, ok := g.StripOrigin(i)
	if !ok {
		return gfx.Rect{}, false
	}
	return gfx.Rect{X: float32(x), Y: float32(y), W: float32(g.StripCellW), H: float32(g.StripCellH)}, true
}

func (g Grid) stripTileInner(i int) (gfx.Rect, bool) {
	r, ok := g.stripTileRect(i)
	if !ok {
		return gfx.Rect{}, false
	}
	inset := float32(g.Border)
	if inset < 1 {
		inset = 1
	}
	w := r.W - 2*inset
	h := r.H - 2*inset
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return gfx.Rect{X: r.X + inset, Y: r.Y + inset, W: w, H: h}, true
}

// StripHighlightSample is a pixel on the focused strip tile's border.
func (g Grid) StripHighlightSample() (x, y int, ok bool) {
	if !g.StripActive {
		return 0, 0, false
	}
	ox, oy, ok := g.StripOrigin(g.StripFocus)
	if !ok {
		return 0, 0, false
	}
	return ox + 1, oy + 1, true
}

// StripLabelOrigin is the top-left of the strip caption band.
func (g Grid) StripLabelOrigin() (x, y int, ok bool) {
	ox, oy, ok := g.StripOrigin(0)
	if !ok {
		return 0, 0, false
	}
	h := g.stripLabelH()
	y = oy - h
	if y < 0 {
		y = 0
	}
	return ox, y, true
}

func stripLabelText(g Grid, th theme.Theme) string {
	label := g.StripLabel
	if label == "" {
		label = "Recent"
	}
	maxW := g.Width - 2*g.Pad
	if maxW < 1 {
		maxW = 1
	}
	return gfx.FitTextWeight(label, th.CaptionPx(), maxW, th.CaptionWeight())
}
