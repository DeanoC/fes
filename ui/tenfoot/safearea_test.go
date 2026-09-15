package tenfoot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
