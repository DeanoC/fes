package tenfoot

import (
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/DeanoC/FogCast/ui/shared"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/theme"
)

// gpuTexture is a Device-backed bitmap plus the CPU source used to decide
// when to upload again. Keys "preview" and "attract" are park/teardown exceptions.
type gpuTexture struct {
	tex          gfx.Texture
	w            int
	h            int
	src          *image.RGBA
	seq          int
	labelSource  string
	labelDisplay string
	// A text entry in labels has no texture. w×h is its layout box, and
	// drawMask draws mask's maskSrc part at maskAt from the box's top-left.
	mask    *gfx.Mask
	maskSrc image.Rectangle
	maskAt  image.Point
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
			clear(labels)
		}
		if snap.Surround.Active {
			drawSurround(dev, snap, labels)
		} else {
			drawNowPlaying(dev, snap, textures, labels)
		}
		return true
	}
	syncTextures(dev, snap, textures)
	syncRoomTextures(dev, snap, textures)
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
		if id == "attract" || id == "preview" || strings.HasPrefix(id, roomTexturePrefix) {
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

// drawMask draws a text entry with its box's top-left at x, y in c.
func drawMask(dev gfx.Device, text gpuTexture, x, y int, c gfx.Color) {
	if text.mask != nil {
		dev.DrawMask(text.mask, text.maskSrc, x+text.maskAt.X, y+text.maskAt.Y, c)
	}
}

// dropUnusedLabels forgets the text entries this frame did not draw.
func dropUnusedLabels(labels map[string]gpuTexture, used map[string]struct{}) {
	for key := range labels {
		if _, ok := used[key]; !ok {
			delete(labels, key)
		}
	}
}

func drawTheme(snap Snapshot) theme.Theme {
	return snap.Theme.Complete()
}

func drawFrame(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture) {
	dev.BeginFrame()
	used := map[string]struct{}{}
	if snap.Session.MediaSelector && snap.Session.ControlsOpen {
		dev.Clear(gfx.Color{})
		if !snap.TapePicker.Open {
			drawLabel(dev, labels, used, "media-return", 48, 48, snap.Grid.Width-96, 20, snap.Status)
		}
	} else if snap.Room.Open {
		drawRoom(dev, snap, textures, labels, used)
		if snap.Detail.Open {
			drawDetail(dev, snap, labels, used, textures)
		}
		drawRoomChoice(dev, snap, labels, used)
		drawLaunchOverlay(dev, snap, labels, used)
	} else {
		background := heroBackground
		if th := drawTheme(snap); th.Name != theme.NameDefault && th.Name != theme.NameNight {
			background = th.SofaBackground
		}
		dev.Clear(background)
		drawHeader(dev, snap, labels, used)
		if snap.Grid.Mode == LayoutList {
			drawListRows(dev, snap, textures, labels, used)
		} else {
			drawCoverCells(dev, snap, textures, labels, used)
		}
		drawDetail(dev, snap, labels, used, textures)
		drawViewPicker(dev, snap, labels, used)
		drawCollectionMenu(dev, snap, labels, used)
	}
	drawRoomPicker(dev, snap, labels, used)
	drawFirmwarePicker(dev, snap, labels, used)
	drawTapePicker(dev, snap, labels, used)
	drawSettings(dev, snap, labels, used)
	if snap.CoreLibrary.Open {
		overlay := snap
		overlay.FirmwarePicker = snap.CoreLibrary
		drawFilePicker(dev, overlay, labels, used, func(row FirmwarePickerRow) string {
			if row.Kind == firmwarePickerKindFile {
				return row.Name + " · " + formatByteSize(row.Size)
			}
			return row.Name
		})
	}
	if snap.Storage.Open {
		overlay := snap
		overlay.FirmwarePicker = snap.Storage
		drawFilePicker(dev, overlay, labels, used, func(row FirmwarePickerRow) string { return row.Name })
	}
	drawFilters(dev, snap, labels, used)
	drawOSK(dev, snap, labels, used)
	drawDebugHUD(dev, snap)
	dropUnusedLabels(labels, used)
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
		focused := i == snap.Grid.Focus && !snap.BrowseActions.Focused
		if focused {
			dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 4), W: float32(snap.Grid.CellW + 8), H: float32(snap.Grid.CellH + 8)}, heroAccent)
		}
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(snap.Grid.CellW), H: float32(snap.Grid.CellH)}, heroSurface)
		if tex, ok := textures[game.ID]; ok {
			dx, dy, dw, dh := shared.CoverDestRect(x, y, snap.Grid.CellW, snap.Grid.CellH-40, tex.w, tex.h)
			drawGPU(dev, tex, dx, dy, dw, dh)
		} else {
			r, g, b := placeholderColor(game.Title)
			fillRect(dev, float32(x+8), float32(y+8), float32(snap.Grid.CellW-16), float32(snap.Grid.CellH-52), r, g, b, 255)
			// Keep missing-art identity on paper rather than variable colour.
			dev.FillRect(gfx.Rect{X: float32(x + 12), Y: float32(y + 16), W: float32(snap.Grid.CellW - 24), H: 30}, heroSurface)
			// Missing art names the platform, not an acronym; the full
			// title is the label under the art.
			drawFESLabel(dev, labels, used, "i:"+game.ID, x+16, y+20, snap.Grid.CellW-32, 18, placeholderSystem(snap, game))
		}
		drawCellBlockTag(dev, labels, used, x+8, y+snap.Grid.CellH-44, snap.Grid.CellW-16, game, snap.KitPlay)
		heroLine(dev, labels, used, x+6, y+snap.Grid.CellH-31, snap.Grid.CellW-12, 20, i == snap.Grid.Focus, heroText, game.Title)
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
		focused := i == snap.Grid.Focus && !snap.BrowseActions.Focused
		if focused {
			dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 2), W: float32(snap.Grid.CellW + 8), H: float32(snap.Grid.CellH + 4)}, heroAccent)
		}
		dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(snap.Grid.CellW), H: float32(snap.Grid.CellH)}, heroSurface)
		tx, ty, tw, th := snap.Grid.listThumbRect(x, y)
		dev.FillRect(gfx.Rect{X: float32(tx), Y: float32(ty), W: float32(tw), H: float32(th)}, heroSurface)
		if tex, ok := textures[game.ID]; ok {
			dx, dy, dw, dh := shared.CoverDestRect(tx, ty, tw, th, tex.w, tex.h)
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
		heroLine(dev, labels, used, textX, y+12, textW, 22, i == snap.Grid.Focus, heroText, title)
		meta := strings.TrimSpace(game.System)
		if year := strings.TrimSpace(game.Year); year != "" {
			if meta != "" {
				meta += "  ·  " + year
			} else {
				meta = year
			}
		}
		if play := classifyLibraryGame(game, snap.KitPlay).Availability.Label(); play != "" {
			if meta != "" {
				meta += "  ·  " + play
			} else {
				meta = play
			}
		}
		drawFESLabel(dev, labels, used, "lm:"+game.ID, textX, y+40, textW, 16, meta)
	}
}

