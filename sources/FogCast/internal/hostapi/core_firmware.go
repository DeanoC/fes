package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type coreFirmwareLibraryService interface {
	CoreFirmware(context.Context, string) (catalog.CoreFirmware, error)
	SelectCoreFirmware(context.Context, string, string) (catalog.CoreFirmware, error)
	CreateCoreEntryWithFirmware(context.Context, string, string, string, string, bool) (catalog.CoreEntry, error)
}

func registerCoreFirmwareLibrary(mux *http.ServeMux, service Service) {
	withService := func(fn func(http.ResponseWriter, *http.Request, coreFirmwareLibraryService)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreFirmwareLibraryService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core firmware library is unavailable")
				return
			}
			fn(w, r, s)
		}
	}
	mux.HandleFunc("GET /api/v1/library/firmware", withService(func(w http.ResponseWriter, r *http.Request, s coreFirmwareLibraryService) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		slot := r.URL.Query().Get("slot")
		if slot == "" {
			slot = protocol.FirmwareRole
		}
		value, err := s.CoreFirmware(r.Context(), slot)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
	mux.HandleFunc("PUT /api/v1/library/firmware", withService(func(w http.ResponseWriter, r *http.Request, s coreFirmwareLibraryService) {
		var request struct {
			Slot    *string `json:"slot"`
			MediaID *string `json:"media_id"`
		}
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if request.Slot == nil || request.MediaID == nil || *request.Slot != protocol.FirmwareRole ||
			(*request.MediaID != "" && !corePackageIDRE.MatchString(*request.MediaID)) {
			writeError(w, 400, "BAD_REQUEST", "household firmware selection is invalid")
			return
		}
		value, err := s.SelectCoreFirmware(r.Context(), *request.Slot, *request.MediaID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}))
}
