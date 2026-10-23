package tenfoot

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/inputmap"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestClampSafeAreaPct(t *testing.T) {
	t.Parallel()
	if got := clampSafeAreaPct(-1); got != 0 {
		t.Fatalf("neg = %v", got)
	}
	if got := clampSafeAreaPct(0.5); got != maxSafeAreaPct {
		t.Fatalf("overflow = %v", got)
	}
	if got := clampSafeAreaPct(0.05); got != 0.05 {
		t.Fatalf("default = %v", got)
	}
}

func TestInsetsFromPct(t *testing.T) {
	t.Parallel()
	got := insetsFromPct(1280, 720, 0.05)
	if got.Left != 64 || got.Right != 64 || got.Top != 36 || got.Bottom != 36 {
		t.Fatalf("insets = %+v", got)
	}
}

func TestTenfootPrefsRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "FogCast", "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.08}); err != nil {
		t.Fatal(err)
	}
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SafeAreaPct != 0.08 {
		t.Fatalf("prefs = %#v", got)
	}
	if got.Layout != "grid" {
		t.Fatalf("default layout = %q", got.Layout)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestAppSafeAreaNudgePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app := NewApp(nil, 1280, 720, 10)
	app.SetPrefsPath(path)
	app.SetSafeAreaPct(0.05)
	app.HandleCommand(CmdSafeAreaIn, app.lastInput)
	if app.Snapshot().SafeAreaPct < 0.054 || app.Snapshot().SafeAreaPct > 0.056 {
		t.Fatalf("pct = %v", app.Snapshot().SafeAreaPct)
	}
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SafeAreaPct != app.Snapshot().SafeAreaPct {
		t.Fatalf("saved %v want %v", got.SafeAreaPct, app.Snapshot().SafeAreaPct)
	}
	if app.Snapshot().Grid.Safe.Left != insetsFromPct(1280, 720, got.SafeAreaPct).Left {
		t.Fatalf("grid insets = %+v", app.Snapshot().Grid.Safe)
	}
}

// Saving a setting from the menu rewrites tenfoot.json. The pad profiles in
// it survive.
func TestPersistingASettingKeepsPadProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	const guid = "0300e9f51f08000001e4000010010000"
	saved := inputmap.Profile{Preset: inputmap.PresetSwapAB, Bindings: map[string]string{"l": "keypad-1"}}
	if err := saveTenfootPrefs(path, tenfootPrefs{Controllers: map[string]inputmap.Profile{guid: saved}}); err != nil {
		t.Fatal(err)
	}
	app := NewApp(nil, 1280, 720, 10)
	app.SetPrefsPath(path)
	app.SetSafeAreaPct(0.05)
	app.HandleCommand(CmdSafeAreaIn, app.lastInput)
	got, err := loadTenfootPrefs(path)
	if err != nil || got.SafeAreaPct < 0.054 {
		t.Fatalf("safe area was not saved: %#v %v", got, err)
	}
	if p := got.Controllers[guid]; p.Preset != saved.Preset || p.Bindings["l"] != "keypad-1" {
		t.Fatalf("saving the safe area changed the pad profile: %#v", got.Controllers)
	}
}

func TestTenfootPrefsLayoutRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "FogCast", "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, Layout: "shelf"}); err != nil {
		t.Fatal(err)
	}
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != "shelf" || got.SafeAreaPct != 0.05 {
		t.Fatalf("prefs = %#v", got)
	}
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, Layout: "nope"}); err != nil {
		t.Fatal(err)
	}
	got, err = loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != "grid" {
		t.Fatalf("invalid layout = %q", got.Layout)
	}
}

