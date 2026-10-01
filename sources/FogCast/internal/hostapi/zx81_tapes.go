package hostapi

import (
	"bytes"
	"net/http"

	"github.com/DeanoC/FogCast/internal/zx81tapes"
)

func registerZX81Tapes(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/library/zx81-tapes", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Tapes []zx81tapes.Entry `json:"tapes"`
		}{zx81tapes.Entries()})
	})
	mux.HandleFunc("POST /api/v1/library/zx81-tapes/{tape_id}/import", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		entry, ok := zx81tapes.Lookup(r.PathValue("tape_id"))
		if !ok {
			writeError(w, 404, "NOT_FOUND", "tape unavailable")
			return
		}
		importer, ok := service.(coreMediaLibraryService)
		if !ok {
			writeError(w, 501, "UNSUPPORTED_OPERATION", "core media library is unavailable")
			return
		}
		media, _, err := importer.ImportCoreMedia(r.Context(), int64(len(entry.Data)), bytes.NewReader(entry.Data))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, media)
	})
}
