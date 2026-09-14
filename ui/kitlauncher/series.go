package kitlauncher

import (
	"strings"
	"time"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/tenfoot"
)

// ComposeSeries builds in-catalog series/related mates for the focused title.
func ComposeSeries(catalog []tenfoot.Game, focused tenfoot.Game, p tenfoot.Presentation) (games []tenfoot.Game, label string) {
	return tenfoot.SeriesMates(catalog, focused, p)
}

// SetSeries replaces the series/related row. An empty row hides and leaves
// series focus.
func (m *Model) SetSeries(games []tenfoot.Game, label string) {
	if m == nil {
		return
	}
	keep := ""
	if m.SeriesFocus >= 0 && m.SeriesFocus < len(m.Series) {
		keep = m.Series[m.SeriesFocus].ID
	}
	m.Series = append([]tenfoot.Game(nil), games...)
	m.SeriesLabel = strings.TrimSpace(label)
	if len(m.Series) == 0 {
		m.leaveSeries()
		m.SeriesFocus = 0
		m.SeriesLabel = ""
		return
	}
	m.SeriesFocus = 0
	if keep != "" {
		for i, game := range m.Series {
			if game.ID == keep {
				m.SeriesFocus = i
				break
			}
		}
	}
	if m.SeriesFocus >= len(m.Series) {
		m.SeriesFocus = len(m.Series) - 1
	}
}

func (m *Model) seriesSubject() (tenfoot.Game, bool) {
	if m == nil {
		return tenfoot.Game{}, false
	}
	if m.DetailOpen {
		return m.focusedGame()
	}
	if m.Focus < 0 || m.Focus >= len(m.Games) {
		return tenfoot.Game{}, false
	}
	return m.Games[m.Focus], true
}

func (m *Model) refreshSeries() {
	if m == nil {
		return
	}
	game, ok := m.seriesSubject()
	if !ok {
		m.SetSeries(nil, "")
		return
	}
	p := tenfoot.Presentation{}
	if m.presentationID == game.ID {
		p = m.presentation
	}
	mates, label := ComposeSeries(m.Catalog, game, p)
	m.SetSeries(mates, label)
}

func (m *Model) seriesGame() (tenfoot.Game, bool) {
	if m == nil || m.SeriesFocus < 0 || m.SeriesFocus >= len(m.Series) {
		return tenfoot.Game{}, false
	}
	return m.Series[m.SeriesFocus], true
}

func (m *Model) enterSeries() {
	if m == nil || len(m.Series) == 0 {
		return
	}
	m.SeriesActive = true
	if m.SeriesFocus < 0 || m.SeriesFocus >= len(m.Series) {
		m.SeriesFocus = 0
	}
}

func (m *Model) leaveSeries() {
	if m == nil {
		return
	}
	m.SeriesActive = false
}

func (m *Model) inputSeries(dx, dy int) {
	if dy < 0 || (dx < 0 && m.SeriesFocus <= 0 && !m.DetailOpen) {
		m.leaveSeries()
		return
	}
	if dx == 0 || len(m.Series) == 0 {
		return
	}
	next := m.SeriesFocus + dx
	if next < 0 {
		next = 0
	}
	if next >= len(m.Series) {
		next = len(m.Series) - 1
	}
	m.SeriesFocus = next
}

func (m *Model) jumpToSeries(now time.Time) {
	game, ok := m.seriesGame()
	if !ok {
		return
	}
	m.jumpTo(game, now)
}

func (m *Model) jumpTo(game tenfoot.Game, now time.Time) {
	id := strings.TrimSpace(game.ID)
	if id == "" {
		return
	}
	found := false
	system := ""
	for _, row := range m.Catalog {
		if row.ID == id {
			found = true
			system = row.System
			game = row
			break
		}
	}
	if !found {
		return
	}
	m.leaveSeries()
	m.leaveStrip()
	m.detailFromStrip = false
	m.presentationID = ""
	m.presentation = tenfoot.Presentation{}
	m.shotIndex = 0
	m.previewAt = time.Time{}
	shelf := normalizeShelf(system)
	if !shelfPresent(m.Shelves, shelf) {
		shelf = ShelfAll
	}
	m.Shelf = shelf
	m.applyFilter(id)
	m.openDetail(now)
	m.refreshSeries()
}

func (m *Model) inputSeriesBrowse(e remoteinput.Event, dx, dy int, now time.Time) string {
	if !significantPad(e, dx, dy) {
		return ""
	}
	m.noteActivity(now)
	if e.Kind == remoteinput.KindButton && e.Action == remoteinput.ActionPress {
		switch e.Code {
		case remoteinput.ButtonA:
			m.jumpToSeries(now)
			return ""
		case remoteinput.ButtonB:
			m.leaveSeries()
			return ""
		case remoteinput.ButtonX:
			m.CyclePack()
			return ""
		case remoteinput.ButtonY:
			m.leaveSeries()
			m.CycleBrowse()
			return ""
		}
	}
	if dx != 0 || dy != 0 {
		m.inputSeries(dx, dy)
	}
	return ""
}

func (m Model) seriesHint() string {
	if m.DetailOpen {
		return "A open | B title | L/R"
	}
	return "A detail | B list | L/R"
}
