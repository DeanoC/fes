//go:build linux

package bootlinux

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

type rootRecorder struct {
	events      []string
	failMount   bool
	failPivot   bool
	failUnmount bool
}

func (r *rootRecorder) Mount(source, target, kind string, flags uintptr, data string) error {
	if flags&unix.MS_MOVE != 0 {
		r.events = append(r.events, "move:"+source+":"+target)
	} else if kind == "ext4" {
		if flags&unix.MS_RDONLY == 0 || data != "noload" {
			return errors.New("candidate was writable or replayed journal")
		}
		r.events = append(r.events, "mount:"+target)
	} else {
		r.events = append(r.events, "private")
	}
	if r.failMount && kind == "ext4" {
		return errors.New("bad filesystem")
	}
	return nil
}
func (r *rootRecorder) Unmount(path string, flags int) error {
	r.events = append(r.events, "unmount:"+path)
	if r.failUnmount {
		return errors.New("busy")
	}
	return nil
}
func (r *rootRecorder) Chdir(path string) error {
	r.events = append(r.events, "chdir:"+path)
	return nil
}
func (r *rootRecorder) PivotRoot(root, old string) error {
	r.events = append(r.events, "pivot:"+root+":"+old)
	if r.failPivot {
		return errors.New("pivot failed")
	}
	return nil
}
func (r *rootRecorder) Exec(path string, argv, env []string) error {
	r.events = append(r.events, "exec:"+path)
	return errors.New("test exec returned")
}

func candidateFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range []string{".fes-bootstrap", "media/fat", "proc", "sys", "dev", "sbin"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "sbin/init"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCleanupRetainsBusyLoopUntilUnmountSucceeds(t *testing.T) {
	ops := &rootRecorder{failUnmount: true}
	closed := false
	r, err := prepareRoot(candidateFixture(t), "/dev/loop0", func() error { closed = true; return nil }, ops)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err == nil || closed {
		t.Fatal("busy mount lost its loop descriptor")
	}
	ops.failUnmount = false
	if err = r.Close(); err != nil || !closed {
		t.Fatalf("loop cleanup failed: %v", err)
	}
}

func TestPrepareRootRejectsSymlinkAndEmptyImageWithoutDeviceAccess(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "image")
	if err := os.WriteFile(image, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRoot(image, dir); err == nil || !strings.Contains(err.Error(), "nonempty regular") {
		t.Fatalf("empty image reached device access: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(image, link); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRoot(link, dir); !errors.Is(err, unix.ELOOP) {
		t.Fatalf("image symlink followed: %v", err)
	}
	if _, err := PrepareRoot(image, link); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("mountpoint symlink followed: %v", err)
	}
}

func TestCandidateInitSymlinkIsConfinedToImage(t *testing.T) {
	root := candidateFixture(t)
	init := filepath.Join(root, "sbin/init")
	if err := os.Rename(init, filepath.Join(root, "sbin/busybox")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("busybox", init); err != nil {
		t.Fatal(err)
	}
	if err := validateCandidate(root); err != nil {
		t.Fatalf("valid BusyBox symlink rejected: %v", err)
	}
	if err := os.Remove(init); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/bin/sh", init); err != nil {
		t.Fatal(err)
	}
	if err := validateCandidate(root); err == nil {
		t.Fatal("candidate init escaped image")
	}
}

// These syscall fakes prevent host mounts; assertions check recovery boundaries,
// read-only policy and the actual ordered syscall arguments of our root switch.
func TestPrepareRejectsMissingInitAndSymlinkMountTargets(t *testing.T) {
	for _, bad := range []string{"init", "fat", "old-root"} {
		t.Run(bad, func(t *testing.T) {
			root := candidateFixture(t)
			path := "sbin/init"
			if bad == "fat" {
				path = "media/fat"
			}
			if bad == "old-root" {
				path = ".fes-bootstrap"
			}
			if err := os.Remove(filepath.Join(root, path)); err != nil {
				t.Fatal(err)
			}
			if bad != "init" {
				if err := os.Symlink(t.TempDir(), filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
			}
			ops := &rootRecorder{}
			closed := false
			_, err := prepareRoot(root, "/dev/loop0", func() error { closed = true; return nil }, ops)
			if err == nil {
				t.Fatal("unsafe candidate accepted")
			}
			if !closed || !strings.Contains(strings.Join(ops.events, ","), "unmount:") {
				t.Fatal("rejected candidate leaked mount or loop")
			}
		})
	}
}

func TestMountFailureReleasesLoopWithoutAttemptingPivot(t *testing.T) {
	ops := &rootRecorder{failMount: true}
	closed := false
	_, err := prepareRoot(candidateFixture(t), "/dev/loop0", func() error { closed = true; return nil }, ops)
	if err == nil || !closed {
		t.Fatal("mount failure retained loop")
	}
	if strings.Contains(strings.Join(ops.events, ","), "pivot:") {
		t.Fatal("failed mount pivoted")
	}
}

func TestPivotKeepsBootstrapAndTransfersFATBeforeExecutingInit(t *testing.T) {
	root := candidateFixture(t)
	ops := &rootRecorder{}
	r, err := prepareRoot(root, "/dev/loop0", func() error { return nil }, ops)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.PivotAndExec("/sbin/init", []string{"/sbin/init"}, nil); err == nil {
		t.Fatal("fake exec should return error")
	}
	got := strings.Join(ops.events, "\n")
	move := strings.Index(got, "move:/media/fat:"+root+"/media/fat")
	pivot := strings.Index(got, "pivot:"+root+":"+root+"/.fes-bootstrap")
	exec := strings.Index(got, "exec:/sbin/init")
	if move < 0 || pivot <= move || exec <= pivot {
		t.Fatalf("unsafe switch order:\n%s", got)
	}
	if strings.Contains(got, "unmount:") {
		t.Fatal("bootstrap removed beneath independent guard")
	}
	if err = r.Close(); err == nil {
		t.Fatal("cleanup allowed after pivot boundary")
	}
}

func TestFailedPivotNeverExecutesCandidate(t *testing.T) {
	ops := &rootRecorder{failPivot: true}
	r, err := prepareRoot(candidateFixture(t), "/dev/loop0", func() error { return nil }, ops)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.PivotAndExec("/sbin/init", []string{"/sbin/init"}, nil); err == nil {
		t.Fatal("pivot failure ignored")
	}
	if strings.Contains(strings.Join(ops.events, ","), "exec:") {
		t.Fatal("candidate executed without pivot")
	}
}
