// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

const stagedSourceID = "native-staged"
const nativeLibraryID = "native"

// SetStagedSourceError quarantines rejected optional content while the healthy
// launch library remains available. It grants no recovery or retry authority.
func (b *ProgramBackend) SetStagedSourceError(err error) {
	b.viewMu.Lock()
	defer b.viewMu.Unlock()
	b.stagedError = err
}

func sameProgramRequest(a, b packagestore.Request) bool {
	if a.Path != b.Path || a.PackageID != b.PackageID || a.ReceiptSHA256 != b.ReceiptSHA256 || a.MetadataPath != b.MetadataPath || a.MetadataSHA256 != b.MetadataSHA256 || a.Tape != b.Tape || len(a.Sources) != len(b.Sources) || len(a.Parts) != len(b.Parts) {
		return false
	}
	for i := range a.Sources {
		if a.Sources[i] != b.Sources[i] {
			return false
		}
	}
	for i := range a.Parts {
		if a.Parts[i] != b.Parts[i] {
			return false
		}
	}
	return true
}

// AttachStagedPrograms preflights and publishes a retained startup catalog as
// one batch. Invalid later records cannot leave earlier sources advertised.
func (b *ProgramBackend) AttachStagedPrograms(ctx context.Context, staged []*StagedProgram) error {
	if ctx == nil || len(staged) < 1 || len(staged) > packagestore.MaxInstallRecords {
		return ErrProgramSession
	}
	plans := make([]*ProgramInstall, len(staged))
	for i, source := range staged {
		plan, err := b.PrepareStagedInstall(ctx, source)
		if err != nil {
			return err
		}
		plans[i] = plan
	}
	return b.AttachProgramInstalls(ctx, plans)
}

// AttachProgramInstall publishes one admitted download or in-process upload.
// Publication never installs content or grants network ownership.
func (b *ProgramBackend) AttachProgramInstall(ctx context.Context, plan *ProgramInstall) error {
	return b.AttachProgramInstalls(ctx, []*ProgramInstall{plan})
}

