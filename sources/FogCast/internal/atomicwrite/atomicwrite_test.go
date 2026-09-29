package atomicwrite

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// plain83 reports whether name is a single-case 8.3 name (base <= 8, optional
// ext <= 3, at most one dot). Kit A evidence (#317): only entries that are NOT
// plain 8.3 names alias longer lookups.
func plain83(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.Count(name, ".") > 1 {
		return false
	}
	if name != strings.ToLower(name) && name != strings.ToUpper(name) {
		return false
	}
	base, ext, _ := strings.Cut(name, ".")
	return len(base) >= 1 && len(base) <= 8 && len(ext) <= 3
}

// kitAliases models the kit's /media/fat exFAT lookup bug: looking up name
// resolves to the existing entry when the entry is a strict prefix of name and
// is not a plain 8.3 name.
func kitAliases(entry, name string) bool {
	return len(name) > len(entry) && strings.HasPrefix(name, entry) && !plain83(entry)
}

func TestKitAliasModelMatchesKitEvidence(t *testing.T) {
	// Observed on kit A, 2026-09-29 (5.15.1-MiSTer, exFAT 1.2.11).
	for _, c := range []struct {
		entry, name string
		want        bool
	}{
		{"scratch.json", "scratch.json.tmp", true},
		{"scratch.json", "scratch.json.bak", true},
		{"scratch.json", "scratch.jsonx", true},
		{"scratch.json", "scratch.js", false},
		{"scratch.json", ".scratch.json.1234.tmp", false},
		{"launcher.json", "launcher.json.x", true},
		{"agent.toml", "agent.toml.bak", true},
		{"abcdefgh", "abcdefgh.tmp", false},
		{"abcdefghi", "abcdefghi.tmp", true},
		{"abcdefgh.ijk", "abcdefgh.ijk.tmp", false},
		{"q.json", "q.json.tmp", true},
		{"a.b.c", "a.b.cx", true},
		{"Mixed.Ab", "Mixed.Ab.tmp", true},
		{"idle.rbf", "idle.rbfx", false},
		{".hidden.json", ".hidden.json.tmp", true},
	} {
		if got := kitAliases(c.entry, c.name); got != c.want {
			t.Errorf("kitAliases(%q, %q) = %v, want %v", c.entry, c.name, got, c.want)
		}
	}
}

var kitTargets = []string{
	"launcher.json", "agent.toml", "catalog.json", "launch-map.json", "u-boot.txt",
	"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	".hidden.json", "a", "idle.rbf", "LAUNCHER.JSON",
}

func TestTempNameNeverHasTargetAsPrefix(t *testing.T) {
	dir := t.TempDir()
	for _, target := range kitTargets {
		for i := 0; i < 32; i++ {
			f, err := os.CreateTemp(dir, TempPattern(target))
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Base(f.Name())
			f.Close()
			os.Remove(f.Name())
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(target)) {
				t.Fatalf("temp %q has target %q as a prefix", name, target)
			}
			if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".tmp") {
				t.Fatalf("temp %q for %q is not .<base>.<random>.tmp", name, target)
			}
			if !SafeTempName(target, name) {
				t.Fatalf("SafeTempName(%q, %q) = false", target, name)
			}
			if kitAliases(target, name) {
				t.Fatalf("temp %q would alias %q on the kit", name, target)
			}
		}
	}
}

func TestSafeTempName(t *testing.T) {
	for _, c := range []struct {
		target, temp string
		want         bool
	}{
		{"/media/fat/fogcast/launcher.json", ".launcher.json.123.tmp", true},
		{"launcher.json", "launcher.json.tmp", false},
		{"launcher.json", "launcher.json.bak", false},
		{"launcher.json", "LAUNCHER.JSON.tmp", false},
		{"launcher.json", "launcher.json", false},
		{"launcher.json", "launcher-123.tmp", false}, // not dot-prefixed
		{".hidden.json", ".hidden.json.1.tmp", false},
		{".hidden.json", "..hidden.json.1.tmp", true},
		{"", ".x.tmp", false},
	} {
		if got := SafeTempName(c.target, c.temp); got != c.want {
			t.Errorf("SafeTempName(%q, %q) = %v, want %v", c.target, c.temp, got, c.want)
		}
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func TestWriteFileTempDoesNotAliasAnyKitNeighbour(t *testing.T) {
	dir := t.TempDir()
	// A /media/fat/fogcast-like directory.
	for _, n := range []string{"agent.toml", "launcher.json", "catalog.json", "launch-map.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old "+n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"cache", "core-data", "launcher-cache", "mesh-content", "releases"} {
		if err := os.Mkdir(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	before := listDir(t, dir)
	for _, target := range []string{"launcher.json", "agent.toml", "launch-map.json", "catalog.json"} {
		var seen string
		path := filepath.Join(dir, target)
		beforeRename = func(temp string) {
			seen = filepath.Base(temp)
			// The live file must be untouched until the rename (the old
			// path+".tmp" write truncated it in place on the kit).
			if got, err := os.ReadFile(path); err != nil || string(got) != "old "+target {
				t.Errorf("%s changed before rename: %q, %v", target, got, err)
			}
			for _, entry := range listDir(t, dir) {
				if entry != seen && kitAliases(entry, seen) {
					t.Errorf("temp %q aliases existing %q under the kit exFAT rule", seen, entry)
				}
			}
		}
		if err := WriteFile(path, []byte("new "+target), 0o600); err != nil {
			t.Fatal(err)
		}
		beforeRename = nil
		if seen == "" || !SafeTempName(target, seen) {
			t.Fatalf("temp for %s = %q", target, seen)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "new "+target {
			t.Fatalf("%s = %q, %v", target, got, err)
		}
	}
	if after := listDir(t, dir); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("directory changed: before %v after %v", before, after)
	}
}

func TestWriteFileNeverTouchesSuffixSiblings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "launcher.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The old code wrote path+".tmp"; a directory there made it fail.
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			// The kit's /media/fat exFAT resolves launcher.json.tmp to
			// launcher.json, so the sibling cannot exist there (#317).
			t.Skip("filesystem aliases target-prefixed names")
		}
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".bak", []byte("bak"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("target = %q", got)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if fi, err := os.Stat(path + ".tmp"); err != nil || !fi.IsDir() {
		t.Fatalf(".tmp sibling disturbed: %v", err)
	}
	if got, _ := os.ReadFile(path + ".bak"); string(got) != "bak" {
		t.Fatalf(".bak sibling = %q", got)
	}
}

func TestWriteFileFailureRemovesOnlyItsTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "launcher.json")
	// A non-empty directory at the target makes the rename fail.
	if err := os.MkdirAll(filepath.Join(path, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.toml"), []byte("agent"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := listDir(t, dir)
	if err := WriteFile(path, []byte("new"), 0o600); err == nil {
		t.Fatal("expected rename failure")
	}
	if after := listDir(t, dir); strings.Join(after, ",") != strings.Join(before, ",") {
		t.Fatalf("failure left or removed files: before %v after %v", before, after)
	}
	if _, err := os.Stat(filepath.Join(path, "keep")); err != nil {
		t.Fatalf("target disturbed: %v", err)
	}
}
