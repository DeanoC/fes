package fbgrid

import (
	"image"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

// tileBoxArt selects 3D box art on focus, a cheap cover perspective when
// that handle is missing, unfocused 2D cover, or 3D-only art when no cover
// exists. Missing both returns nil so callers keep the placeholder.
func tileBoxArt(tile Tile, focused bool) *image.RGBA {
	if focused {
		if tile.Box != nil {
			return tile.Box
		}
		if tile.Cover != nil {
			return tenfoot.FauxBox(tile.Cover)
		}
		return nil
	}
	if tile.Cover != nil {
		return tile.Cover
	}
	return tile.Box
}

func heroBoxArt(box, cover *image.RGBA) *image.RGBA {
	if box != nil {
		return box
	}
	if cover != nil {
		return tenfoot.FauxBox(cover)
	}
	return nil
}

func cabinetExpand(th theme.Theme) float32 {
	w := float32(th.CabinetWidth)
	if w < 1 {
		return 0
	}
	if th.Gap > 0 && w > float32(th.Gap) {
		w = float32(th.Gap)
	}
	return w
}

func clipCabinetOuter(outer, cell gfx.Rect, th theme.Theme) (gfx.Rect, bool) {
	minX := float32(th.Pad)
	if minX < 0 {
		minX = 0
	}
	minY := float32(th.HeaderH + th.Pad)
	if minY < 0 {
		minY = 0
	}
	if outer.X < minX {
		outer.W -= minX - outer.X
		outer.X = minX
	}
	if outer.Y < minY {
		outer.H -= minY - outer.Y
		outer.Y = minY
	}
	if outer.W < 1 || outer.H < 1 {
		return gfx.Rect{}, false
	}
	if outer.X+outer.W <= cell.X && outer.Y+outer.H <= cell.Y {
		return gfx.Rect{}, false
	}
	return outer, true
}

func paintFocusCabinet(d gfx.Device, cell gfx.Rect, th theme.Theme) {
	if d == nil || th.CabinetWidth <= 0 || cell.W < 1 || cell.H < 1 {
		return
	}
	w := cabinetExpand(th)
	if w < 1 {
		return
	}
	chassis := th.Cabinet
	if chassis.A == 0 {
		chassis = gfx.RGB(24, 24, 32)
	}
	outer, ok := clipCabinetOuter(gfx.Rect{X: cell.X - w, Y: cell.Y - w, W: cell.W + 2*w, H: cell.H + 2*w}, cell, th)
	if !ok {
		return
	}
	d.FillRect(outer, chassis)
	lipH := w / 2
	if lipH < 1 {
		lipH = 1
	}
	trim := th.CoverFrame
	if trim.A == 0 {
		trim = th.Highlight
	}
	d.FillRect(gfx.Rect{X: outer.X, Y: outer.Y, W: outer.W, H: lipH}, trim)
	d.FillRect(gfx.Rect{X: outer.X, Y: outer.Y + outer.H - w, W: outer.W, H: w}, mixRGB(chassis, gfx.RGB(0, 0, 0), 0.35))
}

// CabinetSample is a pixel on the focus hardware lip, or false when the
// theme has no cabinet chrome. The lip is sampled in the cell gap, not the
// stage pad that vignette / atmosphere own.
func CabinetSample(cell gfx.Rect, th theme.Theme) (x, y int, ok bool) {
	th = th.Complete()
	if th.CabinetWidth <= 0 || cell.W < 1 || cell.H < 1 {
		return 0, 0, false
	}
	w := cabinetExpand(th)
	if w < 1 {
		return 0, 0, false
	}
	return int(cell.X + cell.W + 1), int(cell.Y + 1), true
}