// AttachProgramInstalls atomically publishes retained, platform-admitted plans.
// The source connection/filesystem is no longer required. Every identity and
// aggregate bound is checked before changing the catalog.
func (b *ProgramBackend) AttachProgramInstalls(ctx context.Context, plans []*ProgramInstall) error {
	if b == nil || b.service == nil || b.service.state == nil || ctx == nil || len(plans) < 1 || len(plans) > packagestore.MaxInstallRecords {
		return ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !b.operation.TryLock() {
		return ErrProgramBusy
	}
	defer b.operation.Unlock()
	if b.service.Status().State != ProgramReady {
		return ErrProgramSession
	}
	b.viewMu.Lock()
	defer b.viewMu.Unlock()
	if len(plans) > packagestore.MaxInstallRecords-len(b.catalog) {
		return ErrProgramSession
	}
	ids := map[string]bool{}
	for _, retained := range b.catalog {
		ids[retained.entry.ID] = true
	}
	remaining := packagestore.MaxInstallBytes - b.catalogBytes
	for _, plan := range plans {
		if plan == nil || plan.content == nil || plan.loader != b.service.state.loader || ids[plan.entry.ID] || plan.content.ContentBytes() > remaining {
			return ErrProgramSession
		}
		ids[plan.entry.ID] = true
		remaining -= plan.content.ContentBytes()
		for _, entry := range b.entries {
			if entry.ID == plan.entry.ID && (entry.Title != plan.entry.Title || !sameProgramRequest(entry.Request, plan.entry.Request)) {
				return ErrProgramSession
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.catalog = append(b.catalog, plans...)
	b.catalogBytes = packagestore.MaxInstallBytes - remaining
	return nil
}

// The reference binds the complete prepared configuration, not just its FPGA
// package. Different ROM/tape receipts can legitimately share that package.
func (p *ProgramInstall) catalogSourceID() string {
	sum := sha256.Sum256(p.record)
	return stagedSourceID + ":" + hex.EncodeToString(sum[:])
}

type programCatalogItem struct {
	plan  *ProgramInstall
	ref   hostclient.CoreReference
	entry *ProgramEntry
}

func (b *ProgramBackend) catalogSnapshot() ([]programCatalogItem, error) {
	b.viewMu.RLock()
	defer b.viewMu.RUnlock()
	items := make([]programCatalogItem, 0, len(b.catalog))
	for _, plan := range b.catalog {
		inspection := plan.content.Selection().Inspection()
		item := programCatalogItem{plan: plan, ref: hostclient.CoreReference{LibrarySourceID: nativeLibraryID, SourceID: plan.catalogSourceID(), CoreID: inspection.Descriptor.Core.ID, PackageID: plan.entry.Request.PackageID}}
		for _, e := range b.entries {
			if e.ID == plan.entry.ID && e.Title == plan.entry.Title && sameProgramRequest(e.Request, plan.entry.Request) {
				copy := e
				item.entry = &copy
				break
			}
		}
		items = append(items, item)
	}
	return items, b.stagedError
}

func (b *ProgramBackend) stagedCatalogRequest(r *http.Request) (response *http.Response, handled bool, err error) {
	path := r.URL.Path
	if path != "/api/v1/core-catalog" && path != "/api/v1/core-catalog/install" && !(strings.HasPrefix(path, "/api/v1/core-catalog/") && strings.HasSuffix(path, "/setup")) {
		return nil, false, nil
	}
	handled = true
	items, sourceError := b.catalogSnapshot()
	if sourceError != nil {
		response, err = programError(r, 422, "STAGED_SOURCE_INVALID", sourceError.Error())
		return
	}
	if path == "/api/v1/core-catalog" && r.Method == http.MethodGet {
		rows := make([]hostclient.AvailableCore, 0, len(items))
		for _, item := range items {
			state := "available"
			if item.entry != nil {
				state = "installed"
			}
			rows = append(rows, hostclient.AvailableCore{CoreReference: item.ref, Label: item.plan.entry.Title, System: "native", Standing: "experimental", ArtifactState: state})
		}
		response, err = programResponse(r, 200, map[string]any{"cores": rows})
		return
	}
	if len(items) == 0 {
		response, err = programError(r, 404, "NOT_FOUND", "staged system unavailable")
		return
	}
	if path == "/api/v1/core-catalog/install" && r.Method == http.MethodPost {
		var data []byte
		var e error
		if r.Body != nil {
			data, e = io.ReadAll(io.LimitReader(r.Body, 4097))
		}
		if e != nil {
			return nil, true, e
		}
		var requested struct {
			SourceID  string `json:"source_id"`
			CoreID    string `json:"core_id"`
			PackageID string `json:"package_id"`
		}
		if len(data) > 4096 {
			response, err = programError(r, 400, "INVALID_REQUEST", "request too large")
			return
		}
		if e = decodeCompleteRecord(append(bytes.TrimSpace(data), '\n'), 4096, &requested); e != nil {
			response, err = programError(r, 400, "INVALID_REQUEST", "invalid selected system")
			return
		}
		for _, item := range items {
			ref := item.ref
			if requested.SourceID != ref.SourceID || requested.CoreID != ref.CoreID || requested.PackageID != ref.PackageID {
				continue
			}
			if e = b.Install(r.Context(), b.service.Status().Generation, item.plan); e != nil {
				response, err = programError(r, 409, "INSTALL_REFUSED", e.Error())
				return
			}
			response, err = programResponse(r, 200, map[string]string{"package_id": ref.PackageID})
			return
		}
		response, err = programError(r, 400, "INVALID_REQUEST", "selected staged system differs")
		return
	}
	if r.Method == http.MethodGet && strings.HasSuffix(path, "/setup") {
		q := r.URL.Query()
		if len(q) == 2 && len(q["source_id"]) == 1 && len(q["package_id"]) == 1 {
			for _, item := range items {
				ref := item.ref
				if path != "/api/v1/core-catalog/"+ref.CoreID+"/setup" || q.Get("source_id") != ref.SourceID || q.Get("package_id") != ref.PackageID || item.entry == nil {
					continue
				}
				setup := hostclient.CoreSetup{CoreReference: ref, Descriptor: item.plan.content.Selection().Inspection().Descriptor,
					Entries: []hostclient.CoreEntry{{GameID: item.entry.ID, Title: item.entry.Title, CoreID: ref.CoreID, PackageID: ref.PackageID}}}
				response, err = programResponse(r, 200, setup)
				return
			}
		}
		response, err = programError(r, 409, "NOT_INSTALLED", "install the selected system first")
		return
	}
	response, err = programError(r, 404, "UNSUPPORTED", "staged operation unavailable")
	return
}
