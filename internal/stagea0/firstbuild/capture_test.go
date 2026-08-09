package firstbuild

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultRequestUsesPreparedNetworkOffImage(t *testing.T) {
	req := DefaultRequest()
	if req.ImageRef != ExpectedContainerReference {
		t.Fatalf("default image = %q, want %q", req.ImageRef, ExpectedContainerReference)
	}
}

func TestArchiveMembersRejectTraversalAndRequireLockedRoot(t *testing.T) {
	if root, err := archiveRoot([]string{
		ExpectedToolchainArchiveRoot + "/",
		ExpectedToolchainArchiveRoot + "/bin/arm-none-linux-gnueabihf-gcc",
	}); err != nil || root != ExpectedToolchainArchiveRoot {
		t.Fatalf("valid archive root = %q, %v", root, err)
	}
	for _, members := range [][]string{
		{"/escape"},
		{"../escape"},
		{"other-root/bin/compiler"},
		{ExpectedToolchainArchiveRoot + "/../escape"},
	} {
		if _, err := archiveRoot(members); !hasCode(err, CodeToolchainDrift) {
			t.Fatalf("archive members %#v error = %v, want %s", members, err, CodeToolchainDrift)
		}
	}
}

func TestResolveArchiveLinksUsesRelativeSymlinkAndFullHardlinkRules(t *testing.T) {
	root := ExpectedToolchainArchiveRoot
	name := root + "/arm-none-linux-gnueabihf/libc/usr/lib/libthread_db.so"
	if got, err := resolveArchiveLink(name, "../../lib/libthread_db.so.1", root, false); err != nil || got != root+"/arm-none-linux-gnueabihf/libc/lib/libthread_db.so.1" {
		t.Fatalf("relative symlink resolution = %q, %v", got, err)
	}
	hardTarget := root + "/bin/ld"
	if got, err := resolveArchiveLink(root+"/bin/ld.bfd", hardTarget, root, true); err != nil || got != hardTarget {
		t.Fatalf("full hardlink resolution = %q, %v", got, err)
	}
	for _, test := range []struct {
		name, target string
		hard         bool
	}{
		{name: root + "/bin/escape", target: "../../outside"},
		{name: root + "/bin/escape", target: "/outside"},
		{name: root + "/bin/escape", target: root + "/../outside", hard: true},
	} {
		if _, err := resolveArchiveLink(test.name, test.target, root, test.hard); !hasCode(err, CodeToolchainDrift) {
			t.Fatalf("unsafe link %#v error = %v, want %s", test, err, CodeToolchainDrift)
		}
	}
}

func TestOutputAndMaterializedTreeGuards(t *testing.T) {
	root := t.TempDir()
	if err := ensureOutputAbsent(filepath.Join(root, "missing")); err != nil {
		t.Fatalf("missing output rejected: %v", err)
	}
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensureOutputAbsent(existing); !hasCode(err, CodeOutputInvalid) {
		t.Fatalf("existing output error = %v, want %s", err, CodeOutputInvalid)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(existing, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureOutputAbsent(link); !hasCode(err, CodeOutputInvalid) {
		t.Fatalf("symlink output error = %v, want %s", err, CodeOutputInvalid)
	}
	before := map[string]string{"Makefile": "file:a", "bin": "dir"}
	after := map[string]string{"Makefile": "file:a", "bin": "dir", "bin/MiSTer": "file:b"}
	if err := validateUnchangedOutsideBin(before, after); err != nil {
		t.Fatalf("bin-only change rejected: %v", err)
	}
	after["Makefile"] = "file:changed"
	if err := validateUnchangedOutsideBin(before, after); !hasCode(err, CodeOutputInvalid) {
		t.Fatalf("outside-bin change error = %v, want %s", err, CodeOutputInvalid)
	}
}
