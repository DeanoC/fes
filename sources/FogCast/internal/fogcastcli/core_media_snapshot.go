package fogcastcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type coreMediaSnapshot struct {
	file     *os.File
	size     int64
	mediaID  string
	closed   bool
	closeErr error
}

func (s *coreMediaSnapshot) Close() error {
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	if err := s.file.Close(); err != nil {
		s.closeErr = fmt.Errorf("close media snapshot: %w", err)
	}
	if err := os.Remove(s.file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.closeErr = errors.Join(s.closeErr, fmt.Errorf("remove media snapshot: %w", err))
	}
	return s.closeErr
}

func snapshotCoreMedia(ctx context.Context, path string) (*coreMediaSnapshot, error) {
	return snapshotMediaFile(ctx, path, catalog.MaxCoreMediaBytes, func(int64) bool { return true }, invalidCoreMediaSnapshot())
}

// snapshotHardDisk snapshots a raw hard-disk image: 1..128 MiB in whole
// 512-byte sectors, the host's hard-disk route limit.
func snapshotHardDisk(ctx context.Context, path string) (*coreMediaSnapshot, error) {
	return snapshotMediaFile(ctx, path, catalog.MaxHardDiskMediaBytes, protocol.AdmitAtaDiskSize, invalidHardDiskSnapshot())
}

func snapshotMediaFile(ctx context.Context, path string, limit int64, admit func(int64) bool, invalid error) (*coreMediaSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > limit || !admit(before.Size()) {
		return nil, invalid
	}
	source, err := os.Open(path)
	if err != nil {
		return nil, invalid
	}
	defer source.Close()
	opened, err := source.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() != before.Size() {
		return nil, invalid
	}
	return snapshotMediaReader(ctx, source, opened.Size(), limit, invalid)
}

// The snapshot fixes both the upload identity and length before opening HTTP.
func snapshotCoreMediaReader(ctx context.Context, source io.Reader, size int64) (*coreMediaSnapshot, error) {
	return snapshotMediaReader(ctx, source, size, catalog.MaxCoreMediaBytes, invalidCoreMediaSnapshot())
}

func snapshotMediaReader(ctx context.Context, source io.Reader, size, limit int64, invalid error) (_ *coreMediaSnapshot, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if size < 1 || size > limit {
		return nil, invalid
	}
	file, err := os.CreateTemp("", "fogcast-media-snapshot-*")
	if err != nil {
		return nil, err
	}
	snapshot := &coreMediaSnapshot{file: file, size: size}
	defer func() {
		if err != nil {
			err = errors.Join(err, snapshot.Close())
		}
	}()
	hash := sha256.New()
	// Limit the read to the stated size plus one so growth cannot extend the copy.
	source = io.LimitReader(source, size+1)
	buffer := make([]byte, 64<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > size {
				return nil, invalid
			}
			if _, err := file.Write(buffer[:n]); err != nil {
				return nil, err
			}
			hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if total != size {
		return nil, invalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	snapshot.mediaID = hex.EncodeToString(hash.Sum(nil))
	return snapshot, nil
}

func invalidCoreMediaSnapshot() error {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Message: fmt.Sprintf("core media must be a regular file of 1..%d bytes", catalog.MaxCoreMediaBytes)}
}

func invalidHardDiskSnapshot() error {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Message: fmt.Sprintf("hard-disk image must be a regular file of %d to %d bytes in whole %d-byte sectors", protocol.AtaDiskMinBytes, catalog.MaxHardDiskMediaBytes, protocol.AtaDiskSectorBytes)}
}
