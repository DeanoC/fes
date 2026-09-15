package kitlauncher

import (
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/FogCast/ui/tenfoot/anim"
)

const (
	defaultAttractIdle  = 60 * time.Second
	defaultAttractCycle = 12 * time.Second
	defaultAttractLimit = 24
	attractIdleRefresh  = 15 * time.Second
	attractWallSize     = 4
)

// AttractWallTile is one cell of the optional 2×2 attract wall.
type AttractWallTile struct {
	Handle string
	Motion bool
}

// AttractView is the stills/motion stage the renderer paints while idle attract is on.
type AttractView struct {
	Active     bool
	Empty      bool
	Title      string
	GameID     string
	Platform   string
	Handle     string
	NextHandle string
	Index      int
	FadeT      float64
	Motion     bool
	Caption    string
	ShotIndex  int
	Wall       []AttractWallTile
	Marquee    string
}

func playableStillItems(items []tenfoot.AttractItem) []tenfoot.AttractItem {
	out := make([]tenfoot.AttractItem, 0, len(items))
	for _, item := range items {
		if item.StillHandle() != "" {
			out = append(out, item)
		}
	}
	return out
}

func attractIdleDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultAttractIdle
	}
	return d
}

func (m *Model) cycleHold() time.Duration {
	if m.attractCycle > 0 {
		return m.attractCycle
	}
	return defaultAttractCycle
}

// SetAttractPlaylist stores host attract rows and idle_seconds. Video-only
// rows are dropped because the CGO-free kit path does not decode H.264;
// video plus stills keep the row for a kit-safe motion preview.
func (m *Model) SetAttractPlaylist(p tenfoot.AttractPlaylist) {
	if p.IdleSeconds > 0 {
		m.attractIdle = time.Duration(p.IdleSeconds) * time.Second
	}
	m.attractIdleReady = true
	items := playableStillItems(p.Items)
	m.attractItems = items
	if !m.AttractActive {
		return
	}
	if len(items) == 0 {
		m.attractIndex = 0
		return
	}
	m.attractIndex = m.attractIndex % len(items)
}

// HydrateAttractIdle marks the idle timer ready with the default 60s when the
// host attract GET fails. An empty playlist still enters the idle panel.
func (m *Model) HydrateAttractIdle() {
	if m.attractIdle <= 0 {
		m.attractIdle = defaultAttractIdle
	}
	m.attractIdleReady = true
}

// SetAttractIdle overrides the host idle for tests and -selftest-attract.
func (m *Model) SetAttractIdle(d time.Duration) {
	m.attractIdle = attractIdleDuration(d)
	m.attractIdleReady = true
}

// SetAttractCycle overrides the still hold for tests and -selftest-attract.
func (m *Model) SetAttractCycle(d time.Duration) {
	if d <= 0 {
		d = defaultAttractCycle
	}
	m.attractCycle = d
}

func (m *Model) noteActivity(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	m.lastInput = now
	if m.AttractActive {
		m.hideAttract()
	}
}

func (m *Model) hideAttract() {
	m.AttractActive = false
	m.attractIndex = 0
	m.attractShownAt = time.Time{}
	m.attractCycleAt = time.Time{}
	m.resetAttractPreview()
}

func (m *Model) resetAttractPreview() {
	m.attractShotIndex = 0
	m.attractPreviewAt = time.Time{}
}

func (m *Model) attractBlocked() bool {
	if m.Busy || !m.Connected || !m.TargetReady || m.DetailOpen || m.SearchOpen {
		return true
	}
	switch m.Session.State {
	case "active", "launching", "stopping", "failed":
		return true
	}
	return false
}

func (m *Model) enterAttract(now time.Time) {
	m.AttractActive = true
	m.attractIndex = 0
	m.attractShownAt = now
	m.attractCycleAt = now.Add(m.cycleHold())
	m.resetAttractPreview()
}

func (m *Model) tickAttract(now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	if m.lastInput.IsZero() {
		m.lastInput = now
	}
	if m.attractBlocked() {
		if m.AttractActive {
			m.hideAttract()
		}
		return
	}
	if m.AttractActive {
		if len(m.attractItems) > 1 && !now.Before(m.attractCycleAt) {
			m.attractIndex = (m.attractIndex + 1) % len(m.attractItems)
			m.attractShownAt = now
			m.attractCycleAt = now.Add(m.cycleHold())
			m.resetAttractPreview()
		}
		m.tickAttractPreview(now)
		return
	}
	if !m.attractIdleReady {
		return
	}
	if now.Sub(m.lastInput) < attractIdleDuration(m.attractIdle) {
		return
	}
	m.enterAttract(now)
	m.tickAttractPreview(now)
}

func (m *Model) currentAttractItem() (tenfoot.AttractItem, bool) {
	if !m.AttractActive || len(m.attractItems) == 0 {
		return tenfoot.AttractItem{}, false
	}
	if m.attractIndex < 0 {
		m.attractIndex = 0
	}
	return m.attractItems[m.attractIndex%len(m.attractItems)], true
}

