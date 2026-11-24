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

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

const PreferencesPath = "FES/PREFS.JSON"
const MaxPreferencesBytes = 65536

func validPreferences(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) > 0 && len(data) <= MaxPreferencesBytes && data[0] == '{' && json.Valid(data)
}

// ReadPreferences reads one bounded application settings object under this
// store's active lease. It supplies no package metadata or programming grant.
func (s *Store) ReadPreferences(ctx context.Context) (data []byte, err error) {
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
	f, err := s.files.Open(PreferencesPath)
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
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > MaxPreferencesBytes {
		return nil, errors.New("package store: invalid bounded preferences file")
	}
	data = make([]byte, int(info.Size())+1)
	offset, stalled := 0, 0
	for offset < len(data) {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		n, e := f.Read(data[offset:])
		if n < 0 || n > len(data)-offset {
			return nil, errors.New("package store: invalid preferences reader count")
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
	if int64(offset) != info.Size() || !validPreferences(data[:offset]) {
		return nil, errors.New("package store: changed or invalid preferences object")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data[:offset], nil
}

// WritePreferences touches only the existing settings file and serializes
// against retention/containment. When the trusted filesystem supports atomic
// copy-on-write replacement (ReplaceFile), updates commit in a single
// directory-entry write and can grow beyond prior preallocation; otherwise it
// falls back to in-place WriteFile. Both paths must finish their device flush
// before success, and neither creates missing files.
func (s *Store) WritePreferences(ctx context.Context, data []byte) error {
	if s == nil || ctx == nil {
		return ErrState
	}
	if !validPreferences(data) || len(data) > MaxPreferencesBytes {
		return errors.New("package store: invalid bounded preferences object")
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
	var err error
	if replacer, ok := s.files.(interface {
		ReplaceFile(context.Context, string, []byte) error
	}); ok {
		// ReplaceFile creates absent targets, so verify PreferencesPath already
		// exists as a regular file before writing.
		f, openErr := s.files.Open(PreferencesPath)
		if openErr != nil {
			if observed := s.observe(); observed != nil {
				return errors.Join(openErr, observed)
			}
			if !errors.Is(openErr, block.ErrReadOnly) && !errors.Is(openErr, fs.ErrNotExist) {
				return s.retire(openErr)
			}
			return openErr
		}
		info, statErr := f.Stat()
		closeErr := f.Close()
		if prepErr := errors.Join(statErr, closeErr); prepErr != nil {
			if observed := s.observe(); observed != nil {
				return errors.Join(prepErr, observed)
			}
			return s.retire(prepErr)
		}
		if !info.Mode().IsRegular() {
			return errors.New("package store: invalid bounded preferences file")
		}
		// Once issued, complete the bounded filesystem replace/flush even if
		// the requester disconnects. A context timeout is not evidence of rollback.
		err = replacer.ReplaceFile(context.Background(), PreferencesPath, data)
	} else {
		writer, ok := s.files.(interface{ WriteFile(string, []byte) error })
		if !ok {
			return block.ErrReadOnly
		}
		// Once issued, complete the bounded filesystem write/flush even if the
		// requester disconnects. A context timeout is not evidence of rollback.
		err = writer.WriteFile(PreferencesPath, data)
	}
	if observed := s.observe(); observed != nil {
		return errors.Join(err, observed)
	}
	if err != nil && !errors.Is(err, block.ErrReadOnly) && !errors.Is(err, fat.ErrCapacity) && !errors.Is(err, fat.ErrNoSpace) && !errors.Is(err, fs.ErrNotExist) {
		return s.retire(err)
	}
	return err
}
