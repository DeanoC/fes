package tenfoot

import (
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

// gpuTexture is a Device-backed bitmap plus the CPU source used to decide
// when to upload again. Keys "preview" and "attract" are park/teardown exceptions.
type gpuTexture struct {
	tex gfx.Texture
	w   int
	h   int
	src *image.RGBA
	seq int
}

func presentFrame(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture, parked bool) bool {
	if snap.Attract.Active {
		for id, item := range textures {
			if id == "attract" {
				continue
			}
			dev.Destroy(item.tex)
			delete(textures, id)
		}
		drawAttract(dev, snap, textures, labels)
		return parked
	}
	if item, ok := textures["attract"]; ok {
		dev.Destroy(item.tex)
		delete(textures, "attract")
	}
	if !snap.GPUParked {
		if item, ok := textures["preview"]; ok {
			dev.Destroy(item.tex)
			delete(textures, "preview")
		}
	}
	return applyGPUPark(dev, snap, textures, labels, parked)
}

func applyGPUPark(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture, parked bool) bool {
	if snap.GPUParked {
		for id, item := range textures {
			if id == "preview" {
				continue
			}
			dev.Destroy(item.tex)
			delete(textures, id)
		}
		if !parked {
			destroyTextures(dev, labels)
		}
		drawNowPlaying(dev, snap, textures, labels)
		return true
	}
	syncTextures(dev, snap, textures)
	drawFrame(dev, snap, textures, labels)
	return false
}

func syncTextures(dev gfx.Device, snap Snapshot, textures map[string]gpuTexture) {
	start, end := snap.Grid.PrefetchRange(prefetchRows)
	needed := map[string]struct{}{}
	for i := start; i < end && i < len(snap.Games); i++ {
		id := snap.Games[i].ID
		img := snap.Covers[id]
		if img == nil {
			continue
		}
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
			fmt.Fprintf(os.Stderr, "tenfoot: cover texture %s: %v\n", id, err)
			continue
		}
		tex.src = img
		textures[id] = tex
	}
	for handle, img := range snap.Screenshots {
		if img == nil {
			continue
		}
		id := screenshotWorkKey(handle)
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
			fmt.Fprintf(os.Stderr, "tenfoot: screenshot texture %s: %v\n", handle, err)
			continue
		}
		tex.src = img
		textures[id] = tex
	}
	for id, item := range textures {
		if id == "attract" || id == "preview" {
			continue
		}
		if _, ok := needed[id]; ok {
			continue
		}
		dev.Destroy(item.tex)
		delete(textures, id)
	}
}

func uploadTexture(dev gfx.Device, img *image.RGBA) (gpuTexture, error) {
	tex, err := dev.CreateRGBA(img)
	if err != nil {
		return gpuTexture{}, err
	}
	return gpuTexture{tex: tex, w: tex.Width(), h: tex.Height()}, nil
}

func drawGPU(dev gfx.Device, tex gpuTexture, x, y, w, h float32) {
	dev.Draw(tex.tex, nil, gfx.Rect{X: x, Y: y, W: w, H: h})
}

func drawFrame(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture) {
	dev.BeginFrame()
	dev.Clear(gfx.RGB(12, 14, 20))
	used := map[string]struct{}{}
	drawHeader(dev, snap, labels, used)
	if snap.Grid.Mode == LayoutList {
		drawListRows(dev, snap, textures, labels, used)
	} else {
		drawCoverCells(dev, snap, textures, labels, used)
	}
	drawDetail(dev, snap, labels, used, textures)
	drawViewPicker(dev, snap, labels, used)
	drawCollectionMenu(dev, snap, labels, used)
	drawSettings(dev, snap, labels, used)
	drawFilters(dev, snap, labels, used)
	drawOSK(dev, snap, labels, used)
	for key, item := range labels {
		if _, ok := used[key]; ok {
			continue
		}
		dev.Destroy(item.tex)
		delete(labels, key)
	}
	dev.Present()
}

