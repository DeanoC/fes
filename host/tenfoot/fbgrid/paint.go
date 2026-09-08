package fbgrid

import (
	"image"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
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
			paintCover(d, tile.Cover, inner)
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
