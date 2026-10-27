//go:build !linux

package storage

import (
	"context"
	"errors"
)

const nonblock = 0

type systemBackend struct{}

func (systemBackend) Discover(context.Context) ([]Drive, error) { return []Drive{}, nil }
func (systemBackend) Mount(context.Context, Drive, string) (string, error) {
	return "", errors.New("drive mounting is supported on Linux; select drives on the FogCast host")
}
