package kitlauncher

import (
	"strings"

	"github.com/DeanoC/FogCast/ui/tenfoot"
)

const stripMax = 6

// ComposeStrip builds a single living-room row from host recents and
// favorites. Recents win on duplicates. Empty inputs yield a hidden row.
func ComposeStrip(recents, favorites []tenfoot.Game) (games []tenfoot.Game, label string) {
	games = make([]tenfoot.Game, 0, stripMax)
	seen := map[string]struct{}{}
	add := func(list []tenfoot.Game) int {
		added := 0
		for _, game := range list {
			if len(games) >= stripMax {
				break
			}
			id := strings.TrimSpace(game.ID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			games = append(games, game)
			added++
		}
		return added
	}
	nRecent := add(recents)
	nFav := add(favorites)
	switch {
	case nRecent > 0 && nFav > 0:
		label = "Recent / Favorites"
	case nFav > 0:
		label = "Favorites"
	case nRecent > 0:
		label = "Recent"
	}
	return games, label
}

// SetStrip replaces the recent/favorites row. An empty row hides and leaves
// strip focus.
func (m *Model) SetStrip(games []tenfoot.Game, label string) {
	if m == nil {
		return
	}
	keep := ""
	if m.StripFocus >= 0 && m.StripFocus < len(m.Strip) {
		keep = m.Strip[m.StripFocus].ID
	}
	m.Strip = append([]tenfoot.Game(nil), games...)
	m.StripLabel = strings.TrimSpace(label)
	if len(m.Strip) == 0 {
		if m.detailFromStrip {
			m.closeDetail()
		}
		m.leaveStrip()
		m.StripFocus = 0
		m.StripLabel = ""
		return
	}
	m.StripFocus = 0
	if keep != "" {
		for i, game := range m.Strip {
			if game.ID == keep {
				m.StripFocus = i
				break
			}
		}
	}
}

// ApplyStrip updates the recent/favorites row without resetting focus when
// the membership is unchanged.
func (m *Model) ApplyStrip(games []tenfoot.Game, label string) {
	if m == nil {
		return
	}
	label = strings.TrimSpace(label)
	if catalogListsEqual(m.Strip, games) && m.StripLabel == label {
		return
	}
	m.SetStrip(games, label)
}

func (m *Model) stripGame() (tenfoot.Game, bool) {
	if m == nil || m.StripFocus < 0 || m.StripFocus >= len(m.Strip) {
		return tenfoot.Game{}, false
	}
	return m.Strip[m.StripFocus], true
}

func (m *Model) enterStrip() {
	if m == nil || len(m.Strip) == 0 {
		return
	}
	m.StripActive = true
	if m.StripFocus < 0 || m.StripFocus >= len(m.Strip) {
		m.StripFocus = 0
	}
}

func (m *Model) leaveStrip() {
	if m == nil {
		return
	}
	m.StripActive = false
	m.detailFromStrip = false
}

func (m *Model) inputStrip(dx, dy int) {
	if dy < 0 {
		m.leaveStrip()
		return
	}
	if dx == 0 || len(m.Strip) == 0 {
		return
	}
	next := m.StripFocus + dx
	if next < 0 {
		next = 0
	}
	if next >= len(m.Strip) {
		next = len(m.Strip) - 1
	}
	m.StripFocus = next
}

func (m Model) stripHint() string {
	return "A detail | B grid | L/R"
}
