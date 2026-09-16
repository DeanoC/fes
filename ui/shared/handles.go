package shared

import (
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
)

const maxScreenshotHandles = 8

// ScreenshotHandles normalizes screenshot IDs, drops invalid entries,
// de-duplicates, and caps the list at the public web limit.
func ScreenshotHandles(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, maxScreenshotHandles)
	seen := map[string]bool{}
	for _, id := range ids {
		handle := hostclient.NormalizeHandle(id)
		if handle == "" || seen[handle] {
			continue
		}
		seen[handle] = true
		out = append(out, handle)
		if len(out) >= maxScreenshotHandles {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CoverHandle chooses a catalog cover before presentation metadata.
func CoverHandle(game Game, presentation Presentation) string {
	if handle := hostclient.NormalizeHandle(game.Cover); handle != "" {
		return handle
	}
	if presentation.Presentation == nil {
		return ""
	}
	return hostclient.NormalizeHandle(presentation.Presentation.CoverArtworkID)
}

// LogoHandle returns a presentation clear-logo handle.
func LogoHandle(presentation Presentation) string {
	if presentation.Presentation == nil {
		return ""
	}
	return hostclient.NormalizeHandle(presentation.Presentation.LogoID)
}

// MarqueeHandle returns a presentation banner handle.
func MarqueeHandle(presentation Presentation) string {
	if presentation.Presentation == nil {
		return ""
	}
	return hostclient.NormalizeHandle(presentation.Presentation.MarqueeID)
}

// Box3DHandle returns a presentation 3D box/cart/spine handle.
func Box3DHandle(presentation Presentation) string {
	if presentation.Presentation == nil {
		return ""
	}
	return hostclient.NormalizeHandle(presentation.Presentation.Box3DID)
}

// BackdropHandle returns a presentation fanart/backdrop handle.
func BackdropHandle(presentation Presentation) string {
	if presentation.Presentation == nil {
		return ""
	}
	return hostclient.NormalizeHandle(presentation.Presentation.BackdropArtworkID)
}

// VideoHandle returns a presentation video handle.
func VideoHandle(presentation Presentation) string {
	if presentation.Presentation == nil {
		return ""
	}
	return hostclient.NormalizeHandle(presentation.Presentation.VideoID)
}

// AttractMarqueeHandle prefers presentation marquee metadata, then the row.
func AttractMarqueeHandle(item AttractItem, presentation Presentation) string {
	if handle := MarqueeHandle(presentation); handle != "" {
		return handle
	}
	return hostclient.NormalizeHandle(item.Marquee)
}

// AttractPreviewHandles selects stills for idle attract, retaining the
// existing video-preview ordering and de-duplication rules.
func AttractPreviewHandles(item AttractItem, presentation Presentation) []string {
	video := hostclient.NormalizeHandle(item.Video)
	if video == "" {
		video = VideoHandle(presentation)
	}
	stills := item.StillHandles()
	if video == "" {
		return stills
	}
	var shots []string
	cover := hostclient.NormalizeHandle(item.Cover)
	backdrop := hostclient.NormalizeHandle(item.Backdrop)
	marquee := AttractMarqueeHandle(item, presentation)
	if presentation.Presentation != nil {
		shots = ScreenshotHandles(presentation.Presentation.ScreenshotIDs)
		if cover == "" {
			cover = hostclient.NormalizeHandle(presentation.Presentation.CoverArtworkID)
		}
		if backdrop == "" {
			backdrop = hostclient.NormalizeHandle(presentation.Presentation.BackdropArtworkID)
		}
	}
	return appendUniqueHandles(shots, backdrop, cover, marquee)
}

// DetailPreviewHandles selects detail screenshots and video posters.
func DetailPreviewHandles(presentation Presentation, cover string) []string {
	var shots []string
	video := ""
	backdrop := ""
	if presentation.Presentation != nil {
		shots = ScreenshotHandles(presentation.Presentation.ScreenshotIDs)
		video = hostclient.NormalizeHandle(presentation.Presentation.VideoID)
		backdrop = hostclient.NormalizeHandle(presentation.Presentation.BackdropArtworkID)
		if cover == "" {
			cover = hostclient.NormalizeHandle(presentation.Presentation.CoverArtworkID)
		}
	}
	cover = hostclient.NormalizeHandle(cover)
	if video == "" {
		return shots
	}
	return appendUniqueHandles(shots, backdrop, cover)
}

// PageIDs returns non-empty game IDs in the clamped page range.
func PageIDs(games []Game, start, end int) []string {
	start, end = clampPage(games, start, end)
	if start >= end {
		return nil
	}
	out := make([]string, 0, end-start)
	for _, game := range games[start:end] {
		if id := strings.TrimSpace(game.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// CollectCoverHandles returns unique catalog cover handles, optionally
// consulting presentation metadata for rows without a catalog cover.
func CollectCoverHandles(games []Game, start, end int, presentation func(string) Presentation) []string {
	start, end = clampPage(games, start, end)
	if start >= end {
		return nil
	}
	out := make([]string, 0, end-start)
	seen := make(map[string]struct{}, end-start)
	for _, game := range games[start:end] {
		var pres Presentation
		if presentation != nil {
			pres = presentation(game.ID)
		}
		handle := CoverHandle(game, pres)
		if handle == "" {
			continue
		}
		if _, ok := seen[handle]; ok {
			continue
		}
		seen[handle] = struct{}{}
		out = append(out, handle)
	}
	return out
}

// CollectLogoHandles returns unique clear-logo handles from presentations.
func CollectLogoHandles(games []Game, start, end int, presentation func(string) Presentation) []string {
	return collectPresentationHandles(games, start, end, presentation, LogoHandle)
}

// CollectBox3DHandles returns unique 3D box handles from presentations.
func CollectBox3DHandles(games []Game, start, end int, presentation func(string) Presentation) []string {
	return collectPresentationHandles(games, start, end, presentation, Box3DHandle)
}

// CollectBackdropHandles returns unique backdrop handles from presentations.
func CollectBackdropHandles(games []Game, start, end int, presentation func(string) Presentation) []string {
	return collectPresentationHandles(games, start, end, presentation, BackdropHandle)
}

func collectPresentationHandles(games []Game, start, end int, presentation func(string) Presentation, selectHandle func(Presentation) string) []string {
	start, end = clampPage(games, start, end)
	if start >= end || presentation == nil {
		return nil
	}
	out := make([]string, 0, end-start)
	seen := make(map[string]struct{}, end-start)
	for _, game := range games[start:end] {
		handle := selectHandle(presentation(game.ID))
		if handle == "" {
			continue
		}
		if _, ok := seen[handle]; ok {
			continue
		}
		seen[handle] = struct{}{}
		out = append(out, handle)
	}
	return out
}

func clampPage(games []Game, start, end int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end > len(games) {
		end = len(games)
	}
	return start, end
}

func appendUniqueHandles(base []string, extra ...string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	add := func(handle string) {
		handle = hostclient.NormalizeHandle(handle)
		if handle == "" || seen[handle] {
			return
		}
		seen[handle] = true
		out = append(out, handle)
	}
	for _, handle := range base {
		add(handle)
	}
	for _, handle := range extra {
		add(handle)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
