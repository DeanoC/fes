package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"regexp"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/internal/kitpackages"
	"github.com/DeanoC/FogCast/protocol"
)

// CorePackageInspector queries runtime compatibility without activating a core.
type CorePackageInspector func(context.Context, int64, io.Reader) (protocol.CoreInspection, *protocol.APIError)

func WithCorePackages(store *kitpackages.Store, inspect CorePackageInspector) Option {
	return func(options *serverOptions) {
		options.corePackagesEnabled = true
		options.corePackages = store
		options.corePackageInspect = inspect
	}
}

var (
	kitCoreIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
	kitPackageIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func registerCorePackageRoutes(mux *http.ServeMux, token string, store *kitpackages.Store, inspect CorePackageInspector) {
	mux.Handle("GET /v1/core-packages", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			corePackageStoreUnavailable(w)
			return
		}
		packages, err := store.List(r.Context())
		if err != nil {
			corePackageError(w, err)
			return
		}
		selections, err := store.Selections(r.Context())
		if err != nil {
			corePackageError(w, err)
			return
		}
		result := protocol.KitCorePackages{Packages: packages, Selections: make([]protocol.KitCoreSelection, 0, len(selections))}
		if result.Packages == nil {
			result.Packages = []corepackage.Inspection{}
		}
		listed := make(map[string]bool, len(result.Packages))
		for _, value := range result.Packages {
			listed[value.PackageID] = true
		}
		for _, selection := range selections {
			// Installation may complete between List and Selections. Include an
			// independently validated selected object before publishing the inventory.
			if !listed[selection.PackageID] {
				value, _, err := store.Read(r.Context(), selection.PackageID)
				if err != nil {
					corePackageError(w, err)
					return
				}
				result.Packages = append(result.Packages, value)
				listed[selection.PackageID] = true
			}
			result.Selections = append(result.Selections, protocol.KitCoreSelection{CoreID: selection.CoreID, PackageID: selection.PackageID})
		}
		writeJSON(w, http.StatusOK, result)
	})))
	mux.Handle("GET /v1/core-selections", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			corePackageStoreUnavailable(w)
			return
		}
		selections, err := store.Selections(r.Context())
		if err != nil {
			corePackageError(w, err)
			return
		}
		result := make([]protocol.KitCoreSelection, 0, len(selections))
		for _, selection := range selections {
			result = append(result, protocol.KitCoreSelection{CoreID: selection.CoreID, PackageID: selection.PackageID})
		}
		writeJSON(w, http.StatusOK, result)
	})))
	mux.Handle("POST /v1/core-packages", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			corePackageStoreUnavailable(w)
			return
		}
		if !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > corepackage.MaxArchiveSize {
			writeBadRequest(w, r, "core installation requires a bounded application/octet-stream archive")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, corepackage.MaxArchiveSize)
		value, created, err := store.Install(r.Context(), r.ContentLength, r.Body)
		if err != nil {
			corePackageError(w, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, protocol.KitCoreInstall{Package: value, Created: created})
	})))
	mux.Handle("PUT /v1/core-selections/{core_id}", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			corePackageStoreUnavailable(w)
			return
		}
		coreID := r.PathValue("core_id")
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request protocol.KitCoreSelectionRequest
		if !kitCoreIDPattern.MatchString(coreID) || !exactContentType(r, "application/json") || decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeBadRequest(w, r, "core selection requires a core ID and one selection object")
			return
		}
		if !kitPackageIDPattern.MatchString(request.PackageID) || (request.ExpectedPackageID != "" && !kitPackageIDPattern.MatchString(request.ExpectedPackageID)) {
			writeBadRequest(w, r, "selection requires valid package identities")
			return
		}
		if inspect == nil {
			writeError(w, http.StatusServiceUnavailable, string(protocol.CodeMiSTerUnavailable), "runtime compatibility inspection is unavailable")
			return
		}
		value, archive, err := store.Read(r.Context(), request.PackageID)
		if err != nil {
			corePackageError(w, err)
			return
		}
		if value.Descriptor.Core.ID != coreID {
			writeBadRequest(w, r, "selected package belongs to another core")
			return
		}
		observed, apiErr := inspect(r.Context(), int64(len(archive)), bytes.NewReader(archive))
		if apiErr != nil {
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		if observed.PackageID != value.PackageID || !reflect.DeepEqual(observed.Descriptor, value.Descriptor) {
			writeError(w, http.StatusBadGateway, string(protocol.CodeDigestMismatch), "runtime inspection did not identify the installed package")
			return
		}
		if !observed.Compatible || observed.CompatibilityError != nil {
			if observed.CompatibilityError != nil {
				writeAPIError(w, http.StatusConflict, observed.CompatibilityError)
			} else {
				writeError(w, http.StatusConflict, string(protocol.CodeUnsupportedOperation), "installed package is incompatible with runtime")
			}
			return
		}
		selection, err := store.Select(r.Context(), coreID, request.ExpectedPackageID, request.PackageID)
		if err != nil {
			corePackageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, protocol.KitCoreSelection{CoreID: selection.CoreID, PackageID: selection.PackageID})
	})))
}

func corePackageStoreUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, string(protocol.CodeCorePackageStoreUnavailable), "durable core package storage is unavailable")
}

func corePackageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, kitpackages.ErrSelectionConflict):
		writeError(w, http.StatusConflict, string(protocol.CodeCoreSelectionConflict), "core selection changed; refresh before selecting")
	case errors.Is(err, corepackage.ErrPackageNotFound):
		writeError(w, http.StatusNotFound, string(protocol.CodeCorePackageNotFound), "core package is not installed")
	case errors.Is(err, kitpackages.ErrCoreMismatch), errors.Is(err, corepackage.ErrInvalidPackage), errors.Is(err, corepackage.ErrInvalidPackageID):
		writeError(w, http.StatusBadRequest, string(protocol.CodeBadRequest), "invalid core package or selection")
	default:
		corePackageStoreUnavailable(w)
	}
}
