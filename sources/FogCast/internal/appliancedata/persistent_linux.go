//go:build linux

package appliancedata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/appliance/blockprobe"
	"golang.org/x/sys/unix"
)

// CheckPersistent positively verifies that path is durable FESDATA3 storage:
// DefaultMountPoint is a writable ext4 mount of the block device Prepare
// selects, including a labelled USB partition, and path is that mount point or
// lies beneath it on the same filesystem with no symlink on the way. path need
// not exist yet, so a caller can check before creating anything. It neither
// mounts nor formats storage.
func CheckPersistent(path string) error {
	mount := DefaultMountPoint
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("appliance data: persistent path must be a clean absolute path")
	}
	if path != mount && !strings.HasPrefix(path, mount+string(filepath.Separator)) {
		return errors.New("appliance data: persistent path is outside FESDATA3")
	}
	if err := realDirectory(mount); err != nil {
		return err
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs(mount, &filesystem); err != nil {
		return err
	}
	if filesystem.Type != unix.EXT4_SUPER_MAGIC || filesystem.Flags&unix.ST_RDONLY != 0 {
		return errors.New("appliance data: FESDATA3 must be a writable ext4 filesystem")
	}
	devicePath, ok := SelectedDevice(Config{})
	if !ok {
		return errors.New("appliance data: FESDATA3 block device is absent")
	}
	if err := blockprobe.RejectsDevice("/sys", devicePath); err != nil {
		return fmt.Errorf("appliance data: %w", err)
	}
	var mounted, device unix.Stat_t
	if err := unix.Stat(mount, &mounted); err != nil {
		return err
	}
	// Stat follows /dev/disk/by-label/FESDATA3. The mounted device number is
	// compared with that labelled partition's block device.
	if err := unix.Stat(devicePath, &device); err != nil {
		return fmt.Errorf("appliance data: verify persistent device: %w", err)
	}
	if device.Mode&unix.S_IFMT != unix.S_IFBLK || uint64(mounted.Dev) != uint64(device.Rdev) {
		return errors.New("appliance data: FESDATA3 is not mounted from the expected device")
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	deviceID := fmt.Sprintf("%d:%d", unix.Major(uint64(device.Rdev)), unix.Minor(uint64(device.Rdev)))
	if !persistentMount(string(data), mount, deviceID) {
		return errors.New("appliance data: FESDATA3 writable ext4 volume mount is missing")
	}
	// Reject a symlink or a nested mount redirecting path to another
	// filesystem. Components that do not exist yet are created later by the
	// caller, beneath a verified directory.
	current := mount
	for _, part := range strings.Split(strings.TrimPrefix(path, mount), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if _, err := os.Lstat(current); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return err
		}
		if err := realDirectory(current); err != nil {
			return err
		}
		var inside unix.Stat_t
		if err := unix.Stat(current, &inside); err != nil {
			return err
		}
		if inside.Dev != mounted.Dev {
			return errors.New("appliance data: persistent path is outside FESDATA3")
		}
	}
	return nil
}

func realDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("appliance data: path %s must be a real directory", path)
	}
	return nil
}

func persistentMount(data, mount, deviceID string) bool {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		separator := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				separator = i
				break
			}
		}
		if separator < 0 || separator+3 >= len(fields) {
			continue
		}
		if fields[2] == deviceID && fields[3] == "/" && filepath.Clean(unescapeMount(fields[4])) == mount && hasOption(fields[5], "rw") && fields[separator+1] == "ext4" && hasOption(fields[separator+3], "rw") {
			return true
		}
	}
	return false
}

func hasOption(options, expected string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == expected {
			return true
		}
	}
	return false
}

