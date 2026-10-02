package tenfoot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fmt"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/theme"
)

// Options configure the native launcher window.
type Options struct {
	// CPUProfile and HeapProfile write optional diagnostic pprof files.
	CPUProfile   string
	HeapProfile  string
	APIBase      string
	APIToken     string
	TargetID     string
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
	// GFX selects sdl (default), software, fpga, fpga-stub, linuxfb, or
	// menu-display. Empty falls back to TENFOOT_GFX, then sdl. linuxfb and
	// menu-display run the shared App directly without SDL. menu-display
	// submits frames to the runtime menu socket. Other alternatives use the
	// SDL window shell. fpga records FC2D, not HDMI FPGA UI.
	GFX string
	// Framebuffer is the Linux framebuffer node for -gfx linuxfb.
	// MenuSocket is the runtime menu socket for -gfx menu-display; empty
	// becomes /run/mister-runtime.sock. The menu-display backend takes no
	// kit lease and only talks to that socket; the tenfoot app keeps its
	// existing host session client and existing status reads (for example
	// the kit-lease status read). Input is auto, none, or comma-separated
	// evdev nodes.
	Framebuffer string
	MenuSocket  string
	Input       string
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
	// HomeRoom is a room id opened as the root room at start and for Home
	// when Home is rooms. Empty falls back to tenfoot.json home_room, then
	// FOGCAST_HOME_ROOM. A missing or invalid id falls back to the picker.
	HomeRoom string
	// CatalogConfig is a FogCast config.toml. menu-display boots
	// BootLocalCatalog from it so the shelf works with the remote host
	// absent. Empty keeps the configured host API. The fogcast-tenfoot
	// flag parser fills this from -catalog-config, FOGCAST_CONFIG,
	// FES_HOST_CONFIG, config.toml beside launcher.json, or the default
	// user config when that file exists.
	CatalogConfig string
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
	if strings.TrimSpace(o.MenuSocket) == "" {
		o.MenuSocket = defaultMenuSocket
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
	o.HomeRoom = strings.TrimSpace(o.HomeRoom)
	if o.HomeRoom == "" && prefsErr == nil {
		o.HomeRoom = strings.TrimSpace(prefs.HomeRoom)
	}
	if o.HomeRoom == "" {
		o.HomeRoom = strings.TrimSpace(os.Getenv("FOGCAST_HOME_ROOM"))
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
	opts = opts.normalized()
	backend, err := gfx.ParseBackend(opts.GFX)
	if err != nil {
		return err
	}
	switch backend {
	case gfx.BackendLinuxFB:
		return runFramebuffer(ctx, opts)
	case gfx.BackendMenuDisplay:
		return runMenuDisplay(ctx, opts)
	default:
		return runWindow(ctx, opts)
	}
}

const defaultMenuSocket = "/run/mister-runtime.sock"

// sizedOptions forces the UI to the device's pixel geometry. linuxfb uses the
// framebuffer mode; menu-display is fixed at the runtime's 1280×720 panel.
func sizedOptions(opts Options, dev interface{ Config() gfx.FBConfig }) Options {
	cfg := dev.Config()
	opts.Width, opts.Height = cfg.Width, cfg.Height
	return opts
}

// configuredApp keeps application setup identical across native display backends.
func configuredApp(opts Options) (*App, error) {
	app := NewApp(NewClient(opts.APIBase, launcherHTTPClient(opts.APIToken, opts.TargetID)).withAPIHost(opts.APIHost), opts.Width, opts.Height, opts.MaxGames)
	if spec := strings.TrimSpace(opts.InputProfile); spec != "" {
		profile, err := inputmap.Resolve(spec)
		if err != nil {
			return nil, fmt.Errorf("input profile: %w", err)
		}
		remap, err := inputmap.NewRemapper(profile)
		if err != nil {
			return nil, fmt.Errorf("input profile: %w", err)
		}
		app.SetRemapper(remap)
	}
	look, err := theme.Resolve(opts.Theme)
	if err != nil {
		return nil, fmt.Errorf("theme: %w", err)
	}
	app.SetTheme(look)
	roomIndex, roomErr := loadRoomIndex(opts)
	if roomErr != nil {
		fmt.Fprintf(os.Stderr, "tenfoot: %v\n", roomErr)
	}
	app.SetRooms(roomIndex, opts.RoomsDir)
	homeRooms, _ := parseHomePref(opts.Home)
	app.SetHomeRooms(homeRooms)
	app.SetHomeRoom(opts.HomeRoom)
	app.SetDebugHUD(opts.DebugHUD)
	app.SetPrefsPath(opts.prefsPath())
	app.SetLayout(parseLayout(opts.Layout))
	app.SetSafeAreaPct(opts.SafeAreaPct)
	app.SetReducedMotion(opts.ReducedMotion)
	app.ConfigureAttract(opts.NoAttract, opts.attractForced())
	// menu-display is the kit_ui=tenfoot entry. Host SDL, software, and
	// linuxfb leave the local-control client unset. A resolved catalog
	// config replaces the remote API with the loopback catalog. A failed
	// boot keeps the configured host.
	if backend, err := gfx.ParseBackend(opts.GFX); err == nil && backend == gfx.BackendMenuDisplay {
		app.EnableKitLocal()
		if served := serveCatalog(opts.CatalogConfig); served != nil {
			app.client = NewClient(served.Base, launcherHTTPClient("", "")).withAPIHost("")
			app.localCatalogClose = served.Close
			app.localContent = served.ContentPath
		}
	}

	return app, nil
}

// serveCatalog boots the local catalog when configPath names a real file.
// A missing path or a failed boot returns nil and leaves the host API in place.
func serveCatalog(configPath string) *hostapi.LocalServe {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}
	paths, err := fogcast.PathsForConfig(configPath)
	if err != nil {
		return nil
	}
	served, err := hostapi.ServeLocal(context.Background(), paths)
	if err != nil {
		return nil
	}
	return served
}
