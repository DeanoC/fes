package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/DeanoC/FogCast-POC/internal/cast"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func registerCastRoutes(mux *http.ServeMux, token string, controller CastController) {
	mux.Handle("POST /v1/cast/start", authenticate(token, exactMethod(http.MethodPost, castStartHandler(controller))))
	mux.Handle("POST /v1/cast/stop", authenticate(token, exactMethod(http.MethodPost, castStopHandler(controller))))
	mux.Handle("GET /v1/cast/status", authenticate(token, exactMethod(http.MethodGet, castStatusHandler(controller))))
}

func registerUnavailableCastRoutes(mux *http.ServeMux, token string) {
	handler := unavailableAuxiliaryHandler()
	mux.Handle("POST /v1/cast/start", authenticate(token, exactMethod(http.MethodPost, handler)))
	mux.Handle("POST /v1/cast/stop", authenticate(token, exactMethod(http.MethodPost, handler)))
	mux.Handle("GET /v1/cast/status", authenticate(token, exactMethod(http.MethodGet, handler)))
}

func unavailableAuxiliaryHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setRequestError(r, protocol.CodeMiSTerUnavailable)
		writeError(w, http.StatusServiceUnavailable, string(protocol.CodeMiSTerUnavailable), "MiSTer is unavailable")
	})
}

func castStartHandler(controller CastController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		var request struct {
			Session    string          `json:"session"`
			Token      string          `json:"token"`
			Generation uint64          `json:"generation"`
			Media      json.RawMessage `json:"media"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF || request.Session == "" || request.Token == "" || request.Generation == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "cast start requires one valid session object")
			return
		}
		var startErr error
		var requestedMedia *protocol.CastMediaSet
		if request.Media == nil {
			startErr = controller.Start(r.Context(), request.Session, request.Token, request.Generation)
		} else {
			if bytes.Equal(bytes.TrimSpace(request.Media), []byte("null")) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "cast start media is invalid")
				return
			}
			var media protocol.CastMediaSet
			if err := json.Unmarshal(request.Media, &media); err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "cast start media is invalid")
				return
			}
			requestedMedia = &media
			mediaController, ok := controller.(MediaCastController)
			if !ok {
				writeError(w, http.StatusServiceUnavailable, "CAST_UNAVAILABLE", "cast audio is unavailable")
				return
			}
			capabilities := mediaController.CastMediaCapabilities(r.Context())
			if capabilities.Version != protocol.CastMediaSetVersion || !capabilities.Video || !capabilities.Audio {
				writeError(w, http.StatusServiceUnavailable, "CAST_UNAVAILABLE", "cast audio is unavailable")
				return
			}
			startErr = mediaController.StartWithMedia(r.Context(), request.Session, request.Token, request.Generation, media)
		}
		if startErr != nil {
			writeError(w, http.StatusServiceUnavailable, "CAST_UNAVAILABLE", "cast session could not be started")
			return
		}
		status := controller.Status(r.Context())
		if requestedMedia != nil && protocol.ValidateCastMediaAcknowledgement(*requestedMedia, status.Media) != nil {
			_ = controller.Stop(r.Context(), request.Session, request.Generation)
			writeError(w, http.StatusServiceUnavailable, "CAST_UNAVAILABLE", "cast media acknowledgement is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func castStopHandler(controller CastController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		var request struct {
			Session    string `json:"session"`
			Generation uint64 `json:"generation"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF || request.Session == "" || request.Generation == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "cast stop requires one valid session identity")
			return
		}
		if err := controller.Stop(r.Context(), request.Session, request.Generation); err != nil {
			if errors.Is(err, cast.ErrStale) {
				writeError(w, http.StatusConflict, "CAST_STALE", "cast session identity does not match")
				return
			}
			writeError(w, http.StatusServiceUnavailable, "CAST_UNAVAILABLE", "cast session could not be stopped")
			return
		}
		writeJSON(w, http.StatusOK, controller.Status(r.Context()))
	})
}

func castStatusHandler(controller CastController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, controller.Status(r.Context()))
	})
}

var _ CastController = (*cast.Controller)(nil)
