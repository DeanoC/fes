package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type namedServiceRestarter interface {
	RestartNamedService(context.Context, ServiceRestartRequest, State, *UnixAdmission, func(ServiceRestartRecord) error, ServiceRestartRecord) ServiceRestartRecord
}

// RestartHandler is the root-only local operator surface. It is served on a
// dedicated mode-0600 Unix socket and is intentionally absent from the
// replaceable agent's HTTP proxy routes.
func (s *Server) RestartHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/v1/kit/software/restart-service" {
			http.NotFound(w, r)
			return
		}
		uid, ok := unixPeerUID(r.Context())
		if !ok || uid != 0 {
			writeRestartError(w, http.StatusForbidden, errors.New("named-service restart requires a root Unix-socket peer"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request ServiceRestartRequest
		if err := decoder.Decode(&request); err != nil {
			writeRestartError(w, http.StatusBadRequest, err)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeRestartError(w, http.StatusBadRequest, errors.New("trailing or oversized request data"))
			return
		}
		if !serviceRestartIDPattern.MatchString(request.RequestID) {
			writeRestartError(w, http.StatusBadRequest, errors.New("request ID must be 64 lowercase hexadecimal characters"))
			return
		}
		result, status := s.executeNamedServiceRestart(r.Context(), request)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(result)
	})
}

func writeRestartError(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) executeNamedServiceRestart(ctx context.Context, request ServiceRestartRequest) (ServiceRestartRecord, int) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	if s.RestartLedger == nil {
		log.Printf("named-service restart request=%s service=%s outcome=not-recorded reason=restart ledger unavailable", request.RequestID, request.Service)
		return ServiceRestartRecord{}, http.StatusServiceUnavailable
	}
	if existing, found, err := s.RestartLedger.Lookup(request.RequestID); err != nil {
		log.Printf("named-service restart request=%s service=%s outcome=not-recorded reason=restart ledger unreadable: %v", request.RequestID, request.Service, err)
		return ServiceRestartRecord{}, http.StatusServiceUnavailable
	} else if found {
		if !sameServiceRestartRequest(existing.Request, request) {
			log.Printf("named-service restart request=%s service=%s outcome=conflict reason=request ID already belongs to a different body", request.RequestID, request.Service)
			return ServiceRestartRecord{}, http.StatusConflict
		}
		log.Printf("named-service restart request=%s service=%s outcome=duplicate stored_outcome=%s stored_phase=%s", request.RequestID, request.Service, existing.Outcome, existing.Phase)
		return existing, http.StatusOK
	}
	now := time.Now().UTC()
	record := ServiceRestartRecord{Request: request, Phase: "accepted", CreatedAt: now, UpdatedAt: now}
	if err := s.RestartLedger.Save(record); err != nil {
		log.Printf("named-service restart request=%s service=%s outcome=not-recorded reason=restart ledger write failed: %v", request.RequestID, request.Service, err)
		return ServiceRestartRecord{}, http.StatusServiceUnavailable
	}
	logServiceRestartRecord(record)
	checkpoint := func(next ServiceRestartRecord) error {
		if err := s.RestartLedger.Save(next); err != nil {
			return err
		}
		logServiceRestartRecord(next)
		return nil
	}
	refuse := func(reason string) (ServiceRestartRecord, int) {
		finished := time.Now().UTC()
		record.Phase, record.Outcome, record.Reason = "refused", "refused", reason
		record.UpdatedAt, record.FinishedAt = finished, &finished
		if err := checkpoint(record); err != nil {
			record.Outcome, record.Phase = "failed", "failed"
			record.Reason = fmt.Sprintf("%s; persist refusal: %v", reason, err)
			return record, http.StatusServiceUnavailable
		}
		return record, http.StatusOK
	}
	if s.Repository == nil || s.Admission == nil {
		return refuse("persistent software state or execution admission is unavailable")
	}
	if s.ExecutionFenced {
		return refuse("execution admission is fenced for an unresolved software operation")
	}
	if s.Gate != nil {
		if err := s.Gate(); err != nil {
			return refuse("appliance state blocks named-service restart: " + err.Error())
		}
	}
	state, err := s.Repository.Read()
	if err != nil {
		return refuse("software operation ledger is unavailable: " + err.Error())
	}
	restarter, ok := s.Controller.(namedServiceRestarter)
	if !ok {
		return refuse("supervisor process controller does not support named-service restart")
	}
	ctx, cancel := context.WithTimeout(ctx, groupRestartTimeout+StopGroupTimeout)
	defer cancel()
	record = restarter.RestartNamedService(ctx, request, state, s.Admission, checkpoint, record)
	if record.Outcome == "" {
		finished := time.Now().UTC()
		record.Outcome, record.Phase = "failed", "failed"
		record.Reason = "supervisor did not produce a final result"
		record.UpdatedAt, record.FinishedAt = finished, &finished
		_ = checkpoint(record)
	}
	return record, http.StatusOK
}

func logServiceRestartRecord(record ServiceRestartRecord) {
	encoded, err := json.Marshal(record)
	if err != nil {
		log.Printf("named-service restart request=%s service=%s phase=%s outcome=%s", record.Request.RequestID, record.Request.Service, record.Phase, record.Outcome)
		return
	}
	log.Printf("named-service restart record=%s", encoded)
}
