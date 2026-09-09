package fbgrid

import (
	"strings"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

const (
	// defaultVignetteInset is how far the soft edge darken reaches into the
	// stage. It stays inside the theme pad so tiles, focus rings, and
	// covers keep their existing samples.
	defaultVignetteInset = 12
	// SessionScrimA is the theme-background overlay when a host session is
	// live. ~59% scrim, ~41% browse.
	SessionScrimA uint8 = 150
	// SessionKitHint is the kit stop chord. East/B and Start stay game
	// controls while a session can stop.
	SessionKitHint = "Select+Start stop"
	// SessionKitRetryHint is the failed-session recovery chord.
	SessionKitRetryHint = "Select+Start retry Stop"
)

// SessionChrome is pause / session overlay state painted over browse or
// detail. Empty State hides the overlay.
type SessionChrome struct {
	State string
	Title string
	Hint  string
}

// SessionLive reports whether chrome should paint a pause overlay.
func SessionLive(s SessionChrome) bool {
	return SessionBadge(s.State) != ""
}

// SessionBadge is the living-room label for a host session state.
func SessionBadge(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "active":
		return "Paused"
	case "launching":
		return "Starting"
	case "stopping":
		return "Stopping"
	case "failed":
		return "Retry Stop"
	default:
		return ""
	}
}

// VignetteInset is the stage edge width that receives the soft vignette.
func VignetteInset(th theme.Theme) int {
	th = th.Complete()
	if th.VignetteA <= 0 {
		return 0
	}
	return defaultVignetteInset
}

// ChromeInset is the pad that bezel and vignette occupy at the stage edge.
func ChromeInset(th theme.Theme) int {
	th = th.Complete()
	inset := VignetteInset(th)
	if th.BezelWidth > inset {
		return th.BezelWidth
	}
	return inset
}

// VignetteAlphaAt is the vignette alpha at a stage pixel. Header and footer
// bars stay unvignetted so chrome samples keep today's colours.
func VignetteAlphaAt(x, y, w, h, headerH, footerH int, th theme.Theme) uint8 {
	th = th.Complete()
	inset := VignetteInset(th)
	if inset < 1 || w < 1 || h < 1 {
		return 0
	}
	y0, y1 := headerH, h-footerH
	if y0 < 0 {
		y0 = 0
	}
	if y1 > h {
		y1 = h
	}
	if y1 <= y0 {
		y0, y1 = 0, h
	}
	if y < y0 || y >= y1 {
		return 0
	}
	dx := x
	if w-1-x < dx {
		dx = w - 1 - x
	}
	dy := y - y0
	if y1-1-y < dy {
		dy = y1 - 1 - y
	}
	dist := dx
	if dy < dist {
		dist = dy
	}
	if dist < 0 || dist >= inset {
		return 0
	}
	return vignetteBandAlpha(dist, inset, th.VignetteA)
}

func vignetteBandAlpha(dist, inset, peak int) uint8 {
	if inset < 1 || peak < 1 || dist < 0 || dist >= inset {
		return 0
	}
	return uint8((peak*(inset-dist) + inset/2) / inset)
}

// VignetteDim is src after the vignette colour at alpha a.
func VignetteDim(src, vignette gfx.Color, a uint8) gfx.Color {
	if a == 0 {
		return src
	}
	va := uint32(a)
	inv := uint32(255 - a)
	return gfx.RGB(
		uint8((uint32(vignette.R)*va+uint32(src.R)*inv)/255),
		uint8((uint32(vignette.G)*va+uint32(src.G)*inv)/255),
		uint8((uint32(vignette.B)*va+uint32(src.B)*inv)/255),
	)
}

// SessionDim is src after the pause scrim.
func SessionDim(src gfx.Color, th theme.Theme) gfx.Color {
	th = th.Complete()
	a := uint32(SessionScrimA)
	inv := uint32(255 - SessionScrimA)
	scrim := th.Background
	return gfx.RGB(
		uint8((uint32(scrim.R)*a+uint32(src.R)*inv)/255),
		uint8((uint32(scrim.G)*a+uint32(src.G)*inv)/255),
		uint8((uint32(scrim.B)*a+uint32(src.B)*inv)/255),
	)
}

