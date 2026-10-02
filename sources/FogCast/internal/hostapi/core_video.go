package hostapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type coreVideoService interface {
	ImportCoreVideoPart(context.Context, int64, io.Reader, string) (catalog.CoreVideoPart, error)
	CoreVideoParts(context.Context) ([]catalog.CoreVideoPart, error)
	CoreEntryVideo(context.Context, string) (fogcast.CoreEntryVideo, error)
}

// Video imports bind an explicit household profile to an exact immutable shell
// and part. Per-entry resolution is read-only; settings choose the preference.
func registerCoreVideo(mux *http.ServeMux, service Service) {
	withService := func(fn func(http.ResponseWriter, *http.Request, coreVideoService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreVideoService)
			if !ok {
				writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "video parts unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("GET /api/v1/library/video-parts", withService(func(w http.ResponseWriter, r *http.Request, s coreVideoService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		parts, err := s.CoreVideoParts(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		if parts == nil {
			parts = []catalog.CoreVideoPart{}
		}
		writeJSON(w, http.StatusOK, parts)
	}))
	mux.HandleFunc("POST /api/v1/library/video-parts/{profile}", withService(func(w http.ResponseWriter, r *http.Request, s coreVideoService) {
		profile := r.PathValue("profile")
		types := r.Header.Values("Content-Type")
		if (profile != "direct" && profile != "scanlines") || len(types) != 1 || types[0] != "application/octet-stream" ||
			len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > catalog.MaxCoreMediaBytes {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "video import requires a direct or scanlines profile and bounded binary archive")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, catalog.MaxCoreMediaBytes)
		value, err := s.ImportCoreVideoPart(r.Context(), r.ContentLength, r.Body, profile)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
	mux.HandleFunc("GET /api/v1/library/core-entries/{game_id}/video", withService(func(w http.ResponseWriter, r *http.Request, s coreVideoService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		if protocol.ValidateGameID(r.PathValue("game_id")) != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid library entry identity")
			return
		}
		value, err := s.CoreEntryVideo(r.Context(), r.PathValue("game_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
}
