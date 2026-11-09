package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/protocol"
)

type kitHostEntryService interface {
	KitHostEntry(context.Context, string, string) (catalog.CoreEntry, error)
}

func registerKitHostEntry(mux *http.ServeMux, service Service) {
	mux.HandleFunc("GET /api/v1/kit/local-cores", func(w http.ResponseWriter, r *http.Request) {
		if rejectCoreLibraryBody(w, r) {
			return
		}
		s, ok := service.(interface {
			KitHostCores(context.Context) ([]localcores.Core, error)
		})
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "kit host inventory unavailable")
			return
		}
		cores, err := s.KitHostCores(r.Context())
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Cores []localcores.Core `json:"cores"`
		}{cores})
	})
	mux.HandleFunc("POST /api/v1/library/local-core-entry", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PackageID string `json:"package_id"`
			GameID    string `json:"game_id"`
		}
		if decodeSingleJSON(w, r, &req) != nil {
			return
		}
		if (req.PackageID == "" && req.GameID == "") || (req.PackageID != "" && protocol.ValidateDigest(req.PackageID) != nil) || (req.GameID != "" && protocol.ValidateGameID(req.GameID) != nil) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid package or game identity")
			return
		}
		s, ok := service.(kitHostEntryService)
		if !ok {
			writeError(w, http.StatusNotImplemented, "UNSUPPORTED_OPERATION", "kit host entry unavailable")
			return
		}
		entry, err := s.KitHostEntry(r.Context(), req.PackageID, req.GameID)
		if err != nil {
			writeCoreLibraryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entry)
	})
}
