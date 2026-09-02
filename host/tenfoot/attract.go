package tenfoot

import (
	"context"
	"image"
	"strings"
	"time"
)

const defaultAttractCycle = 12 * time.Second

type attractResult struct {
	gen    int
	handle string
	image  *image.RGBA
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

func (a *App) setSafeAreaPctLocked(pct float64, persist bool) {
	pct = clampSafeAreaPct(pct)
	a.safeAreaPct = pct
	a.grid.Safe = insetsFromPct(a.grid.Width, a.grid.Height, pct)
	a.grid.Layout(a.grid.Width, a.grid.Height)
	if persist {
		path := a.prefsPath
		if path == "" {
			path = defaultPrefsPath()
		}
		if path != "" {
			_ = saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: pct})
		}
	}
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
	was := a.attractActive || a.attractLoading || a.attractImage != nil
	a.attractActive = false
	a.attractItems = nil
	a.attractIndex = 0
	a.attractImage = nil
	a.attractHandle = ""
	a.attractTitle = ""
	a.attractLoading = false
	a.hold.Clear()
	if was {
		a.attractGen++
	}
}

func stillAttractItems(items []AttractItem) []AttractItem {
	out := make([]AttractItem, 0, len(items))
	for _, item := range items {
		if item.StillHandle() != "" {
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
	return a.attractDisabled || a.session.State == "active" || a.stopPhase == "stopping" || a.searchOpen || a.viewPickerOpen
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
		if len(a.attractItems) > 1 && !now.Before(a.attractCycleAt) {
			a.attractIndex = (a.attractIndex + 1) % len(a.attractItems)
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
	if now.Sub(a.lastInput) < idle {
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
	}
	if !enter {
		return
	}
	items := stillAttractItems(playlist.Items)
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

func (a *App) showAttractItemLocked(now time.Time) {
	item, ok := a.currentAttractItemLocked()
	if !ok {
		a.hideAttractLocked()
		return
	}
	handle := item.StillHandle()
	if handle == "" {
		start := a.attractIndex
		for {
			if len(a.attractItems) == 0 {
				a.hideAttractLocked()
				return
			}
			a.attractIndex = (a.attractIndex + 1) % len(a.attractItems)
			if a.attractIndex == start {
				a.hideAttractLocked()
				return
			}
			item, ok = a.currentAttractItemLocked()
			if !ok {
				a.hideAttractLocked()
				return
			}
			handle = item.StillHandle()
			if handle != "" {
				break
			}
		}
	}
	cycle := a.attractCycle
	if cycle <= 0 {
		cycle = defaultAttractCycle
	}
	a.attractCycleAt = now.Add(cycle)
	a.attractTitle = item.Title
	a.attractImage = nil
	a.attractHandle = handle
	a.attractGen++
	gen := a.attractGen
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go a.fetchAttractArtwork(ctx, gen, handle)
}

func (a *App) fetchAttractArtwork(ctx context.Context, gen int, handle string) {
	data, _, err := a.client.Artwork(ctx, handle)
	result := attractResult{gen: gen, handle: handle, err: err}
	if err == nil {
		img, decodeErr := DecodeStill(data)
		result.image = img
		result.err = decodeErr
	}
	select {
	case a.attractResults <- result:
	case <-ctx.Done():
	default:
	}
}

func (a *App) drainAttractResults() {
	for {
		select {
		case result := <-a.attractResults:
			a.mu.Lock()
			if result.gen == a.attractGen && a.attractActive && result.handle == a.attractHandle && result.err == nil {
				a.attractImage = result.image
			}
			a.mu.Unlock()
		default:
			return
		}
	}
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
