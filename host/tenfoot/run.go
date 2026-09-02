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
	NoAttract    bool
	PrefsPath    string
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
		if o.MaxGames > 400 {
			o.MaxGames = 400
		}
	}
	if !o.SafeAreaSet {
		if pct, ok := parseSafeAreaEnv(os.Getenv("FOGCAST_TENFOOT_SAFE_AREA")); ok {
			o.SafeAreaPct = pct
			o.SafeAreaSet = true
		}
	}
	if !o.SafeAreaSet {
		if prefs, err := loadTenfootPrefs(o.prefsPath()); err == nil {
			o.SafeAreaPct = prefs.SafeAreaPct
			o.SafeAreaSet = true
		}
	}
	if !o.SafeAreaSet {
		o.SafeAreaPct = DefaultSafeAreaPct
	}
	o.SafeAreaPct = clampSafeAreaPct(o.SafeAreaPct)
	if envTruthy(os.Getenv("FOGCAST_TENFOOT_NO_ATTRACT")) {
		o.NoAttract = true
	}
	return o
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
