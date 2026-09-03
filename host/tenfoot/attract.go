package tenfoot

import (
	"context"
	"image"
	"os"
	"strings"
	"time"
)

const (
	defaultAttractCycle       = 12 * time.Second
	defaultAttractIdleRefresh = 15 * time.Second
	maxAttractVideo           = 60 * time.Second
)

type attractResult struct {
	gen    int
	handle string
	image  *image.RGBA
	player attractPlayer
	video  bool
	err    error
}

// AttractSnapshot is the attract stage shown by the renderer.
type AttractSnapshot struct {
	Active      bool
	Title       string
	GameID      string
	Platform    string
	Launchable  bool
	Handle      string
	Image       *image.RGBA
	Video       bool
	FrameSeq    int
	IdleSeconds int
}

func attractIdleDuration(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = defaultAttractIdleSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (a *App) SetAttractDisabled(disabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.attractDisabled = disabled
	if disabled {
		a.hideAttractLocked()
	}
}

func (a *App) SetSafeAreaPct(pct float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setSafeAreaPctLocked(pct, false)
}

func (a *App) SetLayout(mode LayoutKind) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setLayoutLocked(mode, false)
}

func (a *App) setSafeAreaPctLocked(pct float64, persist bool) {
	pct = clampSafeAreaPct(pct)
	a.safeAreaPct = pct
	a.grid.Safe = insetsFromPct(a.grid.Width, a.grid.Height, pct)
	a.grid.Layout(a.grid.Width, a.grid.Height)
	if persist {
		a.persistPrefsLocked("safe-area")
	}
}

func (a *App) setLayoutLocked(mode LayoutKind, persist bool) {
	a.grid.Mode = parseLayout(mode.String())
	a.grid.Layout(a.grid.Width, a.grid.Height)
	if persist {
		a.persistPrefsLocked("layout")
	}
}

func (a *App) cycleLayoutLocked() {
	a.setLayoutLocked(a.grid.Mode.Next(), true)
	a.status = "layout " + a.grid.Mode.Label()
}

func (a *App) persistPrefsLocked(field string) {
	path := a.prefsPath
	if path == "" {
		path = defaultPrefsPath()
	}
	if path == "" {
		return
	}
	existing, err := loadTenfootPrefs(path)
	if err != nil {
		existing = tenfootPrefs{
			SafeAreaPct: DefaultSafeAreaPct,
			Layout:      LayoutGrid.String(),
		}
	}
	switch field {
	case "safe-area":
		existing.SafeAreaPct = a.safeAreaPct
	case "layout":
		existing.Layout = a.grid.Mode.String()
	case "attract":
		existing.AttractEnabled = boolPtr(a.attractPrefEnabled)
	default:
		return
	}
	_ = saveTenfootPrefs(path, existing)
}

func (a *App) SetPrefsPath(path string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prefsPath = path
}

func (a *App) AttractActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.attractActive
}

// DismissAttract hides attract and resets the idle timer. Unmapped input uses this.
func (a *App) DismissAttract(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noteActivityLocked(now)
	if a.attractActive {
		a.hideAttractLocked()
	}
}

func (a *App) noteActivityLocked(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	a.lastInput = now
}

func (a *App) hideAttractLocked() {
	was := a.attractActive || a.attractLoading || a.attractImage != nil || a.attractPlayer != nil || a.attractVideoPath != ""
	a.cancelAttractMediaLocked()
	a.stopAttractVideoLocked()
	a.releaseAttractVideoFileLocked()
	a.attractActive = false
	a.attractItems = nil
	a.attractIndex = 0
	a.attractImage = nil
	a.attractHandle = ""
	a.attractTitle = ""
	a.attractLoading = false
	a.attractTried = nil
	a.hold.Clear()
	if was {
		a.attractGen++
	}
}

func (a *App) stopAttractVideoLocked() {
	if a.attractPlayer != nil {
		a.attractPlayer.Close()
		a.attractPlayer = nil
	}
	a.attractVideo = false
	a.attractEnded = false
	a.attractFrameSeq = 0
}

func (a *App) cancelAttractMediaLocked() {
	if a.attractMediaCancel != nil {
		a.attractMediaCancel()
		a.attractMediaCancel = nil
	}
}

func (a *App) replaceAttractMediaContextLocked() context.Context {
	a.cancelAttractMediaLocked()
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	a.attractMediaCancel = cancel
	return ctx
}

