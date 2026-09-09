package fbgrid

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
	"github.com/DeanoC/FogCast/host/tenfoot/theme"
)

// BadgeKind is one compact metadata chip on a cover or title pane.
type BadgeKind int

const (
	// BadgePlayers is presentation max-players when that field is set.
	BadgePlayers BadgeKind = iota
	// BadgeRating is an optional presentation rating; omitted when empty.
	BadgeRating
	// BadgeCompletion is an optional presentation completion; omitted when empty.
	BadgeCompletion
	// BadgePortable is set when presentation.portable is true or the catalog
	// system is a handheld already in the system table.
	BadgePortable
)

// Badge is one honest chip. Empty labels are never painted.
type Badge struct {
	Kind  BadgeKind
	Label string
}

const (
	badgePadX      = 3
	badgePadY      = 1
	badgeGap       = 3
	badgeInset     = 3
	badgeMaxRunes  = 6
	badgeMaxGrid   = 4
	badgeMaxWall   = 2
	badgeMaxDetail = 4
)

// ComposeBadges builds players, rating, completion, then portable chips.
// Missing fields are skipped rather than invented.
func ComposeBadges(players, rating, completion string, portable bool) []Badge {
	out := make([]Badge, 0, 4)
	if label := FormatPlayersBadge(players); label != "" {
		out = append(out, Badge{Kind: BadgePlayers, Label: label})
	}
	if label := clipBadgeLabel(rating); label != "" {
		out = append(out, Badge{Kind: BadgeRating, Label: label})
	}
	if label := clipBadgeLabel(completion); label != "" {
		out = append(out, Badge{Kind: BadgeCompletion, Label: label})
	}
	if portable {
		out = append(out, Badge{Kind: BadgePortable, Label: "PORT"})
	}
	return out
}

// FormatPlayersBadge turns admitted players text into a short chip.
// "2" becomes "2P", "1-2" stays a range, and empty input stays hidden.
func FormatPlayersBadge(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, "—", "-")
	lower := strings.ToLower(s)
	lower = strings.TrimSpace(strings.TrimSuffix(lower, " players"))
	lower = strings.TrimSpace(strings.TrimSuffix(lower, " player"))
	if lower == "" {
		return ""
	}
	if n, err := strconv.Atoi(lower); err == nil && n >= 1 && n <= 99 {
		return strconv.Itoa(n) + "P"
	}
	if a, b, ok := splitPlayerRange(lower); ok {
		return clipBadgeLabel(a + "-" + b)
	}
	return clipBadgeLabel(strings.ToUpper(lower))
}

func splitPlayerRange(s string) (string, string, bool) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return "", "", false
	}
	a := strings.TrimSpace(parts[0])
	b := strings.TrimSpace(parts[1])
	if _, err := strconv.Atoi(a); err != nil {
		return "", "", false
	}
	if _, err := strconv.Atoi(b); err != nil {
		return "", "", false
	}
	return a, b, true
}

func clipBadgeLabel(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.ToUpper(s)
	if utf8.RuneCountInString(s) <= badgeMaxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:badgeMaxRunes])
}

type badgeLayout struct {
	Rect  gfx.Rect
	Badge Badge
}

func badgeFont(th theme.Theme) (size int, weight gfx.Weight) {
	size = th.CaptionPx()
	if size < 8 {
		size = 8
	}
	return size, th.CaptionWeight()
}

func badgeChipSize(label string, th theme.Theme) (w, h int) {
	size, weight := badgeFont(th)
	tw := gfx.MeasureTextWeight(label, size, weight)
	thgt := gfx.TextHeightWeight(size, weight)
	w = tw + 2*badgePadX
	h = thgt + 2*badgePadY
	if w < 12 {
		w = 12
	}
	if h < 10 {
		h = 10
	}
	return w, h
}

func badgeRowHeight(th theme.Theme) int {
	_, h := badgeChipSize("2P", th)
	return h
}

func tileLabelReserve(kind BrowseKind, th theme.Theme, hasLogo bool) float32 {
	if kind == BrowseCoverflow {
		return 0
	}
	size := th.BodyPx()
	weight := th.BodyWeight()
	if kind == BrowseWall {
		size = th.CaptionPx()
		weight = th.CaptionWeight()
	}
	h := float32(gfx.TextHeightWeight(size, weight) + 4)
	if h < 16 {
		h = 16
	}
	if hasLogo && h < 24 {
		h = 24
	}
	return h
}

func badgeBudget(inner gfx.Rect, kind BrowseKind) int {
	if inner.W < 28 || inner.H < 20 {
		return 0
	}
	if kind == BrowseWall {
		if inner.W < 40 {
			return 1
		}
		return badgeMaxWall
	}
	if inner.H < 36 {
		return 2
	}
	return badgeMaxGrid
}

