package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/DeanoC/FogCast-POC/protocol"
)

type Controller interface {
	Health(string) protocol.Health
	Status() protocol.Status
	Launch(context.Context, protocol.LaunchRequest) (protocol.Status, *protocol.APIError)
	Stop(context.Context) (protocol.Status, *protocol.APIError)
}

func New(controller Controller, token string, version string, logger *slog.Logger, options ...Option) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	settings := serverOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
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
	if settings.content != nil {
		registerContentRoutes(mux, token, settings.content)
	}
	return requestLogger(logger, rejectInvalidV2RouteShapes(mux))
}

func rejectInvalidV2RouteShapes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if invalidV2RoutePath(r.URL) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func invalidV2RoutePath(requestURL *url.URL) bool {
	acceptedEscapedPath := requestURL.EscapedPath()
	escapedPath := acceptedEscapedPath
	if requestURL.RawPath != "" {
		escapedPath = requestURL.RawPath
	}
	decodedPath, err := url.PathUnescape(escapedPath)
	candidate := v2RouteCandidate(requestURL.Path) || v2RouteCandidate(escapedPath)
	if err == nil {
		candidate = candidate || v2RouteCandidate(decodedPath)
	}
	if !candidate {
		return false
	}
	if (requestURL.RawPath != "" && requestURL.RawPath != acceptedEscapedPath) || err != nil || decodedPath != requestURL.Path {
		return true
	}
	return !validV2RouteShape(escapedPath, decodedPath)
}

func v2RouteCandidate(pathValue string) bool {
	return v2Path(pathValue) || v2Path(path.Clean(pathValue))
}

func v2Path(pathValue string) bool {
	return pathValue == "/v2" || strings.HasPrefix(pathValue, "/v2/")
}

func validV2RouteShape(escapedPath string, decodedPath string) bool {
	if strings.Contains(escapedPath, "\\") || strings.Contains(decodedPath, "\\") || decodedPath != path.Clean(decodedPath) {
		return false
	}
	escapedSegments := strings.Split(escapedPath, "/")
	decodedSegments := strings.Split(decodedPath, "/")
	for _, segment := range escapedSegments {
		decodedSegment, err := url.PathUnescape(segment)
		if err != nil || decodedSegment == "." || decodedSegment == ".." || strings.Contains(decodedSegment, "/") {
			return false
		}
	}
	if len(decodedSegments) == 3 && decodedSegments[0] == "" && decodedSegments[1] == "v2" && decodedSegments[2] == "launch" {
		return len(escapedSegments) == 3 && escapedSegments[0] == "" && escapedSegments[1] == "v2" && escapedSegments[2] == "launch"
	}
	if len(decodedSegments) == 5 && decodedSegments[0] == "" && decodedSegments[1] == "v2" && decodedSegments[2] == "cache" && decodedSegments[3] != "" && decodedSegments[4] != "" {
		return len(escapedSegments) == 5 && escapedSegments[0] == "" && escapedSegments[1] == "v2" && escapedSegments[2] == "cache"
	}
	return false
}

func authenticate(token string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations := r.Header.Values("Authorization")
		if len(authorizations) != 1 {
			setRequestError(r, protocol.CodeUnauthorized)
			writeAPIError(w, http.StatusUnauthorized, &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "missing or incorrect bearer token"})
			return
		}
		provided, ok := parseBearerToken(authorizations[0])
		if !ok {
			setRequestError(r, protocol.CodeUnauthorized)
			writeAPIError(w, http.StatusUnauthorized, &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "missing or incorrect bearer token"})
			return
		}
		actual := sha256.Sum256([]byte(provided))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			setRequestError(r, protocol.CodeUnauthorized)
			writeAPIError(w, http.StatusUnauthorized, &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "missing or incorrect bearer token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func parseBearerToken(authorization string) (string, bool) {
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authorization, bearerPrefix) {
		return "", false
	}
	token := authorization[len(bearerPrefix):]
	if token == "" {
		return "", false
	}
	hasData := false
	padding := false
	for i := 0; i < len(token); i++ {
		character := token[i]
		if character == '=' {
			padding = true
			continue
		}
		valid := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("-._~+/", rune(character))
		if padding || !valid {
			return "", false
		}
		hasData = true
	}
	return token, hasData
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
	case protocol.CodeContentNotCached:
		return http.StatusNotFound
	case protocol.CodeBusy:
		return http.StatusConflict
	case protocol.CodeUnsupportedSystem, protocol.CodeInvalidROMPath, protocol.CodeSourceUnavailable, protocol.CodeInvalidArchive, protocol.CodeDigestMismatch:
		return http.StatusUnprocessableEntity
	case protocol.CodeTransferFailed:
		return http.StatusBadRequest
	case protocol.CodeCacheFull:
		return http.StatusInsufficientStorage
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
	digest    string
	size      int64
	hasSize   bool
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

func setRequestContent(r *http.Request, system protocol.System, digest string, size int64, hasSize bool) {
	if value := metadata(r); value != nil {
		value.system = system
		value.digest = digest
		value.size = size
		value.hasSize = hasSize
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
		route := routePath(r.Pattern)
		attributes := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", route),
			slog.String("route", route),
			slog.Int("status", wrapped.status),
			slog.Duration("duration", time.Since(started)),
		}
		if meta.gameID != "" {
			attributes = append(attributes, slog.String("game_id", meta.gameID))
		}
		if meta.system != "" {
			attributes = append(attributes, slog.String("system", string(meta.system)))
		}
		if meta.digest != "" {
			attributes = append(attributes, slog.String("digest", meta.digest))
		}
		if meta.hasSize {
			attributes = append(attributes, slog.Int64("size", meta.size))
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

func routePath(pattern string) string {
	if _, route, ok := strings.Cut(pattern, " "); ok {
		return route
	}
	if pattern == "" {
		return "unmatched"
	}
	return pattern
}