func drawLabel(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, key string, x, y, maxW, sizePx int, text string) {
	drawTintedLabel(dev, labels, used, key, x, y, maxW, sizePx, labelColor, text)
}

// Native Library and shared dialogs use the same paper and ink as Home.
// Keep the dark playback labels independent of this presentation.
func drawFESLabel(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, key string, x, y, maxW, sizePx int, text string) {
	drawTintedLabel(dev, labels, used, key, x, y, maxW, sizePx, heroText, text)
}

func drawTintedLabel(dev gfx.Device, labels map[string]gpuTexture, used map[string]struct{}, key string, x, y, maxW, sizePx int, color gfx.Color, text string) {
	text = strings.TrimSpace(text)
	if text == "" || maxW < 1 {
		return
	}
	// Hidden suffix changes must not upload or invalidate identical glyphs.
	key = labelCacheKey(key, "", maxW, sizePx)
	used[key] = struct{}{}
	tex, ok := labels[key]
	if !ok || tex.labelSource != text {
		labelMu.Lock()
		face := labelFace(sizePx)
		display := ""
		if face != nil {
			display = fitLabel(face, text, maxW)
		}
		if ok && tex.labelDisplay == display {
			labelMu.Unlock()
			tex.labelSource = text
			labels[key] = tex
			drawMask(dev, tex, x, y, color)
			return
		}
		laid, laidOK := layoutLabel(face, display, maxW, sizePx)
		labelMu.Unlock()
		if !laidOK {
			delete(used, key)
			return
		}
		tex = laid
		tex.labelSource, tex.labelDisplay = text, display
		labels[key] = tex
	}
	drawMask(dev, tex, x, y, color)
}

