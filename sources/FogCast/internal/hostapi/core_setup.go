package hostapi

import (
	"context"
	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"net/http"
	"strings"
)

type coreSetupService interface {
	CoreSetup(context.Context, string, string, string) (fogcast.CoreSetup, error)
	CreateCoreSetupEntry(context.Context, fogcast.CoreSetupRequest) (catalog.CoreEntry, error)
}

func registerCoreSetup(mux *http.ServeMux, service Service) {
	with := func(fn func(http.ResponseWriter, *http.Request, coreSetupService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreSetupService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core setup unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("GET /api/v1/core-catalog/{core_id}/setup", with(func(w http.ResponseWriter, r *http.Request, s coreSetupService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		q := r.URL.Query()
		source, pid, cid := q.Get("source_id"), q.Get("package_id"), r.PathValue("core_id")
		if len(q) != 2 || len(q["source_id"]) != 1 || len(q["package_id"]) != 1 || !catalogNameRE.MatchString(source) || !catalogCoreRE.MatchString(cid) || !corePackageIDRE.MatchString(pid) {
			writeError(w, 400, "BAD_REQUEST", "invalid setup selection")
			return
		}
		value, err := s.CoreSetup(r.Context(), source, cid, pid)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("POST /api/v1/core-catalog/entries", with(func(w http.ResponseWriter, r *http.Request, s coreSetupService) {
		var req fogcast.CoreSetupRequest
		if decodeSingleJSON(w, r, &req) != nil {
			return
		}
		if !catalogNameRE.MatchString(req.LibrarySourceID) || !catalogNameRE.MatchString(req.SourceID) || !catalogCoreRE.MatchString(req.CoreID) || !corePackageIDRE.MatchString(req.PackageID) || strings.TrimSpace(req.Title) == "" || len(req.Title) > 256 || len(req.ROMs) > 16 {
			writeError(w, 400, "BAD_REQUEST", "invalid setup entry")
			return
		}
		value, err := s.CreateCoreSetupEntry(r.Context(), req)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, struct {
			SourceID            string            `json:"source_id"`
			PublicationSourceID string            `json:"publication_source_id"`
			Entry               catalog.CoreEntry `json:"entry"`
		}{req.LibrarySourceID, req.SourceID, value})
	}))
}
