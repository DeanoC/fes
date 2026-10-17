package tenfoot

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// kitPersistentStateDir is the writable FAT directory the kit software
// supervisor also publishes as XDG_CONFIG_HOME and XDG_STATE_HOME.
// launcher.json stays beside it. Preferences and room state live under
// FogCast/ inside it. Tests replace it with a temporary directory.
var kitPersistentStateDir = "/media/fat/fogcast/state"

var (
	stateWarnMu      sync.Mutex
	stateWarnWriter  io.Writer
	stateDirWarned   bool
	stateWriteLogged sync.Map
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
	Theme          string  `json:"theme,omitempty"`
	DebugHUD       bool    `json:"debug_hud,omitempty"`
	// Home is "library" or "rooms": the screen shown at start.
	Home string `json:"home,omitempty"`
	// HomeRoom is the room id opened at start and for Home when Home is rooms.
	// Empty uses the Home picker.
	HomeRoom string `json:"home_room,omitempty"`
	// PinnedRooms is the local Home pin list (room ids).
	PinnedRooms []string `json:"pinned_rooms,omitempty"`
	// ReducedMotion skips decorative room animation.
	ReducedMotion bool `json:"reduced_motion,omitempty"`
	// RoomsDir overrides the room pack directory.
	RoomsDir string `json:"rooms_dir,omitempty"`
}

func homePrefValue(rooms bool) string {
	if rooms {
		return "rooms"
	}
	return "library"
}

func parseHomePref(s string) (rooms bool, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "rooms", "room":
		return true, true
	case "library", "grid":
		return false, true
	default:
		return false, false
	}
}

func defaultPrefsPath() string {
	dir := persistentConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "FogCast", "tenfoot.json")
}

// persistentConfigDir is where tenfoot.json and the sibling rooms directory
// are stored. os.UserConfigDir supplies that directory, which on Linux is
// XDG_CONFIG_HOME when the kit supervisor sets it. An existing readable
// directory is kept even when it cannot be written, so a read-only volume
// still restores saved preferences and Home selection. The kit FAT directory
// is used only when that configured directory has no saved state and cannot
// be created, which is HOME=/ on the read-only root.
func persistentConfigDir() string {
	user, userErr := os.UserConfigDir()
	if userErr == nil && (configHasState(user) || configDirCreatable(user)) {
		return user
	}
	if dir := strings.TrimSpace(kitPersistentStateDir); dir != user && (configHasState(dir) || configDirCreatable(dir)) {
		return dir
	}
	if userErr == nil && configDirReadable(user) {
		return user
	}
	noteStateDirUnavailable()
	return ""
}

// configHasState reports saved tenfoot preferences or room files under dir.
func configHasState(dir string) bool {
	base := filepath.Join(strings.TrimSpace(dir), "FogCast")
	if base == "FogCast" {
		return false
	}
	if _, err := os.Stat(filepath.Join(base, "tenfoot.json")); err == nil {
		return true
	}
	info, err := os.Stat(filepath.Join(base, "rooms"))
	return err == nil && info.IsDir()
}

// configDirReadable reports an existing directory that can be listed.
func configDirReadable(dir string) bool {
	info, err := os.Stat(strings.TrimSpace(dir))
	return err == nil && info.IsDir() && unix.Access(dir, unix.R_OK) == nil
}

// configDirCreatable reports a directory saveTenfootPrefs can create with
// MkdirAll. Missing parents are fine when the nearest existing ancestor is
// a writable directory. It does not create anything.
func configDirCreatable(dir string) bool {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" || dir == "." {
		return false
	}
	current := dir
	for {
		info, err := os.Stat(current)
		if err == nil {
			return info.IsDir() && unix.Access(current, unix.W_OK) == nil
		}
		if !os.IsNotExist(err) {
			return false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}

func noteStateDirUnavailable() {
	stateWarnMu.Lock()
	defer stateWarnMu.Unlock()
	if stateDirWarned {
		return
	}
	stateDirWarned = true
	stateWarnLocked("tenfoot: state directory is not writable; settings and room state will not be saved\n")
}

func warnPrefsWrite(path string, err error) {
	if err == nil || path == "" {
		return
	}
	if _, loaded := stateWriteLogged.LoadOrStore(path, struct{}{}); loaded {
		return
	}
	stateWarn("tenfoot: could not save settings %s: %v\n", path, err)
}

func stateWarn(format string, args ...any) {
	stateWarnMu.Lock()
	defer stateWarnMu.Unlock()
	stateWarnLocked(format, args...)
}

func stateWarnLocked(format string, args ...any) {
	w := stateWarnWriter
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, format, args...)
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
