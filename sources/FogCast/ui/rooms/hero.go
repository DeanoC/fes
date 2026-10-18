package rooms

import lua "github.com/yuin/gopher-lua"

// Destination panel placements a room manifest may choose.
const (
	// PanelStrip is the compact selected-destination strip along the
	// bottom of the room. It is the default.
	PanelStrip = "strip"
	// PanelHero is the large FES-drawn selected-destination panel near the
	// top of the room (the Cartridge Lounge Home layout). The room draws its
	// own card and artwork around it; FES draws title, input needs,
	// readiness, destination and actions in the text column.
	PanelHero = "hero"
)

// PanelRect is a rectangle in room coordinates.
type PanelRect struct {
	X, Y, W, H int
}

// HeroLayout is the shared geometry of a hero room. Card is the whole
// selected-item card, Text is the column FES draws over, and Art is where
// the room draws artwork or a fallback motif. Footer is the band FES keeps
// for status and prompts at the bottom of the room.
type HeroLayout struct {
	Header PanelRect
	Card   PanelRect
	Text   PanelRect
	Art    PanelRect
	Shelf  PanelRect
	Footer PanelRect
}

// HeroPanel lays out a hero room of w×h. One function serves the Lua room
// table and the launcher's drawing so the two cannot disagree.
func HeroPanel(w, h int) (HeroLayout, bool) {
	if w < 480 || h < 360 {
		return HeroLayout{}, false
	}
	margin := max(16, w/40)
	header := max(56, h/10)
	footer := max(44, h*7/100)
	cardY := header + margin/2
	cardH := h * 42 / 100
	card := PanelRect{X: margin, Y: cardY, W: w - 2*margin, H: cardH}
	pad := max(16, margin*3/4)
	textW := card.W * 56 / 100
	text := PanelRect{X: card.X + pad, Y: card.Y + pad, W: textW - pad, H: card.H - 2*pad}
	artX := card.X + textW + pad/2
	art := PanelRect{X: artX, Y: card.Y + pad/2, W: card.X + card.W - pad/2 - artX, H: card.H - pad}
	shelfY := card.Y + card.H + margin/2
	shelf := PanelRect{X: margin, Y: shelfY, W: w - 2*margin, H: h - footer - shelfY - margin/4}
	return HeroLayout{
		Header: PanelRect{X: 0, Y: 0, W: w, H: header},
		Card:   card,
		Text:   text,
		Art:    art,
		Shelf:  shelf,
		Footer: PanelRect{X: 0, Y: h - footer, W: w, H: footer},
	}, true
}

// Hero reports whether this room asked for the hero destination panel.
func (r *Instance) Hero() bool {
	return r != nil && r.pack.DestinationPanel == PanelHero
}

func rectTable(L *lua.LState, p PanelRect) *lua.LTable {
	t := L.NewTable()
	t.RawSetString("x", lua.LNumber(p.X))
	t.RawSetString("y", lua.LNumber(p.Y))
	t.RawSetString("w", lua.LNumber(p.W))
	t.RawSetString("h", lua.LNumber(p.H))
	return t
}

// setHeroTable publishes room.hero for a hero room, or nil when the room
// is too small for that layout (the launcher then keeps the strip).
func (r *Instance) setHeroTable() {
	if r.roomTable == nil || !r.Hero() {
		return
	}
	layout, ok := HeroPanel(r.opts.Width, r.opts.Height)
	if !ok {
		r.roomTable.RawSetString("hero", lua.LNil)
		return
	}
	L := r.L
	t := L.NewTable()
	t.RawSetString("header", rectTable(L, layout.Header))
	t.RawSetString("card", rectTable(L, layout.Card))
	t.RawSetString("text", rectTable(L, layout.Text))
	t.RawSetString("art", rectTable(L, layout.Art))
	t.RawSetString("shelf", rectTable(L, layout.Shelf))
	t.RawSetString("footer", rectTable(L, layout.Footer))
	r.roomTable.RawSetString("hero", t)
}
