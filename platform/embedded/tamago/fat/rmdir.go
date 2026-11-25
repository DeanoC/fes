// SPDX-License-Identifier: GPL-3.0-or-later

package fat

import (
	"context"
	"errors"
)

// ErrNotEmpty is returned when RemoveDir targets a non-empty directory.
var ErrNotEmpty = errors.New("fat: directory not empty")

// RemoveDir removes an existing empty directory in crash-safe order: after
// verifying the directory contains no entries other than "." and "..", its
// short 8.3 entry in the parent directory is marked deleted (0xE5) and flushed
// first before any preceding LFN-only sectors are wiped and its cluster chain is
// reclaimed across mirrored FAT tables. Non-empty directories return ErrNotEmpty,
// non-directories return fs.ErrInvalid, root returns fs.ErrInvalid, read-only
// directories return fs.ErrPermission, and missing targets return fs.ErrNotExist.
func (v *Volume) RemoveDir(ctx context.Context, name string) error {
	return v.removeEntry(ctx, "rmdir", name, true)
}
