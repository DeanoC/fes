// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

type installedProgramRecord struct {
	Format  int           `json:"format"`
	Scope   string        `json:"scope"`
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Program programChoice `json:"program"`
}

// ProgramInstall is a private content snapshot admitted to one immutable loader.
// It supplies no SD, firmware, frame or activation permission.
type ProgramInstall struct {
	loader  *ProgramLoader
	content *packagestore.InstallPlan
	entry   ProgramEntry
	record  []byte
}

func validProgramEntry(e ProgramEntry) bool {
	return protocol.ValidateGameID(e.ID) == nil && e.Title != "" && len(e.Title) <= 256 && nonzeroDigest(e.Request.PackageID)
}

// PrepareInstall can run while a core owns the hardware: it uses RAM and the
// compiled compatibility maps only. There is no source filesystem or firmware IO.
func (b *ProgramBackend) PrepareInstall(ctx context.Context, id, title string, request packagestore.Request, files map[string][]byte) (*ProgramInstall, error) {
	if b == nil || b.service == nil || b.service.state == nil || ctx == nil {
		return nil, ErrProgramSession
	}
	state := b.service.Status().State
	if state == ProgramParked || state == ProgramRebootRequired {
		return nil, ErrProgramRetired
	}
	entry := ProgramEntry{ID: id, Title: title, Request: request}
	if !validProgramEntry(entry) {
		return nil, ErrProgramSession
	}
	content, err := packagestore.PrepareInstall(ctx, request, files)
	if err != nil {
		return nil, err
	}
	return b.prepareInstallContent(ctx, id, title, content)
}

func (b *ProgramBackend) prepareInstallContent(ctx context.Context, id, title string, content *packagestore.InstallPlan) (*ProgramInstall, error) {
	if b == nil || b.service == nil || b.service.state == nil || ctx == nil || content == nil {
		return nil, ErrProgramSession
	}
	state := b.service.Status().State
	if state == ProgramParked || state == ProgramRebootRequired {
		return nil, ErrProgramRetired
	}
	entry := ProgramEntry{ID: id, Title: title, Request: content.Request()}
	if !validProgramEntry(entry) {
		return nil, ErrProgramSession
	}
	l := b.service.state.loader
	checked, err := l.Preflight(content.Selection())
	if err != nil {
		return nil, err
	}
	if checked.state.program == nil || checked.state.program.role == programRoleMenu {
		return nil, ErrProgramSession
	}
	request := content.Request()
	if len(request.Parts) != 0 {
		return nil, ErrProgramSession
	}
	choice := programChoice{Path: request.Path, PackageID: request.PackageID, MetadataPath: request.MetadataPath, MetadataSHA256: request.MetadataSHA256, ROMReceiptSHA256: request.ReceiptSHA256}
	for _, s := range request.Sources {
		choice.Sources = append(choice.Sources, programROMSource{ID: s.ID, Role: s.Role, Path: s.Path, SHA256: s.SHA256})
	}
	if request.Tape != (packagestore.TapeRequest{}) {
		tape := request.Tape
		choice.Tape = &tape
	}
	if _, err = requestForProgramChoice(choice); err != nil {
		return nil, err
	}
	record, err := canonicalProgramRecord(installedProgramRecord{1, "nano-installed-program-v1", id, title, choice})
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	entry.Request = request
	return &ProgramInstall{loader: l, content: content, entry: entry, record: record}, nil
}

