package fbgrid

import (
	"image"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/shared"
	"github.com/DeanoC/FogCast/ui/theme"
)

const (
	// AtmosphereScrimA is the theme-background overlay on fanart / cover-wall
	// so chrome and focus stay readable. ~69% scrim, ~31% art.
	AtmosphereScrimA uint8 = 176
	coverWallCols          = 4
	coverWallMinRows       = 3
)

// paintAtmosphere draws dimmed fanart, else a dimmed cover-wall, behind
// chrome. It reports whether any art was painted. Call after Clear.
func paintAtmosphere(d gfx.Device, w, h int, th theme.Theme, fanart *image.RGBA, covers []*image.RGBA) bool {
	if d == nil || w < 1 || h < 1 {
		return false
	}
	stage := gfx.Rect{X: 0, Y: 0, W: float32(w), H: float32(h)}
	if fanart != nil {
		paintCoverFill(d, fanart, stage)
		paintAtmosphereScrim(d, stage, th)
		return true
	}
	covers = compactCovers(covers)
	if len(covers) == 0 {
		return false
	}
	if len(covers) == 1 {
		paintCoverFill(d, covers[0], stage)
		paintAtmosphereScrim(d, stage, th)
		return true
	}
	paintCoverWall(d, covers, w, h)
	paintAtmosphereScrim(d, stage, th)
	return true
}

func paintAtmosphereScrim(d gfx.Device, stage gfx.Rect, th theme.Theme) {
	if d == nil || stage.W < 1 || stage.H < 1 {
		return
	}
	c := th.Background
	c.A = AtmosphereScrimA
	d.SetBlend(gfx.BlendAlpha)
	d.FillRect(stage, c)
	d.SetBlend(gfx.BlendNone)
}

func paintCoverFill(d gfx.Device, img *image.RGBA, cell gfx.Rect) {
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
	dx, dy, dw, dh := shared.CoverFillRect(int(cell.X), int(cell.Y), int(cell.W), int(cell.H), tw, th)
	d.Draw(tex, nil, gfx.Rect{X: dx, Y: dy, W: dw, H: dh})
	d.Destroy(tex)
}

func paintCoverWall(d gfx.Device, covers []*image.RGBA, w, h int) {
	n := len(covers)
	if d == nil || n == 0 || w < 1 || h < 1 {
		return
	}
	cols := coverWallCols
	if cols < 1 {
		cols = 1
	}
	rows := (n + cols - 1) / cols
	if rows < coverWallMinRows {
		rows = coverWallMinRows
	}
	cellW := w / cols
	cellH := h / rows
	if cellW < 1 {
		cellW = 1
	}
	if cellH < 1 {
		cellH = 1
	}
	cells := cols * rows
	for i := 0; i < cells; i++ {
		img := covers[i%n]
		col := i % cols
		row := i / cols
		r := gfx.Rect{
			X: float32(col * cellW),
			Y: float32(row * cellH),
			W: float32(cellW),
			H: float32(cellH),
		}
		paintCoverFill(d, img, r)
	}
}

func compactCovers(covers []*image.RGBA) []*image.RGBA {
	if len(covers) == 0 {
		return nil
	}
	out := make([]*image.RGBA, 0, len(covers))
	for _, img := range covers {
		if img == nil {
			continue
		}
		b := img.Bounds()
		if b.Dx() < 1 || b.Dy() < 1 {
			continue
		}
		out = append(out, img)
	}
	return out
}

func tileCovers(tiles []Tile) []*image.RGBA {
	if len(tiles) == 0 {
		return nil
	}
	out := make([]*image.RGBA, 0, len(tiles))
	for _, tile := range tiles {
		if tile.Cover == nil {
			continue
		}
		out = append(out, tile.Cover)
	}
	return out
}

func gridAtmosphereCovers(g Grid) []*image.RGBA {
	covers := tileCovers(g.Tiles)
	covers = append(covers, tileCovers(g.Strip)...)
	return compactCovers(covers)
}

// AtmosphereDim is the colour of src after the theme-background scrim.
func AtmosphereDim(src, scrim gfx.Color) gfx.Color {
	a := uint32(AtmosphereScrimA)
	inv := uint32(255 - AtmosphereScrimA)
	return gfx.RGB(
		uint8((uint32(scrim.R)*a+uint32(src.R)*inv)/255),
		uint8((uint32(scrim.G)*a+uint32(src.G)*inv)/255),
		uint8((uint32(scrim.B)*a+uint32(src.B)*inv)/255),
	)
}

// AtmosphereSample is a pad pixel in the stage, not on a tile, chrome bar,
// vignette band, or bezel.
func AtmosphereSample(g Grid) (x, y int, ok bool) {
	return StagePadSample(g.Width, g.Height, g.HeaderH, g.FooterH, g.Theme)
}

// WheelAtmosphereSample is a pad pixel around the hero, below the header
// and past the vignette band.
func WheelAtmosphereSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	th = th.Complete()
	return StagePadSample(width, height, th.HeaderH, th.FooterH, th)
}

// DetailAtmosphereSample is a pad pixel on the title pane, below the header.
func DetailAtmosphereSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	return WheelAtmosphereSample(width, height, th)
}