// VignetteSample is a pad pixel inside the vignette band, below the header.
func VignetteSample(w, h, headerH, footerH int, th theme.Theme) (x, y int, ok bool) {
	if w < 1 || h < 1 {
		return 0, 0, false
	}
	th = th.Complete()
	inset := VignetteInset(th)
	if inset < 1 {
		return 0, 0, false
	}
	x = 2
	if x >= inset {
		x = inset / 2
	}
	y = headerH + 2
	if y < 0 {
		y = 0
	}
	if y >= h-footerH {
		y = headerH
	}
	if y < 0 || y >= h || x < 0 || x >= w {
		return 0, 0, false
	}
	if VignetteAlphaAt(x, y, w, h, headerH, footerH, th) == 0 {
		return 0, 0, false
	}
	return x, y, true
}

// BezelSample is a pixel on the outer frame. Missing when BezelWidth is 0.
func BezelSample(w, h int, th theme.Theme) (x, y int, ok bool) {
	if w < 1 || h < 1 {
		return 0, 0, false
	}
	th = th.Complete()
	if th.BezelWidth < 1 {
		return 0, 0, false
	}
	return 0, 0, true
}

// StagePadSample is a pad pixel in the stage, past the vignette and bezel,
// not on a tile or chrome bar.
func StagePadSample(w, h, headerH, footerH int, th theme.Theme) (x, y int, ok bool) {
	if w < 1 || h < 1 {
		return 0, 0, false
	}
	th = th.Complete()
	inset := ChromeInset(th)
	x = inset + 2
	y = headerH + inset + 2
	if y < 0 {
		y = 0
	}
	if y >= h {
		y = h / 2
	}
	if x >= w {
		x = 0
	}
	if y >= h-footerH && footerH > 0 {
		y = headerH + 2
		if y >= h {
			y = h / 2
		}
	}
	return x, y, true
}

// SessionPanelRect is the centered pause badge panel.
func SessionPanelRect(w, h int, th theme.Theme) gfx.Rect {
	th = th.Complete()
	if w < 1 || h < 1 {
		return gfx.Rect{}
	}
	headerH := th.HeaderH
	footerH := th.FooterH
	if headerH < 0 {
		headerH = 0
	}
	if footerH < 0 {
		footerH = 0
	}
	titleH := gfx.TextHeightWeight(th.TitlePx(), th.TitleWeight())
	bodyH := gfx.TextHeightWeight(th.BodyPx(), th.BodyWeight())
	capH := gfx.TextHeightWeight(th.CaptionPx(), th.CaptionWeight())
	pad := 16
	panelH := titleH + bodyH + capH + pad*2 + 12
	panelW := w - 2*th.Pad
	if panelW > 420 {
		panelW = 420
	}
	if panelW < 160 {
		panelW = w - 16
	}
	if panelW < 1 {
		panelW = w
	}
	if panelH < 48 {
		panelH = 48
	}
	stageTop := headerH
	stageH := h - headerH - footerH
	if stageH < panelH {
		stageTop = 0
		stageH = h
	}
	x := (w - panelW) / 2
	if x < 0 {
		x = 0
	}
	y := stageTop + (stageH-panelH)/2
	if y < stageTop {
		y = stageTop
	}
	return gfx.Rect{X: float32(x), Y: float32(y), W: float32(panelW), H: float32(panelH)}
}

// SessionBadgeSample is a pixel in the pause panel fill, away from the ring.
func SessionBadgeSample(w, h int, th theme.Theme) (x, y int, ok bool) {
	r := SessionPanelRect(w, h, th)
	if r.W < 8 || r.H < 8 {
		return 0, 0, false
	}
	return int(r.X) + 1, int(r.Y) + 1, true
}

func paintLivingRoomChrome(d gfx.Device, w, h, headerH, footerH int, th theme.Theme, session SessionChrome) {
	if d == nil || w < 1 || h < 1 {
		return
	}
	th = th.Complete()
	paintVignette(d, w, h, headerH, footerH, th)
	paintBezel(d, w, h, th)
	paintSessionChrome(d, w, h, th, session)
}

