// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"runtime"
	"strings"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

const (
	BTBondsPath        = "FES/BTBONDS.JSON"
	MaxBTBondsBytes    = 16384
	BTFirmwareDir      = "FES/BTFW"
	MaxBTFirmwareBytes = 131072
)

func validBTBonds(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) > 0 && len(data) <= MaxBTBondsBytes && data[0] == '{' && json.Valid(data)
}

func validFirmwareBasename(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	if strings.ContainsAny(name, "/\\:\x00") || name == "." || name == ".." || strings.Contains(name, "..") {
		return false
	}
	return true
}

func (s *Store) readBoundedFile(ctx context.Context, path string, maxBytes int64) (data []byte, err error) {
	if s == nil || ctx == nil {
		return nil, ErrState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = s.observe(); err != nil {
		return nil, err
	}
	if !s.last.Active || !s.last.Prepared {
		return nil, ErrState
	}
	defer func() {
		if e := s.observe(); e != nil {
			data = nil
			err = errors.Join(err, e)
		}
	}()
	f, err := s.files.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := f.Close(); e != nil {
			data = nil
			err = errors.Join(err, e)
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxBytes {
		return nil, errors.New("package store: invalid bounded file size")
	}
	data = make([]byte, int(info.Size())+1)
	offset, stalled := 0, 0
	for offset < len(data) {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		n, e := f.Read(data[offset:])
		if n < 0 || n > len(data)-offset {
			return nil, errors.New("package store: invalid reader count")
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
		return nil, errors.New("package store: changed file length during read")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data[:offset], nil
}

// ReadBTBonds reads the bounded Bluetooth bond store JSON (FES/BTBONDS.JSON)
// under this store's active SD lease.
func (s *Store) ReadBTBonds(ctx context.Context) ([]byte, error) {
	data, err := s.readBoundedFile(ctx, BTBondsPath, MaxBTBondsBytes)
	if err != nil {
		return nil, err
	}
	if !validBTBonds(data) {
		return nil, errors.New("package store: invalid bounded Bluetooth bonds object")
	}
	return data, nil
}

// WriteBTBonds persists the bounded Bluetooth bond store JSON (FES/BTBONDS.JSON)
// using SD-checkpoint flush semantics. On a fresh card where FES/BTBONDS.JSON
// does not yet exist, it provisions a preallocated MaxBTBondsBytes file (via
// CreateFile) space-padded with valid JSON whitespace so future WriteFile
// updates succeed in place.
func (s *Store) WriteBTBonds(ctx context.Context, data []byte) error {
	if s == nil || ctx == nil {
		return ErrState
	}
	if !validBTBonds(data) {
		return errors.New("package store: invalid bounded Bluetooth bonds object")
	}
	data = bytes.Clone(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.observe(); err != nil {
		return err
	}
	if !s.last.Active || !s.last.Prepared {
		return ErrState
	}
	writer, hasWrite := s.files.(interface{ WriteFile(string, []byte) error })
	creator, hasCreate := s.files.(interface {
		CreateFile(context.Context, string, []byte) error
	})
	if !hasWrite && !hasCreate {
		return block.ErrReadOnly
	}

	var err error
	if hasWrite {
		err = writer.WriteFile(BTBondsPath, data)
	} else {
		err = fs.ErrNotExist
	}
	if errors.Is(err, fs.ErrNotExist) && hasCreate {
		err = s.provisionBTBondsLocked(ctx, creator, data)
	}
	if observed := s.observe(); observed != nil {
		return errors.Join(err, observed)
	}
	if err != nil && !errors.Is(err, block.ErrReadOnly) && !errors.Is(err, fat.ErrCapacity) && !errors.Is(err, fat.ErrNoSpace) && !errors.Is(err, fs.ErrNotExist) {
		return s.retire(err)
	}
	return err
}

func (s *Store) provisionBTBondsLocked(ctx context.Context, creator interface {
	CreateFile(context.Context, string, []byte) error
}, data []byte) error {
	padded := make([]byte, MaxBTBondsBytes)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = ' '
	}
	err := creator.CreateFile(ctx, BTBondsPath, padded)
	if errors.Is(err, fs.ErrNotExist) {
		if mk, ok := s.files.(interface {
			Mkdir(context.Context, string) error
		}); ok {
			if mkErr := mk.Mkdir(ctx, "FES"); mkErr == nil || errors.Is(mkErr, fs.ErrExist) {
				err = creator.CreateFile(ctx, BTBondsPath, padded)
			} else {
				err = mkErr
			}
		}
	}
	return err
}

// ReadBTFirmware reads a bounded Bluetooth dongle firmware or config blob
// (e.g. rtl8761bu_fw.bin for 0bda:a728) from FES/BTFW/ under the active lease.
func (s *Store) ReadBTFirmware(ctx context.Context, name string) ([]byte, error) {
	if !validFirmwareBasename(name) {
		return nil, errors.New("package store: invalid Bluetooth firmware filename")
	}
	data, err := s.readBoundedFile(ctx, BTFirmwareDir+"/"+name, MaxBTFirmwareBytes)
	if errors.Is(err, fs.ErrNotExist) {
		upper := strings.ToUpper(name)
		if upper != name {
			return s.readBoundedFile(ctx, BTFirmwareDir+"/"+upper, MaxBTFirmwareBytes)
		}
	}
	return data, err
}
