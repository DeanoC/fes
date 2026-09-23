package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
)

type coreROMService interface {
	CoreEntryROM(context.Context, string) (catalog.CoreEntryROM, error)
	SelectCoreEntryROM(context.Context, string, string, string, string, string) (catalog.CoreEntryROM, error)
}

func registerCoreROMs(mux *http.ServeMux, service Service) {
	withService := func(fn func(http.ResponseWriter, *http.Request, coreROMService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreROMService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core ROM selection unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("GET /api/v1/library/core-entries/{game_id}/rom", withService(func(w http.ResponseWriter, r *http.Request, s coreROMService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		value, err := s.CoreEntryROM(r.Context(), r.PathValue("game_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
	mux.HandleFunc("PUT /api/v1/library/core-entries/{game_id}/rom", withService(func(w http.ResponseWriter, r *http.Request, s coreROMService) {
		var request struct {
			PackageID *string `json:"package_id"`
			ROMID     *string `json:"rom_id"`
			Expected  *string `json:"expected_media_id"`
			ID        *string `json:"media_id"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if request.ROMID == nil || *request.ROMID == "" || request.PackageID == nil || request.Expected == nil || request.ID == nil || !corePackageIDRE.MatchString(*request.PackageID) || *request.Expected != "" && !corePackageIDRE.MatchString(*request.Expected) || *request.ID != "" && !corePackageIDRE.MatchString(*request.ID) {
			writeError(w, 400, "BAD_REQUEST", "invalid ROM selection")
			return
		}
		value, err := s.SelectCoreEntryROM(r.Context(), r.PathValue("game_id"), *request.PackageID, *request.ROMID, *request.Expected, *request.ID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, value)
	}))
}
