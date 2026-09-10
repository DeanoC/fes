package tenfoot

import (
	"context"
	"strings"
)

// Dump-region tokens from the browser shell (catalog.mapDumpRegion). The facets
// endpoint does not return regions; tenfoot mirrors this fixed vocabulary.
var dumpRegionTokens = []string{
	"usa", "japan", "europe", "world", "brazil",
	"korea", "asia", "australia",
	"france", "germany", "spain", "italy", "canada",
	"other",
}

var dumpRegionLabels = map[string]string{
	"usa":        "USA",
	"japan":      "Japan",
	"europe":     "Europe",
	"world":      "World",
	"brazil":     "Brazil",
	"korea":      "Korea",
	"asia":       "Asia",
	"australia":  "Australia",
	"france":     "France",
	"germany":    "Germany",
	"spain":      "Spain",
	"italy":      "Italy",
	"canada":     "Canada",
	"other":      "Other",
}

const (
	filterPaneRoot = iota
	filterPaneGenre
	filterPaneYear
	filterPaneRegion
)

const (
	filterRowGenre = iota
	filterRowYear
	filterRowRegion
	filterRowHidePrerelease
	filterRowHideHacks
	filterRowClear
	filterRowClose
	filterRootRowCount
)

// FilterRow is one sofa filter overlay line.
type FilterRow struct {
	ID     string
	Label  string
	Value  string
	Active bool
}

// FilterSnapshot is the filter overlay shown by the renderer.
type FilterSnapshot struct {
	Open    bool
	Title   string
	Index   int
	Rows    []FilterRow
	Hint    string
	Loading bool
	Status  string
}

func dumpRegionLabel(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if label := dumpRegionLabels[token]; label != "" {
		return label
	}
	if token == "" {
		return "Any"
	}
	return token
}

func (a *App) FiltersOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.filtersOpen
}

func (a *App) filtersSnapshotLocked() FilterSnapshot {
	if !a.filtersOpen {
		return FilterSnapshot{}
	}
	rows := a.filterRowsLocked()
	if a.filterIndex < 0 {
		a.filterIndex = 0
	}
	if n := len(rows); n > 0 && a.filterIndex >= n {
		a.filterIndex = n - 1
	}
	title := "Filters"
	hint := "A select  B back"
	switch a.filterPane {
	case filterPaneGenre:
		title = "Genre"
		hint = "A apply  B back"
		if a.filtersLoading {
			hint = "loading facets"
		} else if len(a.facets.Genres) == 0 {
			hint = "no genres"
		}
	case filterPaneYear:
		title = "Year"
		hint = "A apply  B back"
		if a.filtersLoading {
			hint = "loading facets"
		} else if len(a.facets.Years) == 0 {
			hint = "no years"
		}
	case filterPaneRegion:
		title = "Region"
		hint = "A apply  B back"
	}
	status := a.filterStatus
	if a.filtersLoading && a.filterPane == filterPaneRoot {
		status = "loading facets"
	}
	return FilterSnapshot{
		Open:    true,
		Title:   title,
		Index:   a.filterIndex,
		Rows:    rows,
		Hint:    hint,
		Loading: a.filtersLoading,
		Status:  status,
	}
}

func (a *App) filterRowsLocked() []FilterRow {
	switch a.filterPane {
	case filterPaneGenre:
		return facetChoiceRows("genre", a.filterGenre, a.facets.Genres, identityLabel)
	case filterPaneYear:
		return facetChoiceRows("year", a.filterYear, a.facets.Years, identityLabel)
	case filterPaneRegion:
		return facetChoiceRows("region", a.filterRegion, dumpRegionTokens, dumpRegionLabel)
	default:
		return []FilterRow{
			{ID: "genre", Label: "Genre", Value: choiceOrAny(a.filterGenre, identityLabel)},
			{ID: "year", Label: "Year", Value: choiceOrAny(a.filterYear, identityLabel)},
			{ID: "region", Label: "Region", Value: choiceOrAny(a.filterRegion, dumpRegionLabel)},
			{ID: "hide-prerelease", Label: "Hide prerelease", Value: onOff(a.hidePrerelease), Active: a.hidePrerelease},
			{ID: "hide-hacks", Label: "Hide hacks", Value: onOff(a.hideHacks), Active: a.hideHacks},
			{ID: "clear", Label: "Clear filters", Value: ""},
			{ID: "close", Label: "Close", Value: "B back"},
		}
	}
}

