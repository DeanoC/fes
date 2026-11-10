// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

const InstallContentRoot = "FES/CONTENT"
const InstallCatalogRoot = "FES/INSTALLED"
const MaxInstallBytes = 64 << 20
const MaxInstallRecords = 32
const MaxInstallRecordBytes = 8192

var ErrInstallConflict = errors.New("package store: existing install content differs")

// InstallPlan privately retains validated input. It grants no storage lease,
// platform compatibility, frontend ownership or programming permission.
type InstallPlan struct {
	request   Request
	files     memoryFiles
	selection *Selection
}

func copyRequest(r Request) Request {
	r.Sources = append([]Source(nil), r.Sources...)
	r.Parts = append([]Part(nil), r.Parts...)
	return r
}
func (p *InstallPlan) Request() Request {
	if p == nil {
		return Request{}
	}
	return copyRequest(p.request)
}
func (p *InstallPlan) Selection() *Selection {
	if p == nil {
		return nil
	}
	return p.selection
}
func blobHash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

// PrepareInstall performs no device IO. Input paths are names in this upload,
// never destinations; all persistent paths derive from the copied file bytes.
// The caller must not mutate input while this call is copying it.
func PrepareInstall(ctx context.Context, request Request, files map[string][]byte) (*InstallPlan, error) {
	if ctx == nil || len(files) < 1 || len(files) > 7 || len(request.Sources) > 2 || len(request.Parts) > 2 {
		return nil, ErrState
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request = copyRequest(request)
	required := map[string]bool{request.Path: true}
	if request.MetadataPath != "" {
		required[request.MetadataPath] = true
	}
	if request.Tape.Path != "" {
		required[request.Tape.Path] = true
	}
	for _, s := range request.Sources {
		required[s.Path] = true
	}
	for _, p := range request.Parts {
		required[p.Path] = true
	}
	if len(required) != len(files) {
		return nil, ErrState
	}
	total := 0
	for name, data := range files {
		if !required[name] || !pathOK(name) || len(data) > MaxInstallBytes-total {
			return nil, ErrState
		}
		total += len(data)
	}
	retained := memoryFiles{}
	destinations := map[string]string{}
	for name, data := range files {
		private := make([]byte, len(data))
		for off := 0; off < len(data); off += 32768 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			copy(private[off:min(off+32768, len(data))], data[off:min(off+32768, len(data))])
			runtime.Gosched()
		}
		dest := InstallContentRoot + "/" + blobHash(private) + ".BIN"
		if old, ok := retained[dest]; ok && !bytes.Equal(old, private) {
			return nil, ErrInstallConflict
		}
		retained[dest] = private
		destinations[name] = dest
	}
	request.Path = destinations[request.Path]
	if request.MetadataPath != "" {
		request.MetadataPath = destinations[request.MetadataPath]
	}
	if request.Tape.Path != "" {
		request.Tape.Path = destinations[request.Tape.Path]
	}
	for i := range request.Sources {
		request.Sources[i].Path = destinations[request.Sources[i].Path]
	}
	for i := range request.Parts {
		request.Parts[i].Path = destinations[request.Parts[i].Path]
	}
	selection, err := retainSelection(ctx, retained, request)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &InstallPlan{request: request, files: retained, selection: selection}, nil
}

type memoryFiles map[string][]byte

