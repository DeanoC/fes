package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/internal/kitlease"
)

const (
	defaultDiagnosticEventLimit = 10_000
	maxDiagnosticEventLimit     = 10_000
)

func registerDiagnosticRoutes(mux *http.ServeMux, token string, diagnostics DiagnosticController, manager *kitlease.Manager) {
	mux.Handle("GET /v1/kit/debug/events", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit, err := diagnosticEventLimit(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "DIAGNOSTIC_LIMIT_INVALID", "diagnostic event limit is invalid")
			return
		}
		events := diagnostics.Events(limit)
		writeJSON(w, http.StatusOK, struct {
			Events []flightdiag.Event `json:"events"`
			Count  int                `json:"count"`
		}{Events: events, Count: len(events)})
	})))
	mux.Handle("POST /v1/kit/debug/snapshot-before-reboot", authenticate(token, exactMethod(http.MethodPost, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if manager == nil {
			writeError(w, http.StatusServiceUnavailable, "DIAGNOSTIC_UNAVAILABLE", "kit lease is unavailable")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request flightdiag.SnapshotRequest
		if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(w, http.StatusBadRequest, "DIAGNOSTIC_REQUEST_INVALID", "snapshot request is invalid")
			return
		}
		if request.LeaseGen != manager.Status().Generation {
			writeError(w, http.StatusConflict, "KIT_LEASE_GENERATION_MISMATCH", "snapshot lease generation is stale")
			return
		}
		if sink, ok := diagnostics.(flightdiag.Sink); ok {
			sink.Append(flightdiag.Event{
				FlightID: request.FlightID,
				LeaseGen: request.LeaseGen,
				RunID:    request.RunID,
				Layer:    flightdiag.LayerTarget,
				Kind:     flightdiag.KindFenceRecovery,
				Severity: "ok",
				Detail:   map[string]any{"operation": "snapshot_before_reboot"},
			})
		}
		result, err := diagnostics.SnapshotBeforeReboot(request)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "DIAGNOSTIC_SNAPSHOT_FAILED", "diagnostic snapshot could not be persisted")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}))))
}

func diagnosticEventLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultDiagnosticEventLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxDiagnosticEventLimit {
		return 0, errors.New("limit out of range")
	}
	return limit, nil
}