func TestAppLayoutCyclePersistsAndKeepsFocus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app := catalogApp(20)
	app.SetPrefsPath(path)
	app.SetSafeAreaPct(0.05)
	app.HandleCommand(CmdRight, app.lastInput)
	if app.Snapshot().Grid.Focus != 1 {
		t.Fatalf("focus = %d", app.Snapshot().Grid.Focus)
	}
	app.HandleCommand(CmdLayoutCycle, app.lastInput)
	snap := app.Snapshot()
	if snap.Grid.Mode != LayoutShelf {
		t.Fatalf("mode = %s", snap.Grid.Mode)
	}
	if snap.Grid.Focus != 1 {
		t.Fatalf("shelf focus = %d", snap.Grid.Focus)
	}
	if !strings.Contains(snap.Status, "Shelf") {
		t.Fatalf("status = %q", snap.Status)
	}
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != "shelf" || got.SafeAreaPct != 0.05 {
		t.Fatalf("saved = %#v", got)
	}
	app.HandleCommand(CmdLayoutCycle, app.lastInput)
	if app.Snapshot().Grid.Mode != LayoutList {
		t.Fatalf("mode = %s", app.Snapshot().Grid.Mode)
	}
	app.HandleCommand(CmdDown, app.lastInput)
	if app.Snapshot().Grid.Focus != 2 {
		t.Fatalf("list down focus = %d", app.Snapshot().Grid.Focus)
	}
	app.HandleCommand(CmdLayoutCycle, app.lastInput)
	if app.Snapshot().Grid.Mode != LayoutGrid {
		t.Fatalf("mode = %s", app.Snapshot().Grid.Mode)
	}
}

func TestAppSafeAreaNudgeKeepsLayoutPref(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app := catalogApp(8)
	app.SetPrefsPath(path)
	app.SetSafeAreaPct(0.05)
	app.HandleCommand(CmdLayoutCycle, app.lastInput)
	app.HandleCommand(CmdSafeAreaIn, app.lastInput)
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != "shelf" {
		t.Fatalf("layout clobbered = %#v", got)
	}
}

func TestPersistSafeAreaDoesNotWriteCLILayoutOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.05, Layout: "list"}); err != nil {
		t.Fatal(err)
	}
	app := catalogApp(8)
	app.SetPrefsPath(path)
	app.SetLayout(LayoutShelf)
	app.SetSafeAreaPct(0.05)
	app.HandleCommand(CmdSafeAreaIn, app.lastInput)
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout != "list" {
		t.Fatalf("CLI layout override written to disk: %#v", got)
	}
	if got.SafeAreaPct < 0.054 || got.SafeAreaPct > 0.056 {
		t.Fatalf("pct = %v", got.SafeAreaPct)
	}
}

func TestPersistLayoutDoesNotWriteCLISafeAreaOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.08, Layout: "grid"}); err != nil {
		t.Fatal(err)
	}
	app := catalogApp(8)
	app.SetPrefsPath(path)
	app.SetLayout(LayoutGrid)
	app.SetSafeAreaPct(0)
	app.HandleCommand(CmdLayoutCycle, app.lastInput)
	got, err := loadTenfootPrefs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SafeAreaPct != 0.08 {
		t.Fatalf("CLI safe-area override written to disk: %#v", got)
	}
	if got.Layout != "shelf" {
		t.Fatalf("layout = %#v", got)
	}
}

func TestOptionsNormalizedLoadsLayoutPref(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.08, Layout: "list"}); err != nil {
		t.Fatal(err)
	}
	opts := Options{PrefsPath: path}.normalized()
	if opts.Layout != "list" || opts.SafeAreaPct != 0.08 {
		t.Fatalf("opts = %#v", opts)
	}
	opts = Options{PrefsPath: path, Layout: "shelf", LayoutSet: true, SafeAreaPct: 0, SafeAreaSet: true}.normalized()
	if opts.Layout != "shelf" || opts.SafeAreaPct != 0 {
		t.Fatalf("cli override = %#v", opts)
	}
}

func captureStateWarn(t *testing.T) *bytes.Buffer {
	t.Helper()
	stateWarnMu.Lock()
	prevWarned := stateDirWarned
	prevWriter := stateWarnWriter
	stateDirWarned = false
	buf := &bytes.Buffer{}
	stateWarnWriter = buf
	stateWarnMu.Unlock()
	t.Cleanup(func() {
		stateWarnMu.Lock()
		stateDirWarned = prevWarned
		stateWarnWriter = prevWriter
		stateWarnMu.Unlock()
	})
	return buf
}

