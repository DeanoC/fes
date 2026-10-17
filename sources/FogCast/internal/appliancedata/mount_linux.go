//go:build linux

package appliancedata

import (
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
