package theme

import "strings"

const (
	// PackClassic is today's kit look (the default theme).
	PackClassic = "classic"
	// PackNeon is the high-contrast arcade look.
	PackNeon = "neon"
	// PackSofaDim is the cool dark night look.
	PackSofaDim = "sofa-dim"
)

// Pack is a named living-room look over the theme engine. UI code paints
// PackTheme tokens and should not fork on pack identity.
type Pack struct {
	ID    string
	Label string
	Tag   string
	Short string
}

// Packs is the kit roster: Classic, Neon, and Sofa Dim.
func Packs() []Pack {
	return []Pack{
		{ID: PackClassic, Label: "Classic", Short: "classic"},
		{ID: PackNeon, Label: "Neon", Tag: "NEON", Short: "neon"},
		{ID: PackSofaDim, Label: "Sofa Dim", Tag: "DIM", Short: "dim"},
	}
}

// NormalizePack maps a pack id, builtin theme name, or alias onto a pack
// id. Unknown specs, including custom file names, stay empty.
func NormalizePack(spec string) string {
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case NameDefault, PackClassic:
		return PackClassic
	case NameArcade, PackNeon:
		return PackNeon
	case NameNight, PackSofaDim, "sofa", "sofadim":
		return PackSofaDim
	default:
		return ""
	}
}

// NextPack cycles Classic → Neon → Sofa Dim → Classic. An unknown or
// empty spec starts at Neon so the first press is a visible change.
func NextPack(spec string) string {
	ids := []string{PackClassic, PackNeon, PackSofaDim}
	cur := NormalizePack(spec)
	for i, id := range ids {
		if id == cur {
			return ids[(i+1)%len(ids)]
		}
	}
	return PackNeon
}

// PackByID returns a roster entry.
func PackByID(id string) (Pack, bool) {
	id = NormalizePack(id)
	for _, p := range Packs() {
		if p.ID == id {
			return p, true
		}
	}
	return Pack{}, false
}

// PackTheme is the complete look for a pack id or alias.
func PackTheme(spec string) (Theme, bool) {
	id := NormalizePack(spec)
	if id == "" {
		return Theme{}, false
	}
	return Builtin(id)
}

// PackTag is the short header suffix. Classic stays untagged so default
// chrome is unchanged.
func PackTag(spec string) string {
	p, ok := PackByID(spec)
	if !ok {
		return ""
	}
	return p.Tag
}

// PackLabel is the living-room name.
func PackLabel(spec string) string {
	p, ok := PackByID(spec)
	if !ok {
		return ""
	}
	return p.Label
}

// PackShort is the footer X-hint for this pack.
func PackShort(spec string) string {
	p, ok := PackByID(spec)
	if !ok {
		return ""
	}
	return p.Short
}
