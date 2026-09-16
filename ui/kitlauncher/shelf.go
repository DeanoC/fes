package kitlauncher

import (
	"fmt"
	"github.com/DeanoC/FogCast/hostclient"
	"sort"
	"strings"
)

const ShelfAll = "all"

// fallbackCatalogSystems is the native kit catalog when GET /api/v1/platforms
// is unavailable. Shelves themselves are derived from loaded games.
var fallbackCatalogSystems = []string{"pong", "megadrive", "snes"}

var preferredShelfOrder = []string{"pong", "megadrive", "snes", "nes"}

func normalizeShelf(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return ShelfAll
	}
	return id
}

func deriveShelves(games []hostclient.Game) []string {
	seen := map[string]struct{}{}
	for _, game := range games {
		id := strings.ToLower(strings.TrimSpace(game.System))
		if id == "" {
			continue
		}
		seen[id] = struct{}{}
	}
	out := []string{ShelfAll}
	for _, id := range preferredShelfOrder {
		if _, ok := seen[id]; ok {
			out = append(out, id)
			delete(seen, id)
		}
	}
	extra := make([]string, 0, len(seen))
	for id := range seen {
		extra = append(extra, id)
	}
	sort.Strings(extra)
	return append(out, extra...)
}

func filterGames(games []hostclient.Game, shelf string) []hostclient.Game {
	shelf = normalizeShelf(shelf)
	if shelf == ShelfAll {
		out := make([]hostclient.Game, len(games))
		copy(out, games)
		return out
	}
	out := make([]hostclient.Game, 0, len(games))
	for _, game := range games {
		if strings.EqualFold(strings.TrimSpace(game.System), shelf) {
			out = append(out, game)
		}
	}
	return out
}

func focusedID(games []hostclient.Game, focus int) string {
	if focus < 0 || focus >= len(games) {
		return ""
	}
	return games[focus].ID
}

func focusIndex(games []hostclient.Game, keepID string) int {
	if keepID != "" {
		for i, game := range games {
			if game.ID == keepID {
				return i
			}
		}
	}
	for i, game := range games {
		if game.Launchable {
			return i
		}
	}
	return 0
}

// SetCatalog replaces the loaded library, rebuilds system shelves, and keeps
// the active shelf plus focused game when they are still present.
func (m *Model) SetCatalog(games []hostclient.Game) {
	keep := focusedID(m.Games, m.Focus)
	m.Catalog = append([]hostclient.Game(nil), games...)
	m.Shelves = deriveShelves(m.Catalog)
	m.searchPool = nil
	m.Shelf = normalizeShelf(m.Shelf)
	if !shelfPresent(m.Shelves, m.Shelf) {
		m.Shelf = ShelfAll
	}
	m.applyFilter(keep)
}

// ApplyCatalog merges a host catalog refresh without blanking the shelf.
// Identical lists are a no-op. Browse-identical lists (same ids/titles/covers)
// copy volatile fields in place so search and focus stay put.
func (m *Model) ApplyCatalog(games []hostclient.Game) {
	if m == nil {
		return
	}
	if games == nil {
		games = []hostclient.Game{}
	}
	if catalogListsEqual(m.Catalog, games) {
		return
	}
	if catalogBrowseEqual(m.Catalog, games) {
		m.replaceCatalogFields(games)
		return
	}
	m.SetCatalog(games)
}

func catalogListsEqual(a, b []hostclient.Game) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Title != b[i].Title || a[i].System != b[i].System ||
			a[i].Cover != b[i].Cover || a[i].Launchable != b[i].Launchable ||
			a[i].PlayCount != b[i].PlayCount || a[i].LastPlayedAt != b[i].LastPlayedAt ||
			a[i].Favorite != b[i].Favorite || romCachedValue(a[i].ROMCached) != romCachedValue(b[i].ROMCached) {
			return false
		}
	}
	return true
}

func catalogBrowseEqual(a, b []hostclient.Game) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Title != b[i].Title || a[i].System != b[i].System ||
			a[i].Cover != b[i].Cover || a[i].Launchable != b[i].Launchable {
			return false
		}
	}
	return true
}

func romCachedValue(v *bool) int {
	if v == nil {
		return 0
	}
	if *v {
		return 1
	}
	return -1
}

func (m *Model) replaceCatalogFields(games []hostclient.Game) {
	m.Catalog = append([]hostclient.Game(nil), games...)
	byID := make(map[string]hostclient.Game, len(games))
	for _, game := range games {
		byID[game.ID] = game
	}
	for i, game := range m.Games {
		if next, ok := byID[game.ID]; ok {
			m.Games[i] = next
		}
	}
	if m.searchPool != nil {
		for i, game := range m.searchPool {
			if next, ok := byID[game.ID]; ok {
				m.searchPool[i] = next
			}
		}
	}
}

// CycleShelf moves one system shelf, wrapping through All. Focus stays on the
// current game when it remains visible; otherwise it lands on the first
// launchable title of the new shelf.
func (m *Model) CycleShelf(dir int) {
	if dir == 0 || len(m.Shelves) < 2 {
		return
	}
	idx := 0
	shelf := normalizeShelf(m.Shelf)
	for i, id := range m.Shelves {
		if id == shelf {
			idx = i
			break
		}
	}
	n := len(m.Shelves)
	idx = (idx + dir) % n
	if idx < 0 {
		idx += n
	}
	keep := focusedID(m.Games, m.Focus)
	m.Shelf = m.Shelves[idx]
	m.applyFilter(keep)
}

func (m *Model) applyFilter(keepID string) {
	m.Shelf = normalizeShelf(m.Shelf)
	m.Games = filterSearch(m.baseGames(), m.SearchQuery)
	m.Focus = focusIndex(m.Games, keepID)
	m.refreshSeries()
	if m.DetailOpen && m.detailFromStrip {
		if _, ok := m.stripGame(); !ok {
			m.closeDetail()
			m.leaveStrip()
		}
		return
	}
	if m.DetailOpen && focusedID(m.Games, m.Focus) != keepID {
		m.closeDetail()
	}
}

func shelfPresent(shelves []string, id string) bool {
	for _, shelf := range shelves {
		if shelf == id {
			return true
		}
	}
	return false
}

func (m Model) activeShelf() string {
	return normalizeShelf(m.Shelf)
}

// ShelfLabel is the living-room name for the active shelf.
func (m Model) ShelfLabel() string {
	id := m.activeShelf()
	if id == ShelfAll {
		return "ALL"
	}
	return strings.ToUpper(id)
}

// ShelfCounts is visible/total for chrome (shelf size over full catalog).
func (m Model) ShelfCounts() (visible, total int) {
	visible = len(m.Games)
	total = len(m.Catalog)
	if total == 0 {
		total = visible
	}
	return visible, total
}

// ShelfChrome is "MEGADRIVE 12/40".
func (m Model) ShelfChrome() string {
	visible, total := m.ShelfCounts()
	return fmt.Sprintf("%s %d/%d", m.ShelfLabel(), visible, total)
}

// HeaderChrome is the themed header: FOGCAST, or FOGCAST plus the active shelf.
func (m Model) HeaderChrome() string {
	if len(m.Catalog) == 0 && len(m.Games) == 0 {
		return "FOGCAST"
	}
	return "FOGCAST  " + m.ShelfChrome()
}
