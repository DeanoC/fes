//go:build !linux

package appliancedata

import "errors"

func platformMounter() Mounter { return unavailableMounter{} }

type unavailableMounter struct{}

func (unavailableMounter) MountVolume(string, string) error {
	return errors.New("appliance data partition bind requires linux")
}

func (unavailableMounter) Bind(string, string) error {
	return errors.New("appliance data partition bind requires linux")
}
