// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
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

func ReadStagedProgram(ctx context.Context, store *packagestore.Store, name string) (*StagedProgram, error) {
	if ctx == nil || store == nil {
		return nil, ErrProgramSession
	}
	raw, err := store.ReadStagingRecord(ctx, name)
	if err != nil {
		return nil, err
	}
	var record struct {
		Format  int               `json:"format"`
		Scope   string            `json:"scope"`
		ID      string            `json:"id"`
		Title   string            `json:"title"`
		Program programChoice     `json:"program"`
		Files   map[string]string `json:"files"`
	}
	if err = decodeCompleteRecord(raw, packagestore.MaxInstallRecordBytes, &record); err != nil {
		return nil, err
	}
	request, err := requestForProgramChoice(record.Program)
	if err != nil {
		return nil, err
	}
	if record.Format != 1 || record.Scope != "nano-staged-program-install-v1" || !validProgramEntry(ProgramEntry{ID: record.ID, Title: record.Title, Request: request}) {
		return nil, ErrProgramSession
	}
	content, err := store.StageInstall(ctx, request, record.Files)
	if err != nil {
		return nil, err
	}
	return &StagedProgram{record.ID, record.Title, content}, nil
}

// PrepareStagedInstall applies the same immutable platform preflight as HTTP
// downloads and in-process uploads, without reopening the source filesystem.
func (b *ProgramBackend) PrepareStagedInstall(ctx context.Context, staged *StagedProgram) (*ProgramInstall, error) {
	if staged == nil {
		return nil, ErrProgramSession
	}
	return b.prepareInstallContent(ctx, staged.id, staged.title, staged.content)
}