func drawCoverCells(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture, used map[string]struct{}) {
	start, end := snap.Grid.VisibleRange()
	for i := start; i < end && i < len(snap.Games); i++ {
		x, y, ok := snap.Grid.CellOrigin(i)
		if !ok {
			continue
		}
		game := snap.Games[i]
		focused := i == snap.Grid.Focus
		if focused {
			fillRect(dev, float32(x-4), float32(y-4), float32(snap.Grid.CellW+8), float32(snap.Grid.CellH+8), 255, 184, 48, 255)
		}
		fillRect(dev, float32(x), float32(y), float32(snap.Grid.CellW), float32(snap.Grid.CellH-36), 28, 32, 44, 255)
		if tex, ok := textures[game.ID]; ok {
			dx, dy, dw, dh := coverDestRect(x, y, snap.Grid.CellW, snap.Grid.CellH-36, tex.w, tex.h)
			drawGPU(dev, tex, dx, dy, dw, dh)
		} else {
			r, g, b := placeholderColor(game.Title)
			fillRect(dev, float32(x+8), float32(y+8), float32(snap.Grid.CellW-16), float32(snap.Grid.CellH-52), r, g, b, 255)
			drawLabel(dev, labels, used, "i:"+game.ID, x+16, y+24, snap.Grid.CellW-32, 22, initials(game.Title))
		}
		drawLabel(dev, labels, used, "t:"+game.ID, x+6, y+snap.Grid.CellH-28, snap.Grid.CellW-12, 16, game.Title)
	}
}

func drawListRows(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture, used map[string]struct{}) {
	start, end := snap.Grid.VisibleRange()
	for i := start; i < end && i < len(snap.Games); i++ {
		x, y, ok := snap.Grid.CellOrigin(i)
		if !ok {
			continue
		}
		game := snap.Games[i]
		focused := i == snap.Grid.Focus
		if focused {
			fillRect(dev, float32(x-4), float32(y-2), float32(snap.Grid.CellW+8), float32(snap.Grid.CellH+4), 255, 184, 48, 255)
		}
		fillRect(dev, float32(x), float32(y), float32(snap.Grid.CellW), float32(snap.Grid.CellH), 28, 32, 44, 255)
		tx, ty, tw, th := snap.Grid.listThumbRect(x, y)
		fillRect(dev, float32(tx), float32(ty), float32(tw), float32(th), 18, 20, 28, 255)
		if tex, ok := textures[game.ID]; ok {
			dx, dy, dw, dh := coverDestRect(tx, ty, tw, th, tex.w, tex.h)
			drawGPU(dev, tex, dx, dy, dw, dh)
		} else {
			r, g, b := placeholderColor(game.Title)
			fillRect(dev, float32(tx+2), float32(ty+2), float32(tw-4), float32(th-4), r, g, b, 255)
		}
		textX := tx + tw + 16
		textW := x + snap.Grid.CellW - textX - 12
		if textW < 1 {
			textW = 1
		}
		title := game.Title
		if game.Favorite {
			title = "* " + title
		}
		drawLabel(dev, labels, used, "lt:"+game.ID, textX, y+12, textW, 22, title)
		meta := strings.TrimSpace(game.System)
		if year := strings.TrimSpace(game.Year); year != "" {
			if meta != "" {
				meta += "  ·  " + year
			} else {
				meta = year
			}
		}
		drawLabel(dev, labels, used, "lm:"+game.ID, textX, y+40, textW, 16, meta)
	}
}

func drawLabel(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, key string, x, y, maxW, sizePx int, text string) {
	text = strings.TrimSpace(text)
	if text == "" || maxW < 1 {
		return
	}
	key = labelCacheKey(key, text, maxW, sizePx)
	used[key] = struct{}{}
	tex, ok := labels[key]
	if !ok {
		img := rasterizeLabel(text, maxW, sizePx)
		if img == nil {
			delete(used, key)
			return
		}
		uploaded, err := uploadTexture(dev, img)
		if err != nil {
			delete(used, key)
			return
		}
		tex = uploaded
		labels[key] = tex
	}
	drawGPU(dev, tex, float32(x), float32(y), float32(tex.w), float32(tex.h))
}

func drawHeader(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	x := snap.Grid.contentLeft()
	y := snap.Grid.headerY()
	w := snap.Grid.contentWidth()
	fillRect(dev, float32(x), float32(y), float32(w), float32(snap.Grid.HeaderHeight), 18, 20, 28, 255)
	drawDebug(dev, x+24, y+18, "FOGCAST", 3)
	pad := "KB DEBUG"
	if snap.Gamepads > 0 {
		pad = fmt.Sprintf("PAD %d", snap.Gamepads)
	}
	drawDebug(dev, x+w-160, y+22, pad, 2)
	chrome := snap.ChromeLine()
	drawLabel(dev, labels, used, "chrome", x+24, y+52, w-48, 18, chrome)
	hint := "LB/RB platform  X sort  hold X filter  Y search  hold A view  hold Y fav  SELECT layout  GUIDE settings"
	if snap.GPUParked || snap.Session.State == "active" || snap.Session.RetryStop {
		if snap.Session.RetryStop {
			hint = "B retry Stop  START quit  SELECT layout"
		} else {
			hint = "B stop  START quit  SELECT layout"
		}
		if h := strings.TrimSpace(snap.Session.InputHint); h != "" {
			hint += "  " + h
		} else if !snap.Session.Diagnostic && snap.Session.InputState != "" {
			hint += "  X attach/detach"
		}
	} else if snap.OSK.Open {
		hint = snap.OSK.Hint
	} else if snap.Filters.Open {
		hint = snap.Filters.Hint
	} else if snap.CollectionMenu.Open {
		hint = snap.CollectionMenu.Hint
	} else if snap.ViewPicker {
		hint = "A open  X add/remove  Y manage  B back"
	} else if snap.Detail.Open {
		hint = snap.Detail.Hint
	}
	drawDebug(dev, x+24, y+72, hint, 1)
}

