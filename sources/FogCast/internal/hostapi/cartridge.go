package hostapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/protocol"
)

func registerCartridgeSessionRoute(mux *http.ServeMux, s *sessionCoordinator) {
	mux.HandleFunc("POST /api/v1/session/cartridge", func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.DevelopmentMediaHeaders(r.Header)
		ids, types := r.Header.Values(protocol.HostSessionIDHeader), r.Header.Values("Content-Type")
		if !valid || len(ids) != 1 || ids[0] == "" || b.Target == "" ||
			len(r.Header.Values(protocol.HostTargetHeader)) != 1 ||
			len(r.Header.Values(protocol.HostTargetIDHeader)) > 1 ||
			len(types) != 1 || types[0] != "application/json" || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > 1<<12 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", protocol.CartridgeRequestError().Message)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<12)
		var req protocol.CartridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			protocol.ValidateDigest(req.MediaID) != nil || !protocol.AdmitCartridgeName(req.Name) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", protocol.CartridgeRequestError().Message)
			return
		}
		coordinator := s.forSessionID(ids[0])
		result, err := coordinator.replaceCartridge(r.Context(), ids[0], req.MediaID, b)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

// replaceCartridge relaunches the bound session's title with mediaID as its
// cartridge on the same kit. The launch replaces the running core as any
// launch does; the title's library binding is unchanged.
func (s *sessionCoordinator) replaceCartridge(ctx context.Context, id, mediaID string, b protocol.DevelopmentMediaBinding) (sessionResult, error) {
	if !s.matchesSessionTargetBinding(ctx, b) {
		return sessionResult{}, protocol.CartridgeIdentityError()
	}
	scoped := s.scoped(ctx)
	if s.target != "" {
		scoped = fogcast.WithSessionTarget(scoped, s.target)
	}
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if id != s.id {
		return sessionResult{}, protocol.CartridgeIdentityError()
	}
	status, err := s.service.Status(scoped)
	if err != nil {
		return sessionResult{}, err
	}
	if !b.MatchesCartridgeSwap(status) || status.GameID == nil || *status.GameID == "" {
		return sessionResult{}, protocol.CartridgeIdentityError()
	}
	gameID := *status.GameID
	return s.launchLocked(fogcast.WithCartridge(scoped, gameID, mediaID), gameID, s.target, clientStamp{})
}