func (a *App) releaseAttractVideoFileLocked() {
	if a.attractVideoPath != "" {
		_ = os.Remove(a.attractVideoPath)
		a.attractVideoPath = ""
	}
}

func playableAttractItems(items []AttractItem) []AttractItem {
	out := make([]AttractItem, 0, len(items))
	for _, item := range items {
		if normalizeHandle(item.Video) != "" || item.StillHandle() != "" {
			out = append(out, item)
		}
	}
	return out
}

func (a *App) consumeAttractLocked(cmd Command, now time.Time) bool {
	if !a.attractActive {
		return false
	}
	if cmd == CmdQuit {
		return false
	}
	item, ok := a.currentAttractItemLocked()
	a.hideAttractLocked()
	a.noteActivityLocked(now)
	if cmd == CmdSelect && ok && item.Launchable && strings.TrimSpace(item.GameID) != "" {
		a.startLaunchAttractLocked(item)
	}
	return true
}

func (a *App) currentAttractItemLocked() (AttractItem, bool) {
	if !a.attractActive || len(a.attractItems) == 0 {
		return AttractItem{}, false
	}
	if a.attractIndex < 0 {
		a.attractIndex = 0
	}
	return a.attractItems[a.attractIndex%len(a.attractItems)], true
}

func (a *App) attractBlockedLocked() bool {
	switch a.session.State {
	case "active", "launching", "stopping":
		return true
	}
	switch a.launch.Phase {
	case "launching":
		return true
	}
	return a.attractDisabled || a.stopPhase == "stopping" || a.searchOpen || a.viewPickerOpen || a.settingsOpen
}

func (a *App) tickAttractLocked(now time.Time) {
	if a.lastInput.IsZero() {
		a.lastInput = now
	}
	if a.attractBlockedLocked() {
		if a.attractActive {
			a.hideAttractLocked()
		}
		return
	}
	if a.attractActive {
		// Once frame 1 has armed the cap, do not decode catch-up samples.
		pastCap := a.attractVideo && a.attractFrameSeq > 0 && !now.Before(a.attractCycleAt)
		if !pastCap {
			if a.pumpAttractVideoLocked(now) {
				return
			}
		}
		if a.attractShouldAdvanceLocked(now) {
			if len(a.attractItems) > 1 {
				a.attractIndex = (a.attractIndex + 1) % len(a.attractItems)
			}
			a.showAttractItemLocked(now)
		}
		return
	}
	if a.attractLoading {
		return
	}
	if !a.attractIdleReady {
		a.startAttractFetchLocked(false)
		return
	}
	idle := a.attractIdle
	if idle <= 0 {
		idle = attractIdleDuration(defaultAttractIdleSeconds)
	}
	refresh := a.attractIdleRefresh
	if refresh <= 0 {
		refresh = defaultAttractIdleRefresh
	}
	stale := a.attractIdleAt.IsZero() || now.Sub(a.attractIdleAt) >= refresh
	if now.Sub(a.lastInput) < idle {
		if stale {
			a.startAttractFetchLocked(false)
		}
		return
	}
	a.startAttractFetchLocked(true)
}

func (a *App) startAttractFetchLocked(enter bool) {
	a.attractLoading = true
	a.attractGen++
	gen := a.attractGen
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.fetchAttract(ctx, gen, enter)
}

func (a *App) hydrateAttractIdle(ctx context.Context) {
	a.mu.Lock()
	if a.attractDisabled || a.attractIdleReady || a.attractLoading {
		a.mu.Unlock()
		return
	}
	a.startAttractFetchLocked(false)
	a.mu.Unlock()
}

func (a *App) applyAttractIdleLocked(playlist AttractPlaylist) {
	if playlist.IdleSeconds > 0 {
		a.attractIdle = attractIdleDuration(playlist.IdleSeconds)
		a.attractIdleSeconds = playlist.IdleSeconds
	}
	a.attractIdleReady = true
	a.attractIdleAt = time.Now()
}

