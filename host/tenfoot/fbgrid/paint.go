package fbgrid

import (
	"image"
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
	headerSize := gfx.ScalePx(th.HeaderScale)
	header = gfx.FitText(header, headerSize, g.Width-24)
	d.DrawText(16, chromeTextY(0, g.HeaderH, gfx.TextHeight(headerSize), true), header, headerSize, th.Header)
	for i, tile := range g.Tiles {
		x, y, ok := g.CellOrigin(i)
		if !ok {
			continue
		}
		fill := tile.Color
		flashing := g.ConfirmLeft > 0 && i == g.ConfirmIndex
		if flashing {
			fill = th.Flash
		}
		r := gfx.Rect{X: float32(x), Y: float32(y), W: float32(g.CellW), H: float32(g.CellH)}
		inner := r
		if i == g.Focus {
			d.FillRect(r, th.Highlight)
			inset := float32(g.Border)
			if inset < 1 {
				inset = 1
			}
			inner = gfx.Rect{
				X: r.X + inset,
				Y: r.Y + inset,
				W: r.W - 2*inset,
				H: r.H - 2*inset,
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
			if i == g.Focus {
				paintRectOutline(d, inner, 1, th.Highlight)
			}
		}
		labelSize := gfx.ScalePx(th.LabelScale)
		textH := gfx.TextHeight(labelSize)
		barH := float32(textH + 4)
		if barH < 16 {
			barH = 16
		}
		if barH > inner.H {
			barH = inner.H
		}
		d.FillRect(gfx.Rect{
			X: inner.X,
			Y: inner.Y + inner.H - barH,
			W: inner.W,
			H: barH,
		}, th.LabelBar)
		textX := int(inner.X) + 4
		maxW := int(inner.W) - 8
		if maxW < 1 {
			maxW = 1
		}
		name := gfx.FitText(tile.Name, labelSize, maxW)
		textY := int(inner.Y+inner.H) - textH - 2
		if textY < int(inner.Y) {
			textY = int(inner.Y)
		}
		d.DrawText(textX, textY, name, labelSize, th.Label)
	}
	status := g.Footer
	if status == "" {
		status = g.Status()
	}
	footerTop := g.Height - g.FooterH
	if footerTop < 0 {
		footerTop = 0
	}
	statusSize := gfx.ScalePx(th.StatusScale)
	status = gfx.FitText(status, statusSize, g.Width-16)
	d.DrawText(8, chromeTextY(footerTop, g.Height-footerTop, gfx.TextHeight(statusSize), false), status, statusSize, th.Status)
}

// chromeTextY vertically centers a textH-pixel label in a chrome bar. When the
// glyph is taller than the bar, it overflows away from the tile row.
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
	size := gfx.ScalePx(th.HeaderScale)
	if size > int(cell.H)/2 && int(cell.H) > 0 {
		size = gfx.ScalePx(th.LabelScale)
	}
	if size < 1 {
		size = 1
	}
	tw := gfx.MeasureText(letter, size)
	textH := gfx.TextHeight(size)
	x := int(cell.X) + (int(cell.W)-tw)/2
	y := int(cell.Y) + (int(cell.H)-textH)*2/5
	if y < int(cell.Y)+4 {
		y = int(cell.Y) + 4
	}
	d.DrawText(x, y, letter, size, th.Label)
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
