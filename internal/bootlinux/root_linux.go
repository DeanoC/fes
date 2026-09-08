//go:build linux

package bootlinux

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const BootstrapMount = "/.fes-bootstrap"

// Root holds an independently attached read-only loop image. PrepareRoot must
// finish before arming a trial: failures at this point permit same-boot fallback.
// After PivotAndExec begins, failures require reboot rather than another pivot.
type Root struct {
	path       string
	closeLoop  func() error
	ops        rootOperations
	transition bool
	closed     bool
}

type rootOperations interface {
	Mount(string, string, string, uintptr, string) error
	Unmount(string, int) error
	Chdir(string) error
	PivotRoot(string, string) error
	Exec(string, []string, []string) error
}
type linuxRootOperations struct{}

func (linuxRootOperations) Mount(source, target, kind string, flags uintptr, data string) error {
	return unix.Mount(source, target, kind, flags, data)
}
func (linuxRootOperations) Unmount(path string, flags int) error { return unix.Unmount(path, flags) }
func (linuxRootOperations) Chdir(path string) error              { return unix.Chdir(path) }
func (linuxRootOperations) PivotRoot(root, old string) error     { return unix.PivotRoot(root, old) }
func (linuxRootOperations) Exec(path string, argv, env []string) error {
	return unix.Exec(path, argv, env)
}

// PrepareRoot attaches an already hash-verified regular image to a fresh loop
// device and mounts ext4 read-only without journal replay. The caller must have
// mounted writable /dev, proc, sysfs and /run, and created mountpoint beneath
// /run. The loop driver and /dev/loop-control must be available. No symlink is
// followed in the image path, mountpoint or candidate mount target directories.
// Images must precontain /.fes-bootstrap, /media/fat, /dev, /proc and /sys.
func PrepareRoot(imagePath, mountpoint string) (*Root, error) {
	if !filepath.IsAbs(imagePath) || !filepath.IsAbs(mountpoint) || filepath.Clean(mountpoint) == "/" {
		return nil, errors.New("image and non-root mountpoint must be absolute")
	}
	if err := realDirectory(mountpoint); err != nil {
		return nil, err
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, imagePath, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, fmt.Errorf("open immutable image: %w", err)
	}
	image := os.NewFile(uintptr(fd), imagePath)
	defer image.Close()
	info, err := image.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, errors.New("image must be a nonempty regular file")
	}
	loop, path, err := attachReadOnlyLoop(image)
	if err != nil {
		return nil, err
	}
	return prepareRoot(mountpoint, path, loop.Close, linuxRootOperations{})
}

func prepareRoot(mountpoint, loopPath string, closeLoop func() error, ops rootOperations) (*Root, error) {
	if err := ops.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return nil, errors.Join(fmt.Errorf("make bootstrap mounts private: %w", err), closeLoop())
	}
	if err := ops.Mount(loopPath, mountpoint, "ext4", unix.MS_RDONLY|unix.MS_NOATIME, "noload"); err != nil {
		return nil, errors.Join(fmt.Errorf("mount candidate: %w", err), closeLoop())
	}
	r := &Root{path: mountpoint, closeLoop: closeLoop, ops: ops}
	if err := validateCandidate(mountpoint); err != nil {
		return nil, errors.Join(err, r.Close())
	}
	return r, nil
}

func validateCandidate(root string) error {
	for _, path := range []string{BootstrapMount, "/media/fat", "/dev", "/proc", "/sys"} {
		if err := realDirectory(filepath.Join(root, path)); err != nil {
			return fmt.Errorf("candidate mount target %s: %w", path, err)
		}
	}
	// os.Root resolves relative init symlinks within this image, including the
	// standard BusyBox/merged-usr layout, without allowing an escape to host files.
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	info, err := r.Stat("sbin/init")
	if err != nil {
		return fmt.Errorf("candidate init: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("candidate init is not executable")
	}
	return nil
}

func realDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("directory must be absolute")
	}
	current := "/"
	for _, part := range strings.Split(filepath.Clean(path), "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is not a real directory", current)
		}
	}
	return nil
}

