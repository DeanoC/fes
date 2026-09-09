package kitlauncher

import (
	"fmt"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
	"github.com/DeanoC/FogCast/remoteinput"
)

// WheelItem is one platform on the living-room wheel.
type WheelItem struct {
	ID    string
	Label string
	Count int
}

func (m *Model) inputWheel(e remoteinput.Event, dx, dy int, now time.Time) string {
	if significantPad(e, dx, dy) {
		m.noteActivity(now)
	}
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress {
		switch e.Code {
		case remoteinput.ButtonL:
			m.CycleShelf(-1)
			return ""
		case remoteinput.ButtonR, remoteinput.ButtonSelect:
			m.CycleShelf(1)
			return ""
		case remoteinput.ButtonA:
			m.enterPlatform()
			return ""
		case remoteinput.ButtonX:
			m.CyclePack()
			return ""
		}
	}
	if dx != 0 {
		m.CycleShelf(dx)
		return ""
	}
	if dy != 0 {
		m.CycleShelf(dy)
	}
	return ""
}

func (m *Model) enterPlatform() {
	if m == nil || !m.WheelOpen || len(m.Shelves) == 0 {
		return
	}
	m.WheelOpen = false
	m.fromWheel = true
}

func (m *Model) showWheel(now time.Time) {
	if m == nil {
		return
	}
	m.closeDetail()
	m.leaveStrip()
	m.WheelOpen = true
	m.fromWheel = false
	m.noteActivity(now)
}

func (m *Model) leavePlatform(now time.Time) {
	if m == nil || !m.fromWheel {
		return
	}
	m.showWheel(now)
}

// WheelIndex is the focused platform in Shelves.
func (m Model) WheelIndex() int {
	shelf := m.activeShelf()
	for i, id := range m.Shelves {
		if id == shelf {
			return i
		}
	}
	if len(m.Shelves) == 0 {
		return 0
	}
	return 0
}

// WheelItems is All plus each system, with per-shelf game counts.
func (m Model) WheelItems() []WheelItem {
	items := make([]WheelItem, 0, len(m.Shelves))
	for _, id := range m.Shelves {
		games := filterGames(m.Catalog, id)
		label := strings.ToUpper(id)
		if id == ShelfAll {
			label = "ALL"
		}
		items = append(items, WheelItem{ID: id, Label: label, Count: len(games)})
	}
	return items
}

// WheelStats is the platform-header count chrome: title count, plus a play
// rollup when host games already carry play_count.
func (m Model) WheelStats() string {
	n, _ := m.ShelfCounts()
	games := "0 games"
	switch n {
	case 1:
		games = "1 game"
	default:
		games = fmt.Sprintf("%d games", n)
	}
	plays := m.WheelPlayCount()
	if plays < 1 {
		return games
	}
	if plays == 1 {
		return games + "  |  1 play"
	}
	return games + fmt.Sprintf("  |  %d plays", plays)
}

// WheelPlayCount sums admitted play_count values on the focused shelf.
func (m Model) WheelPlayCount() int64 {
	var n int64
	for _, game := range filterGames(m.Catalog, m.activeShelf()) {
		if game.PlayCount > 0 {
			n += game.PlayCount
		}
	}
	return n
}

// WheelLastPlayed is the shelf title with the newest last_played_at, else the
// first recents row on that shelf. Empty when the host has not admitted either.
func (m Model) WheelLastPlayed() (tenfoot.Game, bool) {
	var best tenfoot.Game
	var at int64
	found := false
	for _, game := range filterGames(m.Catalog, m.activeShelf()) {
		if game.LastPlayedAt > at {
			at = game.LastPlayedAt
			best = game
			found = true
		}
	}
	if found {
		return best, true
	}
	shelf := m.activeShelf()
	for _, game := range m.Recents {
		if shelf != ShelfAll && !strings.EqualFold(strings.TrimSpace(game.System), shelf) {
			continue
		}
		if strings.TrimSpace(game.Title) == "" && strings.TrimSpace(game.ID) == "" {
			continue
		}
		return game, true
	}
	return tenfoot.Game{}, false
}

// WheelFeaturedTitle prefers last-played when the host admitted it, else a
// cheap title from the focused shelf.
func (m Model) WheelFeaturedTitle() string {
	if game, ok := m.WheelLastPlayed(); ok {
		return strings.TrimSpace(game.Title)
	}
	game, ok := m.WheelGame(m.activeShelf())
	if !ok {
		return ""
	}
	return strings.TrimSpace(game.Title)
}

// WheelGame is the first launchable title on shelf, else the first row.
func (m Model) WheelGame(shelf string) (tenfoot.Game, bool) {
	games := filterGames(m.Catalog, shelf)
	for _, game := range games {
		if game.Launchable {
			return game, true
		}
	}
	if len(games) > 0 {
		return games[0], true
	}
	return tenfoot.Game{}, false
}

// WheelPrefetchIDs is one representative game per shelf, focused first.
func (m Model) WheelPrefetchIDs() []string {
	ids := make([]string, 0, len(m.Shelves))
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if game, ok := m.WheelGame(m.activeShelf()); ok {
		add(game.ID)
	}
	for _, shelf := range m.Shelves {
		if game, ok := m.WheelGame(shelf); ok {
			add(game.ID)
		}
	}
	return ids
}

// WheelHeroHandle prefers an attract still for the focused platform, then a
// presentation backdrop, then the representative cover. Empty means placeholder.
func (m Model) WheelHeroHandle(pres tenfoot.Presentation) string {
	if handle := m.wheelAttractHandle(); handle != "" {
		return handle
	}
	if handle := tenfoot.BackdropHandle(pres); handle != "" {
		return handle
	}
	if game, ok := m.WheelGame(m.activeShelf()); ok {
		return tenfoot.CoverHandle(game, pres)
	}
	return ""
}

// WheelLogoHandle is a representative-title clear logo when presentation has one.
func (m Model) WheelLogoHandle(pres tenfoot.Presentation) string {
	return tenfoot.LogoHandle(pres)
}

func (m Model) wheelAttractHandle() string {
	shelf := m.activeShelf()
	for _, item := range m.attractItems {
		if shelf != ShelfAll && !strings.EqualFold(strings.TrimSpace(item.Platform), shelf) {
			continue
		}
		if handle := item.StillHandle(); handle != "" {
			return handle
		}
	}
	return ""
}

// WheelHint is the idle footer on the platform wheel. X names the next pack.
func (m Model) WheelHint() string {
	short := theme.PackShort(theme.NextPack(m.Pack))
	if short == "" {
		return "A open | L/R platform"
	}
	return "A open | L/R platform | X " + short
}

// GridHint is the idle footer on the filtered game grid.
func (m Model) GridHint() string {
	if m.StripActive {
		return m.stripHint()
	}
	next := "Y " + m.Browse.Next().ShortLabel()
	if m.fromWheel {
		return "A play | B platforms | L/R | " + next
	}
	return "A play | B detail | L/R | " + next
}
