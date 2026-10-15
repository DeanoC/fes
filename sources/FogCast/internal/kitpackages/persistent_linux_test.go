//go:build linux

package kitpackages

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentStorageRejectsTemporaryRootBeforeCreation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "library")
	if _, err := OpenPersistent(root); err == nil {
		t.Fatal("temporary production root accepted")
	}
	s := newStore(t)
	if err := s.CheckPersistent(); err == nil {
		t.Fatal("generic store claimed persistent")
	}
}

func TestPersistentMountRequiresExactWritableExt4P3Volume(t *testing.T) {
	valid := "42 33 179:3 / /run/fogcast/fesdata3 rw,relatime - ext4 /dev/mmcblk0p3 rw\n"
	if !persistentMount(valid, "/run/fogcast/fesdata3", "179:3") {
		t.Fatal("exact volume rejected")
	}
	for _, invalid := range []string{
		strings.ReplaceAll(valid, "ext4", "vfat"),
		strings.ReplaceAll(valid, "179:3", "179:1"),
		strings.ReplaceAll(valid, " / /run/", " /subtree /run/"),
		strings.ReplaceAll(valid, "rw,relatime", "ro,relatime"),
		strings.ReplaceAll(valid, "p3 rw", "p3 ro"),
		strings.ReplaceAll(valid, "fesdata3 ", "fesdata3/other "),
		"malformed mountinfo",
	} {
		if persistentMount(invalid, "/run/fogcast/fesdata3", "179:3") {
			t.Fatalf("invalid mount accepted: %s", invalid)
		}
	}
}
