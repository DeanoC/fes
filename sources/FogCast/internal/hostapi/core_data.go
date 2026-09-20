package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

type coreDataService interface {
	CoreSettings(context.Context, string) (fogcast.CoreDataResult, error)
	CoreProgress(context.Context, string) (fogcast.CoreDataResult, error)
	SetCoreSettings(context.Context, string, protocol.CoreSettingsUpdate) (fogcast.CoreDataResult, error)
}

func registerCoreData(mux *http.ServeMux, service Service) {
	for _, route := range []string{"GET /api/v1/library/core-entries/{game_id}/settings", "GET /api/v1/library/core-entries/{game_id}/progress", "PUT /api/v1/library/core-entries/{game_id}/settings"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			s, ok := service.(coreDataService)
			if !ok {
				writeError(w, 501, "UNSUPPORTED_OPERATION", "core data is unavailable")
				return
			}
			if protocol.ValidateGameID(r.PathValue("game_id")) != nil {
				writeError(w, 400, "BAD_REQUEST", "game ID is invalid")
				return
			}
			var result fogcast.CoreDataResult
			var err error
			if r.Method == http.MethodPut {
				var body struct {
					ExpectedPackageID string                `json:"expected_package_id"`
					ExpectedRevision  string                `json:"expected_revision"`
					PaddleSpeed       *protocol.PaddleSpeed `json:"paddle_speed"`
				}
				if decodeSingleJSON(w, r, &body) != nil {
					return
				}
				if body.PaddleSpeed == nil {
					writeError(w, 400, "BAD_REQUEST", "paddle_speed is required")
					return
				}
				update := protocol.CoreSettingsUpdate{ExpectedPackageID: body.ExpectedPackageID, ExpectedRevision: body.ExpectedRevision, PaddleSpeed: *body.PaddleSpeed}
				if !update.Valid() {
					writeError(w, 400, "BAD_REQUEST", "core settings request is invalid")
					return
				}
				result, err = s.SetCoreSettings(r.Context(), r.PathValue("game_id"), update)
			} else {
				if rejectCoreLibraryBody(w, r) {
					return
				}
				if r.Pattern == "GET /api/v1/library/core-entries/{game_id}/progress" {
					result, err = s.CoreProgress(r.Context(), r.PathValue("game_id"))
				} else {
					result, err = s.CoreSettings(r.Context(), r.PathValue("game_id"))
				}
			}
			if err != nil {
				writeCoreLibraryError(w, err)
				return
			}
			writeJSON(w, 200, result)
		})
	}
}