func identityLabel(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "Any"
	}
	return v
}

func choiceOrAny(value string, label func(string) string) string {
	if strings.TrimSpace(value) == "" {
		return "Any"
	}
	return label(value)
}

func onOff(v bool) string {
	if v {
		return "On"
	}
	return "Off"
}

func facetChoiceRows(kind, selected string, options []string, label func(string) string) []FilterRow {
	selected = strings.TrimSpace(selected)
	rows := []FilterRow{{
		ID:     kind + ":any",
		Label:  "Any",
		Active: selected == "",
	}}
	seen := map[string]struct{}{"": {}}
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		rows = append(rows, FilterRow{
			ID:     kind + ":" + option,
			Label:  label(option),
			Active: selected == option,
		})
	}
	return rows
}

func (a *App) openFiltersLocked() {
	if !a.browseHoldEnabledLocked() {
		return
	}
	a.searchOpen = false
	a.closeCollectionOverlaysLocked()
	a.closeDetailLocked()
	a.closeSettingsLocked()
	a.filtersOpen = true
	a.filterPane = filterPaneRoot
	a.filterIndex = 0
	a.filterStatus = ""
	a.filtersLoading = true
	a.filterGen++
	gen := a.filterGen
	ctx := a.ctx
	if ctx == nil {
		a.filtersLoading = false
		return
	}
	go a.fetchFacets(ctx, gen)
}

func (a *App) closeFiltersLocked() {
	if !a.filtersOpen {
		return
	}
	a.filtersOpen = false
	a.filtersLoading = false
	a.filterStatus = ""
	a.filterPane = filterPaneRoot
	a.filterIndex = 0
	a.filterGen++
}

