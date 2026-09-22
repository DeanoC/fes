package hostapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast/protocol"
)

type sessionLiveMediaLoader interface {
	ReplaceLiveMedia(context.Context, string, string, protocol.DevelopmentMediaBinding) (protocol.Status, error)
	ClearLiveMedia(context.Context, protocol.DevelopmentMediaBinding) (protocol.Status, error)
}

func registerLiveMediaSessionRoutes(mux *http.ServeMux, s *sessionCoordinator) {
	mux.HandleFunc("POST /api/v1/session/live-media", func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.DevelopmentMediaHeaders(r.Header)
		ids := r.Header.Values(protocol.HostSessionIDHeader)
		types := r.Header.Values("Content-Type")
		if !valid || len(ids) != 1 || ids[0] == "" || b.Target == "" ||
			len(r.Header.Values(protocol.HostTargetHeader)) != 1 ||
			len(r.Header.Values(protocol.HostTargetIDHeader)) > 1 ||
			len(types) != 1 || types[0] != "application/json" || len(r.TransferEncoding) != 0 ||
			r.ContentLength < 1 || r.ContentLength > 1<<20 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", protocol.LiveMediaRequestError().Message)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req protocol.LiveMediaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
			protocol.ValidateDigest(req.MediaID) != nil || !protocol.AdmitTapeMediaName(req.Name) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", protocol.LiveMediaRequestError().Message)
			return
		}
		result, err := s.replaceLiveMedia(r.Context(), ids[0], req.MediaID, req.Name, b)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/live-media/clear", func(w http.ResponseWriter, r *http.Request) {
		b, valid := protocol.DevelopmentMediaHeaders(r.Header)
		ids := r.Header.Values(protocol.HostSessionIDHeader)
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if !valid || len(ids) != 1 || ids[0] == "" || b.Target == "" ||
			len(r.Header.Values(protocol.HostTargetHeader)) != 1 ||
			len(r.Header.Values(protocol.HostTargetIDHeader)) > 1 ||
			err != nil || len(body) != 0 || len(r.TransferEncoding) != 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", protocol.LiveMediaRequestError().Message)
			return
		}
		result, err := s.clearLiveMedia(r.Context(), ids[0], b)
		if err != nil {
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func (s *sessionCoordinator) replaceLiveMedia(ctx context.Context, id, mediaID, name string, b protocol.DevelopmentMediaBinding) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if id != s.id {
		return sessionResult{}, protocol.LiveMediaIdentityError()
	}
	loader, ok := s.service.(sessionLiveMediaLoader)
	if !ok {
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	status, err := loader.ReplaceLiveMedia(ctx, mediaID, name, b)
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicSession(status, nil)
	s.recordStamp("session.live_media", result, nil, clientStamp{})
	return result, nil
}

func (s *sessionCoordinator) clearLiveMedia(ctx context.Context, id string, b protocol.DevelopmentMediaBinding) (sessionResult, error) {
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if id != s.id {
		return sessionResult{}, protocol.LiveMediaIdentityError()
	}
	loader, ok := s.service.(sessionLiveMediaLoader)
	if !ok {
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	status, err := loader.ClearLiveMedia(ctx, b)
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicSession(status, nil)
	s.recordStamp("session.live_media_clear", result, nil, clientStamp{})
	return result, nil
}