func (a *App) fetchAttract(ctx context.Context, gen int, enter bool) {
	playlist, err := a.client.Attract(ctx, defaultAttractLimit)
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.attractGen {
		return
	}
	a.attractLoading = false
	if err == nil {
		a.applyAttractIdleLocked(playlist)
	} else {
		a.attractIdleReady = true
		a.attractIdleAt = time.Now()
	}
	if !enter {
		return
	}
	items := playableAttractItems(playlist.Items)
	if err != nil || a.attractBlockedLocked() || len(items) == 0 {
		a.lastInput = time.Now()
		return
	}
	now := time.Now()
	idle := a.attractIdle
	if idle <= 0 {
		idle = attractIdleDuration(defaultAttractIdleSeconds)
	}
	if now.Sub(a.lastInput) < idle {
		return
	}
	a.attractItems = items
	a.attractIndex = 0
	a.attractActive = true
	a.hold.Clear()
	a.showAttractItemLocked(now)
}

func (a *App) nextAttractMediaLocked(item AttractItem) (handle string, video bool) {
	if a.attractTried == nil {
		a.attractTried = map[string]bool{}
	}
	if videoHandle := normalizeHandle(item.Video); videoHandle != "" && !a.attractTried[videoHandle] {
		return videoHandle, true
	}
	for _, handle := range item.stillHandles() {
		if !a.attractTried[handle] {
			return handle, false
		}
	}
	return "", false
}

func (a *App) attractShouldAdvanceLocked(now time.Time) bool {
	if a.attractVideo {
		if a.attractPlayer == nil || a.attractFrameSeq == 0 {
			return false
		}
		if a.attractEnded {
			return true
		}
		return !now.Before(a.attractCycleAt)
	}
	return len(a.attractItems) > 1 && !now.Before(a.attractCycleAt)
}

func (a *App) abandonAttractLocked(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	a.lastInput = now
	a.hideAttractLocked()
}

func (a *App) showAttractItemLocked(now time.Time) {
	if len(a.attractItems) == 0 {
		a.abandonAttractLocked(now)
		return
	}
	start := a.attractIndex
	for {
		item, ok := a.currentAttractItemLocked()
		if !ok {
			a.abandonAttractLocked(now)
			return
		}
		handle, video := a.nextAttractMediaLocked(item)
		if handle != "" {
			reuseVideo := video && a.attractVideoPath != "" && a.attractHandle == handle
			cachedPath := a.attractVideoPath
			a.stopAttractVideoLocked()
			if !reuseVideo {
				a.releaseAttractVideoFileLocked()
				a.attractImage = nil
			}
			if !video {
				cycle := a.attractCycle
				if cycle <= 0 {
					cycle = defaultAttractCycle
				}
				a.attractCycleAt = now.Add(cycle)
			}
			a.attractTitle = item.Title
			a.attractHandle = handle
			a.attractVideo = video
			a.attractEnded = false
			a.attractGen++
			gen := a.attractGen
			ctx := a.replaceAttractMediaContextLocked()
			if video {
				if reuseVideo {
					go a.openCachedAttractVideo(ctx, gen, handle, cachedPath)
				} else {
					opener := a.openAttractVideo
					go a.fetchAttractVideo(ctx, gen, handle, opener)
				}
			} else {
				go a.fetchAttractArtwork(ctx, gen, handle)
			}
			return
		}
		a.attractIndex = (a.attractIndex + 1) % len(a.attractItems)
		if a.attractIndex == start {
			a.abandonAttractLocked(now)
			return
		}
	}
}

func (a *App) fetchAttractArtwork(ctx context.Context, gen int, handle string) {
	data, _, err := a.client.Artwork(ctx, handle)
	result := attractResult{gen: gen, handle: handle, err: err}
	if err == nil {
		img, decodeErr := DecodeStill(data)
		result.image = img
		result.err = decodeErr
	}
	a.sendAttractResult(ctx, result)
}

func (a *App) fetchAttractVideo(ctx context.Context, gen int, handle string, opener func(context.Context, *Client, string) (attractPlayer, error)) {
	if opener == nil {
		opener = defaultOpenAttractVideo
	}
	player, err := opener(ctx, a.client, handle)
	if err != nil && player != nil {
		player.Close()
		player = nil
	}
	a.sendAttractResult(ctx, attractResult{gen: gen, handle: handle, player: player, video: true, err: err})
}

func (a *App) openCachedAttractVideo(ctx context.Context, gen int, handle, path string) {
	opener := a.openAttractCached
	var player attractPlayer
	var err error
	if opener != nil {
		player, err = opener(path)
	} else {
		player, err = openAttractVideoPath(path)
	}
	if err != nil && player != nil {
		player.Close()
		player = nil
	}
	a.sendAttractResult(ctx, attractResult{gen: gen, handle: handle, player: player, video: true, err: err})
}

