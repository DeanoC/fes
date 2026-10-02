package kitlauncher

import (
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/shared"
	"github.com/DeanoC/FogCast/ui/theme"
	"strings"
	"time"
)

// WheelItem is one platform on the living-room wheel.
type WheelItem struct {
	ID    string
	Label string
	Count int
}

// Summaries belong to the catalog revision, not the presentation tick.
type wheelSummary struct {
	count               int
	plays               int64
	first, launch, last int
	lastAt              int64
}

func emptyWheelSummary() wheelSummary {
	return wheelSummary{first: -1, launch: -1, last: -1}
}

func (s *wheelSummary) add(game *hostclient.Game, i int) {
	if s.count == 0 {
		s.first = i
	}
	s.count++
	if s.launch < 0 && game.Launchable {
		s.launch = i
	}
	if game.PlayCount > 0 {
		s.plays += game.PlayCount
	}
	if game.LastPlayedAt > s.lastAt {
		s.lastAt, s.last = game.LastPlayedAt, i
	}
}

func (m *Model) rebuildWheelSummaries() {
	all := emptyWheelSummary()
	m.wheelSummaries = make(map[string]wheelSummary, len(m.Shelves))
	for i := range m.Catalog {
		game := &m.Catalog[i]
		all.add(game, i)
		shelf := strings.ToLower(strings.TrimSpace(game.System))
		if shelf == "" || shelf == ShelfAll {
			continue
		}
		summary, ok := m.wheelSummaries[shelf]
		if !ok {
			summary = emptyWheelSummary()
		}
		summary.add(game, i)
		m.wheelSummaries[shelf] = summary
	}
	m.wheelSummaries[ShelfAll] = all
}

func (m Model) wheelSummary(shelf string) wheelSummary {
	shelf = normalizeShelf(shelf)
	if m.wheelSummaries != nil {
		if summary, ok := m.wheelSummaries[shelf]; ok {
			return summary
		}
		return emptyWheelSummary()
	}
	// Models constructed directly by embedders also work without SetCatalog.
	summary := emptyWheelSummary()
	for i := range m.Catalog {
		game := &m.Catalog[i]
		if shelf == ShelfAll || strings.EqualFold(strings.TrimSpace(game.System), shelf) {
			summary.add(game, i)
		}
	}
	return summary
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
	if m.SearchTag() != "" {
		m.exitSearch(now)
	}
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
		count := m.wheelSummary(id).count
		label := strings.ToUpper(id)
		if id == ShelfAll {
			label = "ALL"
		}
		items = append(items, WheelItem{ID: id, Label: label, Count: count})
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
	switch {
	case plays == 1:
		games = games + "  |  1 play"
	case plays > 1:
		games = games + fmt.Sprintf("  |  %d plays", plays)
	}
	if chrome := m.Cache.Chrome(); chrome != "" {
		return games + "  |  " + chrome
	}
	return games
}

// WheelPlayCount sums admitted play_count values on the focused shelf.
func (m Model) WheelPlayCount() int64 {
	return m.wheelSummary(m.activeShelf()).plays
}

// WheelLastPlayed is the shelf title with the newest last_played_at, else the
// first recents row on that shelf. Empty when the host has not admitted either.
func (m Model) WheelLastPlayed() (hostclient.Game, bool) {
	shelf := m.activeShelf()
	summary := m.wheelSummary(shelf)
	if summary.last >= 0 {
		return m.Catalog[summary.last], true
	}

	for _, game := range m.Recents {
		if shelf != ShelfAll && !strings.EqualFold(strings.TrimSpace(game.System), shelf) {
			continue
		}
		if strings.TrimSpace(game.Title) == "" && strings.TrimSpace(game.ID) == "" {
			continue
		}
		return game, true
	}
	return hostclient.Game{}, false
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
func (m Model) WheelGame(shelf string) (hostclient.Game, bool) {
	summary := m.wheelSummary(shelf)
	if summary.launch >= 0 {
		return m.Catalog[summary.launch], true
	}
	if summary.first >= 0 {
		return m.Catalog[summary.first], true
	}
	return hostclient.Game{}, false
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
func (m Model) WheelHeroHandle(pres hostclient.Presentation) string {
	if handle := m.wheelAttractHandle(); handle != "" {
		return handle
	}
	if handle := shared.BackdropHandle(pres); handle != "" {
		return handle
	}
	if game, ok := m.WheelGame(m.activeShelf()); ok {
		return shared.CoverHandle(game, pres)
	}
	return ""
}

// WheelLogoHandle is a representative-title clear logo when presentation has one.
func (m Model) WheelLogoHandle(pres hostclient.Presentation) string {
	return shared.LogoHandle(pres)
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
	if m.SearchOpen {
		return shared.OSKKitHint(m.searchField.Snapshot().Page)
	}
	if m.StripActive {
		return m.stripHint()
	}
	if m.SeriesActive {
		return m.seriesHint()
	}
	next := "Y " + m.Browse.Next().ShortLabel()
	if m.fromWheel {
		return "A play | B platforms | L/R | " + next
	}
	return "A play | B detail | L/R | " + next
}
