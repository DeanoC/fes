// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
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

// AttachStagedProgram publishes one already-retained source to the normal
// Tenfoot core-catalog API. It performs no filesystem or firmware IO and never
// installs automatically at boot. Repeated or conflicting attachment refuses.
func (b *ProgramBackend) AttachStagedProgram(ctx context.Context, staged *StagedProgram) error {
	plan, err := b.PrepareStagedInstall(ctx, staged)
	if err != nil {
		return err
	}
	return b.AttachProgramInstall(ctx, plan)
}

// AttachProgramInstall publishes one privately retained, platform-admitted plan
// through Tenfoot's existing Install/Setup API. A plan from Download needs no
// source connection after attachment. Publication never installs or activates
// content and grants no network ownership. SD-staged and downloaded plans share
// a bounded catalog with unique entry IDs and package references.
func (b *ProgramBackend) AttachProgramInstall(ctx context.Context, plan *ProgramInstall) error {
	if b == nil || b.service == nil || b.service.state == nil || ctx == nil || plan == nil || plan.content == nil || plan.loader != b.service.state.loader {
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
	if len(b.catalog) >= packagestore.MaxInstallRecords || plan.content.ContentBytes() > packagestore.MaxInstallBytes-b.catalogBytes {
		return ErrProgramSession
	}
	for _, retained := range b.catalog {
		if retained.entry.ID == plan.entry.ID || retained.entry.Request.PackageID == plan.entry.Request.PackageID {
			return ErrProgramSession
		}
	}
	for _, entry := range b.entries {
		if entry.ID == plan.entry.ID && (entry.Title != plan.entry.Title || !sameProgramRequest(entry.Request, plan.entry.Request)) {
			return ErrProgramSession
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.catalog = append(b.catalog, plan)
	b.catalogBytes += plan.content.ContentBytes()
	return nil
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
		item := programCatalogItem{plan: plan, ref: hostclient.CoreReference{LibrarySourceID: nativeLibraryID, SourceID: stagedSourceID, CoreID: inspection.Descriptor.Core.ID, PackageID: plan.entry.Request.PackageID}}
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