func drawHeader(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	x, y, w := snap.Grid.contentLeft(), snap.Grid.headerY(), snap.Grid.contentWidth()
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(snap.Grid.HeaderHeight)}, heroBackground)
	title := "FES  |  Library"
	if snap.GPUParked || snap.Session.State == "active" {
		title = "FES  |  Now playing"
	}
	heroLine(dev, labels, used, x+24, y+6, w-150, 28, true, heroText, title)
	heroLine(dev, labels, used, x+w-128, y+14, 104, 16, false, heroSecondary, snap.AffinityBadge())
	drawFESLabel(dev, labels, used, "chrome", x+24, y+43, w-48, 18, snap.chromeBody())
	for i, line := range gfx.WrapText(snap.HeaderHint(), 18, w-48, 2) {
		heroLine(dev, labels, used, x+24, y+68+i*22, w-48, 18, false, heroSecondary, line)
	}
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y + snap.Grid.HeaderHeight - 2), W: float32(w), H: 2}, heroDivider)
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
		dx, dy, dw, dh := shared.CoverDestRect(x, y, maxW, stageH, tex.w, tex.h)
		drawGPU(dev, tex, dx, dy, dw, dh)
		y += int(dh) + 16
	}
	return y
}

func drawNowPlaying(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture) {
	dev.BeginFrame()
	dev.Clear(drawTheme(snap).SofaBackground)
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
	heading := nowPlayingHeading(snap)
	if snap.Session.Diagnostic {
		title = "DIAGNOSTIC RBF"
	} else if heading == nowPlayingHeadingSave || heading == nowPlayingHeadingStop {
		if title == "" {
			title = heading
		}
	} else if title == "" {
		title = "Session active"
	}
	if heading == nowPlayingHeadingSave || heading == nowPlayingHeadingStop {
		drawLabel(dev, labels, used, "np-head", x, y, maxW, 20, heading)
		y += 28
	}
	drawLabel(dev, labels, used, "np-title", x, y, maxW, 28, title)
	y += 40
	if snap.Session.Diagnostic {
		drawLabel(dev, labels, used, "np-diag", x, y, maxW, 16, diagnosticHint)
		y += 28
	}
	meta := strings.TrimSpace(strings.TrimPrefix(snap.NowPlayingLine(), nowPlayingHeading(snap)))
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
	drawHardwareRoomButton(dev, snap, labels, used)
	drawTapePicker(dev, snap, labels, used)
	drawOSK(dev, snap, labels, used)
	drawDebugHUD(dev, snap)
	dropUnusedLabels(labels, used)
	dev.Present()
}

