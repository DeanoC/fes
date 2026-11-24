package tenfoot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/shared"
)

func (a *App) drainResults() {
	for {
		select {
		case result := <-a.results:
			a.applyResult(result)
		default:
			return
		}
	}
}

func (a *App) applyResult(result workResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if result.gen != a.loadGen {
		return
	}
	if a.gpuParked {
		delete(a.inflight, result.key())
		return
	}
	delete(a.inflight, result.key())
	if result.kind == workScreenshot {
		if result.shotGen != a.shotGen {
			return
		}
		if result.err != nil && errors.Is(result.err, context.Canceled) {
			return
		}
		a.applyScreenshotResultLocked(result)
		return
	}
	if result.err != nil && (errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded)) {
		return
	}
	if result.kind == workPresentation && result.err == nil {
		if shots := shared.ScreenshotHandles(result.screenshotIDs); len(shots) > 0 {
			a.shotIDs[result.gameID] = shots
		} else {
			delete(a.shotIDs, result.gameID)
		}
		if presentationComplete(result.state, result.attribution) {
			a.details[result.gameID] = shared.FocusDetail{
				Year:          strings.TrimSpace(result.year),
				Genre:         strings.TrimSpace(result.genre),
				Studio:        strings.TrimSpace(result.studio),
				Players:       strings.TrimSpace(result.players),
				Summary:       strings.TrimSpace(result.summary),
				Attribution:   strings.TrimSpace(result.attribution),
				ScreenshotIDs: shared.ScreenshotHandles(result.screenshotIDs),
			}
		}
		a.clampCarouselLocked()
	}
	if !a.inPrefetchLocked(result.gameID) {
		delete(a.covers, result.gameID)
		return
	}
	slot := a.covers[result.gameID]
	if slot == nil {
		slot = &coverSlot{}
		a.covers[result.gameID] = slot
	}
	if result.kind == workPresentation {
		if result.err != nil || !presentationComplete(result.state, result.attribution) {
			schedulePresentationRetry(slot)
		} else {
			slot.detailTry = 0
			slot.detailNext = time.Time{}
		}
	}
	if result.err != nil {
		if slot.phase == coverReady {
			return
		}
		if result.kind == workPresentation && slot.handle != "" {
			if slot.phase != coverFailed {
				slot.phase = coverArtwork
			}
			return
		}
		slot.phase = coverFailed
		slot.message = result.err.Error()
		return
	}
	switch result.kind {
	case workPresentation:
		complete := presentationComplete(result.state, result.attribution)
		if slot.phase == coverReady && !complete {
			return
		}
		prevHandle := slot.handle
		if result.handle != "" || result.missing {
			slot.handle = result.handle
		}
		if slot.handle == "" {
			slot.image = nil
			slot.phase = coverMissing
			return
		}
		if slot.handle == prevHandle && (slot.phase == coverReady || slot.phase == coverFailed) {
			return
		}
		if slot.handle != prevHandle {
			slot.image = nil
		}
		slot.phase = coverArtwork
	case workArtwork:
		slot.image = result.image
		slot.phase = coverReady
	}
}

func presentationComplete(state, attribution string) bool {
	state = strings.TrimSpace(state)
	switch strings.ToLower(state) {
	case "offline":
		return false
	case "ready":
		return strings.TrimSpace(attribution) != ""
	case "disabled", "unconfigured", "no_match", "ambiguous":
		return true
	default:
		return true
	}
}

func presentationRetryDelay(fails int) time.Duration {
	if fails < 1 {
		fails = 1
	}
	delay := presentationRetryMin
	for i := 1; i < fails && delay < presentationRetryMax; i++ {
		delay *= 2
	}
	if delay > presentationRetryMax {
		return presentationRetryMax
	}
	return delay
}

func schedulePresentationRetry(slot *coverSlot) {
	if slot == nil {
		return
	}
	slot.detailTry++
	slot.detailNext = time.Now().Add(presentationRetryDelay(slot.detailTry))
}

