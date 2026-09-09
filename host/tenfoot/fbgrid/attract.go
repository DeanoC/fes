package fbgrid

import (
	"image"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/host/tenfoot/audioreact"
	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

// AttractWallTile is one 2×2 attract-wall cell.
type AttractWallTile struct {
	Image *image.RGBA
	Video bool
}

// AttractFrame is one living-room stills/motion (or empty idle) paint.
type AttractFrame struct {
	Width, Height int
	Title         string
	Hint          string
	Image         *image.RGBA
	Next          *image.RGBA
	FadeT         float64
	Empty         bool
	VideoBadge    bool
	Caption       string
	Wall          []AttractWallTile
	// Marquee is optional banner/marquee strip art. Nil hides the strip.
	Marquee *image.RGBA
	Theme   theme.Theme
	// Audio is optional edge chrome. Zero skips paint (default).
	Audio audioreact.Sample
}

// PaintAttract draws title chrome and a still (or empty idle panel) using
// theme AttractBackground and typography roles. It does not Present.
func PaintAttract(d gfx.Device, f AttractFrame) {
	if d == nil || f.Width < 1 || f.Height < 1 {
		return
	}
	th := f.Theme.Complete()
	d.BeginFrame()
	d.Clear(th.AttractBackground)
	d.SetBlend(gfx.BlendNone)
	headerH := th.HeaderH
	footerH := th.FooterH
	if headerH < 0 {
		headerH = 0
	}
	if footerH < 0 {
		footerH = 0
	}
	if headerH > 0 {
		d.FillRect(gfx.Rect{X: 0, Y: 0, W: float32(f.Width), H: float32(headerH)}, th.HeaderBar)
	}
	if footerH > 0 {
		fy := f.Height - footerH
		if fy < 0 {
			fy = 0
		}
		d.FillRect(gfx.Rect{X: 0, Y: float32(fy), W: float32(f.Width), H: float32(f.Height - fy)}, th.FooterBar)
	}
	stageY := headerH
	stageH := f.Height - headerH - footerH
	if stageH < 1 {
		stageH = f.Height
		stageY = 0
	}
	stage := gfx.Rect{X: 0, Y: float32(stageY), W: float32(f.Width), H: float32(stageH)}
	if band := AttractMarqueeRect(f.Width, f.Height, f.Marquee, th); band.H >= 1 {
		paintCover(d, f.Marquee, band)
		gap := float32(attractMarqueeGap)
		stage.Y = band.Y + band.H + gap
		stage.H = float32(f.Height-footerH) - stage.Y
		if stage.H < 1 {
			stage.H = 1
		}
	}
	if f.Empty || (f.Image == nil && f.Next == nil && len(f.Wall) < 4) {
		paintAttractIdlePanel(d, stage, th)
	} else if len(f.Wall) >= 4 {
		paintAttractWall(d, stage, f.Wall, th)
	} else {
		paintAttractStill(d, stage, f.Image, f.Next, f.FadeT)
		if f.VideoBadge {
			dest := AttractStillDestFor(f)
			paintVideoBadge(d, dest, th)
		}
	}
	title := f.Title
	if title == "" {
		title = "FOGCAST"
	}
	titleSize := th.TitlePx()
	titleW := th.HeaderWeight()
	title = gfx.FitTextWeight(title, titleSize, f.Width-24, titleW)
	d.DrawTextWeight(16, chromeTextY(0, headerH, gfx.TextHeightWeight(titleSize, titleW), true), title, titleSize, titleW, th.Header)
	hint := f.Hint
	if hint == "" {
		switch {
		case f.Empty:
			hint = "any back"
		case f.VideoBadge && f.Caption != "":
			hint = "A play | any back | " + f.Caption
		case f.VideoBadge:
			hint = "A play | any back | preview"
		default:
			hint = "A play | any back"
		}
	}
	statusSize := th.StatusPx()
	statusW := th.StatusWeight()
	footerTop := f.Height - footerH
	if footerTop < 0 {
		footerTop = 0
	}
	hint = audioreact.AppendHint(hint, f.Audio)
	hint = gfx.FitTextWeight(hint, statusSize, f.Width-16, statusW)
	d.DrawTextWeight(8, chromeTextY(footerTop, footerH, gfx.TextHeightWeight(statusSize, statusW), false), hint, statusSize, statusW, th.Status)
	PaintAudioChrome(d, f.Width, f.Height, th, f.Audio)
}

func paintAttractIdlePanel(d gfx.Device, stage gfx.Rect, th theme.Theme) {
	title := "Idle"
	caption := "No attract stills"
	titleSize := th.TitlePx()
	captionSize := th.BodyPx()
	titleW := th.TitleWeight()
	captionW := th.BodyWeight()
	titleH := gfx.TextHeightWeight(titleSize, titleW)
	captionH := gfx.TextHeightWeight(captionSize, captionW)
	gap := 8
	blockH := titleH + gap + captionH
	x0 := int(stage.X)
	y0 := int(stage.Y) + (int(stage.H)-blockH)/2
	if y0 < int(stage.Y)+8 {
		y0 = int(stage.Y) + 8
	}
	title = gfx.FitTextWeight(title, titleSize, int(stage.W)-32, titleW)
	tw := gfx.MeasureTextWeight(title, titleSize, titleW)
	d.DrawTextWeight(x0+(int(stage.W)-tw)/2, y0, title, titleSize, titleW, th.Header)
	caption = gfx.FitTextWeight(caption, captionSize, int(stage.W)-32, captionW)
	cw := gfx.MeasureTextWeight(caption, captionSize, captionW)
	d.DrawTextWeight(x0+(int(stage.W)-cw)/2, y0+titleH+gap, caption, captionSize, captionW, th.Label)
}

func paintAttractWall(d gfx.Device, stage gfx.Rect, tiles []AttractWallTile, th theme.Theme) {
	if d == nil || len(tiles) < 4 {
		return
	}
	const gap float32 = 6
	cellW := (stage.W - gap) / 2
	cellH := (stage.H - gap) / 2
	if cellW < 8 || cellH < 8 {
		return
	}
	for i := 0; i < 4; i++ {
		col := i % 2
		row := i / 2
		r := gfx.Rect{
			X: stage.X + float32(col)*(cellW+gap),
			Y: stage.Y + float32(row)*(cellH+gap),
			W: cellW,
			H: cellH,
		}
		if tiles[i].Image != nil {
			paintCover(d, tiles[i].Image, r)
		}
		if i == 0 {
			paintRectOutline(d, r, 3, th.Highlight)
			if tiles[i].Video {
				paintVideoBadge(d, r, th)
			}
		}
	}
}

func paintAttractStill(d gfx.Device, stage gfx.Rect, img, next *image.RGBA, fadeT float64) {
	fadeT = anim.Clamp01(fadeT)
	if fadeT <= 0 || next == nil {
		paintCover(d, img, stage)
		return
	}
	if fadeT >= 1 {
		paintCover(d, next, stage)
		return
	}
	if fadeT < 0.5 {
		paintCover(d, img, stage)
		anim.FadeOverlay(d, stage, fadeT*2)
		return
	}
	paintCover(d, next, stage)
	anim.FadeOverlay(d, stage, (1-fadeT)*2)
}

// AttractStageSample is a pixel inside the still stage, below the header.
func AttractStageSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	if width < 1 || height < 1 {
		return 0, 0, false
	}
	th = th.Complete()
	y = th.HeaderH + (height-th.HeaderH-th.FooterH)/2
	if y < th.HeaderH+1 {
		y = th.HeaderH + 1
	}
	if y >= height-th.FooterH {
		y = height / 2
	}
	x = width / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= width {
		x = width - 1
	}
	if y >= height {
		y = height - 1
	}
	return x, y, true
}

