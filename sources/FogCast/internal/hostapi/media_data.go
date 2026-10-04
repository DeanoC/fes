package hostapi

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/protocol"
	"io"
	"net/http"
)

type sessionMediaDataLoader interface {
	InsertLibraryDisk(context.Context, protocol.LibraryMediaBinding) (protocol.Status, error)
	SaveMedia(context.Context, protocol.MediaUnitBinding) (protocol.Status, error)
}

func registerMediaDataSessionRoutes(mux *http.ServeMux, s *sessionCoordinator) {
	for _, save := range []bool{false, true} {
		path := "/api/v1/session/disk/insert"
		if save {
			path = "/api/v1/session/disk/save"
		}
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			b, valid := protocol.MediaUnitHeaders(r.Header)
			ids := r.Header.Values(protocol.HostSessionIDHeader)
			if !valid || len(ids) != 1 || ids[0] == "" || b.Target == "" || len(r.TransferEncoding) != 0 {
				writeError(w, 400, "BAD_REQUEST", protocol.MediaUnitRequestError().Message)
				return
			}
			library := protocol.LibraryMediaBinding{MediaUnitBinding: b}
			r.Body = http.MaxBytesReader(w, r.Body, 1024)
			if save {
				body, err := io.ReadAll(r.Body)
				if err != nil || len(body) != 0 {
					writeError(w, 400, "BAD_REQUEST", protocol.MediaUnitRequestError().Message)
					return
				}
			} else {
				if !exactMediaJSON(r) {
					writeError(w, 400, "BAD_REQUEST", protocol.MediaUnitRequestError().Message)
					return
				}
				var req struct {
					GameID string `json:"game_id"`
					BaseID string `json:"base_media_id"`
				}
				decoder := json.NewDecoder(r.Body)
				decoder.DisallowUnknownFields()
				if decoder.Decode(&req) != nil || decoder.Decode(&struct{}{}) != io.EOF {
					writeError(w, 400, "BAD_REQUEST", protocol.MediaUnitRequestError().Message)
					return
				}
				library.GameID = req.GameID
				library.BaseMediaID = req.BaseID
				if !library.Valid() {
					writeError(w, 400, "BAD_REQUEST", protocol.MediaUnitRequestError().Message)
					return
				}
			}
			c := s.forSessionID(ids[0])
			result, err := c.mediaData(r.Context(), ids[0], library, save)
			if err != nil {
				writeSessionError(w, err)
				return
			}
			writeJSON(w, 200, result)
		})
	}
}
func exactMediaJSON(r *http.Request) bool {
	return len(r.Header.Values("Content-Type")) == 1 && r.Header.Get("Content-Type") == "application/json" && r.ContentLength > 0 && r.ContentLength <= 1024
}
func (s *sessionCoordinator) mediaData(ctx context.Context, id string, b protocol.LibraryMediaBinding, save bool) (sessionResult, error) {
	dev := protocol.DevelopmentMediaBinding{PackageID: b.PackageID, Generation: b.Generation, Target: b.Target, TargetID: b.TargetID}
	if !s.matchesSessionTargetBinding(ctx, dev) {
		return sessionResult{}, protocol.MediaUnitIdentityError()
	}
	ctx = s.scoped(ctx)
	if !s.begin() {
		return sessionResult{}, busyError()
	}
	defer s.end()
	s.observationMu.Lock()
	defer s.observationMu.Unlock()
	if id != s.id {
		return sessionResult{}, protocol.MediaUnitIdentityError()
	}
	loader, ok := s.service.(sessionMediaDataLoader)
	if !ok {
		return sessionResult{}, &protocol.APIError{Code: protocol.CodeUnsupportedOperation, Message: "requested operation is unsupported"}
	}
	var status protocol.Status
	var err error
	if save {
		status, err = loader.SaveMedia(ctx, b.MediaUnitBinding)
	} else {
		status, err = loader.InsertLibraryDisk(ctx, b)
	}
	if err != nil {
		return sessionResult{}, err
	}
	result := s.publicSession(status, nil)
	s.recordStamp("session.disk", result, nil, clientStamp{})
	return result, nil
}
