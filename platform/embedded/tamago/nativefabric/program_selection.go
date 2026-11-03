// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"runtime"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

type programChoice struct {
	Path           string `json:"path"`
	PackageID      string `json:"package_id"`
	MetadataPath   string `json:"metadata_path"`
	MetadataSHA256 string `json:"metadata_sha256"`
}

type programChoices struct {
	Format   int             `json:"format"`
	Scope    string          `json:"scope"`
	Programs []programChoice `json:"programs"`
}

// ReadProgramSelections reads bounded user choices while storage is active.
// It grants no compatibility or hardware permission. Each returned request
// must still pass Store.Retain and ProgramLoader.Preflight before containment.
func ReadProgramSelections(ctx context.Context, files fs.FS, path string) (requests []packagestore.Request, err error) {
	if ctx == nil || files == nil || path == "." || !fs.ValidPath(path) {
		return nil, errors.New("native fabric: active filesystem and selection path required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := files.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			requests = nil
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 8192 {
		return nil, errors.New("native fabric: bounded regular program selection file required")
	}
	data := make([]byte, int(info.Size())+1)
	offset, stalled := 0, 0
	for offset < len(data) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, e := f.Read(data[offset:])
		if n < 0 || n > len(data)-offset {
			return nil, errors.New("native fabric: invalid selection reader count")
		}
		offset += n
		if e != nil && !errors.Is(e, io.EOF) {
			return nil, e
		}
		if errors.Is(e, io.EOF) {
			break
		}
		if n == 0 {
			stalled++
			if stalled >= 100 {
				return nil, io.ErrNoProgress
			}
		} else {
			stalled = 0
		}
		runtime.Gosched()
	}
	if int64(offset) != info.Size() {
		return nil, errors.New("native fabric: program selection length changed")
	}
	var choices programChoices
	if err := decodeCompleteRecord(data[:offset], 8192, &choices); err != nil {
		return nil, err
	}
	if choices.Format != 1 || choices.Scope != "nano-program-selection-v1" || len(choices.Programs) == 0 || len(choices.Programs) > 16 {
		return nil, errors.New("native fabric: unsupported bounded program selection")
	}
	for _, p := range choices.Programs {
		if p.Path == "." || p.MetadataPath == "." || !fs.ValidPath(p.Path) || !fs.ValidPath(p.MetadataPath) ||
			!nonzeroDigest(p.PackageID) || !nonzeroDigest(p.MetadataSHA256) {
			return nil, errors.New("native fabric: incomplete program choice")
		}
		requests = append(requests, packagestore.Request{Path: p.Path, PackageID: p.PackageID, MetadataPath: p.MetadataPath, MetadataSHA256: p.MetadataSHA256})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return requests, nil
}
