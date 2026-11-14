// SPDX-License-Identifier: GPL-3.0-or-later

// Package block is the fixed-sector storage boundary used by the embedded
// filesystem. A completed write is not durable until Flush succeeds.
package block

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const SectorSize = 512

var (
	ErrBounds    = errors.New("block request is outside the device")
	ErrAlignment = errors.New("block request is not sector aligned")
	ErrReadOnly  = errors.New("block device is read-only")
)

type Device interface {
	SectorCount() uint64
	ReadSectors(lba uint64, dst []byte) error
	WriteSectors(lba uint64, src []byte) error
	Flush() error
}

func CheckRange(sectors, lba uint64, length int) error {
	if length < 0 || length%SectorSize != 0 {
		return ErrAlignment
	}
	count := uint64(length / SectorSize)
	if lba > sectors || count > sectors-lba {
		return ErrBounds
	}
	return nil
}

// File is a regular-file-backed device for host tests and image inspection.
// It refuses raw device nodes. Native firmware uses VirtIO instead.
type File struct {
	file     *os.File
	sectors  uint64
	writable bool
}

func OpenFile(path string, writable bool) (*File, error) {
	flags := os.O_RDONLY
	if writable {
		flags = os.O_RDWR
	}
	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size()%SectorSize != 0 {
		f.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("block backing must be a nonempty, sector-aligned regular file")
	}
	return &File{file: f, sectors: uint64(info.Size() / SectorSize), writable: writable}, nil
}

func (f *File) SectorCount() uint64 { return f.sectors }
func (f *File) ReadSectors(lba uint64, dst []byte) error {
	if err := CheckRange(f.sectors, lba, len(dst)); err != nil {
		return err
	}
	if len(dst) == 0 {
		return nil
	}
	n, err := f.file.ReadAt(dst, int64(lba*SectorSize))
	if err != nil {
		return err
	}
	if n != len(dst) {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (f *File) WriteSectors(lba uint64, src []byte) error {
	if !f.writable {
		return ErrReadOnly
	}
	if err := CheckRange(f.sectors, lba, len(src)); err != nil {
		return err
	}
	n, err := f.file.WriteAt(src, int64(lba*SectorSize))
	if err != nil {
		return err
	}
	if n != len(src) {
		return io.ErrShortWrite
	}
	return nil
}
func (f *File) Flush() error { return f.file.Sync() }
func (f *File) Close() error { return f.file.Close() }
