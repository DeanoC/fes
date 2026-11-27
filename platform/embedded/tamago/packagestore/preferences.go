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

// preferenceRefusal reports whether err is one of refusals and left the mount
// healthy. fat.ErrMutation never qualifies, whatever its joined cause: FAT
// retires its mount when IO fails after a mutation started, so an sdhci socket
// write-protect refusal between sectors arrives as ErrMutation joined with
// block.ErrReadOnly.
func preferenceRefusal(err error, refusals ...error) bool {
	if errors.Is(err, fat.ErrMutation) {
		return false
	}
	for _, refusal := range refusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return false
}

// WritePreferences replaces only the existing settings file and serializes
// against retention/containment. The trusted filesystem's copy-on-write
// ReplaceFile commits the update in a single directory-entry write, may grow
// the file beyond its prior allocation and must finish its device flush before
// success. A filesystem without ReplaceFile is read-only. A missing settings
// file is refused, never created.
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
	replacer, ok := s.files.(interface {
		ReplaceFile(context.Context, string, []byte) error
	})
	if !ok {
		return block.ErrReadOnly
	}
	// ReplaceFile creates absent targets, so verify PreferencesPath already
	// exists as a regular file before writing.
	f, err := s.files.Open(PreferencesPath)
	if err != nil {
		if observed := s.observe(); observed != nil {
			return errors.Join(err, observed)
		}
		if !preferenceRefusal(err, block.ErrReadOnly, fs.ErrNotExist) {
			return s.retire(err)
		}
		return err
	}
	info, statErr := f.Stat()
	closeErr := f.Close()
	if err = errors.Join(statErr, closeErr); err != nil {
		if observed := s.observe(); observed != nil {
			return errors.Join(err, observed)
		}
		return s.retire(err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("package store: invalid bounded preferences file")
	}
	// Once issued, complete the bounded filesystem replace/flush even if the
	// requester disconnects. A context timeout is not evidence of rollback.
	s.invalidateCacheLocked()
	err = replacer.ReplaceFile(context.Background(), PreferencesPath, data)
	if observed := s.observe(); observed != nil {
		return errors.Join(err, observed)
	}
	if err != nil && !preferenceRefusal(err, block.ErrReadOnly, fat.ErrCapacity, fat.ErrNoSpace, fs.ErrNotExist) {
		return s.retire(err)
	}
	return err
}