func drawSessionPreview(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture, used map[string]struct{}, x, y, maxW int) int {
	show := snap.Preview.Live || snap.Preview.Image != nil
	if !show {
		if item, ok := textures["preview"]; ok {
			dev.Destroy(item.tex)
			delete(textures, "preview")
		}
		return y
	}
	label := strings.TrimSpace(snap.Preview.Label)
	if label == "" {
		label = previewLabel
	}
	drawLabel(dev, labels, used, "np-preview-label", x, y, maxW, 16, label)
	y += 24
	img := snap.Preview.Image
	if img == nil {
		return y
	}
	b := img.Bounds()
	tw, th := b.Dx(), b.Dy()
	existing, ok := textures["preview"]
	needUpload := !ok || existing.src != img || existing.w != tw || existing.h != th || existing.seq != snap.Preview.FrameSeq
	if needUpload && ok && existing.tex.Valid() && existing.w == tw && existing.h == th && len(img.Pix) > 0 {
		if err := dev.UpdateRGBA(existing.tex, img); err == nil {
			existing.src = img
			existing.seq = snap.Preview.FrameSeq
			textures["preview"] = existing
			needUpload = false
		} else {
			dev.Destroy(existing.tex)
			delete(textures, "preview")
			ok = false
		}
	}
	if needUpload {
		if ok {
			dev.Destroy(existing.tex)
			delete(textures, "preview")
		}
		if tex, err := uploadTexture(dev, img); err == nil {
			tex.src = img
			tex.seq = snap.Preview.FrameSeq
			textures["preview"] = tex
		}
	}
	if tex, ok := textures["preview"]; ok {
		stageH := 280
		remain := snap.Grid.Height - snap.Grid.Safe.Bottom - y - 160
		if remain < stageH {
			stageH = remain
		}
		if stageH < 80 {
			stageH = 80
		}
		dx, dy, dw, dh := coverDestRect(x, y, maxW, stageH, tex.w, tex.h)
		drawGPU(dev, tex, dx, dy, dw, dh)
		y += int(dh) + 16
	}
	return y
}

func drawNowPlaying(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture) {
	dev.BeginFrame()
	dev.Clear(gfx.RGB(12, 14, 20))
	used := map[string]struct{}{}
	drawHeader(dev, snap, labels, used)
	pad := 48
	x := snap.Grid.contentLeft() + pad
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 40
	maxW := snap.Grid.contentWidth() - 2*pad
	if maxW < 1 {
		maxW = snap.Grid.contentWidth()
		x = snap.Grid.contentLeft() + 8
	}
	title := strings.TrimSpace(snap.Session.Title)
	if title == "" {
		title = strings.TrimSpace(snap.Session.GameID)
	}
	if snap.Session.Diagnostic {
		title = "DIAGNOSTIC RBF"
	} else if title == "" {
		title = "Session active"
	}
	drawLabel(dev, labels, used, "np-title", x, y, maxW, 28, title)
	y += 40
	if snap.Session.Diagnostic {
		drawLabel(dev, labels, used, "np-diag", x, y, maxW, 16, diagnosticHint)
		y += 28
	}
	meta := strings.TrimSpace(strings.TrimPrefix(snap.NowPlayingLine(), "Now playing"))
	meta = strings.TrimSpace(strings.TrimPrefix(meta, "  ·  "))
	if snap.Session.Diagnostic {
		meta = strings.TrimSpace(strings.TrimPrefix(snap.NowPlayingLine(), diagnosticLabel))
		meta = strings.TrimSpace(strings.TrimPrefix(meta, "  ·  "))
	}
	if meta == "" {
		meta = snap.Session.State
	}
	drawLabel(dev, labels, used, "np-meta", x, y, maxW, 18, meta)
	y += 32
	y = drawSessionPreview(dev, snap, textures, labels, used, x, y, maxW)
	if progress := strings.TrimSpace(snap.Session.Progress); progress != "" {
		drawLabel(dev, labels, used, "np-progress", x, y, maxW, 16, progress)
		y += 28
	}
	if lease := strings.TrimSpace(snap.KitLease.Line); lease != "" {
		drawLabel(dev, labels, used, "np-lease", x, y, maxW, 16, lease)
		y += 26
	}
	if snap.Session.RetryStop {
		hint := strings.TrimSpace(snap.Session.RetryHint)
		if hint == "" {
			hint = "retry Stop"
		}
		drawLabel(dev, labels, used, "np-retry", x, y, maxW, 16, hint)
		y += 26
	}
	if status := strings.TrimSpace(snap.Status); status != "" && status != snap.NowPlayingLine() {
		drawLabel(dev, labels, used, "np-status", x, y, maxW, 16, status)
		y += 26
	}
	for i, line := range snap.Session.Events {
		if strings.TrimSpace(line) == "" {
			continue
		}
		drawLabel(dev, labels, used, fmt.Sprintf("np-ev-%d", i), x, y, maxW, 15, line)
		y += 22
	}
	for key, item := range labels {
		if _, ok := used[key]; ok {
			continue
		}
		dev.Destroy(item.tex)
		delete(labels, key)
	}
	dev.Present()
}

