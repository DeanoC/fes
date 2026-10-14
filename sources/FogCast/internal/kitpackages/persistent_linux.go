//go:build linux

package kitpackages

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast/internal/appliancedata"
	"golang.org/x/sys/unix"
)

// OpenPersistent positively verifies the existing FESDATA3 mount before
// creating any library directory. It neither mounts nor formats storage.
func OpenPersistent(root string) (*Store, error) {
	if err := checkPersistent(root); err != nil {
		return nil, err
	}
	s, err := New(root)
	if err != nil {
		return nil, err
	}
	s.persistent = true
	if err := s.CheckPersistent(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) CheckPersistent() error { return checkPersistent(s.root) }

func checkPersistent(root string) error {
	if root != DefaultRoot {
		return errors.New("kit packages: persistent library must use the FESDATA3 core-library root")
	}
	mount := appliancedata.DefaultMountPoint
	if err := realDirectories(mount, false); err != nil {
		return err
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs(mount, &filesystem); err != nil {
		return err
	}
	if filesystem.Type != unix.EXT4_SUPER_MAGIC || filesystem.Flags&unix.ST_RDONLY != 0 {
		return errors.New("kit packages: FESDATA3 must be a writable ext4 filesystem")
	}
	var mounted, device unix.Stat_t
	if err := unix.Stat(mount, &mounted); err != nil {
		return err
	}
	if err := unix.Stat(appliancedata.DefaultMMCDevice, &device); err != nil {
		return fmt.Errorf("kit packages: verify persistent device: %w", err)
	}
	if device.Mode&unix.S_IFMT != unix.S_IFBLK || uint64(mounted.Dev) != uint64(device.Rdev) {
		return errors.New("kit packages: FESDATA3 is not mounted from the expected p3 device")
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	deviceID := fmt.Sprintf("%d:%d", unix.Major(uint64(device.Rdev)), unix.Minor(uint64(device.Rdev)))
	if !persistentMount(string(data), mount, deviceID) {
		return errors.New("kit packages: FESDATA3 writable ext4 volume mount is missing")
	}
	// Reject a nested mount redirecting the library to another filesystem.
	if _, err := os.Lstat(root); err == nil {
		if err := realDirectories(root, false); err != nil {
			return err
		}
		var library unix.Stat_t
		if err := unix.Stat(root, &library); err != nil {
			return err
		}
		if library.Dev != mounted.Dev {
			return errors.New("kit packages: library is outside FESDATA3")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
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

func unescapeMount(value string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(value)
}
