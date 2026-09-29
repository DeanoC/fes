package misterruntime

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type readError struct{ err error }

func (r readError) Read([]byte) (int, error) { return 0, r.err }
func TestWriteAtomicDevelopmentRBFReplacesOnlyACompleteFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "development", "core.rbf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte("old-rbf")
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeAtomicDevelopmentRBF(path, 8, bytes.NewReader([]byte("short"))); err == nil {
		t.Fatal("short development RBF was accepted")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, old) {
		t.Fatalf("installed file after short body = %q, %v", got, err)
	}
	if err := leftoverDevelopmentTemp(path); err != nil {
		t.Fatalf("temporary development RBF remains after failure: %v", err)
	}

	want := []byte("complete-development-rbf")
	if err := os.WriteFile(path+".new", []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicDevelopmentRBF(path, int64(len(want)), bytes.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("installed file = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("installed mode = %#o, want 0600", mode)
	}
	if err := leftoverDevelopmentTemp(path); err != nil {
		t.Fatalf("temporary development RBF remains after success: %v", err)
	}
	// A path+".new" sibling is never used as the temporary (it would alias
	// path on the kit's exFAT, #317), so a stale one is left untouched.
	if got, err := os.ReadFile(path + ".new"); err != nil || string(got) != "stale" {
		t.Fatalf("stale .new sibling = %q, %v", got, err)
	}
}

// leftoverDevelopmentTemp reports a dot-prefixed temporary left beside path.
func leftoverDevelopmentTemp(path string) error {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			return fmt.Errorf("found %s", entry.Name())
		}
	}
	return nil
}

func TestWriteAtomicDevelopmentRBFTempIsNotPrefixedByTarget(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "core.rbf")
	// A directory squatting on the old path+".new" name made the old code fail.
	if err := os.Mkdir(path+".new", 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("rbf")
	if err := writeAtomicDevelopmentRBF(path, int64(len(want)), bytes.NewReader(want)); err != nil {
		t.Fatalf("development RBF used a core.rbf-prefixed temporary: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("installed file = %q, %v", got, err)
	}
	if err := leftoverDevelopmentTemp(path); err != nil {
		t.Fatal(err)
	}
}

func TestWriteAtomicDevelopmentRBFRejectsTrailingByteWithoutReplacingInstalledFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "development", "core.rbf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte("old-rbf")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeAtomicDevelopmentRBF(path, 3, bytes.NewReader([]byte("rbf!"))); err == nil {
		t.Error("development RBF with a trailing byte was accepted")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, old) {
		t.Errorf("installed file after trailing byte = %q, %v", got, err)
	}
	if err := leftoverDevelopmentTemp(path); err != nil {
		t.Errorf("temporary development RBF remains after trailing byte: %v", err)
	}
}

func TestWriteAtomicDevelopmentRBFRejectsTrailingReadErrorWithoutReplacingInstalledFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "development", "core.rbf")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte("old-rbf")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}

	content := io.MultiReader(bytes.NewReader([]byte("rbf")), readError{err: errors.New("read failed")})
	if err := writeAtomicDevelopmentRBF(path, 3, content); err == nil {
		t.Error("development RBF trailing read error was ignored")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, old) {
		t.Errorf("installed file after trailing read error = %q, %v", got, err)
	}
	if err := leftoverDevelopmentTemp(path); err != nil {
		t.Errorf("temporary development RBF remains after trailing read error: %v", err)
	}
}
