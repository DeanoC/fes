package ext4image

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// e2fsprogs finds an e2fsprogs tool, which sbin directories may hold off PATH.
// The test skips when e2fsprogs is not installed.
func e2fsprogs(t *testing.T, name string) string {
	t.Helper()
	if tool, err := exec.LookPath(name); err == nil {
		return tool
	}
	for _, dir := range []string{"/usr/sbin", "/sbin"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return filepath.Join(dir, name)
		}
	}
	t.Skip(name + " is not installed")
	return ""
}

// mkfs builds an ext4 image from root with mke2fs, the tool the image recipe
// uses. index rebuilds every directory with a hashed index, as e2fsck -D does.
func mkfs(t *testing.T, root string, index bool, options ...string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rootfs.img")
	args := append(append([]string{"-q", "-F", "-d", root}, options...), path, "32M")
	if output, err := exec.Command(e2fsprogs(t, "mkfs.ext4"), args...).CombinedOutput(); err != nil {
		t.Fatalf("mkfs.ext4 %v: %v\n%s", options, err, output)
	}
	if index {
		// Exit status 1 reports that e2fsck changed the filesystem.
		output, err := exec.Command(e2fsprogs(t, "e2fsck"), "-fyD", path).CombinedOutput()
		var exit *exec.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
			t.Fatalf("e2fsck -D: %v\n%s", err, output)
		}
	}
	image, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { image.Close() })
	return image
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// tree holds a small base record, a large file, and a directory big enough to
// need many blocks, interleaved with file data, an index and an extent tree.
func tree(t *testing.T) (string, []byte, []byte) {
	t.Helper()
	root := t.TempDir()
	base := []byte(`{"board":"de10-nano","requires":{"menu.fes.video.menu-display":"1.0"}}` + "\n")
	write(t, filepath.Join(root, "etc/fes/software-base.json"), base)
	large := make([]byte, 300<<10+123)
	for i := range large {
		large[i] = byte(sha256.Sum256([]byte(fmt.Sprint(i)))[0])
	}
	write(t, filepath.Join(root, "usr/share/fes/large.bin"), large)
	for i := 0; i < 1500; i++ {
		write(t, filepath.Join(root, "etc/many", fmt.Sprintf("entry-with-a-long-name-%04d", i)), []byte(fmt.Sprint(i)))
	}
	if err := os.Symlink("/etc/fes/software-base.json", filepath.Join(root, "etc/link")); err != nil {
		t.Fatal(err)
	}
	return root, base, large
}

func TestReadFileAcrossImageLayouts(t *testing.T) {
	root, base, large := tree(t)
	for _, layout := range []struct {
		name    string
		index   bool
		options []string
	}{
		{"defaults", false, nil},
		{"hashed-directories", true, nil},
		{"recipe-4k", false, []string{"-b", "4096", "-O", "^metadata_csum_seed,^orphan_file"}},
		{"32-bit-descriptors", true, []string{"-b", "1024", "-O", "^64bit,^metadata_csum,^metadata_csum_seed,^orphan_file"}},
		{"small-inodes-2k", false, []string{"-b", "2048", "-I", "128"}},
	} {
		t.Run(layout.name, func(t *testing.T) {
			image := mkfs(t, root, layout.index, layout.options...)
			got, err := ReadFile(image, "/etc/fes/software-base.json", 64<<10)
			if err != nil || !bytes.Equal(got, base) {
				t.Fatalf("software base: %q %v", got, err)
			}
			if got, err = ReadFile(image, "/usr/share/fes/large.bin", 1<<20); err != nil || !bytes.Equal(got, large) {
				t.Fatalf("large file differs: %d bytes, %v", len(got), err)
			}
			for _, i := range []int{0, 777, 1499} {
				name := fmt.Sprintf("/etc/many/entry-with-a-long-name-%04d", i)
				if got, err = ReadFile(image, name, 16); err != nil || string(got) != fmt.Sprint(i) {
					t.Fatalf("%s: %q %v", name, got, err)
				}
			}
		})
	}
}

func TestReadFileRefusals(t *testing.T) {
	root, _, _ := tree(t)
	image := mkfs(t, root, false)
	if _, err := ReadFile(image, "/etc/fes/missing.json", 64<<10); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("missing file", err)
	}
	for _, name := range []string{"/etc/fes", "/etc/link", "etc/fes/software-base.json", "/etc/../etc/fes/software-base.json", "/etc//fes/software-base.json", "/etc/fes/software-base.json/x"} {
		if _, err := ReadFile(image, name, 64<<10); err == nil {
			t.Fatalf("%s read as a regular file", name)
		}
	}
	if _, err := ReadFile(image, "/usr/share/fes/large.bin", 64<<10); err == nil {
		t.Fatal("file over the limit read")
	}
	if _, err := ReadFile(bytes.NewReader(make([]byte, 1<<20)), "/etc/fes/software-base.json", 64<<10); err == nil {
		t.Fatal("non-ext4 image read")
	}
}
