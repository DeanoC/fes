// SPDX-License-Identifier: GPL-3.0-or-later

// Package packagestore retains core packages and selected ROM inputs before
// containing their storage device. It performs no compatibility admission or
// FPGA operation; those remain the runtime's responsibility.
package packagestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"sync"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
	"github.com/DeanoC/misteross/expansion"
)

// Lease belongs exclusively to this store. LastReceipt is cached firmware
// evidence, not a fresh hardware observation. Actual FPGA transitions must
// also check the live firmware bridge owner/generation.
type Lease interface {
	LastReceipt() (sdcontrol.Receipt, bool)
	Quiesce() error
	Resume() error
}

// ReturnLease explicitly supports crossing a completed fabric return. The
// provider must check that return in secure firmware before releasing SD;
// cold-only leases have no automatic adoption path.
type ReturnLease interface {
	Lease
	ResumeAfterReturn(owner, generation uint64) error
}

type Source struct{ ID, Role, Path, SHA256 string }
type Part struct{ ID, Path, SHA256 string }
type Request struct {
	Path, PackageID, ReceiptSHA256 string
	MetadataPath, MetadataSHA256   string
	Sources                        []Source
	Parts                          []Part
	Tape                           TapeRequest
}

var (
	ErrRetired = errors.New("package store: storage ownership uncertain; lease retained")
	ErrState   = errors.New("package store: stale or invalid storage handoff")
)

type Store struct {
	mu       sync.Mutex
	files    fs.FS
	lease    Lease
	last     sdcontrol.Receipt
	returned sdcontrol.Receipt
	retired  bool
}

// Selection contains only privately retained archive/linker bytes. No open
// filesystem handle or mutable source slice escapes.
type Selection struct {
	store      *Store
	generation uint64
	archive    *corepackage.Archive
	linked     *corepackage.LinkedArchive
	metadata   []byte
	tape       []byte
}

// Metadata is a private copy of a bounded, explicitly selected attachment. Its
// digest is an integrity assertion; the runtime must validate its package and
// platform binding before it can influence a launch.
func (p *Selection) Metadata() []byte {
	if p == nil {
		return nil
	}
	return bytes.Clone(p.metadata)
}

func (p *Selection) Archive() *corepackage.Archive {
	if p == nil {
		return nil
	}
	return p.archive
}
func (p *Selection) Linked() *corepackage.LinkedArchive {
	if p == nil {
		return nil
	}
	return p.linked
}
func (p *Selection) Inspection() corepackage.Inspection {
	if p == nil || p.archive == nil {
		return corepackage.Inspection{}
	}
	return p.archive.Inspection()
}

// Handoff can only be produced after successful firmware containment. It is
// invalidated when this store resumes. Its receipt is not FPGA authority.
type Handoff struct {
	store     *Store
	selection *Selection
	receipt   sdcontrol.Receipt
	returned  sdcontrol.Receipt
}

// OwnsHandoff compares private store identity only. It does not validate a
// current lease, resume storage or grant fabric access.
func (s *Store) OwnsHandoff(h *Handoff) bool {
	return s != nil && h != nil && h.store == s && h.selection != nil
}

func (h *Handoff) Receipt() sdcontrol.Receipt {
	if h == nil {
		return sdcontrol.Receipt{}
	}
	return h.receipt
}

// ReturnedFrom identifies the closed fabric return that preceded this store's
// SD cycle. It is cached provenance, not a live bridge or activation grant.
func (h *Handoff) ReturnedFrom() sdcontrol.Receipt {
	if h == nil {
		return sdcontrol.Receipt{}
	}
	return h.returned
}
func (h *Handoff) Selection() (*Selection, error) {
	if h == nil || h.store == nil {
		return nil, ErrState
	}
	s := h.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return nil, ErrRetired
	}
	if s.last.Active || s.last != h.receipt || s.returned != h.returned || h.selection == nil {
		return nil, ErrState
	}
	if err := s.observe(); err != nil {
		return nil, err
	}
	return h.selection, nil
}

