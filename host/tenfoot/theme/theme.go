// Package theme is the data-driven look for kit fbgrid paint and the
// desktop tenfoot shell. Tokens load from a built-in name or a JSON/TOML
// file. There is no scripted theme VM; derived colours stay in Go.
package theme

import (
	"strings"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

const (
	NameDefault = "default"
	NameArcade  = "arcade"
	NameNight   = "night"
)

// Theme is one complete (or partial) look. Zero-alpha colours and non-positive
// spacing inherit Default when Complete runs. Swap looks by loading a
// different Theme; UI code should not fork on name.
type Theme struct {
	Name string

	Background        gfx.Color
	SofaBackground    gfx.Color
	AttractBackground gfx.Color
	Highlight         gfx.Color
	Flash             gfx.Color
	LabelBar          gfx.Color
	Label             gfx.Color
	Status            gfx.Color
	Header            gfx.Color
	HeaderBar         gfx.Color
	FooterBar         gfx.Color
	CoverFrame        gfx.Color
	CoverFrameWidth   int

	Pad     int
	Gap     int
	Border  int
	HeaderH int
	FooterH int
	// HeaderScale, LabelScale and StatusScale are size multipliers. Kit
	// chrome maps them to pixel size 8*scale (the former DebugText glyph
	// height) so existing theme JSON keeps the same hierarchy. There is
	// no font-family picker in this slice.
	HeaderScale int
	LabelScale  int
	StatusScale int

	Systems map[string]gfx.Color
}

// Default is today's kit cover-grid look. SofaBackground and AttractBackground
// keep the desktop shell's existing clears.
func Default() Theme {
	return Theme{
		Name:              NameDefault,
		Background:        gfx.RGB(16, 16, 24),
		SofaBackground:    gfx.RGB(12, 14, 20),
		AttractBackground: gfx.RGB(8, 8, 12),
		Highlight:         gfx.RGB(255, 220, 0),
		Flash:             gfx.RGB(255, 255, 255),
		LabelBar:          gfx.RGB(8, 8, 12),
		Label:             gfx.RGB(236, 240, 248),
		Status:            gfx.RGB(236, 240, 248),
		Header:            gfx.RGB(236, 240, 248),
		HeaderBar:         gfx.RGB(16, 16, 24),
		FooterBar:         gfx.RGB(16, 16, 24),
		CoverFrame:        gfx.Color{},
		CoverFrameWidth:   0,
		Pad:               16,
		Gap:               8,
		Border:            4,
		HeaderH:           36,
		FooterH:           28,
		HeaderScale:       2,
		LabelScale:        1,
		StatusScale:       2,
		Systems:           defaultSystems(),
	}
}

// Arcade is a high-contrast alternate: magenta highlight, crimson fill,
// thick cover frame. It is meant to be obviously different on the kit grid.
func Arcade() Theme {
	bg := gfx.RGB(26, 0, 8)
	return Theme{
		Name:              NameArcade,
		Background:        bg,
		SofaBackground:    bg,
		AttractBackground: gfx.RGB(16, 0, 6),
		Highlight:         gfx.RGB(255, 0, 170),
		Flash:             gfx.RGB(0, 255, 240),
		LabelBar:          gfx.RGB(64, 0, 24),
		Label:             gfx.RGB(255, 240, 220),
		Status:            gfx.RGB(255, 240, 220),
		Header:            gfx.RGB(255, 240, 220),
		HeaderBar:         gfx.RGB(255, 51, 0),
		FooterBar:         gfx.RGB(255, 34, 0),
		CoverFrame:        gfx.RGB(255, 230, 0),
		CoverFrameWidth:   3,
		Pad:               16,
		Gap:               8,
		Border:            6,
		HeaderH:           36,
		FooterH:           28,
		HeaderScale:       2,
		LabelScale:        1,
		StatusScale:       2,
		Systems: map[string]gfx.Color{
			"pong":      gfx.RGB(57, 255, 20),
			"megadrive": gfx.RGB(255, 140, 0),
			"snes":      gfx.RGB(191, 64, 191),
			"default":   gfx.RGB(255, 85, 119),
		},
	}
}

// Night is a cool dark alternate: cyan-blue highlight, near-black fill.
func Night() Theme {
	bg := gfx.RGB(5, 7, 12)
	return Theme{
		Name:              NameNight,
		Background:        bg,
		SofaBackground:    bg,
		AttractBackground: gfx.RGB(3, 5, 8),
		Highlight:         gfx.RGB(61, 184, 255),
		Flash:             gfx.RGB(232, 244, 255),
		LabelBar:          gfx.RGB(10, 16, 24),
		Label:             gfx.RGB(220, 230, 240),
		Status:            gfx.RGB(220, 230, 240),
		Header:            gfx.RGB(220, 230, 240),
		HeaderBar:         gfx.RGB(10, 32, 64),
		FooterBar:         gfx.RGB(10, 32, 64),
		CoverFrame:        gfx.RGB(61, 184, 255),
		CoverFrameWidth:   2,
		Pad:               16,
		Gap:               8,
		Border:            4,
		HeaderH:           36,
		FooterH:           28,
		HeaderScale:       2,
		LabelScale:        1,
		StatusScale:       2,
		Systems: map[string]gfx.Color{
			"pong":      gfx.RGB(200, 160, 40),
			"megadrive": gfx.RGB(40, 90, 160),
			"snes":      gfx.RGB(160, 70, 90),
			"default":   gfx.RGB(70, 90, 130),
		},
	}
}

func defaultSystems() map[string]gfx.Color {
	return map[string]gfx.Color{
		"pong":      gfx.RGB(196, 148, 36),
		"megadrive": gfx.RGB(44, 96, 156),
		"snes":      gfx.RGB(156, 52, 60),
		"default":   gfx.RGB(84, 76, 132),
	}
}

// Builtin returns a built-in theme. Empty and "default" are today's look.
func Builtin(name string) (Theme, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", NameDefault:
		return Default(), true
	case NameArcade:
		return Arcade(), true
	case NameNight:
		return Night(), true
	default:
		return Theme{}, false
	}
}

