//go:build !linux

package software

import "errors"

func OpenPersistent(c Compatibility) (*Store, error) {
	return nil, errors.New("persistent software releases require Linux")
}
func checkPersistent(string) error { return errors.New("persistent software releases require Linux") }