// SelectionAfterReturn verifies a fresh containment from the original store.
// Its private predecessor prevents an unrelated store with matching cached
// receipts from substituting a selection. Live fabric ownership is separate.
func (h *Handoff) SelectionAfterReturn(previous *Handoff, owner, generation uint64) (*Selection, error) {
	if h == nil || previous == nil || h.store == nil || h.store != previous.store || previous.selection == nil ||
		owner == 0 || generation == 0 || generation > ^uint64(0)-3 || previous.receipt.Owner != owner ||
		previous.receipt.Generation >= generation || h.returned != (sdcontrol.Receipt{Owner: owner, Generation: generation}) ||
		h.receipt != (sdcontrol.Receipt{Owner: owner, Generation: generation + 3}) {
		return nil, ErrState
	}
	return h.Selection()
}

func New(files fs.FS, lease Lease) (*Store, error) {
	if files == nil || lease == nil {
		return nil, ErrState
	}
	r, ok := lease.LastReceipt()
	if !ok || r.Owner == 0 || r.Generation == 0 || !r.Active || !r.Prepared {
		return nil, ErrState
	}
	return &Store{files: files, lease: lease, last: r}, nil
}

// ActiveReceipt checks this store's cached lease while it is usable. It makes
// no firmware call and grants no FPGA authority; transitions must independently
// compare fresh secure ownership. A changed lease permanently retires the store.
func (s *Store) ActiveReceipt() (sdcontrol.Receipt, error) {
	if s == nil {
		return sdcontrol.Receipt{}, ErrState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.observe(); err != nil {
		return sdcontrol.Receipt{}, err
	}
	if !s.last.Active || !s.last.Prepared {
		return sdcontrol.Receipt{}, ErrState
	}
	return s.last, nil
}

func (s *Store) retire(err error) error { s.retired = true; return errors.Join(ErrRetired, err) }
func (s *Store) observe() error {
	if s.retired {
		return ErrRetired
	}
	r, ok := s.lease.LastReceipt()
	if !ok || r != s.last {
		return s.retire(ErrState)
	}
	return nil
}
func digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return false
		}
	}
	return true
}
func pathOK(value string) bool { return value != "." && fs.ValidPath(value) }