func (m memoryFiles) Open(name string) (fs.File, error) {
	data, ok := m[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memoryFile{Reader: bytes.NewReader(data), name: path.Base(name)}, nil
}

type memoryFile struct {
	*bytes.Reader
	name   string
	closed bool
}

func (f *memoryFile) Close() error { f.closed = true; return nil }
func (f *memoryFile) Read(b []byte) (int, error) {
	if f.closed {
		return 0, fs.ErrClosed
	}
	return f.Reader.Read(b)
}
func (f *memoryFile) Stat() (fs.FileInfo, error) {
	if f.closed {
		return nil, fs.ErrClosed
	}
	return memoryInfo{f.name, f.Reader.Size()}, nil
}

type memoryInfo struct {
	name string
	size int64
}

func (i memoryInfo) Name() string     { return i.name }
func (i memoryInfo) Size() int64      { return i.size }
func (memoryInfo) Mode() fs.FileMode  { return 0444 }
func (memoryInfo) ModTime() time.Time { return time.Time{} }
func (memoryInfo) IsDir() bool        { return false }
func (memoryInfo) Sys() any           { return nil }

type installWriter interface {
	Mkdir(context.Context, string) error
	CreateFile(context.Context, string, []byte) error
}

// ReadStagingRecord reads bounded, untrusted content under the active lease.
// Its caller must validate its schema; this supplies no write authority.
func (s *Store) ReadStagingRecord(ctx context.Context, name string) (data []byte, err error) {
	if s == nil || ctx == nil || !pathOK(name) {
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
	data, err = readBoundedInput(ctx, s.files, name, MaxInstallRecordBytes, -1, "")
	if errors.Is(err, fat.ErrMutation) {
		return nil, s.retire(err)
	}
	return data, err
}

// StageInstall snapshots exactly the manifest-bound input files into RAM before
// the menu owns SD. Package/ROM validators still decide content validity.
func (s *Store) StageInstall(ctx context.Context, request Request, checksums map[string]string) (plan *InstallPlan, err error) {
	if s == nil || ctx == nil || len(checksums) < 1 || len(checksums) > 7 || len(request.Sources) > 2 || len(request.Parts) > 2 {
		return nil, ErrState
	}
	request = copyRequest(request)
	required := map[string]bool{request.Path: true}
	if request.MetadataPath != "" {
		required[request.MetadataPath] = true
	}
	if request.Tape.Path != "" {
		required[request.Tape.Path] = true
	}
	for _, f := range request.Sources {
		required[f.Path] = true
	}
	for _, f := range request.Parts {
		required[f.Path] = true
	}
	if len(required) != len(checksums) {
		return nil, ErrState
	}
	expected := map[string]string{}
	names := make([]string, 0, len(required))
	for name := range required {
		h, ok := checksums[name]
		raw, e := hex.DecodeString(h)
		if !ok || !pathOK(name) || e != nil || len(raw) != 32 || strings.ToLower(h) != h {
			return nil, ErrState
		}
		expected[name] = h
		names = append(names, name)
	}
	sort.Strings(names)
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
			plan = nil
			err = errors.Join(err, e)
		}
	}()
	files := map[string][]byte{}
	total := 0
	for _, name := range names {
		data, e := readInput(ctx, s.files, name, int64(MaxInstallBytes-total), -1, expected[name])
		if e != nil {
			if errors.Is(e, fat.ErrMutation) {
				return nil, s.retire(e)
			}
			return nil, e
		}
		files[name] = data
		total += len(data)
	}
	return PrepareInstall(ctx, request, files)
}

func safeInstallRefusal(err error) bool {
	return !errors.Is(err, fat.ErrMutation) && (errors.Is(err, block.ErrReadOnly) || errors.Is(err, fat.ErrNoSpace) || errors.Is(err, fat.ErrCapacity) || errors.Is(err, fs.ErrPermission))
}

