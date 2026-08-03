//go:build !darwin && !linux

package romsource

import (
	"errors"
	"os"
)

func exclusiveRenamePreparedAt(_ *os.Root, _ string, _ *os.Root, _ string) error {
	return errors.New("exclusive prepared ROM rename is unavailable")
}
