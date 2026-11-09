package hostapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/meshcontent"
)

// LibrarySourceService exposes only the local cassette source, not control.
type LibrarySourceService interface {
	LibrarySourceEntries(context.Context) ([]catalog.CoreEntry, error)
	LibrarySourceContentAdvertises(context.Context, meshcontent.ContentID) (bool, error)
	OpenLibrarySourceContent(context.Context, meshcontent.ContentID) (io.ReadCloser, error)
}

// NewLibrarySourceHandler serves exactly three read-only source operations.
func NewLibrarySourceHandler(service LibrarySourceService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "source is read-only")
			return
		}
		switch r.URL.Path {
		case "/api/v1/library/core-entries":
			if r.URL.RawQuery != "" {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "unexpected source query")
				return
			}
			entries, err := service.LibrarySourceEntries(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL", "source library unavailable")
				return
			}
			writeJSON(w, http.StatusOK, struct {
				Entries []catalog.CoreEntry `json:"entries"`
			}{entries})
		case "/api/v1/mesh/content/source":
			id, ok := meshHostContentID(w, r)
			if !ok {
				return
			}
			available, err := service.LibrarySourceContentAdvertises(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL", "source library unavailable")
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"advertises": available})
		case "/api/v1/mesh/content/object":
			id, ok := meshHostContentID(w, r)
			if !ok {
				return
			}
			body, err := service.OpenLibrarySourceContent(r.Context(), id)
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "source content unavailable")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL", "source content unavailable")
				return
			}
			defer body.Close()
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_, _ = io.Copy(w, body)
		default:
			writeError(w, http.StatusNotFound, "NOT_FOUND", "source operation unavailable")
		}
	})
}
