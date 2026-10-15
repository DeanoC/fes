//go:build linux

package software

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// OpenPersistent verifies an already-mounted writable FESDATA3 volume. It never
// mounts, formats, or migrates storage and cannot fall back to tmpfs or FAT.
func OpenPersistent(c Compatibility) (*Store, error) {
	if e := checkPersistent(DefaultRoot); e != nil {
		return nil, e
	}
	s, e := New(DefaultRoot, c)
	if e != nil {
		return nil, e
	}
	s.persistent = true
	return s, s.check()
}
func checkPersistent(root string) error {
	if root != DefaultRoot {
		return errors.New("invalid persistent software root")
	}
	mount := filepath.Dir(root)
	if e := realDirectories(mount, false); e != nil {
		return e
	}
	var filesystem unix.Statfs_t
	if e := unix.Statfs(mount, &filesystem); e != nil {
		return e
	}
	if filesystem.Type != unix.EXT4_SUPER_MAGIC || filesystem.Flags&unix.ST_RDONLY != 0 {
		return errors.New("software store requires writable FESDATA3 ext4")
	}
	device := "/dev/disk/by-label/FESDATA3"
	if _, e := os.Stat(device); errors.Is(e, os.ErrNotExist) {
		device = "/dev/mmcblk0p3"
		f, e := os.Open(device)
		if e != nil {
			return e
		}
		b := make([]byte, 136)
		_, e = f.ReadAt(b, 1024)
		f.Close()
		if e != nil {
			return e
		}
		if b[56] != 0x53 || b[57] != 0xef || strings.TrimRight(string(b[120:136]), "\x00") != "FESDATA3" {
			return errors.New("FESDATA3 device label missing")
		}
	} else if e != nil {
		return e
	}
	var mounted, dev unix.Stat_t
	if e := unix.Stat(mount, &mounted); e != nil {
		return e
	}
	if e := unix.Stat(device, &dev); e != nil {
		return e
	}
	if dev.Mode&unix.S_IFMT != unix.S_IFBLK || mounted.Dev != dev.Rdev {
		return errors.New("wrong FESDATA3 device")
	}
	data, e := os.ReadFile("/proc/self/mountinfo")
	if e != nil {
		return e
	}
	deviceID := fmt.Sprintf("%d:%d", unix.Major(uint64(dev.Rdev)), unix.Minor(uint64(dev.Rdev)))
	if !persistentMount(string(data), mount, deviceID) {
		return errors.New("FESDATA3 full writable volume mount missing")
	}
	for _, p := range []string{root, filepath.Join(root, "releases"), filepath.Join(root, "incoming")} {
		info, e := os.Lstat(p)
		if errors.Is(e, os.ErrNotExist) {
			continue
		} else if e != nil {
			return e
		}
		if e = realDirectories(p, false); e != nil {
			return e
		}
		if e = requirePrivateDir(info); e != nil {
			return e
		}
		var st unix.Stat_t
		if e = unix.Stat(p, &st); e != nil {
			return e
		}
		if st.Dev != mounted.Dev {
			return errors.New("software store redirected outside FESDATA3")
		}
	}
	lockPath := filepath.Join(root, ".lock")
	if info, e := os.Lstat(lockPath); e == nil {
		if e = requirePrivateLock(info); e != nil {
			return e
		}
		var st unix.Stat_t
		if e = unix.Stat(lockPath, &st); e != nil {
			return e
		}
		if st.Dev != mounted.Dev {
			return errors.New("software store redirected outside FESDATA3")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return nil
}
func persistentMount(data, mount, device string) bool {
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		sep := -1
		for i := 6; i < len(f); i++ {
			if f[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+3 >= len(f) {
			continue
		}
		if f[2] == device && f[3] == "/" && f[4] == mount && hasOption(f[5], "rw") && f[sep+1] == "ext4" && hasOption(f[sep+3], "rw") {
			return true
		}
	}
	return false
}
func hasOption(s, v string) bool {
	for _, p := range strings.Split(s, ",") {
		if p == v {
			return true
		}
	}
	return false
}