// Close unmounts and releases a prepared candidate before root switching.
// A live mount retains the loop descriptor if unmount fails. A successful pivot
// deliberately keeps the old bootstrap mount for the independent watchdog.
func (r *Root) Close() error {
	if r.closed {
		return nil
	}
	if r.transition {
		return errors.New("root transition has begun; reboot required")
	}
	if err := r.ops.Unmount(r.path, 0); err != nil {
		return err
	}
	r.closed = true
	return r.closeLoop()
}

// PivotAndExec transfers the existing pseudo filesystems and writable FAT view,
// preserves the old bootstrap at /.fes-bootstrap, and executes candidate init.
// The guard's private mount namespace keeps its original FAT and device views.
// /run is deliberately not moved: it contains the new-root mount itself and
// candidate init establishes its own volatile /run. This function is for PID 1;
// the caller must not attempt normal operation after it returns an error.
func (r *Root) PivotAndExec(initPath string, argv, env []string) error {
	if r.closed || r.transition {
		return errors.New("root is closed or already transitioning")
	}
	if initPath != "/sbin/init" {
		return errors.New("only the validated /sbin/init may execute")
	}
	if len(argv) == 0 {
		argv = []string{initPath}
	}
	r.transition = true
	for _, path := range []string{"/media/fat", "/dev", "/proc", "/sys"} {
		if err := r.ops.Mount(path, filepath.Join(r.path, path), "", unix.MS_MOVE, ""); err != nil {
			return fmt.Errorf("move %s into candidate: %w", path, err)
		}
	}
	if err := r.ops.PivotRoot(r.path, filepath.Join(r.path, BootstrapMount)); err != nil {
		return fmt.Errorf("pivot candidate: %w", err)
	}
	if err := r.ops.Chdir("/"); err != nil {
		return err
	}
	if err := r.ops.Exec(initPath, argv, env); err != nil {
		return fmt.Errorf("execute candidate init: %w", err)
	}
	return errors.New("candidate exec unexpectedly returned")
}

func attachReadOnlyLoop(image *os.File) (*os.File, string, error) {
	controlFD, err := unix.Open("/dev/loop-control", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", err
	}
	defer unix.Close(controlFD)
	// LOOP_CTL_GET_FREE reserves nothing; retry an allocation race, never steal
	// or clear a busy loop (in particular the kernel's bootstrap loop8).
	for attempt := 0; attempt < 8; attempt++ {
		number, err := unix.IoctlRetInt(controlFD, unix.LOOP_CTL_GET_FREE)
		if err != nil {
			return nil, "", err
		}
		path := fmt.Sprintf("/dev/loop%d", number)
		if err = ensureLoopNode(number, path); err != nil {
			return nil, "", err
		}
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, "", err
		}
		loop := os.NewFile(uintptr(fd), path)
		var stat unix.Stat_t
		if err = unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFBLK {
			_ = loop.Close()
			return nil, "", errors.New("loop path is not a block device")
		}
		configuration := unix.LoopConfig{Fd: uint32(image.Fd()), Info: unix.LoopInfo64{Flags: unix.LO_FLAGS_READ_ONLY | unix.LO_FLAGS_AUTOCLEAR}}
		if err = unix.IoctlLoopConfigure(fd, &configuration); err == nil {
			return loop, path, nil
		}
		_ = loop.Close()
		if !errors.Is(err, unix.EBUSY) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("no free loop device after bounded retries")
}

func ensureLoopNode(number int, path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeDevice == 0 || info.Mode()&os.ModeCharDevice != 0 || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("loop path is not a block device")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// GET_FREE may create a loop after bootstrap mdev ran. Derive its actual
	// device numbers from sysfs: max_part can change the loop minor stride.
	data, err := os.ReadFile(fmt.Sprintf("/sys/block/loop%d/dev", number))
	if err != nil {
		return err
	}
	parts := strings.Split(strings.TrimSpace(string(data)), ":")
	if len(parts) != 2 {
		return errors.New("invalid loop sysfs device number")
	}
	major, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return err
	}
	minor, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return err
	}
	err = unix.Mknod(path, unix.S_IFBLK|0600, int(unix.Mkdev(uint32(major), uint32(minor))))
	if errors.Is(err, unix.EEXIST) {
		return ensureLoopNode(number, path)
	}
	return err
}
