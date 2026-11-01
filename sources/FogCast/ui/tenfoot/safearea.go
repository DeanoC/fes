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

	"github.com/DeanoC/FogCast/ui/inputmap"
	"github.com/DeanoC/FogCast/ui/rooms"
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
	// Surround draws the TV surround around a running machine on kit HDMI.
	// Unset is on.
	Surround *bool `json:"surround,omitempty"`
	// RoomsDir overrides the room pack directory.
	RoomsDir string `json:"rooms_dir,omitempty"`
	// Controllers holds kit pads' saved profiles, keyed by the GUID tenfoot
	// logs when a pad attaches.
	Controllers map[string]inputmap.Profile `json:"controllers,omitempty"`
	// FirmwareInfoSeen lists the families whose first-use firmware card
	// was continued past.
	FirmwareInfoSeen []string `json:"firmware_info_seen,omitempty"`
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
// XDG_CONFIG_HOME when set and $HOME/.config otherwise. A directory that
// already holds readable settings, room state, or a room pack is kept even
// when it is read-only, so a temporarily read-only volume does not hide
// them. A
// directory that does not exist yet is used when its nearest existing
// ancestor is writable, so the save can create missing parents. The kit FAT
// directory is the fallback when the user directory cannot be read and
// cannot be created.
func persistentConfigDir() string {
	user, userErr := os.UserConfigDir()
	if userErr == nil {
		if dir, ok := chooseConfigDir(user); ok {
			return dir
		}
	}
	if dir := strings.TrimSpace(kitPersistentStateDir); dir != "" && (userErr != nil || dir != user) {
		if chosen, ok := chooseConfigDir(dir); ok {
			return chosen
		}
	}
	noteStateDirUnavailable()
	return ""
}

// chooseConfigDir reports dir when tenfoot should load and save state there.
// Readable settings, room state, or a room pack win even when the directory
// cannot be written. Otherwise dir is used only when it is writable, or when
// it does not exist yet and its nearest existing ancestor is a writable
// directory.
func chooseConfigDir(dir string) (string, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", false
	}
	if configDirHasContent(dir) {
		if !configDirWritable(dir) {
			noteStateDirUnavailable()
		}
		return dir, true
	}
	if configDirWritable(dir) {
		return dir, true
	}
	return "", false
}

// configDirWritable reports whether dir is a writable directory, or does not
// exist yet while its nearest existing ancestor is a writable directory.
// It does not create dir.
func configDirWritable(dir string) bool {
	info, err := os.Stat(dir)
	if err == nil {
		return info.IsDir() && unix.Access(dir, unix.W_OK) == nil
	}
	if !os.IsNotExist(err) {
		return false
	}
	return ancestorWritable(dir)
}

// ancestorWritable reports whether the nearest existing ancestor of dir is a
// writable directory. dir itself is absent.
func ancestorWritable(dir string) bool {
	cur := filepath.Clean(dir)
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		info, err := os.Stat(parent)
		if err == nil {
			return info.IsDir() && unix.Access(parent, unix.W_OK) == nil
		}
		if !os.IsNotExist(err) {
			return false
		}
		cur = parent
	}
}

// configDirHasContent reports whether dir already holds a readable
// tenfoot.json, a room state file, or a room pack. An unreadable file does
// not count. Packs are immediate FogCast/rooms subdirectories with a
// readable room.toml, the same discovery rule as rooms.LoadDir.
func configDirHasContent(dir string) bool {
	base := filepath.Join(dir, "FogCast")
	if readableFile(filepath.Join(base, "tenfoot.json")) {
		return true
	}
	roomsDir := filepath.Join(base, "rooms")
	entries, err := os.ReadDir(roomsDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(roomsDir, name)
		if entry.IsDir() {
			if readableFile(filepath.Join(path, rooms.ManifestName)) {
				return true
			}
			continue
		}
		if strings.HasSuffix(name, ".state.json") && readableFile(path) {
			return true
		}
	}
	return false
}

func readableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	f.Close()
	return true
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