// Install waits for a Ready menu, uses the existing input pause and owned menu
// return, and advertises the entry only after storage readback and menu restore.
// This is a trusted in-process API, not a network listener or upload protocol.
func (b *ProgramBackend) Install(ctx context.Context, generation uint64, plan *ProgramInstall) error {
	if b == nil || b.service == nil || ctx == nil || plan == nil || plan.loader != b.service.state.loader || plan.content == nil {
		return ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.operation.TryLock() {
		return ErrProgramBusy
	}
	defer b.operation.Unlock()
	defer b.publish()
	view := b.service.Status()
	if view.State != ProgramReady || view.Generation != generation || b.inputFault != nil || len(b.entries) >= packagestore.MaxInstallRecords {
		return ErrProgramSession
	}
	for _, entry := range b.entries {
		if entry.ID == plan.entry.ID {
			return errors.New("native installer: catalog ID already exists")
		}
	}
	if err := b.withInputPaused(ctx, func() error { return b.service.install(ctx, generation, plan) }); err != nil {
		return err
	}
	b.viewMu.Lock()
	b.entries = append(b.entries, plan.entry)
	b.viewMu.Unlock()
	return nil
}

func (s *ProgramService) install(ctx context.Context, generation uint64, plan *ProgramInstall) (result error) {
	p, view, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer p.operation.Unlock()
	if view.State != ProgramReady || view.Generation != generation || generation > math.MaxUint64-2 || plan == nil || plan.loader != p.loader || plan.content == nil {
		return ErrProgramSession
	}
	if err := p.pauseNetwork(ctx); err != nil {
		return err
	}
	defer p.finishNetwork(&result)
	work, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	view.State = ProgramLoading
	view.Error = ""
	p.publish(view)
	if p.menu != nil {
		if err = p.closeMenu(true); err != nil {
			return p.fail(err)
		}
	}
	view.Generation++
	p.publish(view)
	err = p.checkpointPreferences(work)
	if err == nil {
		err = p.store.Install(work, plan.content, plan.record)
	}
	if _, storageErr := p.store.ActiveReceipt(); storageErr != nil {
		return p.fail(errors.Join(err, storageErr))
	}
	if p.menu != nil {
		if restoreErr := p.restoreMenu(); restoreErr != nil {
			return p.fail(errors.Join(err, restoreErr))
		}
	}
	view.State = ProgramReady
	if err != nil {
		view.Error = err.Error()
	}
	p.publish(view)
	return err
}

func installedPath(name, expected string) bool {
	prefix := packagestore.InstallContentRoot + "/"
	if !strings.HasPrefix(name, prefix) || len(name) != len(prefix)+68 || !strings.HasSuffix(name, ".BIN") {
		return false
	}
	digest := name[len(prefix) : len(name)-4]
	return nonzeroDigest(digest) && (expected == "" || digest == expected)
}

// ReadInstalledPrograms merges immutable records before menu startup. This is
// catalog discovery only; every launch still retains and admits its package.
func ReadInstalledPrograms(ctx context.Context, store *packagestore.Store, initial []ProgramEntry) ([]ProgramEntry, error) {
	if ctx == nil || store == nil || len(initial) > packagestore.MaxInstallRecords {
		return nil, ErrProgramSession
	}
	entries := make([]ProgramEntry, 0, len(initial))
	seen := map[string]bool{}
	for _, e := range initial {
		if !validProgramEntry(e) || seen[e.ID] {
			return nil, ErrProgramSession
		}
		seen[e.ID] = true
		e.Request.Sources = append([]packagestore.Source(nil), e.Request.Sources...)
		e.Request.Parts = append([]packagestore.Part(nil), e.Request.Parts...)
		entries = append(entries, e)
	}
	records, err := store.ReadInstallRecords(ctx)
	if err != nil {
		return nil, err
	}
	if len(entries)+len(records) > packagestore.MaxInstallRecords {
		return nil, ErrProgramSession
	}
	for _, raw := range records {
		var r installedProgramRecord
		if err = decodeCompleteRecord(raw, packagestore.MaxInstallRecordBytes, &r); err != nil {
			return nil, err
		}
		request, e := requestForProgramChoice(r.Program)
		if e != nil {
			return nil, e
		}
		entry := ProgramEntry{ID: r.ID, Title: r.Title, Request: request}
		if r.Format != 1 || r.Scope != "nano-installed-program-v1" || !validProgramEntry(entry) || seen[r.ID] || !installedPath(request.Path, "") || !installedPath(request.MetadataPath, request.MetadataSHA256) {
			return nil, ErrProgramSession
		}
		for _, source := range request.Sources {
			if !installedPath(source.Path, source.SHA256) {
				return nil, ErrProgramSession
			}
		}
		if request.Tape != (packagestore.TapeRequest{}) && !installedPath(request.Tape.Path, request.Tape.SHA256) {
			return nil, ErrProgramSession
		}
		seen[r.ID] = true
		entries = append(entries, entry)
	}
	return entries, ctx.Err()
}
