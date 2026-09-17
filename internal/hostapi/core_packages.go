package hostapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type coreLibraryService interface {
	ImportCorePackage(context.Context, int64, io.Reader) (corepackage.Inspection, bool, error)
	CorePackages(context.Context) ([]fogcast.InstalledCorePackage, error)
	CorePackage(context.Context, string) (corepackage.Inspection, error)
	CorePackageCompatibility(context.Context, string) (fogcast.CoreCompatibility, error)
	CoreEntries(context.Context) ([]catalog.CoreEntry, error)
	CoreEntry(context.Context, string) (catalog.CoreEntry, error)
	CreateCoreEntry(context.Context, string, string) (catalog.CoreEntry, error)
	SelectCoreEntry(context.Context, string, string, string) (catalog.CoreEntry, error)
}

var corePackageIDRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func registerCoreLibrary(mux *http.ServeMux, service Service) {
	registerCoreMediaLibrary(mux, service)
	withService := func(fn func(http.ResponseWriter, *http.Request, coreLibraryService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreLibraryService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core package library is unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("POST /api/v1/core-packages", withService(func(w http.ResponseWriter, r *http.Request, s coreLibraryService) {
		types := r.Header.Values("Content-Type")
		if len(types) != 1 || types[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > corepackage.MaxArchiveSize {
			writeError(w, 400, "BAD_REQUEST", "package import requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, corepackage.MaxArchiveSize)
		value, created, err := s.ImportCorePackage(r.Context(), r.ContentLength, r.Body)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		code := http.StatusOK
		if created {
			code = http.StatusCreated
		}
		writeJSON(w, code, value)
	}))
	mux.HandleFunc("GET /api/v1/core-packages", withService(func(w http.ResponseWriter, r *http.Request, s coreLibraryService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		values, err := s.CorePackages(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, struct {
			Packages []fogcast.InstalledCorePackage `json:"packages"`
		}{values})
	}))
	mux.HandleFunc("GET /api/v1/core-packages/{package_id}", withService(func(w http.ResponseWriter, r *http.Request, s coreLibraryService) {
		if rejectCoreLibraryBody(w, r) || !validPackagePath(w, r) {
			return
		}
		value, err := s.CorePackage(r.Context(), r.PathValue("package_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("POST /api/v1/core-packages/{package_id}/compatibility", withService(func(w http.ResponseWriter, r *http.Request, s coreLibraryService) {
		if rejectCoreLibraryBody(w, r) || !validPackagePath(w, r) {
			return
		}
		value, err := s.CorePackageCompatibility(r.Context(), r.PathValue("package_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("GET /api/v1/library/core-entries", withService(func(w http.ResponseWriter, r *http.Request, s coreLibraryService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		values, err := s.CoreEntries(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, struct {
			Entries []catalog.CoreEntry `json:"entries"`
		}{values})
	}))
	mux.HandleFunc("POST /api/v1/library/core-entries", withService(func(w http.ResponseWriter, r *http.Request, s coreLibraryService) {
		var request struct {
			Title     string `json:"title"`
			PackageID string `json:"package_id"`
			MediaRole string `json:"media_role"`
			MediaID   string `json:"media_id"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if !corePackageIDRE.MatchString(request.PackageID) {
			writeError(w, 400, "BAD_REQUEST", "package ID is invalid")
			return
		}
		if !validCoreMediaPair(request.MediaRole, request.MediaID) {
			writeError(w, 400, "BAD_REQUEST", "media role and ID are invalid")
			return
		}
		var value catalog.CoreEntry
		var err error
		if request.MediaID != "" {
			media, ok := service.(coreMediaLibraryService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core media library is unavailable")
				return
			}
			value, err = media.CreateCoreMediaEntry(r.Context(), request.Title, request.PackageID, request.MediaRole, request.MediaID)
		} else {
			value, err = s.CreateCoreEntry(r.Context(), request.Title, request.PackageID)
		}
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 201, value)
	}))
	mux.HandleFunc("GET /api/v1/library/core-entries/{game_id}", withService(func(w http.ResponseWriter, r *http.Request, s coreLibraryService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		value, err := s.CoreEntry(r.Context(), r.PathValue("game_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("PUT /api/v1/library/core-entries/{game_id}", withService(func(w http.ResponseWriter, r *http.Request, s coreLibraryService) {
		var request struct {
			PackageID string `json:"package_id"`
			Expected  string `json:"expected_package_id"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if !corePackageIDRE.MatchString(request.PackageID) || !corePackageIDRE.MatchString(request.Expected) {
			writeError(w, 400, "BAD_REQUEST", "package ID is invalid")
			return
		}
		value, err := s.SelectCoreEntry(r.Context(), r.PathValue("game_id"), request.Expected, request.PackageID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
}

func rejectCoreLibraryBody(w http.ResponseWriter, r *http.Request) bool {
	if rejectBody(w, r) == nil {
		return false
	}
	writeError(w, http.StatusBadRequest, "BAD_REQUEST", "package library request body must be empty")
	return true
}

func validPackagePath(w http.ResponseWriter, r *http.Request) bool {
	if !corePackageIDRE.MatchString(r.PathValue("package_id")) {
		writeError(w, 400, "BAD_REQUEST", "package ID is invalid")
		return false
	}
	return true
}

func writeCoreLibraryError(w http.ResponseWriter, err error) {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		code := http.StatusInternalServerError
		switch apiErr.Code {
		case protocol.CodeBadRequest, protocol.CodeInvalidArchive, protocol.CodeUnsupportedOperation, protocol.CodeUnsupportedSystem:
			code = http.StatusBadRequest
		case protocol.CodeROMNotFound:
			code = http.StatusNotFound
		case protocol.CodeBusy, protocol.CodeCorruptData, protocol.CodeIncompatibleData, protocol.CodeStaleRevision:
			code = http.StatusConflict
		case protocol.CodeMiSTerUnavailable:
			code = http.StatusServiceUnavailable
		case protocol.CodeCoreTimeout:
			code = http.StatusGatewayTimeout
		}
		writeJSON(w, code, map[string]any{"error": apiError{Code: string(apiErr.Code), Message: publicErrorMessage(apiErr.Code), Phase: apiErr.Phase, Expected: apiErr.Expected, Observed: apiErr.Observed}})
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL", "package library operation failed")
}
