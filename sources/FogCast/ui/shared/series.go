package shared

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const seriesMax = 6

var reservedSeriesCollections = map[string]struct{}{
	"all": {}, "favorites": {}, "recents": {}, "continue": {},
	"unplayed": {}, "recently_added": {}, "recently-added": {},
}

// SeriesName is the admitted presentation or catalog series string.
func SeriesName(p Presentation) string {
	if p.Presentation == nil {
		return ""
	}
	return strings.TrimSpace(p.Presentation.Series)
}

// RelatedIDs is presentation related / related_ids, trimmed and de-duplicated.
func RelatedIDs(p Presentation) []string {
	if p.Presentation == nil {
		return nil
	}
	info := p.Presentation
	out := make([]string, 0, len(info.Related)+len(info.RelatedIDs))
	seen := map[string]struct{}{}
	add := func(ids []string) {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	add(info.Related)
	add(info.RelatedIDs)
	return out
}

// CollectionID is the admitted presentation collection id, or empty.
func CollectionID(p Presentation) string {
	if p.Presentation == nil {
		return ""
	}
	return strings.TrimSpace(p.Presentation.Collection)
}

// SeriesMates is in-catalog siblings of focused, using admitted series /
// related / collection fields. The focused title is excluded. Empty when
// no sibling exists in catalog.
func SeriesMates(catalog []Game, focused Game, p Presentation) (mates []Game, label string) {
	focusID := strings.TrimSpace(focused.ID)
	if focusID == "" {
		return nil, ""
	}
	byID := make(map[string]Game, len(catalog))
	for _, game := range catalog {
		id := strings.TrimSpace(game.ID)
		if id == "" || id == focusID {
			continue
		}
		byID[id] = game
	}
	out := make([]Game, 0, seriesMax)
	seen := map[string]struct{}{focusID: {}}
	add := func(game Game) {
		if len(out) >= seriesMax {
			return
		}
		id := strings.TrimSpace(game.ID)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, game)
	}
	for _, id := range RelatedIDs(p) {
		if game, ok := byID[id]; ok {
			add(game)
			continue
		}
		want := foldSeriesKey(id)
		if want == "" {
			continue
		}
		for _, game := range catalog {
			if foldSeriesKey(game.Title) == want {
				add(game)
			}
		}
	}
	collection := strings.ToLower(CollectionID(p))
	if collection != "" {
		if _, reserved := reservedSeriesCollections[collection]; !reserved {
			for _, game := range catalog {
				if gameHasCollection(game, collection) {
					add(game)
				}
			}
		}
	}
	series := SeriesName(p)
	if series == "" {
		series = strings.TrimSpace(focused.Series)
	}
	if series != "" {
		for _, game := range catalog {
			if seriesGameMatch(game, series) {
				add(game)
			}
		}
	}
	if len(out) == 0 {
		return nil, ""
	}
	label = series
	if label == "" {
		label = "Related"
	}
	return out, label
}

func seriesGameMatch(game Game, series string) bool {
	if strings.EqualFold(strings.TrimSpace(game.Series), series) {
		return true
	}
	return titleMatchesSeries(game.Title, series)
}

func gameHasCollection(game Game, collectionID string) bool {
	for _, id := range game.Collections {
		if id == collectionID {
			return true
		}
	}
	return false
}

func titleMatchesSeries(title, series string) bool {
	t := foldSeriesKey(title)
	s := foldSeriesKey(series)
	if t == "" || s == "" {
		return false
	}
	if t == s {
		return true
	}
	if !strings.HasPrefix(t, s) {
		return false
	}
	rest := t[len(s):]
	if rest == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(rest)
	if r == utf8.RuneError {
		return false
	}
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case ':', '-', '(', '[', '#', '.':
		return true
	default:
		return false
	}
}

func foldSeriesKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if b.Len() > 0 && !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return b.String()
}
