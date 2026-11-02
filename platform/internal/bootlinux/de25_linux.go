//go:build linux

package bootlinux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

const de25FAT = "/dev/mmcblk0p1"

// PrepareDe25BootVolume implements Terasic's direct ext4 partition bootstrap.
// The boot script still uses root=/dev/mmcblk0p2 rw rootwait. Make that stable
// root read-only, then mount p1 for the shared release/configuration paths.
// It does not access FPGA, watchdog or reset registers.
func PrepareDe25BootVolume() error {
	paths := []string{"/sys/firmware/devicetree/base/model", "/sys/firmware/devicetree/base/compatible",
		"/proc/self/mountinfo", "/sys/class/block/mmcblk0p2/dev"}
	data := make([][]byte, len(paths))
	for i, path := range paths {
		var err error
		if data[i], err = os.ReadFile(path); err != nil {
			return fmt.Errorf("read DE25 boot identity %s: %w", path, err)
		}
	}
	readOnly, err := validateDe25BootVolume(runtime.GOARCH, data[0], data[1], data[2], data[3])
	if err != nil {
		return err
	}
	if err := realDirectory("/media/fat"); err != nil {
		return err
	}
	var device unix.Stat_t
	if err := unix.Lstat(de25FAT, &device); err != nil {
		return err
	}
	if device.Mode&unix.S_IFMT != unix.S_IFBLK {
		return errors.New("DE25 FAT must be a real block device")
	}
	dev, err := os.ReadFile("/sys/class/block/mmcblk0p1/dev")
	if err != nil {
		return err
	}
	if string(dev) != fmt.Sprintf("%d:%d\n", unix.Major(uint64(device.Rdev)), unix.Minor(uint64(device.Rdev))) {
		return errors.New("DE25 FAT block identity differs from sysfs")
	}
	if !readOnly {
		var root unix.Statfs_t
		if err := unix.Statfs("/", &root); err != nil {
			return err
		}
		flags := uintptr(root.Flags) & (unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC | unix.MS_SYNCHRONOUS | unix.MS_NOATIME | unix.MS_NODIRATIME | unix.MS_RELATIME)
		if err := unix.Mount("", "/", "", flags|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
			return fmt.Errorf("make DE25 bootstrap read-only: %w", err)
		}
	}
	if err := unix.Mount(de25FAT, "/media/fat", "vfat", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC,
		"codepage=437,iocharset=iso8859-1,fmask=0077,dmask=0077"); err != nil {
		return fmt.Errorf("mount DE25 release volume: %w", err)
	}
	return nil
}

func validateDe25BootVolume(arch string, model, compatible, mountinfo, rootDevice []byte) (bool, error) {
	if arch != "arm64" || !bytes.Equal(model, []byte("SoCFPGA Agilex5 Terasic DE25-Nano\x00")) ||
		!bytes.Equal(compatible, []byte("intel,socfpga-agilex5-socdk\x00intel,socfpga-agilex5\x00")) {
		return false, errors.New("partition bootstrap requires AArch64 Terasic DE25-Nano Agilex 5")
	}
	root := strings.TrimSuffix(string(rootDevice), "\n")
	if root == "" || string(rootDevice) != root+"\n" || strings.ContainsAny(root, " \t\r\n") {
		return false, errors.New("invalid DE25 root device identity")
	}
	found, readOnly := false, false
	for _, line := range strings.Split(string(mountinfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		separator := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || len(fields) != separator+4 {
			return false, errors.New("malformed bootstrap mount information")
		}
		if fields[4] == "/media/fat" {
			return false, errors.New("DE25 release volume already mounted")
		}
		if fields[4] != "/" {
			continue
		}
		if found || fields[2] != root || fields[3] != "/" || fields[separator+1] != "ext4" ||
			(fields[separator+2] != "/dev/root" && fields[separator+2] != "/dev/mmcblk0p2") {
			return false, errors.New("DE25 bootstrap root must be the complete ext4 mmcblk0p2 partition")
		}
		found = true
		options := "," + fields[5] + ","
		readOnly = strings.Contains(options, ",ro,")
		if readOnly == strings.Contains(options, ",rw,") {
			return false, errors.New("ambiguous bootstrap root mount options")
		}
	}
	if !found {
		return false, errors.New("DE25 bootstrap root mount missing")
	}
	return readOnly, nil
}
