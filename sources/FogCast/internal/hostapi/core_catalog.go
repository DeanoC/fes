package hostapi

import (
	"context"
	"github.com/DeanoC/FogCast/fogcast"
	"net/http"
	"regexp"
)

type coreCatalogService interface {
	AvailableCores(context.Context) ([]fogcast.AvailableCore, error)
	InstallAvailableCore(context.Context, string, string, string) (fogcast.InstalledCorePackage, error)
}

var catalogNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`)
var catalogCoreRE = regexp.MustCompile(`^fes\.[a-z0-9][a-z0-9.-]{0,63}$`)

func registerCoreCatalog(mux *http.ServeMux, service Service) {
	registerCoreSetup(mux, service)
	with := func(fn func(http.ResponseWriter, *http.Request, coreCatalogService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreCatalogService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core catalog unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("GET /api/v1/core-catalog", with(func(w http.ResponseWriter, r *http.Request, s coreCatalogService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		rows, err := s.AvailableCores(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, struct {
			Cores []fogcast.AvailableCore `json:"cores"`
		}{rows})
	}))
	mux.HandleFunc("POST /api/v1/core-catalog/install", with(func(w http.ResponseWriter, r *http.Request, s coreCatalogService) {
		var req struct {
			SourceID  string `json:"source_id"`
			CoreID    string `json:"core_id"`
			PackageID string `json:"package_id"`
		}
		if decodeSingleJSON(w, r, &req) != nil {
			return
		}
		if !catalogNameRE.MatchString(req.SourceID) || !catalogCoreRE.MatchString(req.CoreID) || !corePackageIDRE.MatchString(req.PackageID) {
			writeError(w, 400, "BAD_REQUEST", "invalid catalog selection")
			return
		}
		p, err := s.InstallAvailableCore(r.Context(), req.SourceID, req.CoreID, req.PackageID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, p)
	}))
}
