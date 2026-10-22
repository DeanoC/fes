package tenfoot

import (
	"image"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
)

// SurroundSnapshot is the kit HDMI frame drawn around a running machine
// while it keeps input. It is session-plane content: alpha 0 over the
// machine picture, opaque cabinet art around it.
type SurroundSnapshot struct {
	Active bool
	// Picture is where the shell places the machine picture in the plane.
	// It is empty until the runtime reports it, and the frame is then fully
	// transparent.
	Picture image.Rectangle
}

// surroundShownLocked draws the surround while the machine has input on a
// shown plane. Stop and a failed Stop's retry draw the parked status instead.
func (a *App) surroundShownLocked() bool {
	return a.menuDisplay != nil && a.gpuParked && a.sessionDisplay == protocol.SessionDisplayShown &&
		a.stopPhase != "stopping" && !a.retryStopLock
}

// roomOverlayLocked is true while rooms are drawn over the running machine
// on the kit's session plane: the plane holds launcher focus only while the
// hardware room is open in play, and it is confirmed before the room loads,
// so load and on_resume see the flag too. Host windows and an uncertain plane
// draw rooms opaque.
func (a *App) roomOverlayLocked() bool {
	return a.menuDisplay != nil && a.session.State == "active" &&
		a.sessionDisplay == protocol.SessionDisplayFocused && !a.sessionDisplayUncertain
}

const (
	surroundBezel      = 28
	surroundCorner     = 18
	surroundShade      = 8
	surroundPanelInset = 40
	// A side narrower than this carries no labels.
	surroundPanelMin = 160
)

func drawSurround(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture) {
	dev.BeginFrame()
	used := map[string]struct{}{}
	frame := image.Rect(0, 0, menudisplay.Width, menudisplay.Height)
	picture := snap.Surround.Picture
	dev.SetBlend(gfx.BlendNone)
	// Every frame starts with a clear so FrameCache can reuse an unchanged one.
	dev.Clear(gfx.RGBA(0, 0, 0, 0))
	if !picture.Empty() && picture.In(frame) && picture != frame {
		drawSurroundCabinet(dev, frame, picture, drawTheme(snap).SofaBackground)
		drawSurroundLabels(dev, snap, labels, used, frame, picture)
	}
	for key, item := range labels {
		if _, ok := used[key]; !ok {
			dev.Destroy(item.tex)
			delete(labels, key)
		}
	}
	dev.Present()
}

// drawSurroundCabinet paints an opaque cabinet with a rounded bezel, then
// cuts the machine picture out and shades its edges like a CRT tube.
func drawSurroundCabinet(dev gfx.Device, frame, picture image.Rectangle, base gfx.Color) {
	const bands = 24
	top, bottom := lighten(base, 26), lighten(base, 6)
	for i := 0; i < bands; i++ {
		y0 := frame.Min.Y + frame.Dy()*i/bands
		y1 := frame.Min.Y + frame.Dy()*(i+1)/bands
		fillRectI(dev, image.Rect(frame.Min.X, y0, frame.Max.X, y1), mix(top, bottom, i, bands-1))
	}
	bezel := picture.Inset(-surroundBezel).Intersect(frame)
	fillRoundRect(dev, bezel, surroundCorner, gfx.RGB(8, 8, 10))
	fillRectI(dev, image.Rect(bezel.Min.X+surroundCorner, bezel.Min.Y, bezel.Max.X-surroundCorner, bezel.Min.Y+2), lighten(base, 34))
	fillRectI(dev, picture, gfx.RGBA(0, 0, 0, 0))
	dev.SetBlend(gfx.BlendAlpha)
	for i := 0; i < surroundShade; i++ {
		shade := gfx.RGBA(0, 0, 0, uint8(112-i*14))
		edge := picture.Inset(i)
		fillRectI(dev, image.Rect(edge.Min.X, edge.Min.Y, edge.Max.X, edge.Min.Y+1), shade)
		fillRectI(dev, image.Rect(edge.Min.X, edge.Max.Y-1, edge.Max.X, edge.Max.Y), shade)
		fillRectI(dev, image.Rect(edge.Min.X, edge.Min.Y+1, edge.Min.X+1, edge.Max.Y-1), shade)
		fillRectI(dev, image.Rect(edge.Max.X-1, edge.Min.Y+1, edge.Max.X, edge.Max.Y-1), shade)
	}
	dev.SetBlend(gfx.BlendNone)
}

func drawSurroundLabels(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, frame, picture image.Rectangle) {
	left := image.Rect(frame.Min.X+surroundPanelInset, picture.Min.Y, picture.Min.X-surroundBezel-surroundPanelInset, picture.Max.Y)
	if left.Dx() >= surroundPanelMin {
		title := snap.Session.Title
		if title == "" {
			title = snap.Session.GameID
		}
		drawLabel(dev, labels, used, "surround-title", left.Min.X, left.Min.Y, left.Dx(), 28, title)
		drawLabel(dev, labels, used, "surround-system", left.Min.X, left.Min.Y+40, left.Dx(), 18, snap.Session.System)
	}
	right := image.Rect(picture.Max.X+surroundBezel+surroundPanelInset, picture.Min.Y, frame.Max.X-surroundPanelInset, picture.Max.Y)
	if right.Dx() >= surroundPanelMin && snap.Session.HardwareRoom {
		drawLabel(dev, labels, used, "surround-hint", right.Min.X, right.Max.Y-24, right.Dx(), 16, "Hardware room · "+hardwareRoomWord(snap.Affinity))
	}
}

func fillRectI(dev gfx.Device, r image.Rectangle, c gfx.Color) {
	if !r.Empty() {
		dev.FillRect(gfx.Rect{X: float32(r.Min.X), Y: float32(r.Min.Y), W: float32(r.Dx()), H: float32(r.Dy())}, c)
	}
}

// fillRoundRect fills r with opaque corners of the given radius, one span per
// corner row.
func fillRoundRect(dev gfx.Device, r image.Rectangle, radius int, c gfx.Color) {
	radius = min(radius, r.Dx()/2, r.Dy()/2)
	fillRectI(dev, image.Rect(r.Min.X, r.Min.Y+radius, r.Max.X, r.Max.Y-radius), c)
	for row := 0; row < radius; row++ {
		dy := radius - row
		inset := radius - isqrt(radius*radius-dy*dy)
		fillRectI(dev, image.Rect(r.Min.X+inset, r.Min.Y+row, r.Max.X-inset, r.Min.Y+row+1), c)
		fillRectI(dev, image.Rect(r.Min.X+inset, r.Max.Y-row-1, r.Max.X-inset, r.Max.Y-row), c)
	}
}

func isqrt(n int) int {
	x := 0
	for (x+1)*(x+1) <= n {
		x++
	}
	return x
}

func lighten(c gfx.Color, by uint8) gfx.Color {
	add := func(v uint8) uint8 { return uint8(min(255, int(v)+int(by))) }
	return gfx.RGB(add(c.R), add(c.G), add(c.B))
}

func mix(a, b gfx.Color, i, n int) gfx.Color {
	if n <= 0 {
		return a
	}
	lerp := func(x, y uint8) uint8 { return uint8((int(x)*(n-i) + int(y)*i) / n) }
	return gfx.RGB(lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B))
}
