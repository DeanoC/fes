// SPDX-License-Identifier: GPL-3.0-or-later

package packagestore

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast/protocol"
)

// ReadC64DiskSet retains a small read-only disk set under one active SD lease.
// Live media requests do not have access to this method or the filesystem.
func (s *Store) ReadC64DiskSet(ctx context.Context, disks []TapeRequest) (images [][]byte, err error) {
	if s == nil || ctx == nil || len(disks) < 2 || len(disks) > 4 {
		return nil, ErrState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.observe(); err != nil {
		return nil, err
	}
	if !s.last.Active || !s.last.Prepared {
		return nil, ErrState
	}
	defer func() {
		if e := s.observe(); e != nil {
			images, err = nil, errors.Join(err, e)
		}
	}()
	for _, disk := range disks {
		if disk.Format != C64DiskFormat || disk.Validate() != nil {
			return nil, ErrState
		}
		image, err := readInput(ctx, s.files, disk.Path, protocol.C64DiskBytes, protocol.C64DiskBytes, disk.SHA256)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, nil
}