// AttractLetterboxSample is a pixel in the stage that stays AttractBackground
// around a wide still (top of the stage, just below the header).
func AttractLetterboxSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	if width < 1 || height < 1 {
		return 0, 0, false
	}
	th = th.Complete()
	x = 2
	y = th.HeaderH + 2
	if y >= height-th.FooterH {
		y = th.HeaderH
	}
	if y < 0 {
		y = 0
	}
	if y >= height {
		y = height - 1
	}
	return x, y, true
}

// AttractWallCell is the 2×2 cell rectangle for wall index 0..3.
func AttractWallCell(width, height, index int, th theme.Theme) gfx.Rect {
	th = th.Complete()
	if index < 0 {
		index = 0
	}
	if index > 3 {
		index = 3
	}
	stageY := th.HeaderH
	stageH := height - th.HeaderH - th.FooterH
	if stageH < 1 {
		stageY = 0
		stageH = height
	}
	const gap float32 = 6
	cellW := (float32(width) - gap) / 2
	cellH := (float32(stageH) - gap) / 2
	col := index % 2
	row := index / 2
	return gfx.Rect{
		X: float32(col) * (cellW + gap),
		Y: float32(stageY) + float32(row)*(cellH+gap),
		W: cellW,
		H: cellH,
	}
}

