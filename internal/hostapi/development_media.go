package hostapi

import (
	"context"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

type sessionMediaLoader interface {
	LoadDevelopmentMedia(context.Context, int64, io.Reader, protocol.DevelopmentMediaBinding) (protocol.Status, error)
}

func registerDevelopmentMediaRoute(mux *http.ServeMux, s *sessionCoordinator) {
	mux.HandleFunc("POST /api/v1/session/development-media", func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.DevelopmentMediaHeaders(r.Header)
		types := r.Header.Values("Content-Type")
		ids := r.Header.Values(protocol.HostSessionIDHeader)
		if !valid || len(ids) != 1 || ids[0] == "" || b.Target == "" || len(r.Header.Values(protocol.HostTargetHeader)) != 1 || len(r.Header.Values(protocol.HostTargetIDHeader)) > 1 || len(types) != 1 || types[0] != "application/octet-stream" || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > protocol.MaxDevelopmentMediaBytes {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", protocol.DevelopmentMediaRequestError().Message)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, protocol.MaxDevelopmentMediaBytes)
		result, err := s.loadDevelopmentMedia(r.Context(), ids[0], r.ContentLength, r.Body, b)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
func (s *sessionCoordinator) loadDevelopmentMedia(ctx context.Context, id string, size int64, body io.Reader, b protocol.DevelopmentMediaBinding) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if id != s.id {
		return sessionResult{}, protocol.DevelopmentMediaIdentityError()
	}
	loader, ok := s.service.(sessionMediaLoader)
	if !ok {
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	status, err := loader.LoadDevelopmentMedia(ctx, size, body, b)
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicSession(status, nil)
	s.recordStamp("session.development_media", result, nil, clientStamp{})
	return result, nil
}
