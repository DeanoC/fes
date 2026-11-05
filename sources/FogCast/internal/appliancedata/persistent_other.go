//go:build !linux

package appliancedata

import "errors"

// CheckPersistent always fails off Linux: FESDATA3 exists only on a kit.
func CheckPersistent(string) error {
	return errors.New("appliance data: persistent target storage requires Linux")
}
