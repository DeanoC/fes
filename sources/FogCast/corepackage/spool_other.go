//go:build !unix

package corepackage

import (
	"errors"
	"io"
	"os"
)

func mapReadOnly(file *os.File, size int64) ([]byte, func() error, error) {
	data := make([]byte, size)
	if _, err := file.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	return data, func() error { return nil }, nil
}