func (a *App) fetchFacets(ctx context.Context, gen int) {
	values, err := a.client.Facets(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.filterGen || !a.filtersOpen {
		return
	}
	a.filtersLoading = false
	if err != nil {
		a.facets = FacetValues{Genres: []string{}, Years: []string{}}
		a.filterStatus = "facet list failed"
		return
	}
	a.facets = values
}

func (a *App) handleFiltersLocked(cmd Command) {
	if !a.filtersOpen {
		return
	}
	switch cmd {
	case CmdFilters:
		a.closeFiltersLocked()
		return
	case CmdBack:
		if a.filterPane != filterPaneRoot {
			a.filterIndex = filterRootIndexForPane(a.filterPane)
			a.filterPane = filterPaneRoot
			return
		}
		a.closeFiltersLocked()
		return
	}
	rows := a.filterRowsLocked()
	n := len(rows)
	if n == 0 {
		a.closeFiltersLocked()
		return
	}
	if a.filterIndex < 0 {
		a.filterIndex = 0
	}
	if a.filterIndex >= n {
		a.filterIndex = n - 1
	}
	switch cmd {
	case CmdUp, CmdLeft, CmdViewPrev, CmdTabPrev:
		a.filterIndex = (a.filterIndex - 1 + n) % n
	case CmdDown, CmdRight, CmdViewNext, CmdTab:
		a.filterIndex = (a.filterIndex + 1) % n
	case CmdSelect:
		a.activateFilterRowLocked(rows[a.filterIndex])
	}
}

func (a *App) activateFilterRowLocked(row FilterRow) {
	switch a.filterPane {
	case filterPaneGenre:
		a.applyCatalogFilterLocked("", filterValueFromRow(row, "genre"), a.filterYear, a.filterRegion, a.hidePrerelease, a.hideHacks)
		a.filterPane = filterPaneRoot
		a.filterIndex = filterRowGenre
	case filterPaneYear:
		a.applyCatalogFilterLocked("", a.filterGenre, filterValueFromRow(row, "year"), a.filterRegion, a.hidePrerelease, a.hideHacks)
		a.filterPane = filterPaneRoot
		a.filterIndex = filterRowYear
	case filterPaneRegion:
		a.applyCatalogFilterLocked("", a.filterGenre, a.filterYear, filterValueFromRow(row, "region"), a.hidePrerelease, a.hideHacks)
		a.filterPane = filterPaneRoot
		a.filterIndex = filterRowRegion
	default:
		switch row.ID {
		case "genre":
			a.filterPane = filterPaneGenre
			a.filterIndex = selectedChoiceIndex(a.filterGenre, a.facets.Genres)
		case "year":
			a.filterPane = filterPaneYear
			a.filterIndex = selectedChoiceIndex(a.filterYear, a.facets.Years)
		case "region":
			a.filterPane = filterPaneRegion
			a.filterIndex = selectedChoiceIndex(a.filterRegion, dumpRegionTokens)
		case "hide-prerelease":
			a.applyCatalogFilterLocked("", a.filterGenre, a.filterYear, a.filterRegion, !a.hidePrerelease, a.hideHacks)
		case "hide-hacks":
			a.applyCatalogFilterLocked("", a.filterGenre, a.filterYear, a.filterRegion, a.hidePrerelease, !a.hideHacks)
		case "clear":
			if !a.filtersActiveLocked() {
				return
			}
			a.applyCatalogFilterLocked("clear", "", "", "", false, false)
		case "close":
			a.closeFiltersLocked()
		}
	}
}

func filterRootIndexForPane(pane int) int {
	switch pane {
	case filterPaneGenre:
		return filterRowGenre
	case filterPaneYear:
		return filterRowYear
	case filterPaneRegion:
		return filterRowRegion
	default:
		return 0
	}
}

func filterValueFromRow(row FilterRow, kind string) string {
	prefix := kind + ":"
	if row.ID == prefix+"any" {
		return ""
	}
	return strings.TrimPrefix(row.ID, prefix)
}

func selectedChoiceIndex(selected string, options []string) int {
	selected = strings.TrimSpace(selected)
	if selected == "" {
		return 0
	}
	idx := 1
	seen := map[string]struct{}{"": {}}
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		if _, ok := seen[option]; ok {
			continue
		}
		seen[option] = struct{}{}
		if option == selected {
			return idx
		}
		idx++
	}
	return 0
}

func (a *App) applyCatalogFilterLocked(reason, genre, year, region string, hidePrerelease, hideHacks bool) {
	genre = strings.TrimSpace(genre)
	year = strings.TrimSpace(year)
	region = strings.TrimSpace(region)
	unchanged := a.filterGenre == genre && a.filterYear == year && a.filterRegion == region && a.hidePrerelease == hidePrerelease && a.hideHacks == hideHacks
	a.filterGenre = genre
	a.filterYear = year
	a.filterRegion = region
	a.hidePrerelease = hidePrerelease
	a.hideHacks = hideHacks
	if unchanged {
		return
	}
	if reason == "clear" {
		a.status = "filters cleared"
	}
	a.reloadLocked()
}

func (a *App) filtersActiveLocked() bool {
	return strings.TrimSpace(a.filterGenre) != "" || strings.TrimSpace(a.filterYear) != "" || strings.TrimSpace(a.filterRegion) != "" || a.hidePrerelease || a.hideHacks
}

func (a *App) filterSummaryLocked() string {
	return strings.Join(filterSummaryParts(a.filterGenre, a.filterYear, a.filterRegion, a.hidePrerelease, a.hideHacks), " · ")
}

func filterSummaryParts(genre, year, region string, hidePrerelease, hideHacks bool) []string {
	parts := make([]string, 0, 5)
	if g := strings.TrimSpace(genre); g != "" {
		parts = append(parts, g)
	}
	if y := strings.TrimSpace(year); y != "" {
		parts = append(parts, y)
	}
	if r := strings.TrimSpace(region); r != "" {
		parts = append(parts, dumpRegionLabel(r))
	}
	if hidePrerelease {
		parts = append(parts, "no proto")
	}
	if hideHacks {
		parts = append(parts, "no hacks")
	}
	return parts
}
