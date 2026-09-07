package fbgrid

import (
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

var bg = gfx.RGB(16, 16, 24)

// Paint draws the fake cover-grid onto d. It does not Present.
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
		if g.ConfirmLeft > 0 && i == g.ConfirmIndex {
			fill = Flash
		}
		r := gfx.Rect{X: float32(x), Y: float32(y), W: float32(g.CellW), H: float32(g.CellH)}
		if i == g.Focus {
			d.FillRect(r, Highlight)
			inset := float32(g.Border)
			if inset < 1 {
				inset = 1
			}
			d.FillRect(gfx.Rect{
				X: r.X + inset,
				Y: r.Y + inset,
				W: r.W - 2*inset,
				H: r.H - 2*inset,
			}, fill)
		} else {
			d.FillRect(r, fill)
		}
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
