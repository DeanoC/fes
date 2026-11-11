package hostapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
)

type librarySourceService interface {
	LibrarySources() []fogcast.LibrarySourceConfig
	SetLibrarySources(context.Context, []fogcast.LibrarySourceConfig) error
	RefreshLibrarySource(context.Context, string) (fogcast.LibrarySourceRefresh, error)
}

type remoteCacheService interface {
	RemoteCassetteCache(context.Context, string, int) (catalog.RemoteCacheUsage, error)
	RemoveRemoteCassetteCache(context.Context, []string) (catalog.RemoteCacheCleanup, error)
}

func registerLibrarySources(mux *http.ServeMux, service Service) {
	redactedSources := func(s librarySourceService) []fogcast.LibrarySourceConfig {
		sources := s.LibrarySources()
		for i := range sources {
			sources[i].Token = ""
		}
		return sources
	}
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
	mux.HandleFunc("GET /api/v1/library/sources/cache", func(w http.ResponseWriter, r *http.Request) {
		s, ok := service.(remoteCacheService)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "remote cache is unavailable")
			return
		}
		limit := 100
		if value := r.URL.Query().Get("limit"); value != "" {
			var err error
			limit, err = strconv.Atoi(value)
			if err != nil || limit < 1 || limit > catalog.RemoteCachePageLimit {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid cache page size")
				return
			}
		}
		result, err := s.RemoteCassetteCache(r.Context(), r.URL.Query().Get("after"), limit)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/library/sources/cache/cleanup", func(w http.ResponseWriter, r *http.Request) {
		s, ok := service.(remoteCacheService)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "remote cache is unavailable")
			return
		}
		var request struct {
			MediaIDs []string `json:"media_ids"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		result, err := s.RemoveRemoteCassetteCache(r.Context(), request.MediaIDs)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/library/sources", withSources(func(w http.ResponseWriter, r *http.Request, s librarySourceService) {
		writeJSON(w, http.StatusOK, map[string]any{"sources": redactedSources(s)})
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
		writeJSON(w, http.StatusOK, map[string]any{"sources": redactedSources(s)})
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