// Install serializes with retention and containment under the exact active SD
// lease. The trusted caller must independently admit the prepared selection to
// its platform before this operation. The bounded JSON record is content only,
// and is published last. No existing file is overwritten or repaired.
func (s *Store) Install(ctx context.Context, plan *InstallPlan, record []byte) (err error) {
	trimmed := bytes.TrimSpace(record)
	if s == nil || ctx == nil || plan == nil || plan.selection == nil || len(record) < 1 || len(record) > MaxInstallRecordBytes || len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(record) {
		return ErrState
	}
	record = bytes.Clone(record)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.observe(); err != nil {
		return err
	}
	if !s.last.Active || !s.last.Prepared {
		return ErrState
	}
	defer func() {
		if e := s.observe(); e != nil {
			err = errors.Join(err, e)
		}
	}()
	writer, ok := s.files.(installWriter)
	if !ok {
		return block.ErrReadOnly
	}
	names := make([]string, 0, len(plan.files))
	for name := range plan.files {
		names = append(names, name)
	}
	sort.Strings(names)
	recordName := InstallCatalogRoot + "/" + blobHash(record) + ".JSN"
	all := append(append([]string(nil), names...), recordName)
	missing := map[string]bool{}
	for _, name := range all {
		data := plan.files[name]
		if name == recordName {
			data = record
		}
		_, e := readInput(ctx, s.files, name, int64(len(data)), int64(len(data)), blobHash(data))
		if errors.Is(e, fat.ErrMutation) {
			return s.retire(e)
		}
		if errors.Is(e, fs.ErrNotExist) {
			missing[name] = true
			continue
		}
		if e != nil {
			return errors.Join(ErrInstallConflict, e)
		}
	}
	dirs := []string{"FES", InstallContentRoot, InstallCatalogRoot}
	missingDirs := map[string]bool{}
	for _, name := range dirs {
		if err = ctx.Err(); err != nil {
			return err
		}
		f, e := s.files.Open(name)
		if errors.Is(e, fat.ErrMutation) {
			return s.retire(e)
		}
		if errors.Is(e, fs.ErrNotExist) {
			missingDirs[name] = true
			continue
		}
		if e != nil {
			return e
		}
		info, e := f.Stat()
		if e == nil && info.IsDir() && name == InstallCatalogRoot {
			if dir, ok := f.(fs.ReadDirFile); ok {
				entries, readErr := dir.ReadDir(MaxInstallRecords + 1)
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					e = readErr
				} else if len(entries) > MaxInstallRecords || missing[recordName] && len(entries) == MaxInstallRecords {
					e = ErrState
				}
			} else {
				e = fs.ErrInvalid
			}
		}
		e = errors.Join(e, f.Close())
		if e != nil {
			return e
		}
		if !info.IsDir() {
			return ErrInstallConflict
		}
	}
	mutate := func(call func() error) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := s.observe(); e != nil {
			return e
		}
		e := call()
		if observed := s.observe(); observed != nil {
			return errors.Join(e, observed)
		}
		if e != nil && !safeInstallRefusal(e) {
			return s.retire(e)
		}
		return e
	}
	for _, name := range dirs {
		if missingDirs[name] {
			if err = mutate(func() error { return writer.Mkdir(ctx, name) }); err != nil {
				return err
			}
		}
	}
	for _, name := range names {
		if missing[name] {
			if err = mutate(func() error { return writer.CreateFile(ctx, name, plan.files[name]) }); err != nil {
				return err
			}
		}
		if _, err = readInput(ctx, s.files, name, int64(len(plan.files[name])), int64(len(plan.files[name])), blobHash(plan.files[name])); err != nil {
			return s.retire(err)
		}
	}
	// Record publication comes only after all copied content has fresh readback.
	if checked, ok := s.files.(interface{ Check(context.Context) error }); ok {
		if err = mutate(func() error { return checked.Check(ctx) }); err != nil {
			return s.retire(err)
		}
	}
	if missing[recordName] {
		if err = mutate(func() error { return writer.CreateFile(ctx, recordName, record) }); err != nil {
			return err
		}
	}
	if _, err = readInput(ctx, s.files, recordName, MaxInstallRecordBytes, int64(len(record)), blobHash(record)); err != nil {
		return s.retire(err)
	}
	if checked, ok := s.files.(interface{ Check(context.Context) error }); ok {
		if err = mutate(func() error { return checked.Check(ctx) }); err != nil {
			return s.retire(err)
		}
	}
	return nil
}

// ReadInstallRecords runs at startup before menu construction. Records are
// immutable, hash-named content; the frontend must validate their own schema,
// duplicate IDs and every selected package again before activation.
func (s *Store) ReadInstallRecords(ctx context.Context) (records [][]byte, err error) {
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
			records = nil
			err = errors.Join(err, e)
		}
	}()
	f, err := s.files.Open(InstallCatalogRoot)
	if errors.Is(err, fat.ErrMutation) {
		return nil, s.retire(err)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := f.Close(); e != nil {
			records = nil
			err = errors.Join(err, e)
		}
	}()
	dir, ok := f.(fs.ReadDirFile)
	if !ok {
		return nil, fs.ErrInvalid
	}
	entries, err := dir.ReadDir(MaxInstallRecords + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > MaxInstallRecords {
		return nil, ErrState
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || len(name) != 68 || !strings.HasSuffix(name, ".JSN") || !digest(name[:64]) {
			return nil, ErrState
		}
		data, e := readInput(ctx, s.files, InstallCatalogRoot+"/"+name, MaxInstallRecordBytes, -1, name[:64])
		if e != nil {
			return nil, e
		}
		records = append(records, data)
	}
	return records, ctx.Err()
}
