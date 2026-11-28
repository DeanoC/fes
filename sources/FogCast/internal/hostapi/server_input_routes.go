package hostapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/remoteinput"
)

func registerSessionInputRoutes(mux *http.ServeMux, service Service, session *sessionCoordinator) {
	mux.HandleFunc("GET /api/v1/session/input", func(w http.ResponseWriter, r *http.Request) {
		coordinator := requestSessionCoordinator(session, service, r, "")
		status, ok := coordinator.inputStatus()
		if !ok {
			writeError(w, http.StatusNotFound, "INPUT_UNAVAILABLE", "remote input is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("POST /api/v1/session/input/attach", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectBody(w, r); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "attach request body must be empty")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, "")
		result, err := coordinator.attachInput(r.Context())
		if err != nil {
			if writeRemoteInputMissing(w, err) {
				return
			}
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/input/detach", func(w http.ResponseWriter, r *http.Request) {
		if err := rejectBody(w, r); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "detach request body must be empty")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, "")
		result, err := coordinator.detachInput(r.Context())
		if err != nil {
			if writeRemoteInputMissing(w, err) {
				return
			}
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/session/input/tap", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string             `json:"session_id"`
			Event     *remoteinput.Event `json:"event"`
			Modifiers json.RawMessage    `json:"modifiers"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || body.SessionID == "" || body.Event == nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "input tap is invalid")
			return
		}
		var modifiers uint8
		if len(body.Modifiers) != 0 && (string(body.Modifiers) == "null" || json.Unmarshal(body.Modifiers, &modifiers) != nil) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "input tap modifiers are invalid")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, "")
		duration, err := coordinator.tapCoreKey(r.Context(), body.SessionID, *body.Event, modifiers)
		if err != nil {
			if writeRemoteInputMissing(w, err) {
				return
			}
			if errors.Is(err, host.ErrRemoteInputUnconfirmed) {
				writeError(w, http.StatusServiceUnavailable, "INPUT_UNCONFIRMED", "input may have applied but could not be confirmed")
				return
			}
			if errors.Is(err, host.ErrRemoteInputInvalid) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "input tap or session is invalid")
				return
			}
			if errors.Is(err, host.ErrRemoteInputBusy) {
				writeError(w, http.StatusConflict, "INPUT_BUSY", "input key or source is already held")
				return
			}
			if errors.Is(err, host.ErrRemoteInputNoStream) {
				writeError(w, http.StatusServiceUnavailable, "INPUT_UNAVAILABLE", "no live input stream")
				return
			}
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "host_duration_ms": float64(duration) / float64(time.Millisecond)})
	})
	mux.HandleFunc("POST /api/v1/session/input/event", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Event *remoteinput.Event `json:"event"`
		}
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || body.Event == nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "input event is invalid")
			return
		}
		coordinator := requestSessionCoordinator(session, service, r, "")
		if err := coordinator.sendCoreKey(r.Context(), *body.Event); err != nil {
			if writeRemoteInputMissing(w, err) {
				return
			}
			if errors.Is(err, host.ErrRemoteInputUnconfirmed) {
				writeError(w, http.StatusServiceUnavailable, "INPUT_UNCONFIRMED", "input may have applied but could not be confirmed")
				return
			}
			if errors.Is(err, host.ErrRemoteInputNoStream) {
				writeError(w, http.StatusServiceUnavailable, "INPUT_UNAVAILABLE", "no live input stream")
				return
			}
			writeSessionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
}
