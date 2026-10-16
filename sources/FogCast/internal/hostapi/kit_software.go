package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/appliance/software"
)

type kitSoftwareInventoryService interface {
	KitSoftwareReleases(context.Context) ([]software.Release, error)
}

func registerKitSoftwareInventory(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/kit/software/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "software inventory does not accept a query or body")
			return
		}
		provider, ok := service.(kitSoftwareInventoryService)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "kit software inventory is unavailable")
			return
		}
		releases, err := provider.KitSoftwareReleases(r.Context())
		if err != nil {
			writeSessionError(w, err)
			return
		}
		if releases == nil {
			releases = []software.Release{}
		}
		writeJSON(w, http.StatusOK, releases)
	})
}
