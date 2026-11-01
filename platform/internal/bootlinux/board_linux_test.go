//go:build linux

package bootlinux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapBoardRequiresExactImageAndCPU(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fes-board")
	for _, tc := range []struct {
		raw, arch, want string
	}{
		{"de10-nano\n", "arm", "de10-nano"},
		{"de25-nano\n", "arm64", "de25-nano"},
		{"de10-nano\n", "arm64", ""},
		{"de25-nano\n", "arm", ""},
		{"de25-nano\n", "amd64", ""},
		{"de25-nano", "arm64", ""},
		{"de25-nano\n\n", "arm64", ""},
		{"other\n", "arm64", ""},
		{strings.Repeat("x", 66), "arm64", ""},
	} {
		if err := os.WriteFile(path, []byte(tc.raw), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := ReadBootBoard(path, tc.arch)
		if got != tc.want || (err == nil) != (tc.want != "") {
			t.Fatalf("%q/%s: got %q, %v", tc.raw, tc.arch, got, err)
		}
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{link, filepath.Dir(path), path + ".missing"} {
		if _, err := ReadBootBoard(bad, "arm64"); err == nil {
			t.Fatalf("accepted invalid image board file %s", bad)
		}
	}
}