func retainArchive(ctx context.Context, files fs.FS, path string) (archive *corepackage.Archive, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	f, err := files.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := f.Close(); e != nil {
			archive = nil
			err = errors.Join(err, e)
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("package store: archive is not a regular file")
	}
	return corepackage.RetainArchive(ctx, info.Size(), f)
}
func readInput(ctx context.Context, files fs.FS, path string, limit int64, wantSize int64, wantDigest string) (data []byte, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	f, err := files.Open(path)
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
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit || wantSize >= 0 && info.Size() != wantSize {
		return nil, fmt.Errorf("package store: selected input size differs or exceeds its bound")
	}
	data = make([]byte, int(info.Size())+1)
	sum := sha256.New()
	offset, stalled := 0, 0
	for offset < len(data) {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		chunk := data[offset:min(offset+(32<<10), len(data))]
		n, e := f.Read(chunk)
		if n < 0 || n > len(chunk) {
			return nil, errors.New("package store: invalid reader count")
		}
		sum.Write(chunk[:n])
		offset += n
		runtime.Gosched()
		if e != nil && !errors.Is(e, io.EOF) {
			return nil, e
		}
		if n == 0 {
			stalled++
			if stalled >= 100 {
				return nil, io.ErrNoProgress
			}
		} else {
			stalled = 0
		}
		if errors.Is(e, io.EOF) {
			break
		}
	}
	if int64(offset) != info.Size() {
		return nil, errors.New("package store: selected input length changed")
	}
	data = data[:offset]
	if hex.EncodeToString(sum.Sum(nil)) != wantDigest {
		return nil, errors.New("package store: selected input digest differs")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

// Retain validates a package and, where its existing format supports it,
// links exactly selected ROM/part inputs. Rejected content leaves a healthy
// active SD lease alone. Device failure retires the store without auto-stop.
func (s *Store) Retain(ctx context.Context, request Request) (selection *Selection, err error) {
	if s == nil || ctx == nil {
		return nil, ErrState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.observe(); err != nil {
		return nil, err
	}
	if !s.last.Active || !s.last.Prepared {
		return nil, ErrState
	}
	if !pathOK(request.Path) || !digest(request.PackageID) || len(request.Sources) > 2 || len(request.Parts) > 2 {
		return nil, ErrState
	}
	if (request.MetadataPath == "") != (request.MetadataSHA256 == "") ||
		request.MetadataPath != "" && (!pathOK(request.MetadataPath) || !digest(request.MetadataSHA256)) {
		return nil, ErrState
	}
	if err := request.Tape.Validate(); err != nil {
		return nil, err
	}
	for _, v := range request.Sources {
		if !pathOK(v.Path) || !digest(v.SHA256) {
			return nil, ErrState
		}
	}
	for _, v := range request.Parts {
		if !pathOK(v.Path) || !digest(v.ID) || !digest(v.SHA256) {
			return nil, ErrState
		}
	}
	defer func() {
		if e := s.observe(); e != nil {
			selection = nil
			err = errors.Join(err, e)
		}
	}()
	archive, err := retainArchive(ctx, s.files, request.Path)
	if err != nil {
		return nil, err
	}
	inspection := archive.Inspection()
	if inspection.PackageID != request.PackageID {
		return nil, errors.New("package store: retained package differs from selection")
	}
	selection = &Selection{store: s, generation: s.last.Generation, archive: archive}
	if request.MetadataPath != "" {
		selection.metadata, err = readInput(ctx, s.files, request.MetadataPath, 16<<10, -1, request.MetadataSHA256)
		if err != nil {
			return nil, err
		}
	}
	if request.Tape != (TapeRequest{}) {
		selection.tape, err = readInput(ctx, s.files, request.Tape.Path, MaxRetainedTapeBytes, request.Tape.Size, request.Tape.SHA256)
		if err != nil {
			return nil, err
		}
	}
	if inspection.Descriptor.Format == 2 {
		if len(request.Sources) != 0 || len(request.Parts) != 0 || request.ReceiptSHA256 != "" {
			return nil, ErrState
		}
		return selection, nil
	}
	if !digest(request.ReceiptSHA256) {
		return nil, errors.New("package store: expected ROM activation receipt required")
	}
	d := inspection.Descriptor
	requirements := d.ROMs
	if d.Format == 3 && d.ROM != nil {
		requirements = []corepackage.ROMRequirement{{ID: d.ROM.ID, Role: d.ROM.Role, SourceSize: d.ROM.SourceSize}}
	}
	if len(requirements) != len(request.Sources) {
		return nil, errors.New("package store: ROM selection count differs")
	}
	sources := make([]corepackage.ROMSource, len(requirements))
	for i, r := range requirements {
		v := request.Sources[i]
		if v.ID != r.ID || v.Role != r.Role {
			return nil, errors.New("package store: ROM selection identity or order differs")
		}
		data, e := readInput(ctx, s.files, v.Path, r.SourceSize, r.SourceSize, v.SHA256)
		if e != nil {
			return nil, e
		}
		sources[i] = corepackage.ROMSource{ID: v.ID, Role: v.Role, Data: data}
	}
	parts := make([]corepackage.PartInput, len(request.Parts))
	for i, p := range request.Parts {
		data, e := readInput(ctx, s.files, p.Path, expansion.MaxArchiveBytes, -1, p.SHA256)
		if e != nil {
			return nil, e
		}
		parts[i] = corepackage.PartInput{ID: p.ID, Archive: data}
	}
	selection.linked, err = archive.LinkROMs(ctx, sources, parts...)
	if err != nil {
		return nil, err
	}
	if selection.linked.ReceiptSHA256() != request.ReceiptSHA256 {
		return nil, errors.New("package store: ROM activation receipt differs")
	}
	return selection, nil
}

// Contain serializes with every retain, closes no active file handle, and
// publishes a handoff only after firmware independently drains/resets SD.
func (s *Store) Contain(selection *Selection) (*Handoff, error) {
	if s == nil {
		return nil, ErrState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return nil, ErrRetired
	}
	if !s.last.Active || selection == nil || selection.store != s || selection.generation != s.last.Generation {
		return nil, ErrState
	}
	if err := s.observe(); err != nil {
		return nil, err
	}
	before := s.last
	if before.Generation == ^uint64(0) {
		return nil, s.retire(ErrState)
	}
	if err := s.lease.Quiesce(); err != nil {
		return nil, s.retire(err)
	}
	after, ok := s.lease.LastReceipt()
	if !ok || after.Owner != before.Owner || after.Generation != before.Generation+1 || after.Active || after.Prepared {
		return nil, s.retire(ErrState)
	}
	s.last = after
	return &Handoff{store: s, selection: selection, receipt: after, returned: s.returned}, nil
}

// Resume explicitly reopens the same controller/card. Previous storage
// handoffs and selections cannot authorize another containment cycle.
func (s *Store) Resume() error {
	if s == nil {
		return ErrState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.observe(); err != nil {
		return err
	}
	if s.last.Active {
		return ErrState
	}
	before := s.last
	if before.Generation > ^uint64(0)-2 {
		return s.retire(ErrState)
	}
	if err := s.lease.Resume(); err != nil {
		return s.retire(err)
	}
	after, ok := s.lease.LastReceipt()
	if !ok || after.Owner != before.Owner || after.Generation != before.Generation+2 || !after.Active || !after.Prepared {
		return s.retire(ErrState)
	}
	s.last = after
	return nil
}

// ResumeAfterReturn explicitly changes from retained-package/core ownership
// to a new SD lease. The exact returned owner/generation must come from the
// runtime's successful core closure. The real lease independently validates it
// in firmware and rediscovers the same card before any filesystem access.
func (s *Store) ResumeAfterReturn(owner, generation uint64) error {
	if s == nil {
		return ErrState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumeAfterReturn(owner, generation)
}

// ResumeAfterReturn reopens only the store which issued this still-current
// containment handoff. A copy shares its store; forged and stale handoffs
// cannot select another filesystem. Firmware independently checks the return.
func (h *Handoff) ResumeAfterReturn(owner, generation uint64) error {
	if h == nil || h.store == nil {
		return ErrState
	}
	s := h.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if h.selection == nil || s.last != h.receipt || s.returned != h.returned {
		return ErrState
	}
	return s.resumeAfterReturn(owner, generation)
}

func (s *Store) resumeAfterReturn(owner, generation uint64) error {
	if err := s.observe(); err != nil {
		return err
	}
	if s.last.Active || s.last.Prepared || owner != s.last.Owner ||
		generation <= s.last.Generation || generation > ^uint64(0)-3 {
		return ErrState
	}
	lease, supported := s.lease.(ReturnLease)
	if !supported {
		return ErrState
	}
	if err := lease.ResumeAfterReturn(owner, generation); err != nil {
		return s.retire(err)
	}
	after, valid := lease.LastReceipt()
	if !valid || after != (sdcontrol.Receipt{Owner: owner, Generation: generation + 2, Active: true, Prepared: true}) {
		return s.retire(ErrState)
	}
	s.last = after
	s.returned = sdcontrol.Receipt{Owner: owner, Generation: generation}
	return nil
}
