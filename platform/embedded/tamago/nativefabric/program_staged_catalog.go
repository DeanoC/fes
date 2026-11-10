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
	if !b.operation.TryLock() {
		return ErrProgramBusy
	}
	defer b.operation.Unlock()
	if b.service.Status().State != ProgramReady {
		return ErrProgramSession
	}
	b.viewMu.Lock()
	defer b.viewMu.Unlock()
	if b.staged != nil {
		return ErrProgramSession
	}
	for _, entry := range b.entries {
		if entry.ID == plan.entry.ID && (entry.Title != plan.entry.Title || !sameProgramRequest(entry.Request, plan.entry.Request)) {
			return ErrProgramSession
		}
	}
	b.staged = plan
	return nil
}

func (b *ProgramBackend) stagedCatalogRequest(r *http.Request) (response *http.Response, handled bool, err error) {
	path := r.URL.Path
	if path != "/api/v1/core-catalog" && path != "/api/v1/core-catalog/install" && !(strings.HasPrefix(path, "/api/v1/core-catalog/") && strings.HasSuffix(path, "/setup")) {
		return nil, false, nil
	}
	handled = true
	b.viewMu.RLock()
	plan := b.staged
	sourceError := b.stagedError
	var entry *ProgramEntry
	if plan != nil {
		for _, e := range b.entries {
			if e.ID == plan.entry.ID {
				copy := e
				entry = &copy
				break
			}
		}
	}
	b.viewMu.RUnlock()
	if sourceError != nil {
		response, err = programError(r, 422, "STAGED_SOURCE_INVALID", sourceError.Error())
		return
	}
	var ref hostclient.CoreReference
	if plan != nil {
		inspection := plan.content.Selection().Inspection()
		ref = hostclient.CoreReference{LibrarySourceID: nativeLibraryID, SourceID: stagedSourceID, CoreID: inspection.Descriptor.Core.ID, PackageID: plan.entry.Request.PackageID}
	}
	if path == "/api/v1/core-catalog" && r.Method == http.MethodGet {
		rows := []hostclient.AvailableCore{}
		if plan != nil {
			state := "available"
			if entry != nil {
				state = "installed"
			}
			rows = append(rows, hostclient.AvailableCore{CoreReference: ref, Label: plan.entry.Title, System: "native", Standing: "experimental", ArtifactState: state})
		}
		response, err = programResponse(r, 200, map[string]any{"cores": rows})
		return
	}
	if plan == nil {
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
		if e = decodeCompleteRecord(append(bytes.TrimSpace(data), '\n'), 4096, &requested); e != nil || requested.SourceID != ref.SourceID || requested.CoreID != ref.CoreID || requested.PackageID != ref.PackageID {
			response, err = programError(r, 400, "INVALID_REQUEST", "selected staged system differs")
			return
		}
		if e = b.Install(r.Context(), b.service.Status().Generation, plan); e != nil {
			response, err = programError(r, 409, "INSTALL_REFUSED", e.Error())
			return
		}
		response, err = programResponse(r, 200, map[string]string{"package_id": ref.PackageID})
		return
	}
	if r.Method == http.MethodGet && path == "/api/v1/core-catalog/"+ref.CoreID+"/setup" {
		q := r.URL.Query()
		if len(q) != 2 || len(q["source_id"]) != 1 || len(q["package_id"]) != 1 || q.Get("source_id") != ref.SourceID || q.Get("package_id") != ref.PackageID || entry == nil {
			response, err = programError(r, 409, "NOT_INSTALLED", "install the selected system first")
			return
		}
		setup := hostclient.CoreSetup{CoreReference: ref, Descriptor: plan.content.Selection().Inspection().Descriptor,
			Entries: []hostclient.CoreEntry{{GameID: entry.ID, Title: entry.Title, CoreID: ref.CoreID, PackageID: ref.PackageID}}}
		response, err = programResponse(r, 200, setup)
		return
	}
	response, err = programError(r, 404, "UNSUPPORTED", "staged operation unavailable")
	return
}