func drawDetail(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, textures map[string]gpuTexture) {
	h := snap.Grid.FooterHeight
	if snap.Detail.Open {
		h = detailPaneHeight(snap.Grid)
	}
	if h < 1 {
		return
	}
	x := snap.Grid.contentLeft()
	w := snap.Grid.contentWidth()
	y := snap.Grid.Height - snap.Grid.Safe.Bottom - h
	minY := snap.Grid.headerY() + snap.Grid.HeaderHeight
	if y < minY {
		y = minY
		h = snap.Grid.Height - snap.Grid.Safe.Bottom - y
	}
	if h < 1 {
		return
	}
	if snap.Detail.Open {
		fillRect(dev, float32(x-4), float32(y-4), float32(w+8), float32(h+8), 255, 184, 48, 255)
	}
	fillRect(dev, float32(x), float32(y), float32(w), float32(h), 16, 18, 26, 255)
	detail := snap.FocusDetail
	pad := 24
	title := strings.TrimSpace(detail.Title)
	if title == "" {
		title = "No title focused"
	}
	if detail.Favorite {
		title = "* " + title
	}
	textW := w - 2*pad
	shotW := 0
	if snap.Detail.Open && snap.Detail.Count > 0 {
		shotW = w * 38 / 100
		if shotW < 160 {
			shotW = 160
		}
		if shotW > w/2 {
			shotW = w / 2
		}
		textW = w - shotW - 3*pad
		if textW < 160 {
			textW = w - 2*pad
			shotW = 0
		}
	}
	drawLabel(dev, labels, used, "d-title", x+pad, y+12, textW, 26, title)
	facts, factsX, factsW, attr, attrX, attrW := layoutDetailMeta(detail, x+pad, textW, 16)
	if facts != "" && factsW > 0 {
		drawLabel(dev, labels, used, "d-meta", factsX, y+44, factsW, 16, facts)
	}
	if attr != "" && attrW > 0 {
		drawLabel(dev, labels, used, "d-attr", attrX, y+44, attrW, 16, attr)
	}
	lineY := y + 68
	summaryLines := 2
	if snap.Detail.Open {
		summaryLines = detailSummaryLines
	}
	maxChars := textW / 8
	if maxChars < 20 {
		maxChars = 20
	}
	for i, line := range wrapWords(detail.Summary, maxChars, summaryLines) {
		drawLabel(dev, labels, used, fmt.Sprintf("d-sum-%d", i), x+pad, lineY+i*20, textW, 16, line)
	}
	if shotW > 0 {
		drawScreenshotCarousel(dev, snap, labels, used, textures, x+w-pad-shotW, y+12, shotW, h-24)
	}
}

