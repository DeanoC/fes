package tenfoot

import (
	"context"
	"os"
	"strings"
	"time"
)

// Options configure the native launcher window.
type Options struct {
	APIBase      string
	Width        int
	Height       int
	Fullscreen   bool
	Hidden       bool
	Smoke        bool
	MaxGames     int
	SmokeTimeout time.Duration
	SafeAreaPct  float64
	SafeAreaSet  bool
	Layout       string
	LayoutSet    bool
	NoAttract    bool
	NoAttractSet bool
	PrefsPath    string
	APIHost      string
	// GFX selects the 2D Device: sdl (default), software, or fpga-stub.
	// Empty falls back to TENFOOT_GFX, then sdl. Production sofa runs
	// keep WrapSDLRenderer. linuxfb is the kit framebuffer Device and
	// is not opened from the SDL sofa shell.
	GFX string
}

func (o Options) prefsPath() string {
	if strings.TrimSpace(o.PrefsPath) != "" {
		return o.PrefsPath
	}
	return defaultPrefsPath()
}

func (o Options) normalized() Options {
	if strings.TrimSpace(o.APIBase) == "" {
		o.APIBase = DefaultAPIBase
	}
	if o.Width <= 0 {
		o.Width = 1280
	}
	if o.Height <= 0 {
		o.Height = 720
	}
	if o.MaxGames <= 0 {
		o.MaxGames = defaultMaxGames
	}
	if o.Smoke && o.SmokeTimeout <= 0 {
		o.SmokeTimeout = 45 * time.Second
	}
	if o.Smoke {
		o.Hidden = true
		o.NoAttract = true
		o.NoAttractSet = true
		if o.MaxGames > 400 {
			o.MaxGames = 400
		}
		o.APIHost = smokeAPIHost(o.APIBase, o.APIHost)
	} else {
		o.APIHost = strings.TrimSpace(o.APIHost)
	}
	prefs, prefsErr := loadTenfootPrefs(o.prefsPath())
	if !o.SafeAreaSet {
		if pct, ok := parseSafeAreaEnv(os.Getenv("FOGCAST_TENFOOT_SAFE_AREA")); ok {
			o.SafeAreaPct = pct
			o.SafeAreaSet = true
		}
	}
	if !o.SafeAreaSet && prefsErr == nil {
		o.SafeAreaPct = prefs.SafeAreaPct
		o.SafeAreaSet = true
	}
	if !o.SafeAreaSet {
		o.SafeAreaPct = DefaultSafeAreaPct
	}
	o.SafeAreaPct = clampSafeAreaPct(o.SafeAreaPct)
	if !o.LayoutSet && prefsErr == nil {
		o.Layout = prefs.Layout
	}
	o.Layout = parseLayout(o.Layout).String()
	if envTruthy(os.Getenv("FOGCAST_TENFOOT_NO_ATTRACT")) {
		o.NoAttract = true
		o.NoAttractSet = true
	}
	if !o.NoAttractSet && prefsErr == nil && !prefsAttractEnabled(prefs) {
		o.NoAttract = true
	}
	if strings.TrimSpace(o.GFX) == "" {
		o.GFX = strings.TrimSpace(os.Getenv("TENFOOT_GFX"))
	}
	return o
}

func (o Options) attractForced() bool {
	return o.NoAttract && o.NoAttractSet
}

func envTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Run starts the native launcher. SDL3 builds use the window backend.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return runWindow(ctx, opts.normalized())
}