// Complete fills omitted tokens from Default. It copies Systems so callers
// can mutate the result.
func (t Theme) Complete() Theme {
	d := Default()
	if strings.TrimSpace(t.Name) == "" {
		t.Name = d.Name
	} else {
		t.Name = strings.TrimSpace(t.Name)
	}
	t.Background = completeColor(t.Background, d.Background)
	t.SofaBackground = completeColor(t.SofaBackground, d.SofaBackground)
	t.AttractBackground = completeColor(t.AttractBackground, d.AttractBackground)
	t.Highlight = completeColor(t.Highlight, d.Highlight)
	t.Flash = completeColor(t.Flash, d.Flash)
	t.LabelBar = completeColor(t.LabelBar, d.LabelBar)
	t.Label = completeColor(t.Label, d.Label)
	t.Status = completeColor(t.Status, d.Status)
	t.Header = completeColor(t.Header, d.Header)
	t.HeaderBar = completeColor(t.HeaderBar, d.HeaderBar)
	t.FooterBar = completeColor(t.FooterBar, d.FooterBar)
	t.CoverFrame = completeColor(t.CoverFrame, d.CoverFrame)
	if t.CoverFrameWidth < 0 {
		t.CoverFrameWidth = d.CoverFrameWidth
	}
	if t.Pad <= 0 {
		t.Pad = d.Pad
	}
	if t.Gap <= 0 {
		t.Gap = d.Gap
	}
	if t.Border <= 0 {
		t.Border = d.Border
	}
	if t.HeaderH <= 0 {
		t.HeaderH = d.HeaderH
	}
	if t.FooterH <= 0 {
		t.FooterH = d.FooterH
	}
	if t.HeaderScale <= 0 {
		t.HeaderScale = d.HeaderScale
	}
	if t.LabelScale <= 0 {
		t.LabelScale = d.LabelScale
	}
	if t.StatusScale <= 0 {
		t.StatusScale = d.StatusScale
	}
	t.Systems = mergeSystems(d.Systems, t.Systems)
	return t
}

// SystemColor is the solid-tile fallback for a catalog system id.
func (t Theme) SystemColor(system string) gfx.Color {
	t = t.Complete()
	key := strings.ToLower(strings.TrimSpace(system))
	if c, ok := t.Systems[key]; ok {
		return c
	}
	if c, ok := t.Systems["default"]; ok {
		return c
	}
	return gfx.RGB(84, 76, 132)
}

// Equal reports whether t and o paint the same after Complete.
func (t Theme) Equal(o Theme) bool {
	t, o = t.Complete(), o.Complete()
	if t.Name != o.Name ||
		t.Background != o.Background ||
		t.SofaBackground != o.SofaBackground ||
		t.AttractBackground != o.AttractBackground ||
		t.Highlight != o.Highlight ||
		t.Flash != o.Flash ||
		t.LabelBar != o.LabelBar ||
		t.Label != o.Label ||
		t.Status != o.Status ||
		t.Header != o.Header ||
		t.HeaderBar != o.HeaderBar ||
		t.FooterBar != o.FooterBar ||
		t.CoverFrame != o.CoverFrame ||
		t.CoverFrameWidth != o.CoverFrameWidth ||
		t.Pad != o.Pad ||
		t.Gap != o.Gap ||
		t.Border != o.Border ||
		t.HeaderH != o.HeaderH ||
		t.FooterH != o.FooterH ||
		t.HeaderScale != o.HeaderScale ||
		t.LabelScale != o.LabelScale ||
		t.StatusScale != o.StatusScale {
		return false
	}
	if len(t.Systems) != len(o.Systems) {
		return false
	}
	for k, v := range t.Systems {
		if o.Systems[k] != v {
			return false
		}
	}
	return true
}

func completeColor(c, fallback gfx.Color) gfx.Color {
	if c.A == 0 {
		return fallback
	}
	return c
}

func mergeSystems(base, overlay map[string]gfx.Color) map[string]gfx.Color {
	out := make(map[string]gfx.Color, len(base)+len(overlay))
	for k, v := range base {
		out[strings.ToLower(strings.TrimSpace(k))] = v
	}
	for k, v := range overlay {
		if v.A == 0 {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = v
	}
	return out
}
