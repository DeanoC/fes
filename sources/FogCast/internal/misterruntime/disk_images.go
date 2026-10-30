package misterruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

// DiskImageRoot holds hard-disk base images by digest, as <sha256>.img. The
// runtime reads one at launch and writes it into the DDR window; the guest's
// changes live in the media-data delta, so a staged image never changes.
const DiskImageRoot = "/media/fat/fogcast/disk-images"

// diskImagesKept bounds the staged images on the SD card. The least recently
// launched image goes first; a relaunch of it stages it again.
const diskImagesKept = 4

func WithDiskImageRoot(path string) RuntimeOption {
	return func(runtime *Runtime) {
		runtime.diskImageRoot = path
	}
}

func (r *Runtime) diskImagePath(sha string) string {
	return filepath.Join(r.diskImageRoot, sha+".img")
}

// stagedDiskImage reports a staged image's size, or false when there is no
// regular file of an admitted size under its digest.
func (r *Runtime) stagedDiskImage(sha string) (int64, bool) {
	if r.diskImageRoot == "" || !protocol2Hex64.MatchString(sha) {
		return 0, false
	}
	info, err := os.Lstat(r.diskImagePath(sha))
	if err != nil || !info.Mode().IsRegular() || !protocol.AdmitAtaDiskSize(info.Size()) {
		return 0, false
	}
	return info.Size(), true
}

// DiskImage reports whether an image is staged and marks it recently used.
func (r *Runtime) DiskImage(ctx context.Context, sha string) (protocol.DiskImageStatus, *protocol.APIError) {
	if r.diskImageRoot == "" {
		return protocol.DiskImageStatus{}, unsupportedOperationError()
	}
	if !protocol2Hex64.MatchString(sha) {
		return protocol.DiskImageStatus{}, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "disk image identity is invalid", Phase: "request"}
	}
	size, ok := r.stagedDiskImage(sha)
	if !ok {
		return protocol.DiskImageStatus{SHA256: sha}, nil
	}
	now := time.Now()
	_ = os.Chtimes(r.diskImagePath(sha), now, now)
	return protocol.DiskImageStatus{SHA256: sha, Size: size, Staged: true}, nil
}

// StageDiskImage writes body under its digest after verifying it. It first
// makes room: a full card would otherwise fail every retry before anything
// was evicted.
func (r *Runtime) StageDiskImage(ctx context.Context, sha string, size int64, body io.Reader) (result protocol.DiskImageStatus, apiErr *protocol.APIError) {
	if r.diskImageRoot == "" {
		return result, unsupportedOperationError()
	}
	if !protocol2Hex64.MatchString(sha) || !protocol.AdmitAtaDiskSize(size) || body == nil {
		return result, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "disk image upload is invalid", Phase: "request"}
	}
	if err := os.MkdirAll(r.diskImageRoot, 0o700); err != nil {
		return result, diskImageStorageError()
	}
	r.evictDiskImages(sha)
	temporary, err := os.CreateTemp(r.diskImageRoot, ".stage-*")
	if err != nil {
		return result, diskImageStorageError()
	}
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporary.Name())
		}
	}()
	hash := sha256.New()
	file := &stageWriter{Writer: temporary}
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(&contextReader{ctx: ctx, reader: body}, size+1))
	if err != nil {
		if file.err != nil {
			return result, diskImageWriteError(file.err)
		}
		return result, &protocol.APIError{Code: protocol.CodeTransferFailed, Message: "disk image upload did not complete", Phase: "request"}
	}
	if written != size || hex.EncodeToString(hash.Sum(nil)) != sha {
		return result, &protocol.APIError{Code: protocol.CodeDigestMismatch, Message: "disk image bytes differ from their digest", Phase: "request"}
	}
	if err := temporary.Sync(); err != nil {
		return result, diskImageWriteError(err)
	}
	if err := temporary.Close(); err != nil {
		return result, diskImageWriteError(err)
	}
	if os.Rename(temporary.Name(), r.diskImagePath(sha)) != nil {
		return result, diskImageStorageError()
	}
	keep = true
	if directory, err := os.Open(r.diskImageRoot); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return protocol.DiskImageStatus{SHA256: sha, Size: size, Staged: true}, nil
}

// stageWriter remembers a failed write, so a failure of the card is not
// reported as a failed upload.
type stageWriter struct {
	io.Writer
	err error
}

func (w *stageWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err != nil {
		w.err = err
	}
	return n, err
}

func diskImageWriteError(err error) *protocol.APIError {
	if errors.Is(err, syscall.ENOSPC) {
		return &protocol.APIError{Code: protocol.CodeCacheFull, Message: protocol.DiskImageStorageFullMessage, Phase: "request"}
	}
	return diskImageStorageError()
}

// removeCorruptStagedDisk drops a staged image the runtime found differing
// from its digest, so the probe stops reporting it and the next launch
// stages it again.
func (r *Runtime) removeCorruptStagedDisk(disk *ataDiskRequest) {
	if disk != nil && disk.Path == r.diskImagePath(disk.BaseMediaID) {
		_ = os.Remove(disk.Path)
	}
}

// evictDiskImages removes uploads that never finished and all but the most
// recently used diskImagesKept-1 images other than current.
func (r *Runtime) evictDiskImages(current string) {
	entries, err := os.ReadDir(r.diskImageRoot)
	if err != nil {
		return
	}
	type staged struct {
		name string
		used time.Time
	}
	var images []staged
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".stage-") {
			// A staging upload that did not finish; uploads are serialized.
			_ = os.Remove(filepath.Join(r.diskImageRoot, name))
			continue
		}
		sha, ok := strings.CutSuffix(name, ".img")
		if !ok || !protocol2Hex64.MatchString(sha) || sha == current {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			images = append(images, staged{name, info.ModTime()})
		}
	}
	sort.Slice(images, func(i, j int) bool { return images[i].used.After(images[j].used) })
	for index, image := range images {
		if index >= diskImagesKept-1 {
			_ = os.Remove(filepath.Join(r.diskImageRoot, image.name))
		}
	}
}

func diskImageStorageError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInternal, Message: "disk image could not be stored on the kit", Phase: "request"}
}

// resolveStagedDisk fills a launch binding's disk size from its staged file.
func (r *Runtime) resolveStagedDisk(machine *protocol.LibraryMachineBinding) (*ataDiskRequest, *protocol.APIError) {
	if machine == nil || machine.Disk == nil {
		return nil, nil
	}
	size, ok := r.stagedDiskImage(machine.Disk.SHA256)
	if !ok {
		return nil, &protocol.APIError{Code: protocol.CodeBadRequest, Message: "hard-disk image is not staged on the kit", Phase: "admission"}
	}
	machine.Disk.Size = size
	if !machine.Disk.Valid() {
		return nil, &protocol.APIError{Code: protocol.CodeBadRequest, Message: protocol.HardDiskRequestError().Message, Phase: "admission"}
	}
	g := machine.Disk.Geometry
	return &ataDiskRequest{Path: r.diskImagePath(machine.Disk.SHA256), Size: size, BaseMediaID: machine.Disk.SHA256,
		DataRoot: MediaDataRoot, Cylinders: g.Cylinders, Heads: g.Heads, SectorsPerTrack: g.SectorsPerTrack}, nil
}
