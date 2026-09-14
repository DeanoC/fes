package kitlauncher

import "github.com/DeanoC/FogCast/ui/tenfoot"

func titleArtwork(game tenfoot.Game, lookup func(string) tenfoot.Presentation) []string {
	var pres tenfoot.Presentation
	if lookup != nil {
		pres = lookup(game.ID)
	}
	out := make([]string, 0, 3)
	if handle := tenfoot.CoverHandle(game, pres); handle != "" {
		out = append(out, handle)
	}
	if handle := tenfoot.LogoHandle(pres); handle != "" {
		out = append(out, handle)
	}
	if handle := tenfoot.Box3DHandle(pres); handle != "" {
		out = append(out, handle)
	}
	return out
}

func collectTitleArtwork(games []tenfoot.Game, start, end int, lookup func(string) tenfoot.Presentation) []string {
	if start < 0 {
		start = 0
	}
	if end > len(games) {
		end = len(games)
	}
	if start >= end {
		return nil
	}
	out := make([]string, 0, (end-start)*3)
	for _, game := range games[start:end] {
		out = append(out, titleArtwork(game, lookup)...)
	}
	return out
}

func collectIDs(games []tenfoot.Game, start, end int) []string {
	return tenfoot.PageIDs(games, start, end)
}

// PrefetchArtworkHandles is focus → page → next page → strip → extra → attract.
func PrefetchArtworkHandles(m Model, lookup func(string) tenfoot.Presentation, pageStart, pageEnd, nextEnd int, extra, attract []string) []string {
	var focus []string
	if m.WheelOpen {
		if game, ok := m.WheelGame(m.Shelf); ok {
			focus = titleArtwork(game, lookup)
		}
		page := make([]string, 0, len(m.Shelves)*3)
		for _, id := range m.WheelPrefetchIDs() {
			for _, game := range m.Catalog {
				if game.ID == id {
					page = append(page, titleArtwork(game, lookup)...)
					break
				}
			}
		}
		strip := collectTitleArtwork(m.Strip, 0, len(m.Strip), lookup)
		return tenfoot.PrefetchOrder(focus, extra, page, strip, attract)
	}
	if game, ok := m.FocusedGame(); ok {
		focus = titleArtwork(game, lookup)
	}
	page := collectTitleArtwork(m.Games, pageStart, pageEnd, lookup)
	next := collectTitleArtwork(m.Games, pageEnd, nextEnd, lookup)
	strip := collectTitleArtwork(m.Strip, 0, len(m.Strip), lookup)
	series := collectTitleArtwork(m.Series, 0, len(m.Series), lookup)
	return tenfoot.PrefetchOrder(focus, page, next, strip, series, extra, attract)
}

// PrefetchPresentationIDs is focus → page → next page → strip → series.
func PrefetchPresentationIDs(m Model, pageStart, pageEnd, nextEnd int) []string {
	var focus []string
	if game, ok := m.FocusedGame(); ok && !m.WheelOpen {
		focus = []string{game.ID}
	} else if m.WheelOpen {
		focus = m.WheelPrefetchIDs()
	}
	page := collectIDs(m.Games, pageStart, pageEnd)
	next := collectIDs(m.Games, pageEnd, nextEnd)
	strip := collectIDs(m.Strip, 0, len(m.Strip))
	series := collectIDs(m.Series, 0, len(m.Series))
	return tenfoot.PrefetchOrder(focus, page, next, strip, series)
}
