package kitlauncher

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

const untitledSearchLabel = "UNTITLED"

// SearchTag is the short header suffix while search is open or a query is
// filtering the current shelf. Empty when browse is unfiltered.
func (m Model) SearchTag() string {
	if m.SearchOpen || strings.TrimSpace(m.SearchQuery) != "" {
		return "SEARCH"
	}
	return ""
}

// SearchSnapshot is the renderer-facing keyboard overlay. Closed when the
// OSK is not open; a committed query can still filter the shelf.
func (m Model) SearchSnapshot() tenfoot.OSKSnapshot {
	if !m.SearchOpen {
		return tenfoot.OSKSnapshot{}
	}
	snap := m.searchField.Snapshot()
	snap.Open = true
	snap.Prompt = "Search"
	snap.Hint = tenfoot.OSKKitHint(snap.Page)
	return snap
}

// SearchHaystack is the living-room name used for substring matching: the
// title, else the clear-logo wordmark fallback (system id), else UNTITLED.
func SearchHaystack(game tenfoot.Game) string {
	if title := strings.TrimSpace(game.Title); title != "" {
		return title
	}
	if system := strings.TrimSpace(game.System); system != "" {
		return system
	}
	return untitledSearchLabel
}

func foldSearch(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func matchSearch(game tenfoot.Game, foldedQuery string) bool {
	if foldedQuery == "" {
		return true
	}
	return strings.Contains(foldSearch(SearchHaystack(game)), foldedQuery)
}

func filterSearch(games []tenfoot.Game, query string) []tenfoot.Game {
	folded := foldSearch(query)
	if folded == "" {
		return games
	}
	out := make([]tenfoot.Game, 0, len(games))
	for _, game := range games {
		if matchSearch(game, folded) {
			out = append(out, game)
		}
	}
	return out
}

func (m *Model) baseGames() []tenfoot.Game {
	if m == nil {
		return nil
	}
	if m.Shelves != nil {
		return filterGames(m.Catalog, m.Shelf)
	}
	if m.searchPool != nil {
		return append([]tenfoot.Game(nil), m.searchPool...)
	}
	return m.Games
}

func (m *Model) setSearchQuery(q string) {
	if m == nil {
		return
	}
	keep := focusedID(m.Games, m.Focus)
	if foldSearch(q) == "" {
		keep = m.searchRestoreID
	}
	m.SearchQuery = q
	m.searchField.Buffer = q
	m.applyFilter(keep)
}

func (m *Model) openSearch(now time.Time) {
	if m == nil {
		return
	}
	if m.WheelOpen {
		m.enterPlatform()
	}
	m.closeDetail()
	m.leaveStrip()
	if !m.SearchOpen {
		if m.searchRestoreID == "" {
			m.searchRestoreID = focusedID(m.Games, m.Focus)
		}
		if m.Shelves == nil && m.searchPool == nil {
			m.searchPool = append([]tenfoot.Game(nil), m.Games...)
		}
		m.searchField.Buffer = m.SearchQuery
		m.searchField.OSK.Reset()
	}
	m.SearchOpen = true
	m.noteActivity(now)
}

func (m *Model) closeSearchOSK(now time.Time) {
	if m == nil || !m.SearchOpen {
		return
	}
	m.SearchOpen = false
	m.setSearchQuery(m.searchField.Buffer)
	if foldSearch(m.SearchQuery) == "" {
		m.exitSearch(now)
		return
	}
	m.noteActivity(now)
}

func (m *Model) exitSearch(now time.Time) {
	if m == nil {
		return
	}
	keep := m.searchRestoreID
	m.SearchOpen = false
	m.SearchQuery = ""
	m.searchField.Clear()
	m.searchRestoreID = ""
	m.applyFilter(keep)
	m.searchPool = nil
	m.noteActivity(now)
}

// FocusSearchKey focuses an OSK cell by id so tests and -selftest-search can
// type through the existing gamepad keyboard without a hardware QWERTY.
func (m *Model) FocusSearchKey(id string) bool {
	if m == nil || !m.SearchOpen {
		return false
	}
	return m.searchField.OSK.SelectID(id)
}

func (m *Model) inputSearch(e remoteinput.Event, dx, dy int, now time.Time) string {
	if significantPad(e, dx, dy) {
		m.noteActivity(now)
	}
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress {
		switch e.Code {
		case remoteinput.ButtonStart:
			m.closeSearchOSK(now)
			return ""
		case remoteinput.ButtonA:
			result := m.searchField.Activate()
			if result.Changed {
				m.setSearchQuery(m.searchField.Buffer)
			}
			if result.Done {
				m.closeSearchOSK(now)
			}
			return ""
		case remoteinput.ButtonB:
			if strings.TrimSpace(m.searchField.Buffer) != "" {
				m.searchField.Clear()
				m.setSearchQuery("")
				return ""
			}
			m.exitSearch(now)
			return ""
		case remoteinput.ButtonL:
			m.searchField.CyclePage(-1)
			return ""
		case remoteinput.ButtonR:
			m.searchField.CyclePage(1)
			return ""
		case remoteinput.ButtonY, remoteinput.ButtonX, remoteinput.ButtonSelect:
			return ""
		}
	}
	if dx != 0 || dy != 0 {
		m.searchField.Move(dx, dy)
	}
	return ""
}
