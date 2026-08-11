//go:build !darwin && !linux

package fogcast

import (
	"errors"
	"os"
)

func openConfigSource(_ string) (*os.File, error) {
	return nil, errors.New("config source identity cannot be verified")
}