func (a *App) sendAttractResult(ctx context.Context, result attractResult) {
	closePlayer := func() {
		if result.player != nil {
			result.player.Close()
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		closePlayer()
		return
	}
	a.mu.Lock()
	closed := a.attractClosed
	a.mu.Unlock()
	if closed {
		closePlayer()
		return
	}
	select {
	case a.attractResults <- result:
		a.mu.Lock()
		closed = a.attractClosed
		a.mu.Unlock()
		if closed {
			a.drainAttractResults()
		}
	case <-ctx.Done():
		closePlayer()
	}
}

func (a *App) drainAttractResults() {
	for {
		select {
		case result := <-a.attractResults:
			a.mu.Lock()
			if result.gen != a.attractGen || !a.attractActive || result.handle != a.attractHandle {
				if result.player != nil {
					result.player.Close()
				}
				a.mu.Unlock()
				continue
			}
			if result.video {
				if result.err != nil || result.player == nil {
					a.markAttractTriedLocked(result.handle)
					a.showAttractItemLocked(time.Now())
					a.mu.Unlock()
					continue
				}
				if path := takeAttractVideoFile(result.player); path != "" {
					a.attractVideoPath = path
				}
				a.attractPlayer = result.player
				a.attractVideo = true
				a.pumpAttractVideoLocked(time.Now())
				a.mu.Unlock()
				continue
			}
			if result.err == nil && result.image != nil {
				a.attractImage = result.image
			} else {
				a.markAttractTriedLocked(result.handle)
				a.showAttractItemLocked(time.Now())
			}
			a.mu.Unlock()
		default:
			return
		}
	}
}

func (a *App) markAttractTriedLocked(handle string) {
	if a.attractTried == nil {
		a.attractTried = map[string]bool{}
	}
	a.attractTried[handle] = true
}

// pumpAttractVideoLocked copies the current frame. It returns true when it
// already moved to a fallback still or the next row.
func (a *App) pumpAttractVideoLocked(now time.Time) bool {
	if !a.attractVideo || a.attractPlayer == nil {
		return false
	}
	img, ended, err := a.attractPlayer.Frame()
	if err != nil {
		a.markAttractTriedLocked(a.attractHandle)
		a.stopAttractVideoLocked()
		a.showAttractItemLocked(now)
		return true
	}
	if img != nil {
		a.attractImage = img
		a.attractFrameSeq++
		if a.attractFrameSeq == 1 {
			a.attractCycleAt = now.Add(maxAttractVideo)
		}
	}
	if ended && a.attractFrameSeq == 0 {
		a.markAttractTriedLocked(a.attractHandle)
		a.stopAttractVideoLocked()
		a.showAttractItemLocked(now)
		return true
	}
	if ended {
		a.attractEnded = true
	}
	return false
}

func (a *App) attractSnapshotLocked() AttractSnapshot {
	item, _ := a.currentAttractItemLocked()
	idle := a.attractIdleSeconds
	if idle <= 0 {
		idle = defaultAttractIdleSeconds
	}
	return AttractSnapshot{
		Active:      a.attractActive,
		Title:       a.attractTitle,
		GameID:      item.GameID,
		Platform:    item.Platform,
		Launchable:  item.Launchable,
		Handle:      a.attractHandle,
		Image:       a.attractImage,
		Video:       a.attractVideo && a.attractPlayer != nil,
		FrameSeq:    a.attractFrameSeq,
		IdleSeconds: idle,
	}
}

func (a *App) startLaunchAttractLocked(item AttractItem) {
	if a.launch.Phase == "launching" || a.sessionStopOfferedLocked() {
		return
	}
	for i, game := range a.games {
		if game.ID == item.GameID {
			if a.grid.Focus != i {
				a.navDirty = true
			}
			a.grid.Focus = i
			a.grid.ensureVisible()
			a.startLaunchGameLocked(game)
			return
		}
	}
	game := Game{
		ID:         item.GameID,
		Title:      item.Title,
		System:     item.Platform,
		Launchable: item.Launchable,
		State:      "available",
		RootOnline: true,
	}
	a.startLaunchGameLocked(game)
}