func drawScreenshotCarousel(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, textures map[string]gpuTexture, x, y, w, h int) {
	if w < 1 || h < 1 {
		return
	}
	fillRect(dev, float32(x), float32(y), float32(w), float32(h), 28, 32, 44, 255)
	ids := snap.FocusDetail.ScreenshotIDs
	index := clampCarouselIndex(snap.Detail.Index, len(ids))
	captionH := 22
	innerH := h - captionH
	if innerH < 1 {
		innerH = h
		captionH = 0
	}
	if index >= 0 && index < len(ids) {
		handle := ids[index]
		if tex, ok := textures[screenshotWorkKey(handle)]; ok {
			dx, dy, dw, dh := coverDestRect(x+8, y+8, w-16, innerH-16, tex.w, tex.h)
			drawGPU(dev, tex, dx, dy, dw, dh)
		} else {
			drawLabel(dev, labels, used, "d-shot-miss", x+12, y+innerH/2-8, w-24, 16, "…")
		}
	}
	if captionH > 0 {
		caption := carouselCaption(index, len(ids))
		drawLabel(dev, labels, used, "d-shot-cap", x+8, y+h-captionH, w-16, 16, caption)
	}
}

func drawViewPicker(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	rows := snap.PickerRows
	if len(rows) == 0 {
		rows = snap.Views
	}
	if !snap.ViewPicker || len(rows) == 0 {
		return
	}
	contentW := snap.Grid.contentWidth()
	panelW := 480
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 200 {
		panelW = contentW - 24
	}
	rowH := 28
	headerH := 40
	footerH := 24
	maxRows := 10
	if maxRows > len(rows) {
		maxRows = len(rows)
	}
	panelH := headerH + maxRows*rowH + footerH
	x := snap.Grid.contentLeft() + (contentW-panelW)/2
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 12
	if y+panelH > snap.Grid.footerY()-8 {
		y = snap.Grid.contentTop() + (snap.Grid.contentHeight()-panelH)/2
	}
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	drawLabel(dev, labels, used, "view-title", x+16, y+10, panelW-32, 18, "Library view")
	start := snap.ViewPickerIndex - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(rows) {
		start = len(rows) - maxRows
	}
	if start < 0 {
		start = 0
	}
	for i := 0; i < maxRows; i++ {
		idx := start + i
		if idx >= len(rows) {
			break
		}
		rowY := y + headerH + i*rowH
		if idx == snap.ViewPickerIndex {
			fillRect(dev, float32(x+8), float32(rowY-2), float32(panelW-16), float32(rowH-2), 48, 56, 80, 255)
		}
		label := rows[idx].Label
		if label == "" {
			label = rows[idx].ID
		}
		if rows[idx].ID == snap.Collection && !rows[idx].Create {
			label = label + "  *"
		}
		if rows[idx].Member {
			label = label + "  +"
		}
		drawLabel(dev, labels, used, fmt.Sprintf("view-%d", idx), x+20, rowY, panelW-40, 16, label)
	}
	drawLabel(dev, labels, used, "view-hint", x+16, y+panelH-22, panelW-32, 14, "A open  X add/remove  Y manage")
}

func drawCollectionMenu(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	menu := snap.CollectionMenu
	if !menu.Open {
		return
	}
	contentW := snap.Grid.contentWidth()
	panelW := 420
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 200 {
		panelW = contentW - 24
	}
	rowH := 28
	headerH := 44
	footerH := 28
	rows := menu.Rows
	panelH := headerH + len(rows)*rowH + footerH
	if panelH < headerH+footerH+rowH {
		panelH = headerH + footerH + rowH
	}
	x := snap.Grid.contentLeft() + (contentW-panelW)/2
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 48
	if y+panelH > snap.Grid.footerY()-8 {
		y = snap.Grid.contentTop() + (snap.Grid.contentHeight()-panelH)/2
	}
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	title := strings.TrimSpace(menu.Title)
	if title == "" {
		title = "Collection"
	}
	drawLabel(dev, labels, used, "cmenu-title", x+16, y+12, panelW-32, 18, title)
	for i, row := range rows {
		rowY := y + headerH + i*rowH
		if !menu.Confirm && i == menu.Index {
			fillRect(dev, float32(x+8), float32(rowY-2), float32(panelW-16), float32(rowH-2), 48, 56, 80, 255)
		}
		drawLabel(dev, labels, used, fmt.Sprintf("cmenu-%d", i), x+20, rowY, panelW-40, 16, row)
	}
	hint := strings.TrimSpace(menu.Hint)
	if hint == "" {
		hint = "A select  B back"
	}
	drawLabel(dev, labels, used, "cmenu-hint", x+16, y+panelH-24, panelW-32, 14, hint)
}

