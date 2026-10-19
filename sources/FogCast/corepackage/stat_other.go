//go:build !linux && !darwin

package corepackage

import (
	"os"
	"time"
)

// statKey has no ctime here, so every read verifies the archive again.
func statKey(os.FileInfo) (string, time.Time) { return "", time.Time{} }
