package tenfoot

import (
	"os"
	"path/filepath"
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
