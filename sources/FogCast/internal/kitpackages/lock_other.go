//go:build !unix

package kitpackages

import (
	"context"
	"errors"
	"os"
)

func (s *Store) lock(context.Context) (*os.Root, func(), error) {
	return nil, nil, errors.New("kit packages: durable stores require POSIX file locking")
}