func (m Model) attractPresentationFor(id string) tenfoot.Presentation {
	if m.attractPresentationID == id {
		return m.attractPresentation
	}
	return tenfoot.Presentation{}
}

func (m Model) attractPreviewHandles(item tenfoot.AttractItem) []string {
	return tenfoot.AttractPreviewHandles(item, m.attractPresentationFor(item.GameID))
}

func (m Model) attractHasMotion(item tenfoot.AttractItem) bool {
	p := m.attractPresentationFor(item.GameID)
	video := item.VideoHandle()
	if video == "" {
		video = tenfoot.VideoHandle(p)
	}
	if video == "" {
		return false
	}
	return len(tenfoot.AttractPreviewHandles(item, p)) > 0
}

func (m Model) attractWallEnabled() bool {
	if len(m.attractItems) < attractWallSize {
		return false
	}
	for _, item := range m.attractItems {
		if item.VideoHandle() != "" {
			return true
		}
	}
	return false
}

func (m *Model) clampAttractShot() {
	n := 0
	if item, ok := m.currentAttractItem(); ok {
		n = len(m.attractPreviewHandles(item))
	}
	if n < 1 {
		m.attractShotIndex = 0
		return
	}
	if m.attractShotIndex < 0 {
		m.attractShotIndex = 0
	}
	if m.attractShotIndex >= n {
		m.attractShotIndex = n - 1
	}
}

func (m *Model) tickAttractPreview(now time.Time) {
	if m == nil || !m.AttractActive {
		return
	}
	item, ok := m.currentAttractItem()
	if !ok || !m.attractHasMotion(item) {
		return
	}
	ids := m.attractPreviewHandles(item)
	if len(ids) < 2 {
		return
	}
	if m.attractFading(now) {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	if m.attractPreviewAt.IsZero() {
		m.attractPreviewAt = now
		return
	}
	if now.Sub(m.attractPreviewAt) < defaultPreviewCycle {
		return
	}
	m.attractShotIndex = (m.attractShotIndex + 1) % len(ids)
	m.attractPreviewAt = now
}

func (m Model) attractFading(now time.Time) bool {
	if m.attractWallEnabled() || len(m.attractItems) < 2 {
		return false
	}
	hold := m.cycleHold()
	fade := anim.StillFade
	if fade > hold/2 {
		fade = hold / 2
	}
	if fade <= 0 || m.attractShownAt.IsZero() {
		return false
	}
	return now.Sub(m.attractShownAt) >= hold-fade
}

// ApplyAttractPresentation stores host presentation for the staged attract title.
func (m *Model) ApplyAttractPresentation(id string, p tenfoot.Presentation) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	item, ok := m.currentAttractItem()
	if !ok || item.GameID != id {
		return
	}
	m.attractPresentationID = id
	m.attractPresentation = p
	m.clampAttractShot()
}

func attractPreviewCaption(index, count int) string {
	if count < 2 {
		return "preview"
	}
	if index < 0 {
		index = 0
	}
	if index >= count {
		index = count - 1
	}
	return fmt.Sprintf("preview %d / %d", index+1, count)
}

func (m Model) attractMarqueeHandle(item tenfoot.AttractItem) string {
	handle := tenfoot.AttractMarqueeHandle(item, m.attractPresentationFor(item.GameID))
	if handle == "" {
		return ""
	}
	if item.StillHandle() == handle {
		return ""
	}
	return handle
}

func (m Model) attractShotHandle(item tenfoot.AttractItem) string {
	if !m.attractHasMotion(item) {
		return item.StillHandle()
	}
	ids := m.attractPreviewHandles(item)
	if len(ids) == 0 {
		return item.StillHandle()
	}
	idx := m.attractShotIndex
	if idx < 0 {
		idx = 0
	}
	if idx >= len(ids) {
		idx = len(ids) - 1
	}
	return ids[idx]
}

func (m Model) attractWallTiles() []AttractWallTile {
	if !m.AttractActive || !m.attractWallEnabled() {
		return nil
	}
	out := make([]AttractWallTile, attractWallSize)
	n := len(m.attractItems)
	for i := 0; i < attractWallSize; i++ {
		item := m.attractItems[(m.attractIndex+i)%n]
		handle := item.StillHandle()
		motion := false
		if i == 0 {
			handle = m.attractShotHandle(item)
			motion = m.attractHasMotion(item)
		}
		out[i] = AttractWallTile{Handle: handle, Motion: motion}
	}
	return out
}