func layoutBadges(inner gfx.Rect, badges []Badge, th theme.Theme, max int, reserveH float32) []badgeLayout {
	if max < 1 || len(badges) == 0 || inner.W < 12 || inner.H < 10 {
		return nil
	}
	_, bh := badgeChipSize("2P", th)
	// A row sized to the chip (title pane) is allowed. Cover cells hide chips
	// that would cover too much art.
	if inner.H > float32(bh)*2 {
		if float32(bh) > inner.H*0.28 {
			return nil
		}
		availH := inner.H - float32(badgeInset) - reserveH
		if availH < float32(bh) {
			return nil
		}
	}
	x := inner.X + float32(badgeInset)
	y := inner.Y + float32(badgeInset)
	right := inner.X + inner.W - float32(badgeInset)
	out := make([]badgeLayout, 0, max)
	for _, badge := range badges {
		if len(out) >= max {
			break
		}
		label := strings.TrimSpace(badge.Label)
		if label == "" {
			continue
		}
		bw, bh := badgeChipSize(label, th)
		if x+float32(bw) > right {
			break
		}
		out = append(out, badgeLayout{
			Badge: Badge{Kind: badge.Kind, Label: label},
			Rect:  gfx.Rect{X: x, Y: y, W: float32(bw), H: float32(bh)},
		})
		x += float32(bw + badgeGap)
	}
	return out
}

func paintBadgeLayouts(d gfx.Device, layouts []badgeLayout, th theme.Theme) {
	if d == nil {
		return
	}
	size, weight := badgeFont(th)
	for _, item := range layouts {
		r := item.Rect
		if r.W < 8 || r.H < 8 {
			continue
		}
		d.FillRect(r, th.Highlight)
		text := gfx.FitTextWeight(item.Badge.Label, size, int(r.W)-2*badgePadX, weight)
		if text == "" {
			continue
		}
		d.DrawTextWeight(int(r.X)+badgePadX, int(r.Y)+badgePadY, text, size, weight, th.Background)
	}
}

func paintInner(g Grid, i int) (gfx.Rect, bool) {
	r, ok := g.tileRect(i)
	if !ok {
		return gfx.Rect{}, false
	}
	inner := r
	if i == g.Focus && !g.StripActive {
		if in, ok := g.tileInner(i); ok {
			inner = in
		}
	}
	th := g.Theme.Complete()
	if th.CoverFrameWidth > 0 && inner.W > float32(2*th.CoverFrameWidth) && inner.H > float32(2*th.CoverFrameWidth) {
		fw := float32(th.CoverFrameWidth)
		inner = gfx.Rect{
			X: inner.X + fw,
			Y: inner.Y + fw,
			W: inner.W - 2*fw,
			H: inner.H - 2*fw,
		}
	}
	return inner, true
}

func paintTileBadges(d gfx.Device, g Grid, th theme.Theme, inner gfx.Rect, tile Tile) {
	if d == nil || len(tile.Badges) == 0 {
		return
	}
	max := badgeBudget(inner, g.Kind)
	reserve := tileLabelReserve(g.Kind, th, tile.Logo != nil)
	paintBadgeLayouts(d, layoutBadges(inner, tile.Badges, th, max, reserve), th)
}

func paintDetailBadges(d gfx.Device, row gfx.Rect, badges []Badge, th theme.Theme) {
	if d == nil || len(badges) == 0 || row.W < 12 || row.H < 8 {
		return
	}
	paintBadgeLayouts(d, layoutBadges(row, badges, th, badgeMaxDetail, 0), th)
}

// TileBadgeSample is a pixel inside tile i's badge chip, or false when hidden.
func TileBadgeSample(g Grid, i, badge int) (x, y int, ok bool) {
	if i < 0 || i >= len(g.Tiles) {
		return 0, 0, false
	}
	inner, ok := paintInner(g, i)
	if !ok {
		return 0, 0, false
	}
	th := g.Theme.Complete()
	tile := g.Tiles[i]
	max := badgeBudget(inner, g.Kind)
	reserve := tileLabelReserve(g.Kind, th, tile.Logo != nil)
	layouts := layoutBadges(inner, tile.Badges, th, max, reserve)
	if badge < 0 || badge >= len(layouts) {
		return 0, 0, false
	}
	r := layouts[badge].Rect
	if r.W < 4 || r.H < 4 {
		return 0, 0, false
	}
	return int(r.X + 2), int(r.Y + 2), true
}

// DetailBadgeSample is a pixel inside the title-pane badge row.
func DetailBadgeSample(width, height int, th theme.Theme, f DetailFrame) (x, y int, ok bool) {
	if len(f.Badges) == 0 {
		return 0, 0, false
	}
	_, text, _ := detailShotRect(width, height, th, f)
	rowH := badgeRowHeight(th.Complete())
	row := gfx.Rect{X: text.X, Y: text.Y, W: text.W, H: float32(rowH)}
	// Match PaintDetail: badges sit under the title block.
	titleH := gfx.TextHeightWeight(th.Complete().TitlePx(), th.Complete().TitleWeight())
	if f.Logo != nil {
		if logoH := logoFitHeight(f.Logo, int(text.W), detailLogoMaxH); logoH > titleH {
			titleH = logoH
		}
	}
	row.Y = text.Y + float32(titleH+detailCopyGap)
	layouts := layoutBadges(row, f.Badges, th.Complete(), badgeMaxDetail, 0)
	if len(layouts) == 0 {
		return 0, 0, false
	}
	r := layouts[0].Rect
	return int(r.X + 2), int(r.Y + 2), true
}
