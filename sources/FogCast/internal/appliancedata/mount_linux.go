//go:build linux

package appliancedata

import "golang.org/x/sys/unix"

func platformMounter() Mounter { return linuxMounter{} }

type linuxMounter struct{}

func (linuxMounter) MountVolume(source, target string) error {
	return unix.Mount(source, target, "ext4", unix.MS_NOATIME, "")
}

func (linuxMounter) Bind(source, target string) error {
	return unix.Mount(source, target, "none", unix.MS_BIND, "")
}