func drawDetail(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, textures map[string]gpuTexture) {
	geom, ok := detailGeomOf(snap)
	if !ok {
		return
	}
	x, y, w, h := geom.Pane.X, geom.Pane.Y, geom.Pane.W, geom.Pane.H
	if snap.Detail.Open {
		dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 4), W: float32(w + 8), H: float32(h + 8)}, heroAccent)
	}
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, heroSurface)
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
	if primary, _ := libraryActionRects(snap); primary.W > 0 {
		textW = min(textW, primary.X-x-pad-24)
	}
	if geom.Carousel.W > 0 {
		textW = geom.Carousel.X - x - 2*pad
		if textW < 160 {
			textW = w - 2*pad
		}
	}
	titleLines := 1
	if snap.Detail.Open {
		titleLines = 2
	}
	titleY := y + 10
	for _, line := range gfx.WrapTextWeight(title, 32, textW, titleLines, gfx.WeightBold) {
		heroLine(dev, labels, used, x+pad, titleY, textW, 32, true, heroText, line)
		titleY += 38
	}
	creatorBesideTitle := false
	if studio := strings.TrimSpace(detail.Studio); studio != "" {
		titleW := min(gfx.MeasureTextWeight(title, 32, gfx.WeightBold), textW)
		if byX := x + pad + titleW + 16; byX < x+pad+textW-80 {
			drawFESLabel(dev, labels, used, "d-by", byX, y+22, x+pad+textW-byX, 18, "by "+studio)
			creatorBesideTitle = true
		}
	}
	metaY := titleY + 2
	factDetail := detail
	if creatorBesideTitle {
		factDetail.Studio = ""
	}
	facts, factsX, factsW, attr, attrX, attrW := layoutDetailMeta(factDetail, x+pad, textW, 18)
	if facts != "" && factsW > 0 {
		drawFESLabel(dev, labels, used, "d-meta", factsX, metaY, factsW, 18, facts)
	}
	if attr != "" && attrW > 0 {
		drawFESLabel(dev, labels, used, "d-attr", attrX, metaY, attrW, 18, attr)
	}
	lineY := metaY + 28
	bottom := y + h - 8
	// Readiness comes before history and editorial text. Every later row
	// consumes the space it actually draws, within the same pane boundary.
	lineY += drawDetailReadiness(dev, labels, used, x+pad, lineY, textW, bottom, snap.DetailPlay)
	if snap.Room.Open {
		if hist := strings.TrimSpace(snap.Room.Destination.History.Line()); hist != "" && lineY+gfx.TextHeight(18) <= bottom {
			drawFESLabel(dev, labels, used, "d-history", x+pad, lineY, textW, 18, hist)
			lineY += 26
		}
	}
	credit, note := "", ""
	if snap.Room.Open {
		note = strings.TrimSpace(snap.Room.Destination.Note)
		if note != "" {
			credit = snap.Room.Destination.CuratorAttribution()
			if credit == "" {
				credit = "Note from this room"
			}
		}
	}
	// Reserve room for attribution and as much of the note as fits before
	// filling the remainder with the title's general summary.
	var noteLines []string
	noteH := 0
	for limit := 3; note != "" && limit > 0; limit-- {
		noteLines = gfx.WrapText(note, 18, textW, limit)
		noteH = 8 + gfx.TextHeight(16) + 4 + len(noteLines)*24
		if lineY+noteH <= bottom {
			break
		}
		noteLines, noteH = nil, 0
	}
	summaryLines := 2
	if snap.Detail.Open {
		summaryLines = detailSummaryLines
	}
	for i, line := range gfx.WrapText(detail.Summary, 20, textW, summaryLines) {
		if lineY+gfx.TextHeight(20) > bottom-noteH {
			break
		}
		drawFESLabel(dev, labels, used, fmt.Sprintf("d-sum-%d", i), x+pad, lineY, textW, 20, line)
		lineY += 26
	}
	if len(noteLines) > 0 {
		lineY += 8
		drawFESLabel(dev, labels, used, "d-note-attr", x+pad, lineY, textW, 16, credit)
		lineY += gfx.TextHeight(16) + 4
		for i, line := range noteLines {
			if lineY+gfx.TextHeight(18) > bottom {
				break
			}
			drawFESLabel(dev, labels, used, fmt.Sprintf("d-note-%d", i), x+pad, lineY, textW, 18, line)
			lineY += 24
		}
	}
	if geom.Carousel.W > 0 {
		drawScreenshotCarousel(dev, snap, labels, used, textures, geom.Carousel.X, geom.Carousel.Y, geom.Carousel.W, geom.Carousel.H)
	}
	drawLibraryActions(dev, snap, labels, used)
}

func drawScreenshotCarousel(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}, textures map[string]gpuTexture, x, y, w, h int) {
	if w < 1 || h < 1 {
		return
	}
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, heroSurface)
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
			dx, dy, dw, dh := shared.CoverDestRect(x+8, y+8, w-16, innerH-16, tex.w, tex.h)
			drawGPU(dev, tex, dx, dy, dw, dh)
		} else {
			drawFESLabel(dev, labels, used, "d-shot-miss", x+12, y+innerH/2-8, w-24, 16, "…")
		}
	}
	if captionH > 0 {
		caption := carouselCaption(index, len(ids))
		drawFESLabel(dev, labels, used, "d-shot-cap", x+8, y+h-captionH, w-16, 16, caption)
	}
}

