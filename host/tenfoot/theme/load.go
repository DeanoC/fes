package theme

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/DeanoC/FogCast/host/tenfoot/gfx"
)

const maxThemeBytes = 1 << 20

type fileTheme struct {
	Name              string            `json:"name" toml:"name"`
	Background        string            `json:"background" toml:"background"`
	SofaBackground    string            `json:"sofa_background" toml:"sofa_background"`
	AttractBackground string            `json:"attract_background" toml:"attract_background"`
	Highlight         string            `json:"highlight" toml:"highlight"`
	Flash             string            `json:"flash" toml:"flash"`
	LabelBar          string            `json:"label_bar" toml:"label_bar"`
	Label             string            `json:"label" toml:"label"`
	Status            string            `json:"status" toml:"status"`
	Header            string            `json:"header" toml:"header"`
	HeaderBar         string            `json:"header_bar" toml:"header_bar"`
	FooterBar         string            `json:"footer_bar" toml:"footer_bar"`
	CoverFrame        string            `json:"cover_frame" toml:"cover_frame"`
	CoverFrameWidth   int               `json:"cover_frame_width" toml:"cover_frame_width"`
	Vignette          string            `json:"vignette" toml:"vignette"`
	VignetteA         *int              `json:"vignette_alpha" toml:"vignette_alpha"`
	Bezel             string            `json:"bezel" toml:"bezel"`
	BezelWidth        int               `json:"bezel_width" toml:"bezel_width"`
	Transition        string            `json:"transition" toml:"transition"`
	Pad               int               `json:"pad" toml:"pad"`
	Gap               int               `json:"gap" toml:"gap"`
	Border            int               `json:"border" toml:"border"`
	HeaderH           int               `json:"header_height" toml:"header_height"`
	FooterH           int               `json:"footer_height" toml:"footer_height"`
	HeaderScale       int               `json:"header_scale" toml:"header_scale"`
	LabelScale        int               `json:"label_scale" toml:"label_scale"`
	StatusScale       int               `json:"status_scale" toml:"status_scale"`
	TitleSize         int               `json:"title_px" toml:"title_px"`
	BodySize          int               `json:"body_px" toml:"body_px"`
	CaptionSize       int               `json:"caption_px" toml:"caption_px"`
	StatusSize        int               `json:"status_px" toml:"status_px"`
	TitleBold         *bool             `json:"title_bold" toml:"title_bold"`
	HeaderBold        *bool             `json:"header_bold" toml:"header_bold"`
	BodyBold          *bool             `json:"body_bold" toml:"body_bold"`
	CaptionBold       *bool             `json:"caption_bold" toml:"caption_bold"`
	StatusBold        *bool             `json:"status_bold" toml:"status_bold"`
	Systems           map[string]string `json:"systems" toml:"systems"`
}

// Resolve loads a built-in name or a JSON/TOML file path. Empty selects default.
func Resolve(spec string) (Theme, error) {
	spec = strings.TrimSpace(spec)
	if t, ok := Builtin(spec); ok {
		return t, nil
	}
	return Load(spec)
}

// Load reads a JSON or TOML theme from path. Missing files stay missing.
func Load(path string) (Theme, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Theme{}, errors.New("theme: missing path")
	}
	f, err := os.Open(path)
	if err != nil {
		return Theme{}, fmt.Errorf("theme: open: %w", err)
	}
	defer f.Close()
	t, err := Decode(f, filepath.Ext(path))
	if err != nil {
		return t, err
	}
	if strings.TrimSpace(t.Name) == "" {
		t.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return t.Complete(), nil
}

// Decode reads one JSON or TOML theme from r. ext selects the parser
// (".toml" vs JSON). Omitted tokens stay zero; Load/Resolve call Complete.
func Decode(r io.Reader, ext string) (Theme, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxThemeBytes+1))
	if err != nil {
		return Theme{}, fmt.Errorf("theme: read: %w", err)
	}
	if len(data) > maxThemeBytes {
		return Theme{}, errors.New("theme: file too large")
	}
	var raw fileTheme
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".toml":
		dec := toml.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&raw); err != nil {
			return Theme{}, fmt.Errorf("theme: invalid toml: %w", err)
		}
	default:
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&raw); err != nil {
			return Theme{}, fmt.Errorf("theme: invalid json: %w", err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return Theme{}, errors.New("theme: invalid json")
		}
	}
	return raw.theme()
}