func drawSettings(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	if !snap.Settings.Open || len(snap.Settings.Rows) == 0 {
		return
	}
	contentW := snap.Grid.contentWidth()
	contentH := snap.Grid.contentHeight()
	dev.SetBlend(gfx.BlendAlpha)
	fillRect(dev, float32(snap.Grid.contentLeft()), float32(snap.Grid.contentTop()), float32(contentW), float32(contentH), 8, 8, 12, 180)
	dev.SetBlend(gfx.BlendNone)
	panelW := 720
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 280 {
		panelW = contentW - 24
	}
	rowH := 32
	headerH := 44
	footerH := 28
	rows := snap.Settings.Rows
	panelH := headerH + len(rows)*rowH + footerH
	maxH := contentH - 24
	if maxH < 120 {
		maxH = contentH
	}
	if panelH > maxH {
		panelH = maxH
	}
	x := snap.Grid.contentLeft() + (contentW-panelW)/2
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 12
	if y+panelH > snap.Grid.footerY()-8 {
		y = snap.Grid.contentTop() + (contentH-panelH)/2
	}
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	title := "Settings"
	switch {
	case snap.Settings.TargetCount > 0 && snap.Settings.LibraryCount > 0:
		title = fmt.Sprintf("Settings  ·  %d targets  ·  %d libraries", snap.Settings.TargetCount, snap.Settings.LibraryCount)
	case snap.Settings.TargetCount > 0:
		title = fmt.Sprintf("Settings  ·  %d targets", snap.Settings.TargetCount)
	case snap.Settings.LibraryCount > 0:
		title = fmt.Sprintf("Settings  ·  %d libraries", snap.Settings.LibraryCount)
	}
	drawLabel(dev, labels, used, "set-title", x+16, y+12, panelW-32, 18, title)
	visible := (panelH - headerH - footerH) / rowH
	if visible < 1 {
		visible = 1
	}
	if visible > len(rows) {
		visible = len(rows)
	}
	start := snap.Settings.Index - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(rows) {
		start = len(rows) - visible
	}
	if start < 0 {
		start = 0
	}
	labelW := 180
	if labelW > panelW/3 {
		labelW = panelW / 3
	}
	for i := 0; i < visible; i++ {
		idx := start + i
		if idx >= len(rows) {
			break
		}
		rowY := y + headerH + i*rowH
		if idx == snap.Settings.Index {
			fillRect(dev, float32(x+8), float32(rowY-2), float32(panelW-16), float32(rowH-2), 48, 56, 80, 255)
		}
		row := rows[idx]
		drawLabel(dev, labels, used, fmt.Sprintf("set-l-%s", row.ID), x+20, rowY+4, labelW, 16, row.Label)
		drawLabel(dev, labels, used, fmt.Sprintf("set-v-%s-%d", row.ID, idx), x+20+labelW, rowY+4, panelW-labelW-40, 16, row.Value)
	}
	status := strings.TrimSpace(snap.Settings.Status)
	if status == "" {
		status = strings.TrimSpace(snap.Settings.Hint)
	}
	if status == "" {
		status = "A confirm  B close  Left/Right change"
	}
	if snap.Settings.Loading {
		status = "loading host settings"
	}
	drawLabel(dev, labels, used, "set-status", x+16, y+panelH-24, panelW-32, 14, status)
}