func makeTreeReadOnly(t *testing.T, root string) {
	t.Helper()
	var paths []string
	t.Cleanup(func() {
		for i := len(paths) - 1; i >= 0; i-- {
			if err := os.Chmod(paths[i], 0o755); err != nil {
				t.Errorf("restore %s: %v", paths[i], err)
			}
		}
	})
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		mode := os.FileMode(0o444)
		if info.IsDir() {
			mode = 0o555
		}
		return os.Chmod(path, mode)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeKitPrefs(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, "FogCast", "tenfoot.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPersistentConfigDirLoadsReadOnlyKitState is the kit case: the FAT
// state directory is both XDG_CONFIG_HOME and the fallback, then mounts
// read-only with saved preferences and Home room state still readable.
func TestPersistentConfigDirLoadsReadOnlyKitState(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(state, "FogCast", "rooms"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "FogCast", "tenfoot.json"), []byte("{\n  \"safe_area_pct\": 0.08,\n  \"layout\": \"shelf\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "FogCast", "rooms", "example.fes-cores.state.json"), []byte("{\"package_id\":\"example.pong\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeTreeReadOnly(t, state)
	t.Setenv("XDG_CONFIG_HOME", state)
	useKitStateDir(t, state)
	captureStateWarn(t)

	if got := persistentConfigDir(); got != state {
		t.Fatalf("config dir = %q, want %q", got, state)
	}
	opts := Options{}.normalized()
	if opts.Layout != "shelf" || opts.SafeAreaPct != 0.08 {
		t.Fatalf("opts = %#v", opts)
	}
	room := filepath.Join(filepath.Dir(defaultPrefsPath()), "rooms", "example.fes-cores.state.json")
	data, err := os.ReadFile(room)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "example.pong") {
		t.Fatalf("room state = %s", data)
	}
}

func TestPersistentConfigDirWarnsWhenKitStateIsReadOnlyAndEmpty(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(state, 0o755); err != nil {
			t.Errorf("restore %s: %v", state, err)
		}
	})
	t.Setenv("XDG_CONFIG_HOME", state)
	useKitStateDir(t, state)
	warn := captureStateWarn(t)

	if got := persistentConfigDir(); got != "" {
		t.Fatalf("config dir = %q, want empty", got)
	}
	if strings.Count(warn.String(), "state directory is not writable") != 1 {
		t.Fatalf("warning = %q", warn.String())
	}
}

// TestPersistentConfigDirKeepsReadableReadOnlyState loads saved preferences
// and the Home room file from a config directory that can no longer be
// written, instead of a different writable directory.
func TestPersistentConfigDirKeepsReadableReadOnlyState(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "xdg")
	kit := filepath.Join(root, "kit")
	if err := os.MkdirAll(filepath.Join(user, "FogCast", "rooms"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(kit, 0o755); err != nil {
		t.Fatal(err)
	}
	const saved = "{\n  \"safe_area_pct\": 0.08,\n  \"layout\": \"shelf\",\n  \"home\": \"rooms\"\n}\n"
	if err := os.WriteFile(filepath.Join(user, "FogCast", "tenfoot.json"), []byte(saved), 0o644); err != nil {
		t.Fatal(err)
	}
	const roomBody = "{\n  \"package_id\": \"example.pong\"\n}\n"
	roomFile := filepath.Join(user, "FogCast", "rooms", "example.fes-cores.state.json")
	if err := os.WriteFile(roomFile, []byte(roomBody), 0o644); err != nil {
		t.Fatal(err)
	}
	writeKitPrefs(t, kit, "{\n  \"safe_area_pct\": 0.02,\n  \"layout\": \"list\"\n}\n")
	makeTreeReadOnly(t, user)
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv("HOME", filepath.Join(root, "home"))
	useKitStateDir(t, kit)
	warn := captureStateWarn(t)

	if got := persistentConfigDir(); got != user {
		t.Fatalf("config dir = %q, want read-only %q", got, user)
	}
	if strings.Count(warn.String(), "state directory is not writable") != 1 {
		t.Fatalf("warning = %q", warn.String())
	}
	opts := Options{}.normalized()
	if opts.Layout != "shelf" || opts.SafeAreaPct != 0.08 || opts.Home != "rooms" {
		t.Fatalf("loaded opts = %#v", opts)
	}
	prefsPath := defaultPrefsPath()
	if prefsPath != filepath.Join(user, "FogCast", "tenfoot.json") {
		t.Fatalf("prefs path = %q", prefsPath)
	}
	roomPath := filepath.Join(filepath.Dir(prefsPath), "rooms", "example.fes-cores.state.json")
	data, err := os.ReadFile(roomPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != roomBody {
		t.Fatalf("room state = %s", data)
	}
	if err := saveTenfootPrefs(prefsPath, tenfootPrefs{SafeAreaPct: 0.1, Layout: "grid"}); err == nil {
		t.Fatal("save to read-only config succeeded")
	}
	kitData, err := os.ReadFile(filepath.Join(kit, "FogCast", "tenfoot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kitData), "\"layout\": \"list\"") {
		t.Fatalf("kit prefs changed: %s", kitData)
	}
}

// TestPersistentConfigDirKeepsReadOnlyRoomPack keeps a directory whose only
// saved content is a room pack, so the default room search path still finds
// that pack when the directory cannot be written.
func TestPersistentConfigDirKeepsReadOnlyRoomPack(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "xdg")
	kit := filepath.Join(root, "kit")
	pack := filepath.Join(user, "FogCast", "rooms", "my-room")
	if err := os.MkdirAll(pack, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(kit, "FogCast", "rooms", "other-room"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id = \"my-room\"\ntitle = \"My Room\"\nmain = \"main.lua\"\n"
	if err := os.WriteFile(filepath.Join(pack, rooms.ManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pack, "main.lua"), []byte("function on_enter() end\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(kit, "FogCast", "rooms", "other-room", rooms.ManifestName)
	if err := os.WriteFile(other, []byte("id = \"other-room\"\ntitle = \"Other\"\nmain = \"main.lua\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeTreeReadOnly(t, user)
	t.Setenv("XDG_CONFIG_HOME", user)
	t.Setenv("HOME", filepath.Join(root, "home"))
	useKitStateDir(t, kit)
	captureStateWarn(t)

	if got := persistentConfigDir(); got != user {
		t.Fatalf("config dir = %q, want read-only %q", got, user)
	}
	opts := Options{}.normalized()
	wantRooms := filepath.Join(user, "FogCast", "rooms")
	if opts.RoomsDir != wantRooms {
		t.Fatalf("rooms dir = %q, want %q", opts.RoomsDir, wantRooms)
	}
	found, err := rooms.LoadDir(opts.RoomsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != "my-room" || !found[0].Valid() {
		t.Fatalf("packs = %+v", found)
	}
}

// TestPersistentConfigDirIgnoresReadOnlyRoomsWithoutPack falls back when the
// rooms directory has no state file and no room pack.
func TestPersistentConfigDirIgnoresReadOnlyRoomsWithoutPack(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "xdg")
	kit := filepath.Join(root, "kit")
	notes := filepath.Join(user, "FogCast", "rooms", "notes")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(kit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notes, "readme.txt"), []byte("not a pack"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeTreeReadOnly(t, user)
	t.Setenv("XDG_CONFIG_HOME", user)
	useKitStateDir(t, kit)
	captureStateWarn(t)

	if got := persistentConfigDir(); got != kit {
		t.Fatalf("config dir = %q, want kit %q", got, kit)
	}
}

// TestPersistentConfigDirKeepsReadOnlyRoomState keeps a directory whose only
// saved file is the Home room store.
func TestPersistentConfigDirKeepsReadOnlyRoomState(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "xdg")
	kit := filepath.Join(root, "kit")
	roomDir := filepath.Join(user, "FogCast", "rooms")
	if err := os.MkdirAll(roomDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(kit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roomDir, "example.fes-cores.state.json"), []byte("{\"package_id\":\"example.pong\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeTreeReadOnly(t, user)
	t.Setenv("XDG_CONFIG_HOME", user)
	useKitStateDir(t, kit)
	captureStateWarn(t)

	if got := persistentConfigDir(); got != user {
		t.Fatalf("config dir = %q, want %q", got, user)
	}
}

// TestPersistentConfigDirSkipsReadOnlyDirWithoutState uses a writable
// fallback when the configured directory is read-only and empty.
func TestPersistentConfigDirSkipsReadOnlyDirWithoutState(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "xdg")
	kit := filepath.Join(root, "kit")
	if err := os.MkdirAll(user, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(kit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(user, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(user, 0o755); err != nil {
			t.Errorf("restore %s: %v", user, err)
		}
	})
	t.Setenv("XDG_CONFIG_HOME", user)
	useKitStateDir(t, kit)
	warn := captureStateWarn(t)

	if got := persistentConfigDir(); got != kit {
		t.Fatalf("config dir = %q, want kit %q", got, kit)
	}
	if warn.Len() != 0 {
		t.Fatalf("warning = %q", warn.String())
	}
}

// TestPersistentConfigDirIgnoresUnreadableState falls back when the only
// saved file cannot be read.
func TestPersistentConfigDirIgnoresUnreadableState(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "xdg")
	kit := filepath.Join(root, "kit")
	prefs := filepath.Join(user, "FogCast", "tenfoot.json")
	if err := os.MkdirAll(filepath.Dir(prefs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(kit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prefs, []byte("{\"layout\":\"shelf\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(prefs, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(user, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(user, 0o755)
		_ = os.Chmod(prefs, 0o644)
	})
	t.Setenv("XDG_CONFIG_HOME", user)
	useKitStateDir(t, kit)

	if got := persistentConfigDir(); got != kit {
		t.Fatalf("config dir = %q, want kit %q", got, kit)
	}
}

func TestPersistentConfigDirCreatesMissingXDGParents(t *testing.T) {
	root := t.TempDir()
	kit := filepath.Join(root, "kit")
	writeKitPrefs(t, kit, "{\n  \"safe_area_pct\": 0.02,\n  \"layout\": \"list\"\n}\n")
	useKitStateDir(t, kit)
	warn := captureStateWarn(t)

	cases := []struct {
		name string
		user string
	}{
		{name: "missing config dir", user: filepath.Join(root, "profile", "config")},
		{name: "missing parents", user: filepath.Join(root, "new-profile", "nested", "config")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := tc.user
			t.Setenv("XDG_CONFIG_HOME", user)
			if got := persistentConfigDir(); got != user {
				t.Fatalf("config dir = %q, want %q", got, user)
			}
			path := defaultPrefsPath()
			if err := saveTenfootPrefs(path, tenfootPrefs{SafeAreaPct: 0.08, Layout: "shelf"}); err != nil {
				t.Fatal(err)
			}
			got, err := loadTenfootPrefs(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.Layout != "shelf" || got.SafeAreaPct != 0.08 {
				t.Fatalf("prefs = %#v", got)
			}
		})
	}
	if warn.Len() != 0 {
		t.Fatalf("warning = %q", warn.String())
	}
	kitData, err := os.ReadFile(filepath.Join(kit, "FogCast", "tenfoot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kitData), "\"layout\": \"list\"") {
		t.Fatalf("kit prefs changed: %s", kitData)
	}
}

func TestPersistentConfigDirRejectsUnwritableAncestor(t *testing.T) {
	root := t.TempDir()
	kit := filepath.Join(root, "kit")
	if err := os.MkdirAll(kit, 0o755); err != nil {
		t.Fatal(err)
	}
	useKitStateDir(t, kit)

	t.Run("ancestor is a file", func(t *testing.T) {
		file := filepath.Join(root, "not-a-dir")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		user := filepath.Join(file, "config")
		t.Setenv("XDG_CONFIG_HOME", user)
		if got := persistentConfigDir(); got != kit {
			t.Fatalf("config dir = %q, want kit %q", got, kit)
		}
	})

	t.Run("nearest ancestor is read-only", func(t *testing.T) {
		parent := filepath.Join(root, "ro-parent")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(parent, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(parent, 0o755); err != nil {
				t.Errorf("restore %s: %v", parent, err)
			}
		})
		user := filepath.Join(parent, "missing", "config")
		t.Setenv("XDG_CONFIG_HOME", user)
		if got := persistentConfigDir(); got != kit {
			t.Fatalf("config dir = %q, want kit %q", got, kit)
		}
	})
}