func (a *App) queueVisibleWork(now time.Time) {
	a.mu.Lock()
	if a.gpuParked || a.attractActive {
		a.mu.Unlock()
		return
	}
	// An open room replaces the library grid on screen, so the grid's covers
	// are not visible work. The room requests its own covers. Prefetching the
	// hidden grid would only take the CPU from the room's Lua callbacks.
	start, end := 0, 0
	if a.room == nil {
		start, end = a.prefetchSpanLocked()
	} else {
		a.dropHiddenGridWorkLocked()
	}
	var queue []pendingWork
	order := make([]int, 0, end-start)
	if a.grid.Focus >= start && a.grid.Focus < end {
		order = append(order, a.grid.Focus)
	}
	for i := start; i < end; i++ {
		if i == a.grid.Focus {
			continue
		}
		order = append(order, i)
	}
	for _, i := range order {
		if len(a.inflight) >= maxInflight {
			break
		}
		game := a.games[i]
		slot := a.covers[game.ID]
		if slot == nil {
			slot = &coverSlot{phase: coverIdle}
			if handle := hostclient.NormalizeHandle(game.Cover); handle != "" {
				slot.handle = handle
				slot.phase = coverArtwork
			}
			a.covers[game.ID] = slot
		}
		if _, have := a.details[game.ID]; !have && slot.phase != coverIdle && slot.phase != coverArtwork {
			if _, busy := a.inflight[game.ID]; !busy && i == a.grid.Focus && !now.Before(slot.detailNext) {
				a.inflight[game.ID] = workPresentation
				queue = append(queue, pendingWork{item: workItem{kind: workPresentation, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
				if len(a.inflight) >= maxInflight {
					break
				}
			}
		}
		if _, busy := a.inflight[game.ID]; busy {
			continue
		}
		switch slot.phase {
		case coverIdle:
			a.inflight[game.ID] = workPresentation
			queue = append(queue, pendingWork{item: workItem{kind: workPresentation, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
		case coverArtwork:
			if slot.handle == "" {
				slot.phase = coverMissing
				continue
			}
			a.inflight[game.ID] = workArtwork
			queue = append(queue, pendingWork{item: workItem{kind: workArtwork, gameID: game.ID, handle: slot.handle, gen: a.loadGen}, key: game.ID})
		}
		if len(a.inflight) >= maxInflight {
			break
		}
	}
	queue = a.queueRoomDetailWorkLocked(now, queue)
	queue = a.queueLibraryDetailWorkLocked(now, queue)
	queue = a.queueFocusedScreenshotsLocked(queue)
	a.evictCoversLocked()
	a.evictShotsLocked()
	a.mu.Unlock()
	for _, item := range queue {
		select {
		case a.jobs <- item.item:
		default:
			a.mu.Lock()
			delete(a.inflight, item.key)
			a.mu.Unlock()
		}
	}
}

// dropHiddenGridWorkLocked removes library-grid cover work that was queued
// before a room opened. Workers have not started these jobs, so clearing their
// inflight markers lets the grid request them again once it is back on screen.
// Decodes that a worker already started cannot be cancelled; their results are
// applied as usual. Room detail and screenshot work stays queued, and the
// room's own covers use requestRoomCoverLocked, not this queue.
func (a *App) dropHiddenGridWorkLocked() {
	var keep []workItem
	for drained := false; !drained; {
		select {
		case item := <-a.jobs:
			switch {
			case item.gen != a.loadGen:
				// Stale generation: workers would drop it anyway.
			case item.kind == workScreenshot || (a.detailOpen && a.roomDetail.ID != "" && item.gameID == a.roomDetail.ID):
				keep = append(keep, item)
			default:
				delete(a.inflight, item.key())
			}
		default:
			drained = true
		}
	}
	for _, item := range keep {
		select {
		case a.jobs <- item:
		default:
			delete(a.inflight, item.key())
		}
	}
}

func (a *App) prefetchSpanLocked() (int, int) {
	start, end := a.grid.PrefetchRange(prefetchRows)
	if start < 0 {
		start = 0
	}
	if end > len(a.games) {
		end = len(a.games)
	}
	return start, end
}

func (a *App) inPrefetchLocked(gameID string) bool {
	if a.room != nil && a.detailOpen && a.roomDetail.ID == gameID {
		return true
	}
	if a.room == nil && a.detailOpen && a.libraryDetail.ID == gameID {
		return true
	}
	start, end := a.prefetchSpanLocked()
	for i := start; i < end; i++ {
		if a.games[i].ID == gameID {
			return true
		}
	}
	return false
}

func (a *App) evictCoversLocked() {
	start, end := a.prefetchSpanLocked()
	keep := make(map[string]struct{}, end-start)
	for i := start; i < end; i++ {
		keep[a.games[i].ID] = struct{}{}
	}
	if a.room != nil && a.detailOpen && a.roomDetail.ID != "" {
		keep[a.roomDetail.ID] = struct{}{}
	}
	if a.room == nil && a.detailOpen && strings.TrimSpace(a.libraryDetail.ID) != "" {
		keep[a.libraryDetail.ID] = struct{}{}
	}
	for id := range a.covers {
		if _, ok := keep[id]; ok {
			continue
		}
		// Drop the slot even when its request is still running. A later result
		// is discarded by applyResult if the game remains outside the window;
		// retaining the slot would expose stale artwork until that request ends.
		delete(a.covers, id)
	}
}

func (a *App) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-a.jobs:
			a.mu.Lock()
			jobCtx := a.jobCtx
			gen := a.loadGen
			parked := a.gpuParked
			shotGen := a.shotGen
			shotCtx := a.shotCtx
			a.mu.Unlock()
			if jobCtx == nil {
				jobCtx = ctx
			}
			workCtx := jobCtx
			if item.kind == workScreenshot && shotCtx != nil {
				workCtx = shotCtx
			}
			if item.gen != gen || parked || (item.kind == workScreenshot && item.shotGen != shotGen) {
				a.dropStaleWork(item)
				continue
			}
			result := a.doWork(workCtx, item)
			if item.gen != gen || workCtx.Err() != nil {
				a.dropStaleWork(item)
				continue
			}
			if item.kind == workScreenshot {
				a.mu.Lock()
				staleShot := item.shotGen != a.shotGen
				a.mu.Unlock()
				if staleShot {
					a.dropStaleWork(item)
					continue
				}
			}
			select {
			case a.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (a *App) dropStaleWork(item workItem) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loadGen != item.gen {
		return
	}
	if item.kind == workScreenshot && a.shotGen != item.shotGen {
		return
	}
	delete(a.inflight, item.key())
}

func (a *App) doWork(ctx context.Context, item workItem) workResult {
	switch item.kind {
	case workPresentation:
		pres, err := a.client.GamePresentation(ctx, item.gameID)
		if err != nil {
			return workResult{kind: workPresentation, gameID: item.gameID, gen: item.gen, err: err}
		}
		handle := shared.CoverHandle(hostclient.Game{ID: item.gameID}, pres)
		result := workResult{kind: workPresentation, gameID: item.gameID, handle: handle, missing: handle == "", state: pres.State, gen: item.gen}
		if pres.Presentation != nil {
			result.year = pres.Presentation.Year
			result.genre = pres.Presentation.Genre
			result.summary = pres.Presentation.Summary
			result.studio = pres.Presentation.Studio
			result.players = pres.Presentation.Players
			result.screenshotIDs = shared.ScreenshotHandles(pres.Presentation.ScreenshotIDs)
		}
		result.attribution = pres.AttributionLabel()
		return result
	case workArtwork:
		data, _, err := a.client.Artwork(ctx, item.handle)
		if err != nil {
			return workResult{kind: workArtwork, gameID: item.gameID, gen: item.gen, err: err}
		}
		img, err := shared.DecodeCover(data)
		if err != nil {
			return workResult{kind: workArtwork, gameID: item.gameID, gen: item.gen, err: err}
		}
		return workResult{kind: workArtwork, gameID: item.gameID, handle: item.handle, image: img, gen: item.gen}
	case workScreenshot:
		data, _, err := a.client.Artwork(ctx, item.handle)
		if err != nil {
			return workResult{kind: workScreenshot, gameID: item.gameID, handle: item.handle, gen: item.gen, shotGen: item.shotGen, err: err}
		}
		img, err := shared.DecodeScreenshot(data)
		if err != nil {
			return workResult{kind: workScreenshot, gameID: item.gameID, handle: item.handle, gen: item.gen, shotGen: item.shotGen, err: err}
		}
		return workResult{kind: workScreenshot, gameID: item.gameID, handle: item.handle, image: img, gen: item.gen, shotGen: item.shotGen}
	default:
		return workResult{kind: item.kind, gameID: item.gameID, gen: item.gen, err: fmt.Errorf("unknown work")}
	}
}