func drawViewPicker(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	panel, ok := viewPickerPanel(snap)
	if !ok {
		return
	}
	rows := viewPickerRows(snap)
	x, y, panelW, panelH := panel.X, panel.Y, panel.W, panel.H
	dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 4), W: float32(panelW + 8), H: float32(panelH + 8)}, heroDivider)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(panelW), H: float32(panelH)}, heroSurface)
	heroLine(dev, labels, used, x+16, y+10, panelW-32, 26, true, heroText, "Library view")
	for i := 0; i < panel.Visible; i++ {
		idx := panel.Start + i
		if idx >= len(rows) {
			break
		}
		rowY := panel.rowY(i)
		if idx == snap.ViewPickerIndex {
			dev.FillRect(gfx.Rect{X: float32(x + 8), Y: float32(rowY - 2), W: float32(panelW - 16), H: float32(panel.RowH - 2)}, heroRaised)
			heroOutline(dev, x+8, rowY-2, panelW-16, panel.RowH-2, 2, heroAccent)
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
		drawFESLabel(dev, labels, used, fmt.Sprintf("view-%d", idx), x+20, rowY+7, panelW-40, 20, label)
	}
	drawFESLabel(dev, labels, used, "view-hint", x+16, y+panelH-22, panelW-32, 18, viewPickerFooterHint(snap.Affinity))
}

func drawCollectionMenu(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	menu := snap.CollectionMenu
	panel, ok := collectionMenuPanel(snap)
	if !ok {
		return
	}
	x, y, panelW, panelH := panel.X, panel.Y, panel.W, panel.H
	dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 4), W: float32(panelW + 8), H: float32(panelH + 8)}, heroDivider)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(panelW), H: float32(panelH)}, heroSurface)
	title := strings.TrimSpace(menu.Title)
	if title == "" {
		title = "Collection"
	}
	heroLine(dev, labels, used, x+16, y+12, panelW-32, 26, true, heroText, title)
	for i, row := range menu.Rows {
		rowY := panel.rowY(i)
		if !menu.Confirm && i == menu.Index {
			dev.FillRect(gfx.Rect{X: float32(x + 8), Y: float32(rowY - 2), W: float32(panelW - 16), H: float32(panel.RowH - 2)}, heroRaised)
			heroOutline(dev, x+8, rowY-2, panelW-16, panel.RowH-2, 2, heroAccent)
		}
		drawFESLabel(dev, labels, used, fmt.Sprintf("cmenu-%d", i), x+20, rowY+7, panelW-40, 20, row)
	}
	hint := strings.TrimSpace(menu.Hint)
	if hint == "" {
		hint = collectionHintFor(snap.Affinity, menu.Confirm)
	}
	drawFESLabel(dev, labels, used, "cmenu-hint", x+16, y+panelH-24, panelW-32, 18, hint)
}

func drawSettings(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	panel, ok := settingsPanel(snap)
	if !ok {
		return
	}
	x, y, panelW, panelH := panel.X, panel.Y, panel.W, panel.H
	dimOutsidePanel(dev, snap.Grid, rectI{x - 4, y - 4, panelW + 8, panelH + 8})
	rows := snap.Settings.Rows
	dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 4), W: float32(panelW + 8), H: float32(panelH + 8)}, heroDivider)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(panelW), H: float32(panelH)}, heroSurface)
	title := "Settings"
	switch {
	case snap.Settings.TargetCount > 0 && snap.Settings.LibraryCount > 0:
		title = fmt.Sprintf("Settings  ·  %d targets  ·  %d libraries", snap.Settings.TargetCount, snap.Settings.LibraryCount)
	case snap.Settings.TargetCount > 0:
		title = fmt.Sprintf("Settings  ·  %d targets", snap.Settings.TargetCount)
	case snap.Settings.LibraryCount > 0:
		title = fmt.Sprintf("Settings  ·  %d libraries", snap.Settings.LibraryCount)
	}
	heroLine(dev, labels, used, x+16, y+12, panelW-32, 26, true, heroText, title)
	if cache, ok := dev.(interface{ CacheBackdrop() }); ok {
		cache.CacheBackdrop()
	}
	labelW := 240
	if labelW > panelW/3 {
		labelW = panelW / 3
	}
	for i := 0; i < panel.Visible; i++ {
		idx := panel.Start + i
		if idx >= len(rows) {
			break
		}
		rowY := panel.rowY(i)
		if idx == snap.Settings.Index {
			dev.FillRect(gfx.Rect{X: float32(x + 8), Y: float32(rowY - 2), W: float32(panelW - 16), H: float32(panel.RowH - 2)}, heroRaised)
			heroOutline(dev, x+8, rowY-2, panelW-16, panel.RowH-2, 2, heroAccent)
		}
		row := rows[idx]
		drawFESLabel(dev, labels, used, fmt.Sprintf("set-l-%s", row.ID), x+20, rowY+8, labelW, 20, row.Label)
		drawFESLabel(dev, labels, used, fmt.Sprintf("set-v-%s-%d", row.ID, idx), x+20+labelW, rowY+8, panelW-labelW-40, 20, row.Value)
	}
	status := strings.TrimSpace(snap.Settings.Status)
	if status == "" {
		status = strings.TrimSpace(snap.Settings.Hint)
	}
	if status == "" {
		status = settingsDefaultHint(snap.Affinity)
	}
	if snap.Settings.Loading {
		status = "loading host settings"
	}
	for i, line := range gfx.WrapText(status, 18, panelW-32, 2) {
		drawFESLabel(dev, labels, used, fmt.Sprintf("set-status-%d", i), x+16, y+panelH-panel.FooterH+6+i*22, panelW-32, 18, line)
	}
}

