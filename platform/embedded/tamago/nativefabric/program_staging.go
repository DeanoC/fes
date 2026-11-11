// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"io/fs"
)

// StagedProgram holds validated private RAM content, never an open SD handle.
// It grants no platform/SD permission. Read it before menu construction.
type StagedProgram struct {
	id, title string
	content   *packagestore.InstallPlan
}

// ReadInstallerBootstrap accepts an explicitly empty initial library. It reads
// only the two bounded manifests under the active store lease, then merges
// published install records. No dummy playable core is required.
func ReadInstallerBootstrap(ctx context.Context, store *packagestore.Store) ([]ProgramEntry, error) {
	if ctx == nil || store == nil {
		return nil, ErrProgramSession
	}
	raw, err := store.ReadStagingRecord(ctx, "FES/CORESEL.JSN")
	if err != nil {
		return nil, err
	}
	choices, err := decodeProgramSelectionMode(ctx, raw, true)
	if err != nil {
		return nil, err
	}
	raw, err = store.ReadStagingRecord(ctx, "FES/UI.JSON")
	if err != nil {
		return nil, err
	}
	entries, err := decodeProgramCatalogMode(raw, choices, true)
	if err != nil {
		return nil, err
	}
	return ReadInstalledPrograms(ctx, store, entries)
}

// ErrStagedCatalogAbsent denotes only a missing optional control record, never
// a missing member. The caller must still check storage ownership independently.
var ErrStagedCatalogAbsent = errors.New("native fabric: optional staged catalog absent")

type stagedProgramRecord struct {
	ID      string            `json:"id"`
	Title   string            `json:"title"`
	Program programChoice     `json:"program"`
	Files   map[string]string `json:"files"`
}

type stagedProgramCatalog struct {
	Format   int                   `json:"format"`
	Scope    string                `json:"scope"`
	Programs []stagedProgramRecord `json:"programs"`
}

// ReadStagedPrograms snapshots the entire optional source before menu ownership.
// It never returns a partial catalog. The 8 KiB control-record bound is unchanged;
// at most 32 unique entries/packages and 64 MiB of input content are admitted.
func ReadStagedPrograms(ctx context.Context, store *packagestore.Store, name string) ([]*StagedProgram, error) {
	if ctx == nil || store == nil {
		return nil, ErrProgramSession
	}
	raw, err := store.ReadStagingRecord(ctx, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrStagedCatalogAbsent
		}
		return nil, err
	}
	var catalog stagedProgramCatalog
	if err = decodeCompleteRecord(raw, packagestore.MaxInstallRecordBytes, &catalog); err != nil {
		return nil, err
	}
	if catalog.Format != 1 || catalog.Scope != "nano-staged-program-catalog-v1" || len(catalog.Programs) < 1 || len(catalog.Programs) > packagestore.MaxInstallRecords {
		return nil, ErrProgramSession
	}
	requests := make([]packagestore.Request, len(catalog.Programs))
	ids, packages := map[string]bool{}, map[string]bool{}
	for i, record := range catalog.Programs {
		request, err := requestForProgramChoice(record.Program)
		if err != nil {
			return nil, err
		}
		if !validProgramEntry(ProgramEntry{ID: record.ID, Title: record.Title, Request: request}) || ids[record.ID] || packages[request.PackageID] {
			return nil, ErrProgramSession
		}
		ids[record.ID], packages[request.PackageID] = true, true
		requests[i] = request
	}
	result := make([]*StagedProgram, 0, len(requests))
	remaining := packagestore.MaxInstallBytes
	for i, request := range requests {
		record := catalog.Programs[i]
		content, err := store.StageInstall(ctx, request, record.Files, remaining)
		if err != nil {
			return nil, err
		}
		remaining -= content.ContentBytes()
		result = append(result, &StagedProgram{record.ID, record.Title, content})
	}
	return result, nil
}

// PrepareStagedInstall applies the same immutable platform preflight as HTTP
// downloads and in-process uploads, without reopening the source filesystem.
func (b *ProgramBackend) PrepareStagedInstall(ctx context.Context, staged *StagedProgram) (*ProgramInstall, error) {
	if staged == nil {
		return nil, ErrProgramSession
	}
	return b.prepareInstallContent(ctx, staged.id, staged.title, staged.content)
}
