package tenfoot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// DefaultSafeAreaPct is a 5% overscan gutter on each edge.
	DefaultSafeAreaPct = 0.05
	maxSafeAreaPct     = 0.20
	safeAreaNudge      = 0.005
)

// SafeArea is the overscan gutter in pixels. Chrome draws inside it.
type SafeArea struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

func clampSafeAreaPct(pct float64) float64 {
	if pct < 0 {
		return 0
	}
	if pct > maxSafeAreaPct {
		return maxSafeAreaPct
	}
	return pct
}

func insetsFromPct(width, height int, pct float64) SafeArea {
	pct = clampSafeAreaPct(pct)
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	left := int(float64(width)*pct + 0.5)
	top := int(float64(height)*pct + 0.5)
	return SafeArea{Left: left, Top: top, Right: left, Bottom: top}
}

func (g Grid) contentLeft() int {
	return g.Safe.Left
}

func (g Grid) contentTop() int {
	return g.Safe.Top
}

func (g Grid) contentWidth() int {
	w := g.Width - g.Safe.Left - g.Safe.Right
	if w < 1 {
		return 1
	}
	return w
}

func (g Grid) contentHeight() int {
	h := g.Height - g.Safe.Top - g.Safe.Bottom
	if h < 1 {
		return 1
	}
	return h
}

func (g Grid) headerY() int {
	return g.Safe.Top
}

func (g Grid) footerY() int {
	y := g.Height - g.Safe.Bottom - g.FooterHeight
	if y < g.Safe.Top {
		return g.Safe.Top
	}
	return y
}

type tenfootPrefs struct {
	SafeAreaPct    float64 `json:"safe_area_pct"`
	Layout         string  `json:"layout"`
	AttractEnabled *bool   `json:"attract_enabled,omitempty"`
}

func defaultPrefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "FogCast", "tenfoot.json")
}

func loadTenfootPrefs(path string) (tenfootPrefs, error) {
	if path == "" {
		return tenfootPrefs{}, fmt.Errorf("prefs path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tenfootPrefs{}, err
	}
	var prefs tenfootPrefs
	if err := json.Unmarshal(data, &prefs); err != nil {
		return tenfootPrefs{}, err
	}
	prefs.SafeAreaPct = clampSafeAreaPct(prefs.SafeAreaPct)
	prefs.Layout = parseLayout(prefs.Layout).String()
	return prefs, nil
}

func saveTenfootPrefs(path string, prefs tenfootPrefs) error {
	if path == "" {
		return fmt.Errorf("prefs path is empty")
	}
	prefs.SafeAreaPct = clampSafeAreaPct(prefs.SafeAreaPct)
	prefs.Layout = parseLayout(prefs.Layout).String()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func parseSafeAreaEnv(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	pct, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return clampSafeAreaPct(pct), true
}
