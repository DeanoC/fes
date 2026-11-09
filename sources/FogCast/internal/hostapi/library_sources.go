package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/fogcast"
)

type librarySourceService interface {
	LibrarySources() []fogcast.LibrarySourceConfig
	SetLibrarySources(context.Context, []fogcast.LibrarySourceConfig) error
	RefreshLibrarySource(context.Context, string) (fogcast.LibrarySourceRefresh, error)
}

func registerLibrarySources(mux *http.ServeMux, service Service) {
	withSources := func(fn func(http.ResponseWriter, *http.Request, librarySourceService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(librarySourceService)
			if !ok {
				writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "library sources are unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("GET /api/v1/library/sources", withSources(func(w http.ResponseWriter, r *http.Request, s librarySourceService) {
		writeJSON(w, http.StatusOK, map[string]any{"sources": s.LibrarySources()})
	}))
	mux.HandleFunc("PUT /api/v1/library/sources", withSources(func(w http.ResponseWriter, r *http.Request, s librarySourceService) {
		var request struct {
			Sources []fogcast.LibrarySourceConfig `json:"sources"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if request.Sources == nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "sources must be an array")
			return
		}
		if err := s.SetLibrarySources(r.Context(), request.Sources); err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sources": s.LibrarySources()})
	}))
	mux.HandleFunc("POST /api/v1/library/sources/{source_id}/refresh", withSources(func(w http.ResponseWriter, r *http.Request, s librarySourceService) {
		result, err := s.RefreshLibrarySource(r.Context(), r.PathValue("source_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}))
}
