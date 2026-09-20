package tenfoot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/ui/rooms"
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
	// GFX selects the 2D Device: sdl (default), software, fpga, or
	// fpga-stub. Empty falls back to TENFOOT_GFX, then sdl. fpga records
	// the FC2D command stream and rasters with Software (not HDMI FPGA
	// UI). Production sofa runs keep WrapSDLRenderer. linuxfb is the kit
	// framebuffer Device and is not opened from the SDL sofa shell.
	GFX string
	// InputProfile is a built-in name (identity, swap-ab) or a JSON file
	// path. Empty is identity.
	InputProfile string
	// Theme is a built-in or pack name (default/classic, arcade/neon,
	// night/sofa-dim) or a JSON/TOML
	// file path. Empty is default.
	Theme string
	// DebugHUD paints the optional corner overlay (flight, lease gen/ttl,
	// last error). Off by default. -debug-hud, tenfoot.json debug_hud, or
	// FOGCAST_DEBUG_HUD.
	DebugHUD    bool
	DebugHUDSet bool
	// RoomsDir holds room packs (one directory per room with room.toml).
	// Empty falls back to tenfoot.json rooms_dir, FOGCAST_ROOMS, then
	// <config>/FogCast/rooms.
	RoomsDir string
	// Home is "library" or "rooms": the screen shown at start. Empty falls
	// back to tenfoot.json home, then library.
	Home    string
	HomeSet bool
	// ReducedMotion skips decorative room animation. Empty falls back to
	// FOGCAST_TENFOOT_REDUCED_MOTION / FOGCAST_REDUCED_MOTION, then
	// tenfoot.json reduced_motion.
	ReducedMotion    bool
	ReducedMotionSet bool
}

func defaultRoomsDir(prefsPath string) string {
	if strings.TrimSpace(prefsPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(prefsPath), "rooms")
}

func (o Options) prefsPath() string {
	if strings.TrimSpace(o.PrefsPath) != "" {
		return o.PrefsPath
	}
	return defaultPrefsPath()
}

func (o Options) normalized() Options {
	if strings.TrimSpace(o.APIBase) == "" {
		o.APIBase = hostclient.DefaultAPIBase
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
	if strings.TrimSpace(o.Theme) == "" && prefsErr == nil {
		o.Theme = strings.TrimSpace(prefs.Theme)
	}
	if strings.TrimSpace(o.Theme) == "" {
		o.Theme = strings.TrimSpace(os.Getenv("FOGCAST_THEME"))
	}
	if envTruthy(os.Getenv("FOGCAST_DEBUG_HUD")) {
		o.DebugHUD = true
		o.DebugHUDSet = true
	}
	if !o.DebugHUDSet && prefsErr == nil && prefs.DebugHUD {
		o.DebugHUD = true
	}
	if strings.TrimSpace(o.RoomsDir) == "" && prefsErr == nil {
		o.RoomsDir = strings.TrimSpace(prefs.RoomsDir)
	}
	if strings.TrimSpace(o.RoomsDir) == "" {
		o.RoomsDir = strings.TrimSpace(os.Getenv("FOGCAST_ROOMS"))
	}
	if strings.TrimSpace(o.RoomsDir) == "" {
		o.RoomsDir = defaultRoomsDir(o.prefsPath())
	}
	if !o.HomeSet && prefsErr == nil {
		if _, ok := parseHomePref(prefs.Home); ok {
			o.Home = strings.ToLower(strings.TrimSpace(prefs.Home))
		}
	}
	if rooms, ok := parseHomePref(o.Home); ok {
		o.Home = homePrefValue(rooms)
	} else {
		o.Home = homePrefValue(false)
	}
	if envTruthy(os.Getenv("FOGCAST_TENFOOT_REDUCED_MOTION")) || envTruthy(os.Getenv("FOGCAST_REDUCED_MOTION")) {
		o.ReducedMotion = true
		o.ReducedMotionSet = true
	}
	if !o.ReducedMotionSet && prefsErr == nil {
		o.ReducedMotion = prefs.ReducedMotion
	}
	return o
}

// loadRoomIndex discovers room packs for opts, merging the embedded
// examples first so a user pack with the same id wins.
func loadRoomIndex(opts Options) (*rooms.Index, error) {
	packs, err := rooms.LoadDir(opts.RoomsDir)
	if err != nil {
		return rooms.NewIndex(rooms.Examples()), err
	}
	return rooms.NewIndex(rooms.Examples(), packs), nil
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
