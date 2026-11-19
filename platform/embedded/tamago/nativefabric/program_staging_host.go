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
	seenIDs := map[string]bool{}
	remaining := packagestore.MaxInstallBytes
	for _, id := range ids {
		entry, ok := byID[id]
		if !ok || seenIDs[id] {
			return nil, ErrProgramSession
		}
		seenIDs[id] = true
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
			case "FES/CORESEL.JSN", "FES/UI.JSON", "FES/INSTALL.JSN", "FES/MENU.FCP", "FES/MENU.JSN", "FES/MENUSEL.JSN", packagestore.PreferencesPath:
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

// BuildLauncherStaging extends the existing installer staging with one menu
// export and a fresh, preallocated preferences file. Use only on new media:
// these bootstrap files must never replace an installed library or settings.
// Boot FIT/SPL, compiled maps and trusted card plan remain separate inputs.
func BuildLauncherStaging(ctx context.Context, files, menu fs.FS, ids []string) (map[string][]byte, error) {
	if ctx == nil || menu == nil {
		return nil, ErrProgramSession
	}
	result, err := BuildStagedCatalog(ctx, files, ids)
	if err != nil {
		return nil, err
	}
	choices, err := ReadProgramSelections(ctx, menu, "FES/CORESEL.JSN")
	if err != nil {
		return nil, err
	}
	if len(choices) != 1 {
		return nil, errors.New("native launcher: exactly one menu export required")
	}
	r := choices[0]
	if len(r.Sources) != 0 || len(r.Parts) != 0 || r.Tape != (packagestore.TapeRequest{}) {
		return nil, errors.New("native launcher: menu cannot select ROM, parts or tape")
	}
	payload, err := readStagedExport(ctx, menu, r.Path, packagestore.MaxInstallBytes)
	if err != nil {
		return nil, err
	}
	metadata, err := readStagedExport(ctx, menu, r.MetadataPath, 16384)
	if err != nil {
		return nil, err
	}
	plan, err := packagestore.PrepareInstall(ctx, r, map[string][]byte{r.Path: payload, r.MetadataPath: metadata})
	if err != nil {
		return nil, err
	}
	var selected selectedMenuProgramRecord
	if err = decodeCompleteRecord(metadata, 16384, &selected); err != nil {
		return nil, err
	}
	d := plan.Selection().Inspection().Descriptor
	if selected.Format != 1 || selected.Scope != "nano-selected-menu-program-v1" || selected.PackageID != r.PackageID ||
		selected.BuildID != d.Build.ID || d.Format != 2 || d.Target.ProgrammingProfile != selected.Platform.ProgrammingProfile ||
		d.Target.Platform != selected.Platform.Platform || d.Target.Device != selected.Platform.Device {
		return nil, errors.New("native launcher: menu export metadata and package differ")
	}
	result["FES/MENU.FCP"], result["FES/MENU.JSN"] = payload, metadata
	result["FES/MENUSEL.JSN"], err = canonicalProgramRecord(programChoices{1, "nano-program-selection-v1", []programChoice{{
		Path: "FES/MENU.FCP", PackageID: r.PackageID, MetadataPath: "FES/MENU.JSN", MetadataSHA256: r.MetadataSHA256,
	}}})
	if err != nil {
		return nil, err
	}
	// FAT WriteFile preserves the allocated chain as the JSON size changes.
	// Reserve the entire existing preferences allowance before first boot.
	prefs := bytes.Repeat([]byte{' '}, packagestore.MaxPreferencesBytes)
	copy(prefs, []byte("{\"home\":\"library\",\"attract_enabled\":false}\n"))
	result[packagestore.PreferencesPath] = prefs
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
