package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
)

type coreFirmwareEntryService interface {
	CreateCoreEntryWithFirmware(context.Context, string, string, string, string, bool) (catalog.CoreEntry, error)
}

type bundledFirmwareService interface {
	BundledFirmware(context.Context) (protocol.BundledFirmwareList, error)
}

func registerCoreFirmwareLibrary(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/library/firmware/bundled", func(w http.ResponseWriter, r *http.Request) {
		s, ok := service.(bundledFirmwareService)
		if !ok {
			writeError(w, 501, "UNSUPPORTED_OPERATION", "bundled firmware is unavailable")
			return
		}
		if rejectCoreLibraryBody(w, r) {
			return
		}
		value, err := s.BundledFirmware(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	})
}
