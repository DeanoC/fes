package hostapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// meshHostContent is the host content node the kit reads. A service that
// does not implement it leaves the routes unavailable. These GETs are not
// a pull: the kit still pulls with an empty body from its own store.
type meshHostContent interface {
	MeshContentAdvertises(context.Context, meshcontent.ContentID) bool
	OpenMeshContent(context.Context, meshcontent.ContentID) (io.ReadCloser, error)
}

func registerMeshHostContent(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/mesh/content/source", func(w http.ResponseWriter, r *http.Request) {
		s, ok := service.(meshHostContent)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "mesh content is unavailable")
			return
		}
		id, ok := meshHostContentID(w, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"advertises": s.MeshContentAdvertises(r.Context(), id)})
	})
	mux.HandleFunc("GET /api/v1/mesh/content/object", func(w http.ResponseWriter, r *http.Request) {
		s, ok := service.(meshHostContent)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "mesh content is unavailable")
			return
		}
		id, ok := meshHostContentID(w, r)
		if !ok {
			return
		}
		body, err := s.OpenMeshContent(r.Context(), id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "mesh content is not on this host")
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL", "mesh content is unavailable")
			return
		}
		defer body.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, body)
	})
}

func meshHostContentID(w http.ResponseWriter, r *http.Request) (meshcontent.ContentID, bool) {
	query := r.URL.Query()
	raw := query["id"]
	if len(query) != 1 || len(raw) != 1 {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh content requires one id")
		return meshcontent.ContentID{}, false
	}
	id, err := meshcontent.ParseContentID(raw[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mesh content-id is invalid")
		return meshcontent.ContentID{}, false
	}
	return id, true
}
