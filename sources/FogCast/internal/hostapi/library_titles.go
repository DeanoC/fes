package hostapi

import (
	"net/http"

	"github.com/DeanoC/FogCast/fogcast"
)

func registerLibraryTitles(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/library/titles", func(w http.ResponseWriter, r *http.Request) {
		handleLibraryTitles(w, r, service)
	})
}

// handleLibraryTitles serves the combined library. The body is
// ProjectMeshBackendLibrary. Skipped titles stay off the document. A
// catalog that cannot be read is 500, not an empty title list. A content
// id the wire parser rejects fails the response the same way. The route
// does not launch and does not report Ready. A query string is not a filter.
func handleLibraryTitles(w http.ResponseWriter, r *http.Request, service Service) {
	rows, skipped := service.MeshBackendLibrary(r.Context())
	for _, skip := range skipped {
		if skip.Reason == fogcast.MeshSkipCatalogUnavailable {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
			return
		}
	}
	writeLibraryTitles(w, rows)
}

func writeLibraryTitles(w http.ResponseWriter, rows []fogcast.MeshBackendRow) {
	doc, err := fogcast.MeshLibraryTitles(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "catalog is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}
