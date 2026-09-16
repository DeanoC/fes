package tenfoot

import (
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"image"
)

const (
	detailPaneMaxH     = 320
	detailSummaryLines = 5
	screenshotPrefix   = "shot:"
)

type shotSlot struct {
	phase   coverPhase
	image   *image.RGBA
	message string
}

// DetailSnapshot is renderer-facing focused-pane state.
type DetailSnapshot struct {
	Open  bool
	Index int
	Count int
	Hint  string
}

func screenshotWorkKey(handle string) string {
	return screenshotPrefix + handle
}

func (item workItem) key() string {
	if item.kind == workScreenshot {
		return screenshotWorkKey(item.handle)
	}
	return item.gameID
}

func (result workResult) key() string {
	if result.kind == workScreenshot {
		return screenshotWorkKey(result.handle)
	}
	return result.gameID
}

func clampCarouselIndex(index, n int) int {
	if n < 1 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= n {
		return n - 1
	}
	return index
}

func stepCarousel(ids []string, failed func(string) bool, index, delta int) int {
	n := len(ids)
	if n == 0 {
		return 0
	}
	index = clampCarouselIndex(index, n)
	if delta == 0 {
		return skipFailedCarousel(ids, failed, index)
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	start := index
	i := start
	for range ids {
		i = (i + dir + n) % n
		if failed == nil || !failed(ids[i]) {
			return i
		}
		if i == start {
			break
		}
	}
	return start
}

func skipFailedCarousel(ids []string, failed func(string) bool, index int) int {
	n := len(ids)
	if n == 0 {
		return 0
	}
	index = clampCarouselIndex(index, n)
	if failed == nil || !failed(ids[index]) {
		return index
	}
	return stepCarousel(ids, failed, index, 1)
}

func detailHint(count int) string {
	if count > 1 {
		return "A launch  B back  LEFT/RIGHT screenshots  GUIDE settings"
	}
	return "A launch  B back  GUIDE settings"
}

func (a *App) detailEnterFromBrowseLocked() bool {
	if len(a.games) == 0 || a.grid.Count < 1 {
		return false
	}
	if a.grid.Mode != LayoutShelf {
		return false
	}
	_, end := a.grid.VisibleRange()
	return end >= a.grid.Count
}

func (a *App) openDetailLocked() {
	if a.detailOpen || len(a.games) == 0 {
		return
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return
	}
	a.detailOpen = true
	a.carouselIndex = skipFailedCarousel(a.focusDetailLocked().ScreenshotIDs, a.shotFailedLocked, a.carouselIndex)
}

func (a *App) closeDetailLocked() {
	if !a.detailOpen {
		return
	}
	a.detailOpen = false
	a.cancelScreenshotWorkLocked()
}

func (a *App) cancelScreenshotWorkLocked() {
	a.shotGen++
	a.cancelScreenshotContextLocked()
	for key, kind := range a.inflight {
		if kind == workScreenshot {
			delete(a.inflight, key)
		}
	}
}

func (a *App) cancelScreenshotContextLocked() {
	if a.shotCancel != nil {
		a.shotCancel()
		a.shotCancel = nil
	}
	a.shotCtx = nil
}

func (a *App) screenshotContextLocked() context.Context {
	if a.shotCtx != nil && a.shotCtx.Err() == nil {
		return a.shotCtx
	}
	parent := a.jobCtx
	if parent == nil {
		parent = a.ctx
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.shotCancel = cancel
	a.shotCtx = ctx
	return ctx
}

func (a *App) handleDetailLocked(cmd Command) bool {
	if !a.detailOpen {
		return false
	}
	switch cmd {
	case CmdUp, CmdBack:
		a.closeDetailLocked()
		return true
	case CmdDown:
		return true
	case CmdLeft:
		a.stepCarouselLocked(-1)
		return true
	case CmdRight:
		a.stepCarouselLocked(1)
		return true
	case CmdSelect:
		a.startLaunchLocked()
		return true
	case CmdTab:
		if len(a.focusDetailLocked().ScreenshotIDs) > 1 {
			a.stepCarouselLocked(1)
		} else {
			a.startLaunchLocked()
		}
		return true
	case CmdTabPrev:
		if len(a.focusDetailLocked().ScreenshotIDs) > 1 {
			a.stepCarouselLocked(-1)
		} else {
			a.closeDetailLocked()
		}
		return true
	case CmdStop:
		a.startStopLocked()
		return true
	case CmdFilterPrev:
		a.closeDetailLocked()
		a.cyclePlatformLocked(-1)
		return true
	case CmdFilterNext:
		a.closeDetailLocked()
		a.cyclePlatformLocked(1)
		return true
	case CmdSortCycle:
		a.closeDetailLocked()
		a.cycleSortLocked()
		return true
	case CmdSearch:
		a.closeDetailLocked()
		a.openSearchLocked()
		return true
	case CmdViewPrev:
		a.closeDetailLocked()
		a.cycleViewLocked(-1)
		return true
	case CmdViewNext:
		a.closeDetailLocked()
		a.cycleViewLocked(1)
		return true
	case CmdViewPicker:
		a.closeDetailLocked()
		a.openViewPickerLocked()
		return true
	case CmdFavorite:
		a.toggleFavoriteLocked()
		return true
	case CmdFilters:
		a.closeDetailLocked()
		a.openFiltersLocked()
		return true
	default:
		return false
	}
}

func (a *App) stepCarouselLocked(delta int) {
	ids := a.focusDetailLocked().ScreenshotIDs
	if len(ids) < 2 {
		return
	}
	a.carouselIndex = stepCarousel(ids, a.shotFailedLocked, a.carouselIndex, delta)
}

func (a *App) shotFailedLocked(handle string) bool {
	slot := a.shots[handle]
	return slot != nil && slot.phase == coverFailed
}

func (a *App) clampCarouselLocked() {
	ids := a.focusDetailLocked().ScreenshotIDs
	a.carouselIndex = skipFailedCarousel(ids, a.shotFailedLocked, a.carouselIndex)
}

func (a *App) applyScreenshotResultLocked(result workResult) {
	handle := hostclient.NormalizeHandle(result.handle)
	if handle == "" {
		return
	}
	slot := a.shots[handle]
	if slot == nil {
		slot = &shotSlot{}
		a.shots[handle] = slot
	}
	if result.err != nil {
		slot.phase = coverFailed
		slot.image = nil
		slot.message = result.err.Error()
		a.clampCarouselLocked()
		return
	}
	if result.image == nil {
		slot.phase = coverFailed
		slot.image = nil
		a.clampCarouselLocked()
		return
	}
	slot.image = result.image
	slot.phase = coverReady
	slot.message = ""
}

func (a *App) queueFocusedScreenshotsLocked(queue []pendingWork) []pendingWork {
	if a.gpuParked || a.attractActive || !a.detailOpen {
		return queue
	}
	if len(a.inflight) >= maxInflight {
		return queue
	}
	if a.grid.Focus < 0 || a.grid.Focus >= len(a.games) {
		return queue
	}
	ids := a.focusDetailLocked().ScreenshotIDs
	if len(ids) == 0 {
		return queue
	}
	a.clampCarouselLocked()
	order := make([]string, 0, len(ids))
	if a.carouselIndex >= 0 && a.carouselIndex < len(ids) {
		order = append(order, ids[a.carouselIndex])
	}
	for _, handle := range ids {
		if a.carouselIndex >= 0 && a.carouselIndex < len(ids) && handle == ids[a.carouselIndex] {
			continue
		}
		order = append(order, handle)
	}
	for _, handle := range order {
		if len(a.inflight) >= maxInflight {
			break
		}
		key := screenshotWorkKey(handle)
		if _, busy := a.inflight[key]; busy {
			continue
		}
		slot := a.shots[handle]
		if slot == nil {
			slot = &shotSlot{phase: coverArtwork}
			a.shots[handle] = slot
		}
		switch slot.phase {
		case coverReady, coverFailed:
			continue
		}
		slot.phase = coverArtwork
		gameID := a.games[a.grid.Focus].ID
		a.screenshotContextLocked()
		a.inflight[key] = workScreenshot
		queue = append(queue, pendingWork{
			item: workItem{kind: workScreenshot, gameID: gameID, handle: handle, gen: a.loadGen, shotGen: a.shotGen},
			key:  key,
		})
		break
	}
	return queue
}

func (a *App) evictShotsLocked() {
	keep := map[string]struct{}{}
	for _, handle := range a.focusDetailLocked().ScreenshotIDs {
		keep[handle] = struct{}{}
	}
	for handle := range a.shots {
		if _, ok := keep[handle]; ok {
			continue
		}
		if _, busy := a.inflight[screenshotWorkKey(handle)]; busy {
			continue
		}
		delete(a.shots, handle)
	}
}

func (a *App) detailSnapshotLocked() DetailSnapshot {
	ids := a.focusDetailLocked().ScreenshotIDs
	index := clampCarouselIndex(a.carouselIndex, len(ids))
	return DetailSnapshot{
		Open:  a.detailOpen,
		Index: index,
		Count: len(ids),
		Hint:  detailHintFor(a.affinity.current.Kind, len(ids)),
	}
}

func (a *App) screenshotImagesLocked() map[string]*image.RGBA {
	ids := a.focusDetailLocked().ScreenshotIDs
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]*image.RGBA, len(ids))
	for _, handle := range ids {
		slot := a.shots[handle]
		if slot == nil || slot.image == nil {
			continue
		}
		out[handle] = slot.image
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func carouselCaption(index, count int) string {
	if count < 1 {
		return ""
	}
	return fmt.Sprintf("%d / %d", clampCarouselIndex(index, count)+1, count)
}

func detailPaneHeight(g Grid) int {
	h := g.contentHeight() - g.HeaderHeight - 16
	if h > detailPaneMaxH {
		h = detailPaneMaxH
	}
	if h < defaultFooterHeight {
		h = defaultFooterHeight
	}
	return h
}
