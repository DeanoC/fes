package kitlauncher

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/anim"
	"github.com/DeanoC/FogCast/remoteinput"
)

const (
	defaultAttractIdle  = 60 * time.Second
	defaultAttractCycle = 12 * time.Second
	defaultAttractLimit = 24
	attractIdleRefresh  = 15 * time.Second
)

// AttractView is the stills stage the renderer paints while idle attract is on.
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
// rows are dropped; kit v1 paints stills (backdrop, then cover, then marquee).
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
}

func (m *Model) attractBlocked() bool {
	if m.Busy || !m.Connected || !m.TargetReady || m.DetailOpen {
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
		}
		return
	}
	if !m.attractIdleReady {
		return
	}
	if now.Sub(m.lastInput) < attractIdleDuration(m.attractIdle) {
		return
	}
	m.enterAttract(now)
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
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress && e.Code == remoteinput.ButtonA && ok && item.Launchable && strings.TrimSpace(item.GameID) != "" {
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
	if len(m.Games) == 0 || m.Focus < 0 || m.Focus >= len(m.Games) {
		return ""
	}
	return m.Games[m.Focus].ID
}

// AttractView is the current stills stage, including crossfade progress.
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
	view.Handle = item.StillHandle()
	view.Index = m.attractIndex
	if len(m.attractItems) < 2 {
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

// AttractPrefetchHandles is the current still plus the next still for Keep/Request.
func (m *Model) AttractPrefetchHandles() []string {
	if !m.AttractActive || len(m.attractItems) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
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
		add(item.StillHandle())
	}
	if len(m.attractItems) > 1 {
		next := m.attractItems[(m.attractIndex+1)%len(m.attractItems)]
		add(next.StillHandle())
	}
	return out
}
