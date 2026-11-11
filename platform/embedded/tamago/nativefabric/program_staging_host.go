//go:build !tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

type stagedExportReader struct {
	ctx     context.Context
	r       io.Reader
	stalled int
}

func (r *stagedExportReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	p = p[:min(len(p), 32768)]
	n, err := r.r.Read(p)
	if n < 0 || n > len(p) {
		return 0, ErrProgramSession
	}
	if n == 0 && err == nil {
		r.stalled++
		if r.stalled >= 100 {
			return 0, io.ErrNoProgress
		}
	} else {
		r.stalled = 0
	}
	return n, err
}
func readStagedExport(ctx context.Context, files fs.FS, name string, limit int) (data []byte, err error) {
	if name == "." || !fs.ValidPath(name) || limit < 1 {
		return nil, ErrProgramSession
	}
	f, err := files.Open(name)
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
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > int64(limit) {
		return nil, ErrProgramSession
	}
	data, err = io.ReadAll(io.LimitReader(&stagedExportReader{ctx: ctx, r: f}, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit || int64(len(data)) != info.Size() {
		return nil, ErrProgramSession
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

// BuildStagedCatalog creates private host-side installer input from selected
// export entries. It validates archive/ROM content but grants no platform or SD
// authority. The output has an empty bootstrap library and explicit Install
// catalog; matched boot/menu artifacts must be supplied independently.
func BuildStagedCatalog(ctx context.Context, files fs.FS, ids []string) (map[string][]byte, error) {
	if ctx == nil || files == nil || len(ids) < 1 || len(ids) > packagestore.MaxInstallRecords {
		return nil, ErrProgramSession
	}
	choices, err := ReadProgramSelections(ctx, files, "FES/CORESEL.JSN")
	if err != nil {
		return nil, err
	}
	entries, err := ReadProgramCatalog(ctx, files, "FES/UI.JSON", choices)
	if err != nil {
		return nil, err
	}
	byID := map[string]ProgramEntry{}
	for _, entry := range entries {
		if !validProgramEntry(entry) {
			return nil, ErrProgramSession
		}
		if _, exists := byID[entry.ID]; exists {
			return nil, ErrProgramSession
		}
		byID[entry.ID] = entry
	}
	result := map[string][]byte{}
	catalog := stagedProgramCatalog{Format: 1, Scope: "nano-staged-program-catalog-v1"}
	seenIDs, seenPackages := map[string]bool{}, map[string]bool{}
	remaining := packagestore.MaxInstallBytes
	for _, id := range ids {
		entry, ok := byID[id]
		if !ok || seenIDs[id] || seenPackages[entry.Request.PackageID] {
			return nil, ErrProgramSession
		}
		seenIDs[id], seenPackages[entry.Request.PackageID] = true, true
		r := entry.Request
		choice := programChoice{Path: r.Path, PackageID: r.PackageID, MetadataPath: r.MetadataPath, MetadataSHA256: r.MetadataSHA256, ROMReceiptSHA256: r.ReceiptSHA256}
		names := []string{r.Path, r.MetadataPath}
		for _, source := range r.Sources {
			choice.Sources = append(choice.Sources, programROMSource{source.ID, source.Role, source.Path, source.SHA256})
			names = append(names, source.Path)
		}
		if r.Tape != (packagestore.TapeRequest{}) {
			tape := r.Tape
			choice.Tape = &tape
			names = append(names, tape.Path)
		}
		upload := map[string][]byte{}
		sums := map[string]string{}
		total := 0
		for _, name := range names {
			if _, exists := upload[name]; exists {
				continue
			}
			switch name {
			case "FES/CORESEL.JSN", "FES/UI.JSON", "FES/INSTALL.JSN", "FES/MENU.FCP", "FES/MENU.JSN", "FES/MENUSEL.JSN":
				return nil, ErrProgramSession
			}
			data, err := readStagedExport(ctx, files, name, remaining-total)
			if err != nil {
				return nil, err
			}
			total += len(data)
			if old, exists := result[name]; exists && !bytes.Equal(old, data) {
				return nil, ErrProgramSession
			}
			upload[name], result[name] = data, data
			sums[name] = fmt.Sprintf("%x", sha256.Sum256(data))
		}
		plan, err := packagestore.PrepareInstall(ctx, r, upload)
		if err != nil {
			return nil, err
		}
		remaining -= plan.ContentBytes()
		catalog.Programs = append(catalog.Programs, stagedProgramRecord{entry.ID, entry.Title, choice, sums})
	}
	raw, err := canonicalProgramRecord(catalog)
	if err != nil {
		return nil, err
	}
	if len(raw) > packagestore.MaxInstallRecordBytes {
		return nil, ErrProgramSession
	}
	result["FES/INSTALL.JSN"] = raw
	result["FES/CORESEL.JSN"], err = canonicalProgramRecord(programChoices{1, "nano-program-selection-v1", []programChoice{}})
	if err != nil {
		return nil, err
	}
	result["FES/UI.JSON"] = []byte("{\"entries\":[],\"format\":1,\"scope\":\"nano-tenfoot-catalog-v1\"}\n")
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