func (raw fileTheme) theme() (Theme, error) {
	t := Theme{
		Name:            strings.TrimSpace(raw.Name),
		CoverFrameWidth: raw.CoverFrameWidth,
		BezelWidth:      raw.BezelWidth,
		Pad:             raw.Pad,
		Gap:             raw.Gap,
		Border:          raw.Border,
		HeaderH:         raw.HeaderH,
		FooterH:         raw.FooterH,
		HeaderScale:     raw.HeaderScale,
		LabelScale:      raw.LabelScale,
		StatusScale:     raw.StatusScale,
		TitleSize:       raw.TitleSize,
		BodySize:        raw.BodySize,
		CaptionSize:     raw.CaptionSize,
		StatusSize:      raw.StatusSize,
	}
	if raw.TitleBold != nil {
		t.TitleBold = *raw.TitleBold
		t.titleBoldSet = true
	}
	if raw.HeaderBold != nil {
		t.HeaderBold = *raw.HeaderBold
		t.headerBoldSet = true
	}
	if raw.BodyBold != nil {
		t.BodyBold = *raw.BodyBold
		t.bodyBoldSet = true
	}
	if raw.CaptionBold != nil {
		t.CaptionBold = *raw.CaptionBold
		t.captionBoldSet = true
	}
	if raw.StatusBold != nil {
		t.StatusBold = *raw.StatusBold
		t.statusBoldSet = true
	}
	if raw.VignetteA != nil {
		t.VignetteA = *raw.VignetteA
		t.vignetteASet = true
	}
	var err error
	if t.Transition, err = parseTransition(raw.Transition); err != nil {
		return Theme{}, err
	}
	if t.Background, err = parseHex(raw.Background); err != nil {
		return Theme{}, err
	}
	if t.SofaBackground, err = parseHex(raw.SofaBackground); err != nil {
		return Theme{}, err
	}
	if t.AttractBackground, err = parseHex(raw.AttractBackground); err != nil {
		return Theme{}, err
	}
	if t.Highlight, err = parseHex(raw.Highlight); err != nil {
		return Theme{}, err
	}
	if t.Flash, err = parseHex(raw.Flash); err != nil {
		return Theme{}, err
	}
	if t.LabelBar, err = parseHex(raw.LabelBar); err != nil {
		return Theme{}, err
	}
	if t.Label, err = parseHex(raw.Label); err != nil {
		return Theme{}, err
	}
	if t.Status, err = parseHex(raw.Status); err != nil {
		return Theme{}, err
	}
	if t.Header, err = parseHex(raw.Header); err != nil {
		return Theme{}, err
	}
	if t.HeaderBar, err = parseHex(raw.HeaderBar); err != nil {
		return Theme{}, err
	}
	if t.FooterBar, err = parseHex(raw.FooterBar); err != nil {
		return Theme{}, err
	}
	if t.CoverFrame, err = parseHex(raw.CoverFrame); err != nil {
		return Theme{}, err
	}
	if t.Vignette, err = parseHex(raw.Vignette); err != nil {
		return Theme{}, err
	}
	if t.Bezel, err = parseHex(raw.Bezel); err != nil {
		return Theme{}, err
	}
	if len(raw.Systems) > 0 {
		t.Systems = make(map[string]gfx.Color, len(raw.Systems))
		for k, v := range raw.Systems {
			c, err := parseHex(v)
			if err != nil {
				return Theme{}, fmt.Errorf("theme: systems.%s: %w", k, err)
			}
			t.Systems[strings.ToLower(strings.TrimSpace(k))] = c
		}
	}
	return t, nil
}

func parseTransition(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "none", "curtain", "wipe", "glitch", "static":
		if s == "static" {
			return "glitch", nil
		}
		return s, nil
	default:
		return "", fmt.Errorf("theme: transition %q must be none, curtain, wipe, or glitch", s)
	}
}

func parseHex(s string) (gfx.Color, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return gfx.Color{}, nil
	}
	if !strings.HasPrefix(s, "#") {
		return gfx.Color{}, fmt.Errorf("theme: color %q must be #RRGGBB", s)
	}
	s = s[1:]
	var n uint64
	var err error
	switch len(s) {
	case 6:
		n, err = strconv.ParseUint(s, 16, 32)
		if err != nil {
			return gfx.Color{}, fmt.Errorf("theme: color #%s: %w", s, err)
		}
		return gfx.RGBA(uint8(n>>16), uint8(n>>8), uint8(n), 255), nil
	case 8:
		n, err = strconv.ParseUint(s, 16, 32)
		if err != nil {
			return gfx.Color{}, fmt.Errorf("theme: color #%s: %w", s, err)
		}
		return gfx.RGBA(uint8(n>>24), uint8(n>>16), uint8(n>>8), uint8(n)), nil
	default:
		return gfx.Color{}, fmt.Errorf("theme: color #%s must be #RRGGBB or #RRGGBBAA", s)
	}
}

// FormatColor is #RRGGBB, or #RRGGBBAA when alpha is not opaque.
func FormatColor(c gfx.Color) string {
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}
