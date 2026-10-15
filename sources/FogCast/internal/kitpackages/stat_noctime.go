//go:build !linux

package kitpackages

import "os"

func devOf(os.FileInfo) uint64 { return 0 }

func ctimeOf(os.FileInfo) int64 { return 0 }
