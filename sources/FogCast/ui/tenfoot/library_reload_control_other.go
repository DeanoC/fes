//go:build !linux

package tenfoot

import (
	"context"
	"errors"
)

// RequestLibraryReload is available on a Linux kit launcher.
func RequestLibraryReload(context.Context) error {
	return errors.New("library reload control requires Linux")
}
