//go:build !unix

package kitpackages

import "os"

func inodeOf(info os.FileInfo) uint64 { return 0 }