func paintVignette(d gfx.Device, w, h, headerH, footerH int, th theme.Theme) {
	if d == nil {
		return
	}
	th = th.Complete()
	inset := VignetteInset(th)
	if inset < 1 || th.VignetteA <= 0 {
		return
	}
	y0 := headerH
	y1 := h - footerH
	if y0 < 0 {
		y0 = 0
	}
	if y1 > h {
		y1 = h
	}
	if y1-y0 < 2*inset+2 {
		return
	}
	d.SetBlend(gfx.BlendAlpha)
	c := th.Vignette
	innerH := y1 - y0 - 2*inset
	if innerH < 1 {
		innerH = 1
	}
	for i := 0; i < inset; i++ {
		a := vignetteBandAlpha(i, inset, th.VignetteA)
		if a == 0 {
			continue
		}
		col := gfx.RGBA(c.R, c.G, c.B, a)
		d.FillRect(gfx.Rect{X: 0, Y: float32(y0 + i), W: float32(w), H: 1}, col)
		d.FillRect(gfx.Rect{X: 0, Y: float32(y1 - 1 - i), W: float32(w), H: 1}, col)
		d.FillRect(gfx.Rect{X: float32(i), Y: float32(y0 + inset), W: 1, H: float32(innerH)}, col)
		d.FillRect(gfx.Rect{X: float32(w - 1 - i), Y: float32(y0 + inset), W: 1, H: float32(innerH)}, col)
	}
	d.SetBlend(gfx.BlendNone)
}

func paintBezel(d gfx.Device, w, h int, th theme.Theme) {
	if d == nil || w < 1 || h < 1 {
		return
	}
	th = th.Complete()
	if th.BezelWidth < 1 {
		return
	}
	c := th.Bezel
	if c.A == 0 {
		c = th.Highlight
	}
	paintRectOutline(d, gfx.Rect{X: 0, Y: 0, W: float32(w), H: float32(h)}, float32(th.BezelWidth), c)
}

func paintSessionChrome(d gfx.Device, w, h int, th theme.Theme, session SessionChrome) {
	if d == nil || !SessionLive(session) || w < 1 || h < 1 {
		return
	}
	th = th.Complete()
	badge := SessionBadge(session.State)
	title := strings.TrimSpace(session.Title)
	hint := strings.TrimSpace(session.Hint)
	if hint == "" {
		if session.State == "failed" {
			hint = SessionKitRetryHint
		} else {
			hint = SessionKitHint
		}
	}
	scrim := th.Background
	scrim.A = SessionScrimA
	d.SetBlend(gfx.BlendAlpha)
	d.FillRect(gfx.Rect{X: 0, Y: 0, W: float32(w), H: float32(h)}, scrim)
	d.SetBlend(gfx.BlendNone)

	panel := SessionPanelRect(w, h, th)
	if panel.W < 8 || panel.H < 8 {
		return
	}
	d.FillRect(panel, th.Highlight)
	inset := float32(th.Border)
	if inset < 2 {
		inset = 2
	}
	inner := gfx.Rect{
		X: panel.X + inset,
		Y: panel.Y + inset,
		W: panel.W - 2*inset,
		H: panel.H - 2*inset,
	}
	if inner.W < 1 {
		inner.W = 1
	}
	if inner.H < 1 {
		inner.H = 1
	}
	d.FillRect(inner, th.LabelBar)

	textX := int(inner.X) + 12
	maxW := int(inner.W) - 24
	if maxW < 1 {
		maxW = 1
	}
	titleSize := th.TitlePx()
	titleW := th.TitleWeight()
	bodySize := th.BodyPx()
	bodyW := th.BodyWeight()
	capSize := th.CaptionPx()
	capW := th.CaptionWeight()
	y := int(inner.Y) + 10
	badge = gfx.FitTextWeight(badge, titleSize, maxW, titleW)
	d.DrawTextWeight(textX, y, badge, titleSize, titleW, th.Header)
	y += gfx.TextHeightWeight(titleSize, titleW) + 6
	if title != "" {
		title = gfx.FitTextWeight(title, bodySize, maxW, bodyW)
		d.DrawTextWeight(textX, y, title, bodySize, bodyW, th.Label)
		y += gfx.TextHeightWeight(bodySize, bodyW) + 6
	}
	if hint != "" {
		hint = gfx.FitTextWeight(hint, capSize, maxW, capW)
		d.DrawTextWeight(textX, y, hint, capSize, capW, th.Status)
	}
}