// The opaque settings panel overwrites its entire border and interior. Only
// blend the visible backdrop; shading hidden pixels costs a full alpha pass
// and cannot contribute to the resulting frame.
func dimOutsidePanel(dev gfx.Device, grid Grid, panel rectI) {
	x, y := grid.contentLeft(), grid.contentTop()
	right, bottom := x+grid.contentWidth(), y+grid.contentHeight()
	x0, y0 := max(x, panel.X), max(y, panel.Y)
	x1, y1 := min(right, panel.X+panel.W), min(bottom, panel.Y+panel.H)
	dev.SetBlend(gfx.BlendAlpha)
	if x0 >= x1 || y0 >= y1 {
		fillRect(dev, float32(x), float32(y), float32(right-x), float32(bottom-y), 8, 8, 12, 180)
	} else {
		fillRect(dev, float32(x), float32(y), float32(right-x), float32(y0-y), 8, 8, 12, 180)
		fillRect(dev, float32(x), float32(y1), float32(right-x), float32(bottom-y1), 8, 8, 12, 180)
		fillRect(dev, float32(x), float32(y0), float32(x0-x), float32(y1-y0), 8, 8, 12, 180)
		fillRect(dev, float32(x1), float32(y0), float32(right-x1), float32(y1-y0), 8, 8, 12, 180)
	}
	dev.SetBlend(gfx.BlendNone)
}

func drawFilters(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	panel, ok := filtersPanel(snap)
	if !ok {
		return
	}
	contentW := snap.Grid.contentWidth()
	contentH := snap.Grid.contentHeight()
	dev.SetBlend(gfx.BlendAlpha)
	fillRect(dev, float32(snap.Grid.contentLeft()), float32(snap.Grid.contentTop()), float32(contentW), float32(contentH), 8, 8, 12, 180)
	dev.SetBlend(gfx.BlendNone)
	x, y, panelW, panelH := panel.X, panel.Y, panel.W, panel.H
	rows := snap.Filters.Rows
	dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 4), W: float32(panelW + 8), H: float32(panelH + 8)}, heroDivider)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(panelW), H: float32(panelH)}, heroSurface)
	title := strings.TrimSpace(snap.Filters.Title)
	if title == "" {
		title = "Filters"
	}
	heroLine(dev, labels, used, x+16, y+12, panelW-32, 26, true, heroText, title)
	labelW := 180
	if labelW > panelW/2 {
		labelW = panelW / 2
	}
	for i := 0; i < panel.Visible; i++ {
		idx := panel.Start + i
		if idx >= len(rows) {
			break
		}
		rowY := panel.rowY(i)
		if idx == snap.Filters.Index {
			dev.FillRect(gfx.Rect{X: float32(x + 8), Y: float32(rowY - 2), W: float32(panelW - 16), H: float32(panel.RowH - 2)}, heroRaised)
			heroOutline(dev, x+8, rowY-2, panelW-16, panel.RowH-2, 2, heroAccent)
		}
		row := rows[idx]
		label := row.Label
		if row.Active {
			label += "  *"
		}
		drawFESLabel(dev, labels, used, fmt.Sprintf("flt-l-%s-%d", row.ID, idx), x+20, rowY+8, labelW, 20, label)
		if strings.TrimSpace(row.Value) != "" {
			drawFESLabel(dev, labels, used, fmt.Sprintf("flt-v-%s-%d", row.ID, idx), x+20+labelW, rowY+8, panelW-labelW-40, 20, row.Value)
		}
	}
	status := strings.TrimSpace(snap.Filters.Status)
	if status == "" {
		status = strings.TrimSpace(snap.Filters.Hint)
	}
	if status == "" {
		status = filterHintFor(snap.Affinity, false)
	}
	for i, line := range gfx.WrapText(status, 18, panelW-32, 2) {
		drawFESLabel(dev, labels, used, fmt.Sprintf("flt-status-%d", i), x+16, y+panelH-panel.FooterH+6+i*22, panelW-32, 18, line)
	}
}

