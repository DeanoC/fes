package hostapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type coreMediaLibraryService interface {
	ImportCoreMedia(context.Context, int64, io.Reader) (catalog.CoreMedia, bool, error)
	CoreMedia(context.Context, string) (catalog.CoreMedia, error)
	CreateCoreMediaEntry(context.Context, string, string, string, string) (catalog.CoreEntry, error)
	SelectCoreEntryMedia(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
}

type coreMediaCapabilitiesService interface {
	CoreMediaCapabilities(context.Context, string) (protocol.CoreMediaCapabilities, error)
}

func validCoreMediaPair(role, id string) bool {
	return role == "" && id == "" || role == "blob" && corePackageIDRE.MatchString(id)
}

func registerCoreMediaLibrary(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/core-packages/{package_id}/media-capabilities", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) || !validPackagePath(w, r) {
			return
		}
		s, ok := service.(coreMediaCapabilitiesService)
		if !ok {
			writeError(w, 501, "UNSUPPORTED_OPERATION", "core media capabilities are unavailable")
			return
		}
		value, err := s.CoreMediaCapabilities(r.Context(), r.PathValue("package_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	})
	withService := func(fn func(http.ResponseWriter, *http.Request, coreMediaLibraryService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreMediaLibraryService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core media library is unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("POST /api/v1/core-media", withService(func(w http.ResponseWriter, r *http.Request, s coreMediaLibraryService) {
		types := r.Header.Values("Content-Type")
		if len(types) != 1 || types[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > catalog.MaxCoreMediaBytes {
			writeError(w, 400, "BAD_REQUEST", "media import requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, catalog.MaxCoreMediaBytes)
		value, created, err := s.ImportCoreMedia(r.Context(), r.ContentLength, r.Body)
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
	mux.HandleFunc("GET /api/v1/core-media/{media_id}", withService(func(w http.ResponseWriter, r *http.Request, s coreMediaLibraryService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		if !corePackageIDRE.MatchString(r.PathValue("media_id")) {
			writeError(w, 400, "BAD_REQUEST", "media ID is invalid")
			return
		}
		value, err := s.CoreMedia(r.Context(), r.PathValue("media_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
	mux.HandleFunc("PUT /api/v1/library/core-entries/{game_id}/media", withService(func(w http.ResponseWriter, r *http.Request, s coreMediaLibraryService) {
		var request struct {
			ExpectedPackageID *string `json:"expected_package_id"`
			ExpectedMediaID   *string `json:"expected_media_id"`
			MediaRole         *string `json:"media_role"`
			MediaID           *string `json:"media_id"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if protocol.ValidateGameID(r.PathValue("game_id")) != nil || request.ExpectedPackageID == nil || request.ExpectedMediaID == nil ||
			request.MediaRole == nil || request.MediaID == nil ||
			!corePackageIDRE.MatchString(*request.ExpectedPackageID) || (*request.ExpectedMediaID != "" && !corePackageIDRE.MatchString(*request.ExpectedMediaID)) || !validCoreMediaPair(*request.MediaRole, *request.MediaID) {
			writeError(w, 400, "BAD_REQUEST", "core media selection is invalid")
			return
		}
		value, err := s.SelectCoreEntryMedia(r.Context(), r.PathValue("game_id"), *request.ExpectedPackageID, *request.ExpectedMediaID, *request.MediaRole, *request.MediaID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
}
