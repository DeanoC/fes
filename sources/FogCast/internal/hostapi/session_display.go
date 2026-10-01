package hostapi

import (
	"context"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

type sessionDisplayService interface {
	SetSessionDisplay(context.Context, bool, protocol.DevelopmentMediaBinding) (protocol.Status, error)
}

func registerSessionDisplayRoute(mux *http.ServeMux, s *sessionCoordinator) {
	mux.HandleFunc("POST /api/v1/session/display", func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.DevelopmentMediaHeaders(r.Header)
		ids, types := r.Header.Values(protocol.HostSessionIDHeader), r.Header.Values("Content-Type")
		if !valid || len(ids) != 1 || ids[0] == "" || b.Target == "" ||
			len(r.Header.Values(protocol.HostTargetHeader)) != 1 || len(r.Header.Values(protocol.HostTargetIDHeader)) > 1 ||
			len(types) != 1 || types[0] != "application/json" || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > 128 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", protocol.SessionDisplayRequestError().Message)
			return
		}
		var request protocol.SessionDisplayRequest
		if decodeSingleJSON(w, r, &request) != nil {
			return
		}
		if request.Visible == nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", protocol.SessionDisplayRequestError().Message)
			return
		}
		coordinator := s.forSessionID(ids[0])
		result, err := coordinator.setSessionDisplay(r.Context(), ids[0], *request.Visible, b)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

// A launcher mutation must refer to that paired kit's captured session, even
// when a different target becomes foreground while a picker is open.
func (s *sessionCoordinator) matchesLauncherBinding(ctx context.Context, b protocol.DevelopmentMediaBinding) bool {
	target := launcherTargetFromContext(ctx)
	return target == "" || (s.target == target && b.Target == target)
}

func (s *sessionCoordinator) setSessionDisplay(ctx context.Context, id string, visible bool, b protocol.DevelopmentMediaBinding) (sessionResult, error) {
	if !s.matchesLauncherBinding(ctx, b) {
		return sessionResult{}, protocol.SessionDisplayIdentityError()
	}
	ctx = s.scoped(ctx)
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if id != s.id {
		return sessionResult{}, protocol.SessionDisplayIdentityError()
	}
	display, ok := s.service.(sessionDisplayService)
	if !ok {
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	status, err := display.SetSessionDisplay(ctx, visible, b)
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicSession(status, nil)
	s.recordStamp("session.display", result, nil, clientStamp{})
	return result, nil
}