func drawOSK(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
	geom, keys, ok := oskLayout(snap)
	if !ok {
		return
	}
	contentW := snap.Grid.contentWidth()
	contentH := snap.Grid.contentHeight()
	dev.SetBlend(gfx.BlendAlpha)
	fillRect(dev, float32(snap.Grid.contentLeft()), float32(snap.Grid.contentTop()), float32(contentW), float32(contentH), 8, 8, 12, 180)
	dev.SetBlend(gfx.BlendNone)
	x, y, panelW, panelH := geom.X, geom.Y, geom.W, geom.H
	keyH := geom.KeyH
	dev.FillRect(gfx.Rect{X: float32(x - 4), Y: float32(y - 4), W: float32(panelW + 8), H: float32(panelH + 8)}, heroDivider)
	dev.FillRect(gfx.Rect{X: float32(x), Y: float32(y), W: float32(panelW), H: float32(panelH)}, heroSurface)
	prompt := strings.TrimSpace(snap.OSK.Prompt)
	if prompt == "" {
		prompt = "Search"
	}
	query := snap.OSK.Buffer
	drawFESLabel(dev, labels, used, "osk-query", x+16, y+12, panelW-32, 22, prompt+": "+query+"_")
	for _, key := range keys {
		if key.Focus {
			dev.FillRect(gfx.Rect{X: float32(key.X - 2), Y: float32(key.Y - 2), W: float32(key.W + 4), H: float32(key.H + 4)}, heroAccent)
			dev.FillRect(gfx.Rect{X: float32(key.X), Y: float32(key.Y), W: float32(key.W), H: float32(key.H)}, heroRaised)
		} else {
			dev.FillRect(gfx.Rect{X: float32(key.X), Y: float32(key.Y), W: float32(key.W), H: float32(key.H)}, heroRaised)
		}
		labelSize := 20
		if keyH < 28 {
			labelSize = 12
		}
		drawFESLabel(dev, labels, used, fmt.Sprintf("osk-%d-%d-%s", key.Row, key.Col, key.ID), key.X+4, key.Y+(keyH-labelSize)/2, key.W-8, labelSize, key.Label)
	}
	hint := strings.TrimSpace(snap.OSK.Hint)
	if hint == "" {
		hint = oskHintFor(snap.Affinity, snap.OSK.Page)
	}
	drawFESLabel(dev, labels, used, "osk-hint", x+16, y+panelH-24, panelW-32, 18, hint)
}

func drawAttract(dev gfx.Device, snap Snapshot, textures, labels map[string]gpuTexture) {
	dev.BeginFrame()
	dev.Clear(drawTheme(snap).AttractBackground)
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
			dx, dy, dw, dh := shared.CoverDestRect(x, y, w, stageH, tex.w, tex.h)
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
	dropUnusedLabels(labels, used)
	dev.Present()
}

func fillRect(dev gfx.Device, x, y, w, h float32, r, g, b, a uint8) {
	dev.FillRect(gfx.Rect{X: x, Y: y, W: w, H: h}, gfx.RGBA(r, g, b, a))
}

func drawDebugHUD(dev gfx.Device, snap Snapshot) {
	if !snap.DebugHUD.Enabled || len(snap.DebugHUD.Lines) == 0 {
		return
	}
	th := drawTheme(snap)
	size := th.CaptionPx()
	if size < 10 {
		size = 10
	}
	x := snap.Grid.contentLeft() + 8
	y := snap.Grid.footerY() - (size+2)*len(snap.DebugHUD.Lines) - 4
	if y < snap.Grid.contentTop()+snap.Grid.HeaderHeight {
		y = snap.Grid.contentTop() + 8
	}
	for _, line := range snap.DebugHUD.Lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		dev.DrawText(x, y, line, size, th.Status)
		y += size + 2
	}
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
