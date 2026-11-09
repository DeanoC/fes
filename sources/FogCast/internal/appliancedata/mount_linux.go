//go:build linux

package appliancedata

import (
	"fmt"

	"github.com/DeanoC/FogCast/appliance/blockprobe"
	"golang.org/x/sys/unix"
)

func platformMounter() Mounter { return linuxMounter{} }

type linuxMounter struct{}

func (linuxMounter) MountVolume(source, target string) error {
	if err := blockprobe.RejectsDevice("/sys", source); err != nil {
		return err
	}
	return unix.Mount(source, target, "ext4", unix.MS_NOATIME, "")
}

func (linuxMounter) Bind(source, target string) error {
	return unix.Mount(source, target, "none", unix.MS_BIND, "")
}

func (linuxMounter) Unbind(volumeRoot, target string) error {
	if !mountpointBusy("/proc/self/mountinfo", target) {
		return fmt.Errorf("game library target is not mounted: %s", target)
	}
	var volume, mounted unix.Stat_t
	if err := unix.Stat(volumeRoot, &volume); err != nil {
		return err
	}
	if err := unix.Stat(target, &mounted); err != nil {
		return err
	}
	if volume.Dev != mounted.Dev {
		return fmt.Errorf("refusing to unbind game library outside FESDATA3: %s", target)
	}
	return unix.Unmount(target, 0)
}
