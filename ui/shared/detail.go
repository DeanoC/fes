package shared

import (
	"github.com/DeanoC/FogCast/hostclient"
	"strings"
)

// FocusDetail is the renderer-neutral focused title projection. Catalog and
// presentation wire values remain owned by hostclient; this type only joins
// the fields used by the sofa and kit detail panes.
type FocusDetail struct {
	Title         string
	Platform      string
	Year          string
	Genre         string
	Studio        string
	Players       string
	Region        string
	Summary       string
	Attribution   string
	Favorite      bool
	VideoID       string
	ScreenshotIDs []string
	Series        string
	RelatedIDs    []string
	Collection    string
	Cached        string
}

// MetaFacts joins admitted catalog/presentation facts for the detail strip.
// Empty fields are omitted. Play-count and last-played stay off this pane;
// the kit platform wheel rolls them up when the games payload carries them.
func (d FocusDetail) MetaFacts() string {
	parts := make([]string, 0, 6)
	for _, part := range []string{d.Platform, d.Year, d.Genre, d.Studio, d.Players, d.Region, d.Cached} {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "  ·  ")
}

// MetaLine joins optional metadata and provider attribution.
func (d FocusDetail) MetaLine() string {
	parts := make([]string, 0, 2)
	for _, part := range []string{d.MetaFacts(), d.Attribution} {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "  ·  ")
}

// GameDetail builds focused-title metadata from a catalog row and an optional
// presentation payload. Catalog title/system/year/genre/region/favorite come
// from the games row; studio, players, summary, attribution, video_id, and
// screenshot_ids come from presentation when that object is present.
func GameDetail(game Game, p Presentation) FocusDetail {
	d := FocusDetail{
		Title:    strings.TrimSpace(game.Title),
		Platform: strings.TrimSpace(game.System),
		Year:     strings.TrimSpace(game.Year),
		Genre:    strings.TrimSpace(game.Genre),
		Favorite: game.Favorite,
	}
	if region := strings.TrimSpace(game.Region); region != "" {
		d.Region = DumpRegionLabel(region)
	}
	if p.Presentation != nil {
		info := p.Presentation
		if year := strings.TrimSpace(info.Year); year != "" {
			d.Year = year
		}
		if genre := strings.TrimSpace(info.Genre); genre != "" {
			d.Genre = genre
		}
		d.Studio = strings.TrimSpace(info.Studio)
		d.Players = strings.TrimSpace(info.Players)
		d.Summary = strings.TrimSpace(info.Summary)
		d.VideoID = hostclient.NormalizeHandle(info.VideoID)
		d.ScreenshotIDs = ScreenshotHandles(info.ScreenshotIDs)
		d.Series = strings.TrimSpace(info.Series)
		d.RelatedIDs = RelatedIDs(p)
		d.Collection = strings.TrimSpace(info.Collection)
	}
	if d.Series == "" {
		d.Series = strings.TrimSpace(game.Series)
	}
	if game.ROMCached != nil {
		if *game.ROMCached {
			d.Cached = "ON KIT"
		} else {
			d.Cached = "NEEDS ROM"
		}
	}
	d.Attribution = strings.TrimSpace(p.AttributionLabel())
	return d
}

var dumpRegionLabels = map[string]string{
	"usa": "USA", "japan": "Japan", "europe": "Europe", "world": "World",
	"brazil": "Brazil", "korea": "Korea", "asia": "Asia", "australia": "Australia",
	"france": "France", "germany": "Germany", "spain": "Spain", "italy": "Italy",
	"canada": "Canada", "other": "Other",
}

// DumpRegionLabel converts the host's stable region token to UI copy.
func DumpRegionLabel(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if label := dumpRegionLabels[token]; label != "" {
		return label
	}
	if token == "" {
		return "Any"
	}
	return token
}