func drawFilters(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	if !snap.Filters.Open || len(snap.Filters.Rows) == 0 {
		return
	}
	contentW := snap.Grid.contentWidth()
	contentH := snap.Grid.contentHeight()
	dev.SetBlend(gfx.BlendAlpha)
	fillRect(dev, float32(snap.Grid.contentLeft()), float32(snap.Grid.contentTop()), float32(contentW), float32(contentH), 8, 8, 12, 180)
	dev.SetBlend(gfx.BlendNone)
	panelW := 560
	if panelW > contentW-48 {
		panelW = contentW - 48
	}
	if panelW < 280 {
		panelW = contentW - 24
	}
	rowH := 28
	headerH := 44
	footerH := 28
	rows := snap.Filters.Rows
	maxH := contentH - 24
	if maxH < 120 {
		maxH = contentH
	}
	visible := (maxH - headerH - footerH) / rowH
	if visible < 1 {
		visible = 1
	}
	if visible > len(rows) {
		visible = len(rows)
	}
	if visible > 12 {
		visible = 12
	}
	panelH := headerH + visible*rowH + footerH
	if panelH > maxH {
		panelH = maxH
	}
	x := snap.Grid.contentLeft() + (contentW-panelW)/2
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 12
	if y+panelH > snap.Grid.footerY()-8 {
		y = snap.Grid.contentTop() + (contentH-panelH)/2
	}
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	title := strings.TrimSpace(snap.Filters.Title)
	if title == "" {
		title = "Filters"
	}
	drawLabel(dev, labels, used, "flt-title", x+16, y+12, panelW-32, 18, title)
	start := snap.Filters.Index - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > len(rows) {
		start = len(rows) - visible
	}
	if start < 0 {
		start = 0
	}
	labelW := 180
	if labelW > panelW/2 {
		labelW = panelW / 2
	}
	for i := 0; i < visible; i++ {
		idx := start + i
		if idx >= len(rows) {
			break
		}
		rowY := y + headerH + i*rowH
		if idx == snap.Filters.Index {
			fillRect(dev, float32(x+8), float32(rowY-2), float32(panelW-16), float32(rowH-2), 48, 56, 80, 255)
		}
		row := rows[idx]
		label := row.Label
		if row.Active {
			label += "  *"
		}
		drawLabel(dev, labels, used, fmt.Sprintf("flt-l-%s-%d", row.ID, idx), x+20, rowY+4, labelW, 16, label)
		if strings.TrimSpace(row.Value) != "" {
			drawLabel(dev, labels, used, fmt.Sprintf("flt-v-%s-%d", row.ID, idx), x+20+labelW, rowY+4, panelW-labelW-40, 16, row.Value)
		}
	}
	status := strings.TrimSpace(snap.Filters.Status)
	if status == "" {
		status = strings.TrimSpace(snap.Filters.Hint)
	}
	if status == "" {
		status = "A select  B back"
	}
	drawLabel(dev, labels, used, "flt-status", x+16, y+panelH-24, panelW-32, 14, status)
}

func drawOSK(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	if !snap.OSK.Open || len(snap.OSK.Rows) == 0 {
		return
	}
	contentW := snap.Grid.contentWidth()
	contentH := snap.Grid.contentHeight()
	dev.SetBlend(gfx.BlendAlpha)
	fillRect(dev, float32(snap.Grid.contentLeft()), float32(snap.Grid.contentTop()), float32(contentW), float32(contentH), 8, 8, 12, 180)
	dev.SetBlend(gfx.BlendNone)
	panelW := contentW - 48
	if panelW < 280 {
		panelW = contentW - 24
	}
	if panelW < 1 {
		panelW = contentW
	}
	headerH := 48
	footerH := 28
	gap := 6
	rows := snap.OSK.Rows
	maxH := contentH - 24
	if maxH < 120 {
		maxH = contentH
	}
	keyH := 40
	panelH := headerH + len(rows)*keyH + (len(rows)-1)*gap + footerH + 16
	if panelH > maxH {
		remain := maxH - headerH - footerH - 16 - (len(rows)-1)*gap
		if remain < len(rows)*24 {
			remain = len(rows) * 24
		}
		keyH = remain / len(rows)
		if keyH < 22 {
			keyH = 22
		}
		panelH = headerH + len(rows)*keyH + (len(rows)-1)*gap + footerH + 16
		if panelH > maxH {
			panelH = maxH
		}
	}
	x := snap.Grid.contentLeft() + (contentW-panelW)/2
	y := snap.Grid.headerY() + snap.Grid.HeaderHeight + 12
	if y+panelH > snap.Grid.footerY()-8 {
		y = snap.Grid.contentTop() + (contentH-panelH)/2
	}
	if y < snap.Grid.contentTop()+8 {
		y = snap.Grid.contentTop() + 8
	}
	fillRect(dev, float32(x-4), float32(y-4), float32(panelW+8), float32(panelH+8), 255, 184, 48, 255)
	fillRect(dev, float32(x), float32(y), float32(panelW), float32(panelH), 18, 20, 28, 255)
	prompt := strings.TrimSpace(snap.OSK.Prompt)
	if prompt == "" {
		prompt = "Search"
	}
	query := snap.OSK.Buffer
	drawLabel(dev, labels, used, "osk-query", x+16, y+12, panelW-32, 18, prompt+": "+query+"_")
	innerX := x + 12
	innerW := panelW - 24
	if innerW < 1 {
		innerW = 1
	}
	refCols := 10
	unitW := (innerW - (refCols-1)*gap) / refCols
	if unitW < 8 {
		unitW = 8
	}
	rowY := y + headerH
	for r, row := range rows {
		colX := innerX
		for c, key := range row {
			span := key.Span
			if span < 1 {
				span = 1
			}
			kw := span*unitW + (span-1)*gap
			if r < 3 {
				kw = unitW
			}
			if key.Focus {
				fillRect(dev, float32(colX-2), float32(rowY-2), float32(kw+4), float32(keyH+4), 255, 184, 48, 255)
				fillRect(dev, float32(colX), float32(rowY), float32(kw), float32(keyH), 48, 56, 80, 255)
			} else {
				fillRect(dev, float32(colX), float32(rowY), float32(kw), float32(keyH), 32, 36, 48, 255)
			}
			labelSize := 16
			if keyH < 28 {
				labelSize = 12
			}
			drawLabel(dev, labels, used, fmt.Sprintf("osk-%d-%d-%s", r, c, key.ID), colX+4, rowY+(keyH-labelSize)/2, kw-8, labelSize, key.Label)
			colX += kw + gap
		}
		rowY += keyH + gap
	}
	hint := strings.TrimSpace(snap.OSK.Hint)
	if hint == "" {
		hint = oskHint(snap.OSK.Page)
	}
	drawLabel(dev, labels, used, "osk-hint", x+16, y+panelH-24, panelW-32, 14, hint)
}

