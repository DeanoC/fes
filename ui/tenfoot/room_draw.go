package tenfoot

import (
	"fmt"
	"strings"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

const roomTexturePrefix = "room:"

// syncRoomTextures uploads the open room's decoded images and destroys
// textures for images the room no longer holds (or when the room closes).
// Room textures live in the shared map under roomTexturePrefix so the grid's
// prefetch reaping in syncTextures leaves them alone.
func syncRoomTextures(dev gfx.Device, snap Snapshot, textures map[string]gpuTexture) {
	needed := map[string]struct{}{}
	if snap.Room.Open {
		for key, img := range snap.Room.Images {
			if img == nil {
				continue
			}
			id := roomTexturePrefix + key
			needed[id] = struct{}{}
			if existing, ok := textures[id]; ok && existing.src == img {
				continue
			}
			if existing, ok := textures[id]; ok {
				dev.Destroy(existing.tex)
				delete(textures, id)
			}
			tex, err := uploadTexture(dev, img)
			if err != nil {
				continue
			}
			tex.src = img
			textures[id] = tex
		}
	}
	for id, item := range textures {
		if !strings.HasPrefix(id, roomTexturePrefix) {
			continue
		}
		if _, ok := needed[id]; ok {
			continue
		}
		dev.Destroy(item.tex)
		delete(textures, id)
	}
}

func intersectRect(a, b gfx.Rect) (gfx.Rect, bool) {
	x0 := max32(a.X, b.X)
	y0 := max32(a.Y, b.Y)
	x1 := min32(a.X+a.W, b.X+b.W)
	y1 := min32(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return gfx.Rect{}, false
	}
	return gfx.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, true
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// drawClipped blits src→dst limited to clip, cropping src proportionally.
func drawClipped(dev gfx.Device, tex gpuTexture, src *gfx.Rect, dst, clip gfx.Rect) {
	if dst.W <= 0 || dst.H <= 0 {
		return
	}
	visible, ok := intersectRect(dst, clip)
	if !ok {
		return
	}
	full := gfx.Rect{X: 0, Y: 0, W: float32(tex.w), H: float32(tex.h)}
	if src == nil {
		src = &full
	}
	if visible == dst {
		if *src == full {
			dev.Draw(tex.tex, nil, dst)
		} else {
			dev.Draw(tex.tex, src, dst)
		}
		return
	}
	sx := src.X + (visible.X-dst.X)/dst.W*src.W
	sy := src.Y + (visible.Y-dst.Y)/dst.H*src.H
	sw := visible.W / dst.W * src.W
	sh := visible.H / dst.H * src.H
	dev.Draw(tex.tex, &gfx.Rect{X: sx, Y: sy, W: sw, H: sh}, visible)
}

func roomTextKey(op rooms.Op) string {
	weight := "r"
	if op.Bold {
		weight = "b"
	}
	return fmt.Sprintf("room-text|%s|%d|%s|%02x%02x%02x%02x|%d", op.Text, op.Size, weight, op.Color.R, op.Color.G, op.Color.B, op.Color.A, op.MaxW)
}

func roomTextTexture(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, op rooms.Op) (gpuTexture, bool) {
	key := roomTextKey(op)
	used[key] = struct{}{}
	if tex, ok := labels[key]; ok {
		return tex, true
	}
	weight := gfx.WeightRegular
	if op.Bold {
		weight = gfx.WeightBold
	}
	img := gfx.RasterizeTextWeight(op.Text, op.Size, weight, op.Color, op.MaxW)
	if img == nil {
		delete(used, key)
		return gpuTexture{}, false
	}
	tex, err := uploadTexture(dev, img)
	if err != nil {
		delete(used, key)
		return gpuTexture{}, false
	}
	labels[key] = tex
	return tex, true
}

// drawRoom replays the room's display list inside the safe content area.
func drawRoom(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture, used map[string]struct{}) {
	th := drawTheme(snap)
	room := snap.Room
	clear := th.SofaBackground
	if room.Frame.HasClear {
		clear = room.Frame.Clear
	}
	dev.Clear(clear)
	if room.Err != "" {
		drawRoomError(dev, snap, labels, used)
		drawRoomChrome(dev, snap, labels, used)
		return
	}
	ox, oy := float32(room.OffsetX), float32(room.OffsetY)
	bounds := gfx.Rect{X: ox, Y: oy, W: float32(room.Width), H: float32(room.Height)}
	clips := []gfx.Rect{bounds}
	dev.SetBlend(gfx.BlendAlpha)
	for _, op := range room.Frame.Ops {
		clip := clips[len(clips)-1]
		switch op.Kind {
		case rooms.OpRect:
			if r, ok := intersectRect(gfx.Rect{X: ox + op.X, Y: oy + op.Y, W: op.W, H: op.H}, clip); ok {
				dev.FillRect(r, op.Color)
			}
		case rooms.OpImage:
			tex, ok := textures[roomTexturePrefix+op.Image]
			if !ok {
				continue
			}
			drawClipped(dev, tex, op.Src, gfx.Rect{X: ox + op.X, Y: oy + op.Y, W: op.W, H: op.H}, clip)
		case rooms.OpText:
			tex, ok := roomTextTexture(dev, labels, used, op)
			if !ok {
				continue
			}
			x := ox + op.X
			switch op.Align {
			case "center":
				if op.MaxW > 0 {
					x += (float32(op.MaxW) - float32(tex.w)) / 2
				} else {
					x -= float32(tex.w) / 2
				}
			case "right":
				if op.MaxW > 0 {
					x += float32(op.MaxW) - float32(tex.w)
				} else {
					x -= float32(tex.w)
				}
			}
			drawClipped(dev, tex, nil, gfx.Rect{X: x, Y: oy + op.Y, W: float32(tex.w), H: float32(tex.h)}, clip)
		case rooms.OpClipPush:
			next, ok := intersectRect(gfx.Rect{X: ox + op.X, Y: oy + op.Y, W: op.W, H: op.H}, clip)
			if !ok {
				next = gfx.Rect{}
			}
			clips = append(clips, next)
		case rooms.OpClipPop:
			if len(clips) > 1 {
				clips = clips[:len(clips)-1]
			}
		case rooms.OpBlend:
			dev.SetBlend(op.Blend)
		}
	}
	dev.SetBlend(gfx.BlendAlpha)
	drawRoomDestination(dev, snap, labels, used)
	drawRoomChrome(dev, snap, labels, used)
}

func drawRoomDestination(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	pane, ok := roomDestGeom(snap)
	if !ok {
		return
	}
	d := snap.Room.Destination
	x, y, w, h := pane.X, pane.Y, pane.W, pane.H
	fillRect(dev, float32(x), float32(y), float32(w), float32(h), 0, 0, 0, 170)
	th := drawTheme(snap)
	fillRect(dev, float32(x), float32(y), 6, float32(h), th.Highlight.R, th.Highlight.G, th.Highlight.B, th.Highlight.A)
	pad := 20
	title := strings.TrimSpace(d.Label)
	if title == "" {
		title = "Selected destination"
	}
	drawLabel(dev, labels, used, "rd-title", x+pad, y+12, w-2*pad, 24, title)
	meta := strings.ToUpper(strings.TrimSpace(d.System))
	if meta == "" {
		meta = strings.ToUpper(strings.TrimSpace(d.Platform))
	}
	switch d.Kind {
	case rooms.KindRoom:
		meta = "ROOM"
	case rooms.KindLibrary:
		meta = "LIBRARY"
	case rooms.KindUnresolved:
		meta = "UNRESOLVED"
	}
	if hist := strings.TrimSpace(d.History.Line()); hist != "" {
		if meta != "" {
			meta = meta + "  ·  " + hist
		} else {
			meta = hist
		}
	}
	if meta != "" {
		drawLabel(dev, labels, used, "rd-sys", x+pad, y+42, w-2*pad, 16, meta)
	}
	status := strings.TrimSpace(d.Status)
	if status == "" && d.Kind == rooms.KindUnresolved {
		status = "Matching this location…"
	}
	if status != "" {
		drawLabel(dev, labels, used, "rd-status", x+pad, y+66, w-2*pad, 16, status)
	}
	action := strings.TrimSpace(d.Action)
	if action != "" {
		drawLabel(dev, labels, used, "rd-action", x+pad, y+88, w-2*pad, 16, action)
	}
}

func drawRoomChoice(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	panel, ok := roomChoicePanel(snap)
	if !ok {
		return
	}
	rows := snap.Room.Choice.Rows
	x, y, panelW, panelH := panel.X, panel.Y, panel.W, panel.H
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	drawLabel(dev, labels, used, "rchoice-title", x+16, y+12, panelW-32, 18, "Choose an edition")
	for i := 0; i < panel.Visible; i++ {
		idx := panel.Start + i
		if idx >= len(rows) {
			break
		}
		rowY := panel.rowY(i)
		if idx == snap.Room.Choice.Index {
			fillRect(dev, float32(x+8), float32(rowY), float32(panelW-16), float32(panel.RowH-4), 48, 56, 80, 255)
		}
		label := strings.TrimSpace(rows[idx].Title)
		if label == "" {
			label = rows[idx].ID
		}
		sys := strings.ToUpper(strings.TrimSpace(rows[idx].System))
		if sys != "" {
			label = label + "  ·  " + sys
		}
		if !rows[idx].LaunchEligible() {
			label = label + "  (unavailable)"
		}
		drawLabel(dev, labels, used, fmt.Sprintf("rchoice-%d", idx), x+20, rowY+8, panelW-40, 16, label)
	}
	hint := strings.TrimSpace(snap.Room.Choice.Hint)
	if hint == "" {
		hint = roomChoiceHint(snap.Affinity)
	}
	drawLabel(dev, labels, used, "rchoice-hint", x+16, y+panelH-24, panelW-32, 14, hint)
}

// drawRoomChrome keeps host/kit health, launch progress and the input hint
// visible over any room without the library header.
func drawRoomChrome(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	g := snap.Grid
	x := g.contentLeft()
	w := g.contentWidth()
	y := g.footerY() - 22
	line := strings.TrimSpace(snap.Health.Line)
	if lease := strings.TrimSpace(snap.KitLease.Line); lease != "" {
		if line != "" {
			line += "  ·  "
		}
		line += lease
	}
	if !launchOverlayVisible(snap) {
		switch snap.Launch.Phase {
		case "launching", "error", "host":
			if msg := strings.TrimSpace(snap.Launch.Message); msg != "" {
				line = msg
			}
		}
	}
	if strings.TrimSpace(snap.Status) != "" && strings.HasPrefix(snap.Status, "room failed") {
		line = snap.Status
	}
	if line != "" {
		drawLabel(dev, labels, used, "room-status", x+8, y, w*2/3, 14, line)
	}
	hint := snap.HeaderHint()
	drawDebug(dev, x+w-8*len(hint)-8, y+4, hint, 1)
}

func drawLaunchOverlay(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	panel, ok := launchOverlayPanel(snap)
	if !ok {
		return
	}
	copy := launchOverlayCopy(snap)
	x, y, panelW, panelH := panel.X, panel.Y, panel.W, panel.H
	borderR, borderG, borderB := uint8(255), uint8(184), uint8(48)
	if copy.Failed {
		borderR, borderG, borderB = 200, 64, 48
	}
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), borderR, borderG, borderB, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	drawLabel(dev, labels, used, "launch-ov-head", x+20, y+16, panelW-40, 20, copy.Heading)
	if copy.Title != "" {
		drawLabel(dev, labels, used, "launch-ov-title", x+20, y+44, panelW-40, 18, copy.Title)
	}
	bodyY := y + 72
	if phase := strings.TrimSpace(copy.Phase); phase != "" && phase != copy.Title {
		drawLabel(dev, labels, used, "launch-ov-phase", x+20, bodyY, panelW-40, 16, phase)
		bodyY += 24
	}
	if reason := strings.TrimSpace(copy.Reason); reason != "" && reason != copy.Phase {
		drawLabel(dev, labels, used, "launch-ov-reason", x+20, bodyY, panelW-40, 16, reason)
	}
	hint := strings.TrimSpace(copy.Hint)
	if hint == "" {
		if copy.Failed {
			hint = launchOverlayFailHint(snap.Affinity)
		} else {
			hint = launchOverlayBusyHint(snap.Affinity)
		}
	}
	drawLabel(dev, labels, used, "launch-ov-hint", x+20, y+panelH-28, panelW-40, 14, hint)
}

func drawRoomError(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	g := snap.Grid
	contentW := g.contentWidth()
	panelW := 760
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	lines := gfx.WrapText(snap.Room.Err, 16, panelW-40, 8)
	panelH := 64 + len(lines)*22 + 40
	x := g.contentLeft() + (contentW-panelW)/2
	y := g.contentTop() + (g.contentHeight()-panelH)/2
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 200, 64, 48, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	drawLabel(dev, labels, used, "room-err-title", x+20, y+14, panelW-40, 20, "Room failed: "+snap.Room.Title)
	for i, line := range lines {
		drawLabel(dev, labels, used, fmt.Sprintf("room-err-%d", i), x+20, y+52+i*22, panelW-40, 16, line)
	}
	drawLabel(dev, labels, used, "room-err-hint", x+20, y+panelH-26, panelW-40, 14, "Fix the script and reopen the room. "+backWord(snap.Affinity)+" returns home.")
}

func drawRoomPicker(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	panel, ok := roomPickerPanel(snap)
	if !ok {
		return
	}
	rows := snap.RoomPicker.Rows
	x, y, panelW, panelH := panel.X, panel.Y, panel.W, panel.H
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	drawLabel(dev, labels, used, "rooms-title", x+20, y+14, panelW-40, 22, "Rooms")
	for i := 0; i < panel.Visible; i++ {
		idx := panel.Start + i
		if idx >= len(rows) {
			break
		}
		row := rows[idx]
		rowY := panel.rowY(i)
		if idx == snap.RoomPicker.Index {
			fillRect(dev, float32(x+8), float32(rowY), float32(panelW-16), float32(panel.RowH-4), 48, 56, 80, 255)
		}
		label := row.Label
		if row.Invalid {
			label = label + "  (unavailable)"
		}
		if snap.Room.Open && !row.Library && row.ID == snap.Room.ID {
			label = label + "  *"
		}
		drawLabel(dev, labels, used, fmt.Sprintf("rooms-%d", idx), x+24, rowY+4, panelW-48, 18, label)
		if detail := strings.TrimSpace(row.Detail); detail != "" {
			drawLabel(dev, labels, used, fmt.Sprintf("rooms-%d-detail", idx), x+24, rowY+26, panelW-48, 13, detail)
		}
	}
	drawLabel(dev, labels, used, "rooms-hint", x+20, y+panelH-22, panelW-40, 14, roomPickerHint(snap.Affinity))
}
