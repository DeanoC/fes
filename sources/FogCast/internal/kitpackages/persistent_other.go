//go:build !linux

package kitpackages

import (
	"context"
	"errors"
)

func OpenPersistent(string) (*Store, error) {
	return nil, errors.New("kit packages: persistent target storage requires Linux")
}
func OpenPersistentForHost(context.Context, string) (*Store, error) {
	return nil, errors.New("kit packages: persistent target storage requires Linux")
}
func (s *Store) CheckPersistent() error {
	return errors.New("kit packages: persistent target storage requires Linux")
}