func drawAttract(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture) {
	dev.BeginFrame()
	dev.Clear(gfx.RGB(8, 8, 12))
	used := map[string]struct{}{}
	x := snap.Grid.contentLeft()
	y := snap.Grid.contentTop()
	w := snap.Grid.contentWidth()
	h := snap.Grid.contentHeight()
	titleH := 56
	stageH := h - titleH
	if stageH < 1 {
		stageH = h
		titleH = 0
	}
	if img := snap.Attract.Image; img != nil {
		b := img.Bounds()
		tw, th := b.Dx(), b.Dy()
		existing, ok := textures["attract"]
		needUpload := !ok || existing.src != img || existing.w != tw || existing.h != th || existing.seq != snap.Attract.FrameSeq
		if needUpload && ok && existing.tex.Valid() && existing.w == tw && existing.h == th && len(img.Pix) > 0 {
			if err := dev.UpdateRGBA(existing.tex, img); err == nil {
				existing.src = img
				existing.seq = snap.Attract.FrameSeq
				textures["attract"] = existing
				needUpload = false
			} else {
				dev.Destroy(existing.tex)
				delete(textures, "attract")
				ok = false
			}
		}
		if needUpload {
			if ok {
				dev.Destroy(existing.tex)
				delete(textures, "attract")
			}
			if tex, err := uploadTexture(dev, img); err == nil {
				tex.src = img
				tex.seq = snap.Attract.FrameSeq
				textures["attract"] = tex
			}
		}
		if tex, ok := textures["attract"]; ok {
			dx, dy, dw, dh := coverDestRect(x, y, w, stageH, tex.w, tex.h)
			drawGPU(dev, tex, dx, dy, dw, dh)
		}
	} else if item, ok := textures["attract"]; ok {
		dev.Destroy(item.tex)
		delete(textures, "attract")
	}
	title := strings.TrimSpace(snap.Attract.Title)
	if title == "" {
		title = strings.TrimSpace(snap.Attract.GameID)
	}
	if title != "" && titleH > 0 {
		drawLabel(dev, labels, used, "attract-title", x+24, y+stageH+12, w-48, 28, title)
	}
	for key, item := range labels {
		if _, ok := used[key]; ok {
			continue
		}
		dev.Destroy(item.tex)
		delete(labels, key)
	}
	dev.Present()
}

func fillRect(dev gfx.Device, x, y, w, h float32, r, g, b, a uint8) {
	dev.FillRect(gfx.Rect{X: x, Y: y, W: w, H: h}, gfx.RGBA(r, g, b, a))
}

func drawDebug(dev gfx.Device, x, y int, text string, scale int) {
	dev.DebugText(x, y, text, scale)
}

func fitText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if maxChars < 1 || len(runes) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-3]) + "..."
}

func initials(title string) string {
	fields := strings.Fields(title)
	if len(fields) == 0 {
		return "?"
	}
	out := []rune{[]rune(fields[0])[0]}
	if len(fields) > 1 {
		out = append(out, []rune(fields[1])[0])
	}
	return strings.ToUpper(string(out))
}

func placeholderColor(title string) (uint8, uint8, uint8) {
	var h uint32
	for i := 0; i < len(title); i++ {
		h = h*33 + uint32(title[i])
	}
	return uint8(40 + h%80), uint8(50 + (h>>8)%90), uint8(70 + (h>>16)%100)
}

func destroyTextures(dev gfx.Device, textures map[string]gpuTexture) {
	for id, item := range textures {
		dev.Destroy(item.tex)
		delete(textures, id)
	}
}
