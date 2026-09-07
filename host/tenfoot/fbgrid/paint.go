package fbgrid

import (
	"image"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

var bg = gfx.RGB(16, 16, 24)

var labelBar = gfx.RGB(8, 8, 12)

// Paint draws the cover-grid onto d. It does not Present.
func Paint(d gfx.Device, g Grid) {
	if d == nil || g.Width < 1 || g.Height < 1 {
		return
	}
	d.BeginFrame()
	d.Clear(bg)
	d.SetBlend(gfx.BlendNone)
	header := g.Header
	if header == "" {
		header = "FOGCAST GRID"
	}
	d.DebugText(16, 10, header, 2)
	for i, tile := range g.Tiles {
		x, y, ok := g.CellOrigin(i)
		if !ok {
			continue
		}
		fill := tile.Color
		flashing := g.ConfirmLeft > 0 && i == g.ConfirmIndex
		if flashing {
			fill = Flash
		}
		r := gfx.Rect{X: float32(x), Y: float32(y), W: float32(g.CellW), H: float32(g.CellH)}
		inner := r
		if i == g.Focus {
			d.FillRect(r, Highlight)
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
		if !flashing {
			paintCover(d, tile.Cover, inner)
		}
		barH := float32(16)
		if barH > inner.H {
			barH = inner.H
		}
		d.FillRect(gfx.Rect{
			X: inner.X,
			Y: inner.Y + inner.H - barH,
			W: inner.W,
			H: barH,
		}, labelBar)
		labelY := y + g.CellH - 16
		if labelY < y {
			labelY = y
		}
		d.DebugText(x+4, labelY, tile.Name, 1)
	}
	status := g.Footer
	if status == "" {
		status = g.Status()
	}
	sy := g.Height - 22
	if sy < 0 {
		sy = 0
	}
	d.DebugText(8, sy, status, 2)
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
