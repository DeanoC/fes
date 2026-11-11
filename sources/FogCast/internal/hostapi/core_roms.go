package hostapi

import (
	"context"
	"net/http"
	"regexp"

	"github.com/DeanoC/FogCast/catalog"
)

type coreROMService interface {
	CoreEntryROM(context.Context, string, string) (catalog.CoreEntryROM, error)
	SelectCoreEntryROM(context.Context, string, string, string, string, string) (catalog.CoreEntryROM, error)
}

// ROM IDs use the identifier syntax of sealed core package descriptors.
var coreROMIDRE = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)

type coreROMResponse struct {
	GameID         string `json:"game_id"`
	PackageID      string `json:"package_id,omitempty"`
	ROMID          string `json:"rom_id,omitempty"`
	MediaID        string `json:"media_id"`
	BundledMediaID string `json:"bundled_media_id,omitempty"`
	BundledDefault bool   `json:"bundled_default,omitempty"`
	SourceSize     int64  `json:"source_size,omitempty"`
}

func coreROMResponseFor(value catalog.CoreEntryROM) coreROMResponse {
	response := coreROMResponse{
		GameID: value.GameID, PackageID: value.PackageID, ROMID: value.ROMID,
		SourceSize: value.SourceSize,
	}
	if value.BundledDefault {
		response.BundledMediaID = value.MediaID
		response.BundledDefault = true
	} else {
		response.MediaID = value.MediaID
	}
	return response
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
		query := r.URL.Query()
		if len(query) != 1 || len(query["rom_id"]) != 1 || !coreROMIDRE.MatchString(query.Get("rom_id")) {
			writeError(w, 400, "BAD_REQUEST", "invalid ROM selection")
			return
		}
		value, err := s.CoreEntryROM(r.Context(), r.PathValue("game_id"), query.Get("rom_id"))
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, 200, coreROMResponseFor(value))
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
