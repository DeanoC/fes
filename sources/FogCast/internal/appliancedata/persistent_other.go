//go:build !linux

package appliancedata

import (
	"context"
	"errors"
)

// CheckPersistent always fails off Linux: FESDATA3 exists only on a kit.
func CheckPersistent(string) error {
	return errors.New("appliance data: persistent target storage requires Linux")
}

func CheckPersistentContext(context.Context, string) error {
	return errors.New("appliance data: persistent target storage requires Linux")
}

func verifyMountedVolume(string, string, string) error {
	return errors.New("appliance data: persistent target storage requires Linux")
}
