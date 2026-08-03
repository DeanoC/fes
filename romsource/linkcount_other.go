//go:build !darwin && !linux

package romsource

import "io/fs"

func preparedLinkCount(_ fs.FileInfo) (uint64, bool) {
	return 0, false
}