// AttractVideoBadgeSample is a pixel inside the VIDEO badge fill on a still stage.
func AttractVideoBadgeSample(width, height int, img *image.RGBA, th theme.Theme) (x, y int, ok bool) {
	return AttractVideoBadgeSampleFor(AttractFrame{Width: width, Height: height, Image: img, Theme: th})
}

// AttractVideoBadgeSampleFor is the VIDEO badge sample for a full attract frame.
func AttractVideoBadgeSampleFor(f AttractFrame) (x, y int, ok bool) {
	dest := AttractStillDestFor(f)
	if dest.W < 24 || dest.H < 12 {
		return 0, 0, false
	}
	return int(dest.X + 5), int(dest.Y + 5), true
}

// AttractWallBadgeSample is a pixel inside the VIDEO badge on wall cell 0.
func AttractWallBadgeSample(width, height int, th theme.Theme) (x, y int, ok bool) {
	r := AttractWallCell(width, height, 0, th)
	if r.W < 24 || r.H < 12 {
		return 0, 0, false
	}
	return int(r.X + 5), int(r.Y + 5), true
}

// AttractStillDest is the aspect-fit rectangle used for a still inside the stage.
func AttractStillDest(width, height int, img *image.RGBA, th theme.Theme) gfx.Rect {
	return AttractStillDestFor(AttractFrame{Width: width, Height: height, Image: img, Theme: th})
}

// AttractStillDestFor is the still dest after an optional marquee strip.
func AttractStillDestFor(f AttractFrame) gfx.Rect {
	th := f.Theme.Complete()
	stageY := th.HeaderH
	stageH := f.Height - th.HeaderH - th.FooterH
	if stageH < 1 {
		stageY = 0
		stageH = f.Height
	}
	if band := AttractMarqueeRect(f.Width, f.Height, f.Marquee, th); band.H >= 1 {
		stageY = int(band.Y + band.H + float32(attractMarqueeGap))
		stageH = f.Height - th.FooterH - stageY
		if stageH < 1 {
			stageH = 1
		}
	}
	if f.Image == nil {
		return gfx.Rect{X: 0, Y: float32(stageY), W: float32(f.Width), H: float32(stageH)}
	}
	b := f.Image.Bounds()
	dx, dy, dw, dh := tenfoot.CoverDestRect(0, stageY, f.Width, stageH, b.Dx(), b.Dy())
	return gfx.Rect{X: dx, Y: dy, W: dw, H: dh}
}

const (
	attractMarqueeMaxH = 72
	attractMarqueeGap  = 6
	attractMarqueeMinH = 24
)

// AttractMarqueeRect is the banner strip under the header, or empty when hidden.
func AttractMarqueeRect(width, height int, img *image.RGBA, th theme.Theme) gfx.Rect {
	if img == nil || width < 8 || height < 1 {
		return gfx.Rect{}
	}
	th = th.Complete()
	stageY := th.HeaderH
	stageH := height - th.HeaderH - th.FooterH
	if stageH < 1 {
		stageY = 0
		stageH = height
	}
	bandH := logoFitHeight(img, width, attractMarqueeMaxH)
	if bandH < 1 {
		return gfx.Rect{}
	}
	if bandH+attractMarqueeGap+attractMarqueeMinH > stageH {
		return gfx.Rect{}
	}
	return gfx.Rect{X: 0, Y: float32(stageY), W: float32(width), H: float32(bandH)}
}

// AttractMarqueeSample is a pixel inside a painted marquee strip.
func AttractMarqueeSample(width, height int, img *image.RGBA, th theme.Theme) (x, y int, ok bool) {
	r := AttractMarqueeRect(width, height, img, th)
	if r.W < 4 || r.H < 4 {
		return 0, 0, false
	}
	return int(r.X + r.W/2), int(r.Y + r.H/2), true
}
