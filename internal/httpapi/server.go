package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/clawzai2-tech/mister-remote/protocol"
)

type Controller interface {
	Health(string) protocol.Health
	Status() protocol.Status
	Launch(context.Context, protocol.LaunchRequest) (protocol.Status, *protocol.APIError)
	Stop(context.Context) (protocol.Status, *protocol.APIError)
}

func New(controller Controller, token string, version string, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, controller.Health(version))
	})
	mux.Handle("GET /v1/status", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := controller.Status()
		setRequestState(r, status)
		writeJSON(w, http.StatusOK, status)
	})))
	mux.Handle("POST /v1/launch", authenticate(token, launchHandler(controller)))
	mux.Handle("POST /v1/stop", authenticate(token, stopHandler(controller)))
	return requestLogger(logger, mux)
}

func authenticate(token string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			setRequestError(r, protocol.CodeUnauthorized)
			writeAPIError(w, http.StatusUnauthorized, &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "missing or incorrect bearer token"})
			return
		}
		provided := strings.TrimPrefix(authorization, "Bearer ")
		actual := sha256.Sum256([]byte(provided))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			setRequestError(r, protocol.CodeUnauthorized)
			writeAPIError(w, http.StatusUnauthorized, &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "missing or incorrect bearer token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func launchHandler(controller Controller) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request protocol.LaunchRequest
		if err := decoder.Decode(&request); err != nil {
			writeBadRequest(w, r, "request body must contain one valid launch object")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeBadRequest(w, r, "request body must contain exactly one JSON object")
			return
		}
		setRequestLaunch(r, request)
		status, apiErr := controller.Launch(r.Context(), request)
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func stopHandler(controller Controller) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 {
			writeBadRequest(w, r, "stop request body must be empty")
			return
		}
		status, apiErr := controller.Stop(r.Context())
		setRequestState(r, status)
		if apiErr != nil {
			setRequestError(r, apiErr.Code)
			writeAPIError(w, statusForError(apiErr.Code), apiErr)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, message string) {
	setRequestError(r, protocol.CodeBadRequest)
	writeAPIError(w, http.StatusBadRequest, &protocol.APIError{Code: protocol.CodeBadRequest, Message: message})
}

func statusForError(code protocol.ErrorCode) int {
	switch code {
	case protocol.CodeBadRequest:
		return http.StatusBadRequest
	case protocol.CodeUnauthorized:
		return http.StatusUnauthorized
	case protocol.CodeROMNotFound:
		return http.StatusNotFound
	case protocol.CodeBusy:
		return http.StatusConflict
	case protocol.CodeUnsupportedSystem, protocol.CodeInvalidROMPath:
		return http.StatusUnprocessableEntity
	case protocol.CodeMiSTerUnavailable, protocol.CodeCoreTimeout:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeAPIError(w http.ResponseWriter, status int, apiErr *protocol.APIError) {
	writeJSON(w, status, protocol.ErrorEnvelope{Error: *apiErr})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type requestMetadata struct {
	gameID    string
	system    protocol.System
	state     protocol.State
	errorCode protocol.ErrorCode
}

type requestMetadataKey struct{}

func metadata(r *http.Request) *requestMetadata {
	value, _ := r.Context().Value(requestMetadataKey{}).(*requestMetadata)
	return value
}

func setRequestLaunch(r *http.Request, request protocol.LaunchRequest) {
	if value := metadata(r); value != nil {
		value.gameID = request.GameID
		value.system = request.System
	}
}

func setRequestState(r *http.Request, status protocol.Status) {
	if value := metadata(r); value != nil {
		value.state = status.State
		if status.GameID != nil {
			value.gameID = *status.GameID
		}
		if status.System != nil {
			value.system = *status.System
		}
	}
}

func setRequestError(r *http.Request, code protocol.ErrorCode) {
	if value := metadata(r); value != nil {
		value.errorCode = code
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		meta := &requestMetadata{}
		r = r.WithContext(context.WithValue(r.Context(), requestMetadataKey{}, meta))
		wrapped := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		attributes := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", wrapped.status),
			slog.Duration("duration", time.Since(started)),
		}
		if meta.gameID != "" {
			attributes = append(attributes, slog.String("game_id", meta.gameID))
		}
		if meta.system != "" {
			attributes = append(attributes, slog.String("system", string(meta.system)))
		}
		if meta.state != "" {
			attributes = append(attributes, slog.String("state", string(meta.state)))
		}
		if meta.errorCode != "" {
			attributes = append(attributes, slog.String("error_code", string(meta.errorCode)))
		}
		logger.LogAttrs(r.Context(), slog.LevelInfo, "request", attributes...)
	})
}
