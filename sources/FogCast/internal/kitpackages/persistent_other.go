//go:build !linux

package kitpackages

import "errors"

func OpenPersistent(string) (*Store, error) {
	return nil, errors.New("kit packages: persistent target storage requires Linux")
}
func (s *Store) CheckPersistent() error {
	return errors.New("kit packages: persistent target storage requires Linux")
}
