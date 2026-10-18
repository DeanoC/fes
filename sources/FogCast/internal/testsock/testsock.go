// Package testsock gives tests a Unix socket path that fits the kernel's
// sun_path limit (108 bytes on Linux, 104 on macOS) whatever TMPDIR is.
// t.TempDir embeds the test name, so a socket under it overflows on any
// machine with a long TMPDIR, and Listen fails with "invalid argument".
package testsock

import (
	"os"
	"path/filepath"
	"testing"
)

// maxPath leaves room under the smallest sun_path limit FES builds for.
const maxPath = 100

// Path returns name inside a fresh private directory that is removed when
// the test ends. It uses os.TempDir when the result fits, else /tmp.
func Path(t testing.TB, name string) string {
	t.Helper()
	base := os.TempDir()
	// MkdirTemp appends up to 10 digits to the pattern.
	if len(filepath.Join(base, "fs-0123456789", name)) > maxPath {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "fs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, name)
	if len(path) > maxPath {
		t.Fatalf("unix socket path is %d bytes: %s", len(path), path)
	}
	return path
}