func (m *Model) padDelta(e remoteinput.Event) (dx, dy int) {
	if e.Kind == remoteinput.KindAxis {
		switch e.Code {
		case remoteinput.AxisLeftX:
			return m.axisStep(&m.axisX, e.Value), 0
		case remoteinput.AxisLeftY:
			return 0, m.axisStep(&m.axisY, e.Value)
		}
		return 0, 0
	}
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress {
		switch e.Code {
		case remoteinput.ButtonDPadUp:
			return 0, -1
		case remoteinput.ButtonDPadDown:
			return 0, 1
		case remoteinput.ButtonDPadLeft:
			return -1, 0
		case remoteinput.ButtonDPadRight:
			return 1, 0
		}
	}
	return 0, 0
}

func significantPad(e remoteinput.Event, dx, dy int) bool {
	if dx != 0 || dy != 0 {
		return true
	}
	return e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress
}

func (m *Model) inputAttract(e remoteinput.Event, dx, dy int, now time.Time) string {
	if !significantPad(e, dx, dy) {
		return ""
	}
	item, ok := m.currentAttractItem()
	m.noteActivity(now)
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress && e.Code == remoteinput.ButtonA && ok && item.Launchable && strings.TrimSpace(item.GameID) != "" && m.canLaunch() {
		// focusedGame prefers the strip while it is active; attract A launches the still.
		m.leaveStrip()
		if !m.focusGame(item.GameID) {
			m.launchID = strings.TrimSpace(item.GameID)
		}
		return "launch"
	}
	return ""
}

func (m *Model) focusGame(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for i, game := range m.Games {
		if game.ID == id {
			m.Focus = i
			return true
		}
	}
	found := false
	for _, game := range m.Catalog {
		if game.ID == id {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	m.Shelf = ShelfAll
	m.applyFilter(id)
	return focusedID(m.Games, m.Focus) == id
}

func (m *Model) consumeLaunchID() string {
	id := strings.TrimSpace(m.launchID)
	m.launchID = ""
	if id != "" {
		return id
	}
	if game, ok := m.focusedGame(); ok {
		return game.ID
	}
	return ""
}

// AttractView is the current stills/motion stage, including crossfade progress.
func (m *Model) AttractView(now time.Time) AttractView {
	if !m.AttractActive {
		return AttractView{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	view := AttractView{Active: true, Empty: len(m.attractItems) == 0}
	if view.Empty {
		view.Title = "FOGCAST"
		return view
	}
	item, ok := m.currentAttractItem()
	if !ok {
		view.Empty = true
		view.Title = "FOGCAST"
		return view
	}
	view.Title = strings.TrimSpace(item.Title)
	if view.Title == "" {
		view.Title = strings.TrimSpace(item.GameID)
	}
	view.GameID = item.GameID
	view.Platform = item.Platform
	view.Handle = m.attractShotHandle(item)
	view.Marquee = m.attractMarqueeHandle(item)
	view.Index = m.attractIndex
	view.Motion = m.attractHasMotion(item)
	if view.Motion {
		ids := m.attractPreviewHandles(item)
		view.ShotIndex = m.attractShotIndex
		if view.ShotIndex < 0 {
			view.ShotIndex = 0
		}
		if n := len(ids); n > 0 && view.ShotIndex >= n {
			view.ShotIndex = n - 1
		}
		view.Caption = attractPreviewCaption(view.ShotIndex, len(ids))
	}
	view.Wall = m.attractWallTiles()
	if len(view.Wall) >= attractWallSize || len(m.attractItems) < 2 {
		return view
	}
	next := m.attractItems[(m.attractIndex+1)%len(m.attractItems)]
	view.NextHandle = next.StillHandle()
	hold := m.cycleHold()
	fade := anim.StillFade
	if fade > hold/2 {
		fade = hold / 2
	}
	if fade <= 0 {
		return view
	}
	elapsed := now.Sub(m.attractShownAt)
	start := hold - fade
	if elapsed >= start {
		view.FadeT = float64(elapsed-start) / float64(fade)
		if view.FadeT > 1 {
			view.FadeT = 1
		}
		if view.FadeT < 0 {
			view.FadeT = 0
		}
	}
	return view
}

// AttractPrefetchHandles is the current preview stills plus the next title's stills.
func (m *Model) AttractPrefetchHandles() []string {
	if !m.AttractActive || len(m.attractItems) == 0 {
		return nil
	}
	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(handle string) {
		handle = strings.TrimSpace(handle)
		if handle == "" {
			return
		}
		if _, ok := seen[handle]; ok {
			return
		}
		seen[handle] = struct{}{}
		out = append(out, handle)
	}
	item, ok := m.currentAttractItem()
	if ok {
		for _, handle := range m.attractPreviewHandles(item) {
			add(handle)
		}
		add(item.StillHandle())
		add(m.attractMarqueeHandle(item))
	}
	if m.attractWallEnabled() {
		n := len(m.attractItems)
		for i := 1; i < attractWallSize; i++ {
			add(m.attractItems[(m.attractIndex+i)%n].StillHandle())
		}
		return out
	}
	if len(m.attractItems) > 1 {
		next := m.attractItems[(m.attractIndex+1)%len(m.attractItems)]
		add(next.StillHandle())
	}
	return out
}
