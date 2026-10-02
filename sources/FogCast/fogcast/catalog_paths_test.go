package fogcast

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCatalogConfigPrefersSiblingAndExplicit(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "launcher.json")
	sibling := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(launcher, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("token = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	getenv := func(string) string { return "" }
	if got := ResolveCatalogConfig("", launcher, getenv); got != sibling {
		t.Fatalf("sibling = %q", got)
	}
	missing := filepath.Join(dir, "missing.toml")
	if got := ResolveCatalogConfig(missing, launcher, getenv); got != "" {
		t.Fatalf("missing explicit fell through to %q", got)
	}
	other := filepath.Join(dir, "other.toml")
	if err := os.WriteFile(other, []byte("token = \"y\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveCatalogConfig("", launcher, func(key string) string {
		if key == "FOGCAST_CONFIG" {
			return other
		}
		return ""
	}); got != other {
		t.Fatalf("env = %q", got)
	}
	paths, err := PathsForConfig(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Config != sibling || filepath.Dir(paths.Index) != filepath.Join(dir, "state") {
		t.Fatalf("paths = %+v", paths)
	}
	if defaults, err := DefaultPaths(); err == nil {
		if paths.Index == defaults.Index {
			t.Fatal("sibling config reused the home library index")
		}
	}
}
