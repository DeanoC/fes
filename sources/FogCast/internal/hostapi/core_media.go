package hostapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type coreMediaLibraryService interface {
	ImportCoreMedia(context.Context, int64, io.Reader) (catalog.CoreMedia, bool, error)
	CoreMedia(context.Context, string) (catalog.CoreMedia, error)
	CreateCoreMediaEntry(context.Context, string, string, string, string) (catalog.CoreEntry, error)
	SelectCoreEntryMedia(context.Context, string, string, string, string, string) (catalog.CoreEntry, error)
}

type coreEntryMediaNameService interface {
	SetCoreEntryMediaName(context.Context, string, string, string, string) (catalog.CoreEntry, error)
}

type coreMediaCapabilitiesService interface {
	CoreMediaCapabilities(context.Context, string) (protocol.CoreMediaCapabilities, error)
}

func validCoreMediaPair(role, id string) bool {
	return role == "" && id == "" || (role == "blob" || role == protocol.DiskRole || role == protocol.CassetteRole || role == protocol.HardDiskRole) && corePackageIDRE.MatchString(id)
}

type hardDiskMediaService interface {
	ImportHardDiskMedia(context.Context, int64, io.Reader) (fogcast.HardDiskMedia, bool, error)
	SetCoreEntryDiskGeometry(context.Context, string, string, string, *protocol.DiskGeometry) (catalog.CoreEntry, error)
}

// registerHardDiskMedia adds the hard-disk role's own import, the one route
// that admits more than 32 MiB, and the entry's geometry override.
func registerHardDiskMedia(mux *http.ServeMux, service Service) {
	withService := func(fn func(http.ResponseWriter, *http.Request, hardDiskMediaService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(hardDiskMediaService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "hard-disk media is unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("POST /api/v1/core-media/hard-disk", withService(func(w http.ResponseWriter, r *http.Request, s hardDiskMediaService) {
		types := r.Header.Values("Content-Type")
		if len(types) != 1 || types[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 || !protocol.AdmitAtaDiskSize(r.ContentLength) {
			writeError(w, 400, "BAD_REQUEST", "hard-disk import requires a raw image of 1 to 128 MiB in whole 512-byte sectors")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, catalog.MaxHardDiskMediaBytes)
		value, created, err := s.ImportHardDiskMedia(r.Context(), r.ContentLength, r.Body)
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
	mux.HandleFunc("PUT /api/v1/library/core-entries/{game_id}/disk-geometry", withService(func(w http.ResponseWriter, r *http.Request, s hardDiskMediaService) {
		var request struct {
			ExpectedPackageID *string                `json:"expected_package_id"`
			ExpectedMediaID   *string                `json:"expected_media_id"`
			Geometry          *protocol.DiskGeometry `json:"geometry"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if protocol.ValidateGameID(r.PathValue("game_id")) != nil || request.ExpectedPackageID == nil || request.ExpectedMediaID == nil ||
			!corePackageIDRE.MatchString(*request.ExpectedPackageID) || !corePackageIDRE.MatchString(*request.ExpectedMediaID) {
			writeError(w, 400, "BAD_REQUEST", "disk geometry request is invalid")
			return
		}
		value, err := s.SetCoreEntryDiskGeometry(r.Context(), r.PathValue("game_id"), *request.ExpectedPackageID, *request.ExpectedMediaID, request.Geometry)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
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
	mux.HandleFunc("PUT /api/v1/library/core-entries/{game_id}/media-name", func(w http.ResponseWriter, r *http.Request) {
		s, ok := service.(coreEntryMediaNameService)
		if !ok {
			writeError(w, 501, "UNSUPPORTED_OPERATION", "core media names are unavailable")
			return
		}
		var request struct {
			ExpectedPackageID *string `json:"expected_package_id"`
			MediaID           *string `json:"media_id"`
			MediaName         *string `json:"media_name"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if protocol.ValidateGameID(r.PathValue("game_id")) != nil || request.ExpectedPackageID == nil || request.MediaID == nil || request.MediaName == nil ||
			!corePackageIDRE.MatchString(*request.ExpectedPackageID) || !corePackageIDRE.MatchString(*request.MediaID) || !catalog.AdmitCoreEntryMediaName(*request.MediaName) {
			writeError(w, 400, "BAD_REQUEST", "core media name is invalid")
			return
		}
		value, err := s.SetCoreEntryMediaName(r.Context(), r.PathValue("game_id"), *request.ExpectedPackageID, *request.MediaID, *request.MediaName)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	})
}
