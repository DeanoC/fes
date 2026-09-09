package fbgrid

import (
	"image"
	"strings"
	"unicode"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

// Paint draws the cover-grid onto d using g.Theme, or theme.Default when
// Theme is zero. It does not Present.
func Paint(d gfx.Device, g Grid) {
	if d == nil || g.Width < 1 || g.Height < 1 {
		return
	}
	th := g.Theme.Complete()
	d.BeginFrame()
	d.Clear(th.Background)
	d.SetBlend(gfx.BlendNone)
	paintAtmosphere(d, g.Width, g.Height, th, g.Atmosphere, gridAtmosphereCovers(g))
	if g.HeaderH > 0 {
		d.FillRect(gfx.Rect{X: 0, Y: 0, W: float32(g.Width), H: float32(g.HeaderH)}, th.HeaderBar)
	}
	if g.FooterH > 0 {
		fy := g.Height - g.FooterH
		if fy < 0 {
			fy = 0
		}
		d.FillRect(gfx.Rect{X: 0, Y: float32(fy), W: float32(g.Width), H: float32(g.Height - fy)}, th.FooterBar)
	}
	header := g.Header
	if header == "" {
		header = "FOGCAST GRID"
	}
	headerSize := th.TitlePx()
	headerW := th.HeaderWeight()
	header = gfx.FitTextWeight(header, headerSize, g.Width-24, headerW)
	d.DrawTextWeight(16, chromeTextY(0, g.HeaderH, gfx.TextHeightWeight(headerSize, headerW), true), header, headerSize, headerW, th.Header)
	for _, i := range paintOrder(g) {
		if i < 0 || i >= len(g.Tiles) {
			continue
		}
		paintTile(d, g, th, i, g.Tiles[i])
	}
	paintCoverflowTitle(d, g, th)
	paintSplitHero(d, g, th)
	paintStrip(d, g, th)
	paintEmptyLabel(d, g, th)
	status := g.Footer
	if status == "" {
		status = g.Status()
	}
	footerTop := g.Height - g.FooterH
	if footerTop < 0 {
		footerTop = 0
	}
	statusSize := th.StatusPx()
	statusW := th.StatusWeight()
	status = gfx.FitTextWeight(status, statusSize, g.Width-16, statusW)
	d.DrawTextWeight(8, chromeTextY(footerTop, g.Height-footerTop, gfx.TextHeightWeight(statusSize, statusW), false), status, statusSize, statusW, th.Status)
	paintLivingRoomChrome(d, g.Width, g.Height, g.HeaderH, g.FooterH, th, g.Session)
	PaintAudioChrome(d, g.Width, g.Height, th, g.Audio)
}

func paintOrder(g Grid) []int {
	if g.Kind == BrowseCoverflow {
		return coverflowPaintOrder(g)
	}
	n := len(g.Tiles)
	order := make([]int, 0, n)
	focus, confirm := -1, -1
	for i := range g.Tiles {
		switch {
		case i == g.Focus:
			focus = i
		case g.ConfirmLeft > 0 && i == g.ConfirmIndex:
			confirm = i
		default:
			order = append(order, i)
		}
	}
	if confirm >= 0 && confirm != focus {
		order = append(order, confirm)
	}
	if focus >= 0 {
		order = append(order, focus)
	}
	return order
}

func paintTile(d gfx.Device, g Grid, th theme.Theme, i int, tile Tile) {
	if g.Kind == BrowseSplit {
		paintSplitRow(d, g, th, i, tile)
		return
	}
	r, ok := g.tileRect(i)
	if !ok {
		return
	}
	fill := tile.Color
	flashing := g.ConfirmLeft > 0 && i == g.ConfirmIndex
	if flashing {
		fill = g.confirmFill(tile.Color, th.Flash)
	}
	inner := r
	focused := i == g.Focus && !g.StripActive && !g.SeriesActive
	if focused {
		d.FillRect(r, th.Highlight)
		if in, ok := g.tileInner(i); ok {
			inner = in
		}
		d.FillRect(inner, fill)
	} else {
		d.FillRect(r, fill)
	}
	if th.CoverFrameWidth > 0 && inner.W > float32(2*th.CoverFrameWidth) && inner.H > float32(2*th.CoverFrameWidth) {
		d.FillRect(inner, th.CoverFrame)
		fw := float32(th.CoverFrameWidth)
		inner = gfx.Rect{
			X: inner.X + fw,
			Y: inner.Y + fw,
			W: inner.W - 2*fw,
			H: inner.H - 2*fw,
		}
		d.FillRect(inner, fill)
	}
	if !flashing {
		if tile.Cover != nil {
			d.FillRect(inner, letterboxFill(fill, th))
			paintCover(d, tile.Cover, inner)
		} else {
			paintPlaceholder(d, inner, tile.Name, fill, th, tile.CoverKind == CoverLoading)
		}
		if focused {
			paintRectOutline(d, inner, 1, th.Highlight)
		}
		paintTileBadges(d, g, th, inner, tile)
	}
	if g.Kind == BrowseCoverflow {
		return
	}
	labelSize := th.BodyPx()
	labelW := th.BodyWeight()
	if g.Kind == BrowseWall {
		labelSize = th.CaptionPx()
		labelW = th.CaptionWeight()
	}
	textH := gfx.TextHeightWeight(labelSize, labelW)
	barH := float32(textH + 4)
	if barH < 16 {
		barH = 16
	}
	if tile.Logo != nil && barH < 24 {
		barH = 24
	}
	if barH > inner.H {
		barH = inner.H
	}
	bar := gfx.Rect{
		X: inner.X,
		Y: inner.Y + inner.H - barH,
		W: inner.W,
		H: barH,
	}
	d.FillRect(bar, th.LabelBar)
	if tile.Logo != nil {
		padX := float32(4)
		padY := float32(2)
		logoCell := gfx.Rect{
			X: bar.X + padX,
			Y: bar.Y + padY,
			W: bar.W - 2*padX,
			H: bar.H - 2*padY,
		}
		if logoCell.W < 1 {
			logoCell.W = 1
		}
		if logoCell.H < 1 {
			logoCell.H = 1
		}
		paintCover(d, tile.Logo, logoCell)
		return
	}
	textX := int(inner.X) + 4
	maxW := int(inner.W) - 8
	if maxW < 1 {
		maxW = 1
	}
	name := gfx.FitTextWeight(tile.Name, labelSize, maxW, labelW)
	textY := int(inner.Y+inner.H) - textH - 2
	if textY < int(inner.Y) {
		textY = int(inner.Y)
	}
	d.DrawTextWeight(textX, textY, name, labelSize, labelW, th.Label)
}

func paintSplitRow(d gfx.Device, g Grid, th theme.Theme, i int, tile Tile) {
	r, ok := g.tileRect(i)
	if !ok {
		return
	}
	fill := tile.Color
	flashing := g.ConfirmLeft > 0 && i == g.ConfirmIndex
	if flashing {
		fill = g.confirmFill(tile.Color, th.Flash)
	}
	inner := r
	focused := i == g.Focus && !g.StripActive && !g.SeriesActive
	if focused {
		d.FillRect(r, th.Highlight)
		if in, ok := g.tileInner(i); ok {
			inner = in
		}
		d.FillRect(inner, fill)
	} else {
		d.FillRect(r, fill)
	}
	if flashing {
		return
	}
	padX := float32(6)
	padY := float32(4)
	cell := gfx.Rect{
		X: inner.X + padX,
		Y: inner.Y + padY,
		W: inner.W - 2*padX,
		H: inner.H - 2*padY,
	}
	if cell.W < 1 {
		cell.W = 1
	}
	if cell.H < 1 {
		cell.H = 1
	}
	if tile.Logo != nil {
		paintCover(d, tile.Logo, cell)
	} else {
		labelSize := th.BodyPx()
		labelW := th.BodyWeight()
		if focused {
			labelW = th.TitleWeight()
		}
		name := gfx.FitTextWeight(tile.Name, labelSize, int(cell.W), labelW)
		textH := gfx.TextHeightWeight(labelSize, labelW)
		y := int(cell.Y) + (int(cell.H)-textH)/2
		if y < int(cell.Y) {
			y = int(cell.Y)
		}
		d.DrawTextWeight(int(cell.X), y, name, labelSize, labelW, th.Label)
	}
	if focused {
		paintRectOutline(d, inner, 1, th.Highlight)
	}
}

func paintSplitHero(d gfx.Device, g Grid, th theme.Theme) {
	if d == nil || g.Kind != BrowseSplit {
		return
	}
	if g.Focus < 0 || g.Focus >= len(g.Tiles) {
		return
	}
	cover, titleR, metaR := g.splitHeroSlots()
	if cover.W < 1 || cover.H < 1 {
		return
	}
	tile := g.Tiles[g.Focus]
	fill := tile.Color
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
	if tile.Cover != nil {
		d.FillRect(inner, letterboxFill(fill, th))
		paintCover(d, tile.Cover, inner)
	} else {
		paintPlaceholder(d, inner, tile.Name, fill, th, tile.CoverKind == CoverLoading)
	}
	if titleR.W >= 1 && titleR.H >= 1 && tile.Name != "" {
		size := th.TitlePx()
		weight := th.TitleWeight()
		name := gfx.FitTextWeight(tile.Name, size, int(titleR.W), weight)
		textH := gfx.TextHeightWeight(size, weight)
		y := int(titleR.Y) + (int(titleR.H)-textH)/2
		if y < int(titleR.Y) {
			y = int(titleR.Y)
		}
		d.DrawTextWeight(int(titleR.X), y, name, size, weight, th.Header)
	}
	if metaR.W < 1 || metaR.H < 1 {
		return
	}
	lines := splitMetaLines(tile.Meta, th, int(metaR.W))
	if len(lines) > 0 && metaR.W >= 1 && metaR.H >= 1 {
		metaSize := th.BodyPx()
		metaW := th.BodyWeight()
		lineH := gfx.TextHeightWeight(metaSize, metaW)
		y := int(metaR.Y)
		for _, line := range lines {
			d.DrawTextWeight(int(metaR.X), y, line, metaSize, metaW, th.Label)
			y += lineH
		}
	}
	paintSplitSeries(d, g, th)
}

func paintSplitSeries(d gfx.Device, g Grid, th theme.Theme) {
	n := g.splitSeriesVisible()
	if d == nil || n == 0 {
		return
	}
	if lx, ly, ok := g.splitSeriesLabelOrigin(); ok {
		label := g.SeriesLabel
		if label == "" {
			label = "Series"
		}
		size := th.CaptionPx()
		weight := th.CaptionWeight()
		maxW := g.Width - 2*g.Pad
		if maxW < 1 {
			maxW = 1
		}
		label = gfx.FitTextWeight(label, size, maxW, weight)
		textH := gfx.TextHeightWeight(size, weight)
		d.DrawTextWeight(lx, chromeTextY(ly, textH+2, textH, true), label, size, weight, th.Header)
	}
	for i := 0; i < n; i++ {
		r, ok := g.splitSeriesTileRect(i)
		if !ok {
			continue
		}
		tile := g.Series[i]
		fill := tile.Color
		inner := r
		focused := g.SeriesActive && i == g.SeriesFocus
		if focused {
			d.FillRect(r, th.Highlight)
			inset := float32(g.Border)
			if inset < 1 {
				inset = 1
			}
			inner = gfx.Rect{X: r.X + inset, Y: r.Y + inset, W: r.W - 2*inset, H: r.H - 2*inset}
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

func paintCoverflowTitle(d gfx.Device, g Grid, th theme.Theme) {
	if d == nil || g.Kind != BrowseCoverflow || g.StripActive {
		return
	}
	if g.Focus < 0 || g.Focus >= len(g.Tiles) {
		return
	}
	r, ok := g.tileRect(g.Focus)
	if !ok {
		return
	}
	tile := g.Tiles[g.Focus]
	reserve := g.coverflowTitleReserve()
	if reserve < 1 {
		return
	}
	bar := gfx.Rect{
		X: float32(g.Pad),
		Y: r.Y + r.H + 4,
		W: float32(g.Width - 2*g.Pad),
		H: float32(reserve - 4),
	}
	if bar.Y+bar.H > float32(g.Height-g.FooterH-g.stripReserve()) {
		bar.Y = float32(g.Height-g.FooterH-g.stripReserve()) - bar.H
	}
	if bar.W < 1 || bar.H < 1 {
		return
	}
	maxW := int(bar.W)
	if tile.Logo != nil {
		logoH := int(bar.H)
		if logoH < 16 {
			logoH = 16
		}
		logoCell := gfx.Rect{X: bar.X, Y: bar.Y, W: bar.W, H: float32(logoH)}
		paintCover(d, tile.Logo, logoCell)
		return
	}
	name := tile.Name
	if name == "" {
		return
	}
	size := th.TitlePx()
	weight := th.TitleWeight()
	name = gfx.FitTextWeight(name, size, maxW, weight)
	textH := gfx.TextHeightWeight(size, weight)
	x := int(bar.X) + (maxW-gfx.MeasureTextWeight(name, size, weight))/2
	y := int(bar.Y) + (int(bar.H)-textH)/2
	if y < int(bar.Y) {
		y = int(bar.Y)
	}
	d.DrawTextWeight(x, y, name, size, weight, th.Header)
}

func paintStrip(d gfx.Device, g Grid, th theme.Theme) {
	n := g.stripVisible()
	if d == nil || n == 0 {
		return
	}
	lx, ly, ok := g.StripLabelOrigin()
	if ok {
		label := stripLabelText(g, th)
		size := th.CaptionPx()
		weight := th.CaptionWeight()
		textH := gfx.TextHeightWeight(size, weight)
		y := chromeTextY(ly, g.stripLabelH(), textH, true)
		d.DrawTextWeight(lx, y, label, size, weight, th.Header)
	}
	for i := 0; i < n; i++ {
		paintStripTile(d, g, th, i, g.Strip[i])
	}
}

func paintStripTile(d gfx.Device, g Grid, th theme.Theme, i int, tile Tile) {
	r, ok := g.stripTileRect(i)
	if !ok {
		return
	}
	fill := tile.Color
	inner := r
	focused := g.StripActive && i == g.StripFocus
	if focused {
		d.FillRect(r, th.Highlight)
		if in, ok := g.stripTileInner(i); ok {
			inner = in
		}
		d.FillRect(inner, fill)
	} else {
		d.FillRect(r, fill)
	}
	if th.CoverFrameWidth > 0 && inner.W > float32(2*th.CoverFrameWidth) && inner.H > float32(2*th.CoverFrameWidth) {
		d.FillRect(inner, th.CoverFrame)
		fw := float32(th.CoverFrameWidth)
		inner = gfx.Rect{
			X: inner.X + fw,
			Y: inner.Y + fw,
			W: inner.W - 2*fw,
			H: inner.H - 2*fw,
		}
		d.FillRect(inner, fill)
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
	labelSize := th.CaptionPx()
	labelW := th.CaptionWeight()
	textH := gfx.TextHeightWeight(labelSize, labelW)
	barH := float32(textH + 4)
	if barH < 12 {
		barH = 12
	}
	if tile.Logo != nil && barH < 18 {
		barH = 18
	}
	if barH > inner.H {
		barH = inner.H
	}
	bar := gfx.Rect{
		X: inner.X,
		Y: inner.Y + inner.H - barH,
		W: inner.W,
		H: barH,
	}
	d.FillRect(bar, th.LabelBar)
	if tile.Logo != nil {
		padX := float32(2)
		padY := float32(1)
		logoCell := gfx.Rect{
			X: bar.X + padX,
			Y: bar.Y + padY,
			W: bar.W - 2*padX,
			H: bar.H - 2*padY,
		}
		if logoCell.W < 1 {
			logoCell.W = 1
		}
		if logoCell.H < 1 {
			logoCell.H = 1
		}
		paintCover(d, tile.Logo, logoCell)
		return
	}
	textX := int(inner.X) + 2
	maxW := int(inner.W) - 4
	if maxW < 1 {
		maxW = 1
	}
	name := gfx.FitTextWeight(tile.Name, labelSize, maxW, labelW)
	textY := int(inner.Y+inner.H) - textH - 1
	if textY < int(inner.Y) {
		textY = int(inner.Y)
	}
	d.DrawTextWeight(textX, textY, name, labelSize, labelW, th.Label)
}

// chromeTextY vertically centers a textH-pixel label in a chrome bar. When the
// glyph is taller than the bar, it overflows away from the tile row.
func paintEmptyLabel(d gfx.Device, g Grid, th theme.Theme) {
	if d == nil || len(g.Tiles) > 0 {
		return
	}
	label := strings.TrimSpace(g.EmptyLabel)
	if label == "" {
		return
	}
	size := th.TitlePx()
	weight := th.TitleWeight()
	maxW := g.Width - 24
	if maxW < 8 {
		maxW = 8
	}
	label = gfx.FitTextWeight(label, size, maxW, weight)
	textH := gfx.TextHeightWeight(size, weight)
	x := (g.Width - gfx.MeasureTextWeight(label, size, weight)) / 2
	if x < 8 {
		x = 8
	}
	stageTop := g.HeaderH
	stageH := g.Height - g.HeaderH - g.FooterH
	if stageH < 1 {
		stageH = g.Height
		stageTop = 0
	}
	y := stageTop + (stageH-textH)/3
	if y < stageTop {
		y = stageTop
	}
	d.DrawTextWeight(x, y, label, size, weight, th.Header)
}

func chromeTextY(barTop, barH, textH int, overflowUp bool) int {
	if textH < 1 {
		textH = 1
	}
	if barH < 1 {
		if overflowUp {
			return barTop - textH
		}
		return barTop
	}
	if textH <= barH {
		return barTop + (barH-textH)/2
	}
	if overflowUp {
		return barTop + barH - textH
	}
	return barTop
}

func paintCover(d gfx.Device, img *image.RGBA, cell gfx.Rect) {
	if d == nil || img == nil {
		return
	}
	b := img.Bounds()
	tw, th := b.Dx(), b.Dy()
	if tw < 1 || th < 1 {
		return
	}
	tex, err := d.CreateRGBA(img)
	if err != nil {
		return
	}
	dx, dy, dw, dh := tenfoot.CoverDestRect(int(cell.X), int(cell.Y), int(cell.W), int(cell.H), tw, th)
	d.Draw(tex, nil, gfx.Rect{X: dx, Y: dy, W: dw, H: dh})
	d.Destroy(tex)
}

func paintPlaceholder(d gfx.Device, cell gfx.Rect, name string, fill gfx.Color, th theme.Theme, loading bool) {
	if d == nil || cell.W < 1 || cell.H < 1 {
		return
	}
	panel := placeholderPanel(fill, th, loading)
	d.FillRect(cell, panel)
	accent := placeholderAccent(panel, th)
	paintRectOutline(d, cell, 1, accent)
	if cell.W > 8 && cell.H > 16 {
		d.FillRect(gfx.Rect{X: cell.X + 4, Y: cell.Y + cell.H*0.28, W: cell.W - 8, H: 1}, accent)
		d.FillRect(gfx.Rect{X: cell.X + 4, Y: cell.Y + cell.H*0.48, W: cell.W - 8, H: 1}, accent)
	}
	if loading {
		return
	}
	letter := placeholderLetter(name)
	if letter == "" {
		return
	}
	size := th.CaptionPx()
	letterW := th.CaptionWeight()
	if size > int(cell.H)/2 && int(cell.H) > 0 {
		size = th.BodyPx()
		letterW = th.BodyWeight()
	}
	if size < 1 {
		size = 1
	}
	tw := gfx.MeasureTextWeight(letter, size, letterW)
	textH := gfx.TextHeightWeight(size, letterW)
	x := int(cell.X) + (int(cell.W)-tw)/2
	y := int(cell.Y) + (int(cell.H)-textH)*2/5
	if y < int(cell.Y)+4 {
		y = int(cell.Y) + 4
	}
	d.DrawTextWeight(x, y, letter, size, letterW, th.Label)
}

func paintRectOutline(d gfx.Device, r gfx.Rect, width float32, c gfx.Color) {
	if d == nil || width < 1 || r.W < 2*width || r.H < 2*width {
		return
	}
	d.FillRect(gfx.Rect{X: r.X, Y: r.Y, W: r.W, H: width}, c)
	d.FillRect(gfx.Rect{X: r.X, Y: r.Y + r.H - width, W: r.W, H: width}, c)
	d.FillRect(gfx.Rect{X: r.X, Y: r.Y, W: width, H: r.H}, c)
	d.FillRect(gfx.Rect{X: r.X + r.W - width, Y: r.Y, W: width, H: r.H}, c)
}

func letterboxFill(fill gfx.Color, th theme.Theme) gfx.Color {
	return mixRGB(fill, th.LabelBar, 0.55)
}

// PlaceholderPanel is the solid fill under missing or loading cover art.
func PlaceholderPanel(fill gfx.Color, th theme.Theme, loading bool) gfx.Color {
	return placeholderPanel(fill, th, loading)
}

func placeholderPanel(fill gfx.Color, th theme.Theme, loading bool) gfx.Color {
	if loading {
		return mixRGB(fill, th.HeaderBar, 0.30)
	}
	return mixRGB(fill, th.LabelBar, 0.45)
}

func placeholderAccent(panel gfx.Color, th theme.Theme) gfx.Color {
	return mixRGB(panel, th.Header, 0.22)
}

func placeholderLetter(name string) string {
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return string(unicode.ToUpper(r))
		}
	}
	return ""
}

func mixRGB(a, b gfx.Color, t float32) gfx.Color {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	u := 1 - t
	return gfx.RGB(
		uint8(float32(a.R)*u+float32(b.R)*t+0.5),
		uint8(float32(a.G)*u+float32(b.G)*t+0.5),
		uint8(float32(a.B)*u+float32(b.B)*t+0.5),
	)
}
