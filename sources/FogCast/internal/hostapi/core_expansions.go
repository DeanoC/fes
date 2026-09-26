package hostapi

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/misteross/expansion"
)

type coreExpansionService interface {
	ImportCoreExpansion(context.Context, int64, io.Reader) (catalog.CoreExpansion, error)
	CoreExpansions(context.Context) ([]catalog.CoreExpansion, error)
	CoreEntryExpansion(context.Context, string) (catalog.CoreEntryExpansion, error)
	SelectCoreEntryExpansion(context.Context, string, string, string, string) (catalog.CoreEntryExpansion, error)
}

type coreSlotExpansionService interface {
	CoreEntrySlotExpansions(context.Context, string) (fogcast.CoreEntrySlotExpansions, error)
	SelectCoreEntrySlotExpansion(context.Context, string, string, int, string, string) (fogcast.CoreEntrySlotExpansions, error)
}

// registerCoreSlotExpansions exposes one card per physical slot of a
// multi-socket shell title. PUT is compare-and-swap on the slot's card.
func registerCoreSlotExpansions(mux *http.ServeMux, service Service) {
	withService := func(fn func(http.ResponseWriter, *http.Request, coreSlotExpansionService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreSlotExpansionService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core slot expansions unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("GET /api/v1/library/core-entries/{game_id}/expansions", withService(func(w http.ResponseWriter, r *http.Request, s coreSlotExpansionService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		value, err := s.CoreEntrySlotExpansions(r.Context(), r.PathValue("game_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("PUT /api/v1/library/core-entries/{game_id}/expansions/{slot}", withService(func(w http.ResponseWriter, r *http.Request, s coreSlotExpansionService) {
		var request struct {
			PackageID *string `json:"package_id"`
			Expected  *string `json:"expected_expansion_id"`
			ID        *string `json:"expansion_id"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		raw := r.PathValue("slot")
		slot, err := strconv.Atoi(raw)
		if err != nil || strconv.Itoa(slot) != raw || slot < 1 || slot > 7 ||
			request.PackageID == nil || request.Expected == nil || request.ID == nil || !corePackageIDRE.MatchString(*request.PackageID) ||
			*request.Expected != "" && !corePackageIDRE.MatchString(*request.Expected) || *request.ID != "" && !corePackageIDRE.MatchString(*request.ID) {
			writeError(w, 400, "BAD_REQUEST", "invalid slot expansion selection")
			return
		}
		value, err := s.SelectCoreEntrySlotExpansion(r.Context(), r.PathValue("game_id"), *request.PackageID, slot, *request.Expected, *request.ID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
}

func registerCoreExpansions(mux *http.ServeMux, service Service) {
	registerCoreSlotExpansions(mux, service)
	withService := func(fn func(http.ResponseWriter, *http.Request, coreExpansionService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreExpansionService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core expansions unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("POST /api/v1/core-expansions", withService(func(w http.ResponseWriter, r *http.Request, s coreExpansionService) {
		types := r.Header.Values("Content-Type")
		if len(types) != 1 || types[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > expansion.MaxArchiveBytes {
			writeError(w, 400, "BAD_REQUEST", "expansion import requires bounded binary archive")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, expansion.MaxArchiveBytes)
		value, err := s.ImportCoreExpansion(r.Context(), r.ContentLength, r.Body)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("GET /api/v1/core-expansions", withService(func(w http.ResponseWriter, r *http.Request, s coreExpansionService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		value, err := s.CoreExpansions(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("GET /api/v1/library/core-entries/{game_id}/expansion", withService(func(w http.ResponseWriter, r *http.Request, s coreExpansionService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		value, err := s.CoreEntryExpansion(r.Context(), r.PathValue("game_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("PUT /api/v1/library/core-entries/{game_id}/expansion", withService(func(w http.ResponseWriter, r *http.Request, s coreExpansionService) {
		var request struct {
			PackageID *string `json:"package_id"`
			Expected  *string `json:"expected_expansion_id"`
			ID        *string `json:"expansion_id"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if request.PackageID == nil || request.Expected == nil || request.ID == nil || !corePackageIDRE.MatchString(*request.PackageID) || *request.Expected != "" && !corePackageIDRE.MatchString(*request.Expected) || *request.ID != "" && !corePackageIDRE.MatchString(*request.ID) {
			writeError(w, 400, "BAD_REQUEST", "invalid expansion selection")
			return
		}
		value, err := s.SelectCoreEntryExpansion(r.Context(), r.PathValue("game_id"), *request.PackageID, *request.Expected, *request.ID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
}
