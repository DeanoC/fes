package hostapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

func registerSessionRoutes(mux *http.ServeMux, service Service, session *sessionCoordinator, mediaPreview http.Handler) {
	mux.HandleFunc("GET /api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if !configuredSessionTarget(service, target) {
			writeError(w, http.StatusNotFound, string(protocol.CodeTargetNotFound), "target is not configured")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, target, true)
		result, err := coordinator.status(r.Context())
		if err != nil {
			var apiErr *protocol.APIError
			if errors.As(err, &apiErr) && (apiErr.Code == protocol.CodeKitLeaseDenied || apiErr.Code == protocol.CodeKitLeaseBusy) {
				writeSessionError(w, err)
				return
			}
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{Code: "TARGET_UNAVAILABLE", Message: "target status is unavailable"}, "connection": targetConnection(service, target)})
			return
		}
		result.Connection = targetConnection(service, target)
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sessions" {
			http.NotFound(w, r)
			return
		}
		lister, ok := service.(interface{ PlaySessions() []fogcast.PlaySession })
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}})
			return
		}
		plays := lister.PlaySessions()
		sessions := make([]map[string]any, 0, len(plays))
		for _, play := range plays {
			item := map[string]any{"target": play.Target, "execution": play.Execution, "state": protocol.StateActive}
			if play.TargetID != "" {
				item["target_id"] = play.TargetID
			}
			if play.GameID != "" {
				item["game_id"] = play.GameID
			}
			if play.System != "" {
				item["system"] = play.System
			}
			sessions = append(sessions, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	})
	if mediaPreview != nil {
		mux.Handle("GET /api/v1/session/preview", mediaPreview)
	}
	mux.HandleFunc("GET /api/v1/session/events", func(w http.ResponseWriter, r *http.Request) {
		coordinator := requestSessionCoordinator(session, service, r, "", true)
		var after uint64
		if raw := r.URL.Query().Get("after"); raw != "" {
			if _, err := fmt.Sscanf(raw, "%d", &after); err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "after must be a non-negative sequence")
				return
			}
		}
		writeJSON(w, http.StatusOK, sessionEventsResult{Events: publicEvents(coordinator.eventsAfter(after))})
	})
	mux.HandleFunc("POST /api/v1/session/launch", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			GameID       string `json:"game_id"`
			Target       string `json:"target"`
			ClientTsUTC  string `json:"client_ts_utc"`
			ClientMonoMS *int64 `json:"client_mono_ms"`
			FlightID     string `json:"flight_id"`
		}
		if err := decodeSingleJSON(w, r, &request); err != nil {
			return
		}
		if protocol.ValidateGameID(request.GameID) != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "game ID is invalid")
			return
		}
		stamp := parseClientStamp(r, request.ClientTsUTC, request.ClientMonoMS, request.FlightID)
		// Launch routing is intentionally independent from the foreground
		// session: preserve an omitted target so FogCast can apply placement.
		target := request.Target
		pairedTarget := fogcast.PairedTargetFromContext(r.Context())
		if target == "" && pairedTarget == "" {
			target = launcherTargetFromContext(r.Context())
		}
		coordinatorTarget := target
		if !configuredSessionTarget(service, target) {
			writeError(w, http.StatusNotFound, string(protocol.CodeTargetNotFound), "target is not configured")
			return
		}
		if target == "" && pairedTarget == "" {
			if selected, ok := service.(interface{ SelectedTargetConfig() fogcast.TargetConfig }); ok {
				coordinatorTarget = selected.SelectedTargetConfig().Name
			}
		}
		coordinator := requestSessionCoordinator(session, service, r, coordinatorTarget)
		result, err := coordinator.launch(r.Context(), request.GameID, target, stamp)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/development-rbf", func(w http.ResponseWriter, r *http.Request) {
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > protocol.MaxDevelopmentRBFBytes {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "development RBF upload requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, protocol.MaxDevelopmentRBFBytes)
		coordinator := requestSessionCoordinator(session, service, r, "")
		result, err := coordinator.loadDevelopmentRBF(r.Context(), r.ContentLength, r.Body, parseClientStamp(r, "", nil, ""))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/development-core", func(w http.ResponseWriter, r *http.Request) {
		contentTypes := r.Header.Values("Content-Type")
		if len(contentTypes) != 1 || contentTypes[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > corepackage.MaxArchiveSize {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "development core upload requires a bounded application/octet-stream body")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, corepackage.MaxArchiveSize)
		coordinator := requestSessionCoordinator(session, service, r, "")
		result, err := coordinator.loadDevelopmentCore(r.Context(), r.ContentLength, r.Body, parseClientStamp(r, "", nil, ""))
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	registerDevelopmentMediaRoute(mux, session)
	registerLiveMediaSessionRoutes(mux, session)
	registerSessionDisplayRoute(mux, session)
	registerCartridgeSessionRoute(mux, session)
	mux.HandleFunc("POST /api/v1/session/stop", func(w http.ResponseWriter, r *http.Request) {
		stamp, retainLease, releaseIdle, expected, explicitTarget, err := decodeOptionalStopRequest(w, r)
		if err != nil {
			return
		}
		pairedTarget := launcherTargetFromContext(r.Context())
		if pairedTarget != "" && explicitTarget != "" && explicitTarget != pairedTarget {
			writeError(w, http.StatusForbidden, "TARGET_MISMATCH", "launcher target does not match the paired target")
			return
		}
		if !configuredSessionTarget(service, explicitTarget) {
			writeError(w, http.StatusNotFound, string(protocol.CodeTargetNotFound), "target is not configured")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, explicitTarget, true)
		result, err := coordinator.stop(r.Context(), stamp, retainLease, releaseIdle, expected)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
