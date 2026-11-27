// SPDX-License-Identifier: GPL-3.0-or-later

// Package targethttp implements a reusable FogCast target-agent HTTP handler
// over FogCast protocol and corepackage types for bare-metal or lightweight
// embedded targets.
package targethttp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/DeanoC/FogCast/corepackage"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

const (
	defaultAgentVersion = "tamago-agent"
	maxJSONRequestBytes = 4096
)

var (
	// ErrInvalidConfig is returned when Config fails validation in New.
	ErrInvalidConfig = errors.New("targethttp: invalid configuration")

	coreIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
	packageIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Controller supplies agent health, live status, and coordinated stop transitions.
type Controller interface {
	Health(agentVersion string) protocol.Health
	Status() protocol.Status
	Stop(ctx context.Context) (protocol.Status, *protocol.APIError)
}

// PackageStore exposes durable core package inventory, installation, and
// per-core selection. Read is used to reconcile selections against concurrent
// installs between List and Selections.
type PackageStore interface {
	List(ctx context.Context) ([]corepackage.Inspection, error)
	Read(ctx context.Context, packageID string) (corepackage.Inspection, error)
	Selections(ctx context.Context) ([]protocol.KitCoreSelection, error)
	Install(ctx context.Context, size int64, r io.Reader) (corepackage.Inspection, bool, error)
	Select(ctx context.Context, coreID string, req protocol.KitCoreSelectionRequest) *protocol.APIError
}

// LaunchRequest selects a core or package (and optional game ID) for POST /v1/launch.
type LaunchRequest struct {
	CoreID    string `json:"core_id,omitempty"`
	PackageID string `json:"package_id,omitempty"`
	GameID    string `json:"game_id,omitempty"`
}

// Launcher executes bounded remote launch transitions on the target runtime.
type Launcher interface {
	Launch(ctx context.Context, req LaunchRequest) (protocol.Status, *protocol.APIError)
}

// DiagnosticsProvider exposes an optional JSON-encodable diagnostic snapshot.
type DiagnosticsProvider interface {
	Diagnostics(ctx context.Context) (any, *protocol.APIError)
}

// KitLeaseProvider reports the target owner's current execution lease.
type KitLeaseProvider interface {
	Status() kitlease.Status
}

// AgentStats summarizes cumulative HTTP requests, auth rejections, and errors.
type AgentStats struct {
	Requests       uint64
	AuthFailures   uint64
	Errors         uint64
	ActiveRequests int64
}

// Config configures a FogCast-compatible target HTTP handler.
type Config struct {
	Controller           Controller
	Token                string // Bearer token; constant-time SHA-256 comparison
	AllowUnauthenticated bool   // Explicit opt-in for open LAN game-appliance deployments
	TargetID             string // Exposed in /v1/health with a valid bearer or in explicit open mode
	AgentVersion         string // Reported in /v1/health (default "tamago-agent")
	Packages             PackageStore
	Launcher             Launcher
	Diagnostics          DiagnosticsProvider
	KitLease             KitLeaseProvider
	ReadOnly             bool  // Advertise observation routes only.
	MaxUploadBytes       int64 // Max archive upload size (default corepackage.MaxArchiveSize; <0 invalid)
}

// Handler serves FogCast target-agent routes over http.Handler.
type Handler struct {
	cfg            Config
	tokenDigest    [32]byte
	maxUploadBytes int64
	mux            *http.ServeMux

	requests       atomic.Uint64
	authFailures   atomic.Uint64
	errors         atomic.Uint64
	activeRequests atomic.Int64
}

// New validates cfg and constructs a FogCast-compatible HTTP agent Handler.
func New(cfg Config) (*Handler, error) {
	if cfg.Controller == nil {
		return nil, fmt.Errorf("%w: missing controller", ErrInvalidConfig)
	}
	if cfg.Token == "" && !cfg.AllowUnauthenticated {
		return nil, fmt.Errorf("%w: token required unless AllowUnauthenticated is true", ErrInvalidConfig)
	}
	if cfg.Token != "" && !ValidBearerToken(cfg.Token) {
		return nil, fmt.Errorf("%w: invalid bearer token", ErrInvalidConfig)
	}
	if cfg.MaxUploadBytes < 0 || cfg.MaxUploadBytes > corepackage.MaxArchiveSize {
		return nil, fmt.Errorf("%w: MaxUploadBytes out of range", ErrInvalidConfig)
	}
	if cfg.AgentVersion == "" {
		cfg.AgentVersion = defaultAgentVersion
	}
	maxUpload := cfg.MaxUploadBytes
	if maxUpload == 0 {
		maxUpload = corepackage.MaxArchiveSize
	}

	h := &Handler{
		cfg:            cfg,
		tokenDigest:    sha256.Sum256([]byte(cfg.Token)),
		maxUploadBytes: maxUpload,
		mux:            http.NewServeMux(),
	}
	h.registerRoutes()
	return h, nil
}

// ServeHTTP dispatches HTTP requests and tracks request/auth/error statistics.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.requests.Add(1)
	h.activeRequests.Add(1)
	defer h.activeRequests.Add(-1)

	sw := &statusRecorder{ResponseWriter: w}
	h.mux.ServeHTTP(sw, r)
	if sw.status >= http.StatusBadRequest {
		h.errors.Add(1)
	}
}

// Stats returns a snapshot of cumulative request, auth-failure, and error counters.
func (h *Handler) Stats() AgentStats {
	return AgentStats{
		Requests:       h.requests.Load(),
		AuthFailures:   h.authFailures.Load(),
		Errors:         h.errors.Load(),
		ActiveRequests: h.activeRequests.Load(),
	}
}

func (h *Handler) registerRoutes() {
	h.mux.HandleFunc("GET /v1/health", h.handleHealth)
	h.mux.Handle("GET /v1/status", h.authenticate(h.handleStatus))
	h.mux.Handle("GET /v1/core-packages", h.authenticate(h.handleListCorePackages))
	h.mux.Handle("GET /v1/core-selections", h.authenticate(h.handleListCoreSelections))
	h.mux.Handle("GET /v1/diagnostics", h.authenticate(h.handleDiagnostics))
	h.mux.Handle("GET /v1/kit/lease", h.requireBearer(h.handleKitLease))
	if h.cfg.ReadOnly {
		return
	}
	h.mux.Handle("POST /v1/stop", h.authenticate(h.handleStop))
	h.mux.Handle("POST /v1/core-packages", h.authenticate(h.handleInstallCorePackage))
	h.mux.Handle("PUT /v1/core-selections/{core_id}", h.authenticate(h.handleSelectCorePackage))
	h.mux.Handle("POST /v1/launch", h.authenticate(h.handleLaunch))
}

func (h *Handler) hasBearer(r *http.Request) bool {
	if h.cfg.Token == "" {
		return false
	}
	authz := r.Header.Values("Authorization")
	if len(authz) != 1 {
		return false
	}
	provided, ok := parseBearerToken(authz[0])
	if !ok {
		return false
	}
	actual := sha256.Sum256([]byte(provided))
	return subtle.ConstantTimeCompare(actual[:], h.tokenDigest[:]) == 1
}

func (h *Handler) authenticate(next http.HandlerFunc) http.Handler {
	if h.cfg.AllowUnauthenticated {
		return next
	}
	return h.requireBearer(next)
}

func (h *Handler) requireBearer(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.hasBearer(r) {
			h.authFailures.Add(1)
			writeAPIError(w, http.StatusUnauthorized, &protocol.APIError{
				Code:    protocol.CodeUnauthorized,
				Message: "missing or incorrect bearer token",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := h.cfg.Controller.Health(h.cfg.AgentVersion)
	health.TargetID = h.cfg.TargetID
	if !h.hasBearer(r) {
		health.BootID = ""
		if !h.cfg.AllowUnauthenticated {
			health.TargetID = ""
			health.Software = nil
		}
	}
	writeJSON(w, http.StatusOK, health)
}

func (h *Handler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.cfg.Controller.Status())
}

func (h *Handler) handleKitLease(w http.ResponseWriter, _ *http.Request) {
	if h.cfg.KitLease == nil {
		writeAPIError(w, http.StatusServiceUnavailable, &protocol.APIError{
			Code: protocol.CodeUnavailable, Message: "kit lease status is unavailable",
		})
		return
	}
	writeJSON(w, http.StatusOK, h.cfg.KitLease.Status())
}

func (h *Handler) handleStop(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) != 0 {
		writeBadRequest(w, "stop request body must be empty")
		return
	}
	status, apiErr := h.cfg.Controller.Stop(r.Context())
	if apiErr != nil {
		writeAPIError(w, statusForAPIError(apiErr), apiErr)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) handleListCorePackages(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Packages == nil {
		writePackageStoreUnavailable(w)
		return
	}
	packages, err := h.cfg.Packages.List(r.Context())
	if err != nil {
		writePackageError(w, err)
		return
	}
	selections, err := h.cfg.Packages.Selections(r.Context())
	if err != nil {
		writePackageError(w, err)
		return
	}
	if packages == nil {
		packages = []corepackage.Inspection{}
	}
	if selections == nil {
		selections = []protocol.KitCoreSelection{}
	}
	listed := make(map[string]bool, len(packages))
	for _, pkg := range packages {
		listed[pkg.PackageID] = true
	}
	for _, sel := range selections {
		if listed[sel.PackageID] {
			continue
		}
		// Match internal/httpapi: fetch any package installed and selected between List and Selections.
		pkg, err := h.cfg.Packages.Read(r.Context(), sel.PackageID)
		if err != nil {
			writePackageError(w, err)
			return
		}
		packages = append(packages, pkg)
		listed[sel.PackageID] = true
	}
	writeJSON(w, http.StatusOK, protocol.KitCorePackages{
		Packages:   packages,
		Selections: selections,
	})
}

func (h *Handler) handleListCoreSelections(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Packages == nil {
		writePackageStoreUnavailable(w)
		return
	}
	selections, err := h.cfg.Packages.Selections(r.Context())
	if err != nil {
		writePackageError(w, err)
		return
	}
	if selections == nil {
		selections = []protocol.KitCoreSelection{}
	}
	writeJSON(w, http.StatusOK, selections)
}

func (h *Handler) handleInstallCorePackage(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Packages == nil {
		writePackageStoreUnavailable(w)
		return
	}
	if !exactContentType(r, "application/octet-stream") || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > h.maxUploadBytes {
		writeBadRequest(w, "core installation requires a bounded application/octet-stream archive")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)
	pkg, created, err := h.cfg.Packages.Install(r.Context(), r.ContentLength, r.Body)
	if err != nil {
		writePackageError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, protocol.KitCoreInstall{
		Package: pkg,
		Created: created,
	})
}

func (h *Handler) handleSelectCorePackage(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Packages == nil {
		writePackageStoreUnavailable(w)
		return
	}
	coreID := r.PathValue("core_id")
	if !coreIDPattern.MatchString(coreID) || !exactContentType(r, "application/json") {
		writeBadRequest(w, "core selection requires a core ID and JSON body")
		return
	}
	var req protocol.KitCoreSelectionRequest
	if err := decodeStrictJSON(w, r, &req); err != nil {
		writeBadRequest(w, "invalid core selection JSON payload")
		return
	}
	if !packageIDPattern.MatchString(req.PackageID) || (req.ExpectedPackageID != "" && !packageIDPattern.MatchString(req.ExpectedPackageID)) {
		writeBadRequest(w, "selection requires valid package identities")
		return
	}
	if apiErr := h.cfg.Packages.Select(r.Context(), coreID, req); apiErr != nil {
		writeAPIError(w, statusForAPIError(apiErr), apiErr)
		return
	}
	writeJSON(w, http.StatusOK, protocol.KitCoreSelection{
		CoreID:    coreID,
		PackageID: req.PackageID,
	})
}

func (h *Handler) handleLaunch(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Launcher == nil {
		writeAPIError(w, http.StatusNotImplemented, &protocol.APIError{
			Code:    protocol.CodeUnsupportedOperation,
			Message: "remote launch is not enabled on this target",
		})
		return
	}
	if !exactContentType(r, "application/json") {
		writeBadRequest(w, "launch request requires application/json body")
		return
	}
	var req LaunchRequest
	if err := decodeStrictJSON(w, r, &req); err != nil {
		writeBadRequest(w, "invalid launch JSON payload")
		return
	}
	if req.CoreID == "" && req.PackageID == "" {
		writeBadRequest(w, "launch request requires core_id or package_id")
		return
	}
	if req.CoreID != "" && !coreIDPattern.MatchString(req.CoreID) {
		writeBadRequest(w, "invalid core_id")
		return
	}
	if req.PackageID != "" && !packageIDPattern.MatchString(req.PackageID) {
		writeBadRequest(w, "invalid package_id")
		return
	}
	status, apiErr := h.cfg.Launcher.Launch(r.Context(), req)
	if apiErr != nil {
		writeAPIError(w, statusForAPIError(apiErr), apiErr)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Diagnostics == nil {
		writeAPIError(w, http.StatusNotImplemented, &protocol.APIError{
			Code:    protocol.CodeUnsupportedOperation,
			Message: "diagnostics endpoint is not enabled on this target",
		})
		return
	}
	snap, apiErr := h.cfg.Diagnostics.Diagnostics(r.Context())
	if apiErr != nil {
		writeAPIError(w, statusForAPIError(apiErr), apiErr)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON data")
	}
	return nil
}

func exactContentType(r *http.Request, want string) bool {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType := strings.TrimSpace(strings.Split(values[0], ";")[0])
	return strings.EqualFold(mediaType, want)
}

func parseBearerToken(authorization string) (string, bool) {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || !ValidBearerToken(token) {
		return "", false
	}
	return token, true
}

// ValidBearerToken reports whether token can be sent as an RFC 6750 bearer credential.
func ValidBearerToken(token string) bool {
	hasData := false
	padding := false
	for i := 0; i < len(token); i++ {
		c := token[i]
		if c == '=' {
			padding = true
			continue
		}
		valid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("-._~+/", rune(c))
		if padding || !valid {
			return false
		}
		hasData = true
	}
	return hasData
}

func writePackageStoreUnavailable(w http.ResponseWriter) {
	writeAPIError(w, http.StatusServiceUnavailable, &protocol.APIError{
		Code:    protocol.CodeCorePackageStoreUnavailable,
		Message: "durable core package storage is unavailable",
	})
}

func writePackageError(w http.ResponseWriter, err error) {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		writeAPIError(w, statusForAPIError(apiErr), apiErr)
		return
	}
	if errors.Is(err, corepackage.ErrPackageNotFound) {
		writeAPIError(w, http.StatusNotFound, &protocol.APIError{
			Code:    protocol.CodeCorePackageNotFound,
			Message: "core package is not installed",
		})
		return
	}
	if errors.Is(err, corepackage.ErrInvalidPackage) || errors.Is(err, corepackage.ErrInvalidPackageID) {
		writeAPIError(w, http.StatusBadRequest, &protocol.APIError{
			Code:    protocol.CodeBadRequest,
			Message: err.Error(),
		})
		return
	}
	writePackageStoreUnavailable(w)
}

func statusForAPIError(apiErr *protocol.APIError) int {
	if apiErr.StatusCode >= 400 && apiErr.StatusCode <= 599 {
		return apiErr.StatusCode
	}
	switch apiErr.Code {
	case protocol.CodeBadRequest, protocol.CodeTransferFailed:
		return http.StatusBadRequest
	case protocol.CodeUnauthorized:
		return http.StatusUnauthorized
	case protocol.CodeROMNotFound, protocol.CodeContentNotCached, protocol.CodeTargetNotFound, protocol.CodeCorePackageNotFound:
		return http.StatusNotFound
	case protocol.CodeBusy, protocol.CodeKitLeaseBusy, protocol.CodeCoreSelectionConflict,
		protocol.CodeStaleRevision, protocol.CodeIncompatibleData, protocol.CodeCorruptData:
		return http.StatusConflict
	case protocol.CodeUnsupportedSystem, protocol.CodeUnsupportedOperation, protocol.CodeInvalidROMPath,
		protocol.CodeSourceUnavailable, protocol.CodeInvalidArchive, protocol.CodeDigestMismatch:
		return http.StatusUnprocessableEntity
	case protocol.CodeCacheFull:
		return http.StatusInsufficientStorage
	case protocol.CodeUnavailable, protocol.CodeMiSTerUnavailable, protocol.CodeCoreTimeout,
		protocol.CodeCorePackageStoreUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeBadRequest(w http.ResponseWriter, msg string) {
	writeAPIError(w, http.StatusBadRequest, &protocol.APIError{
		Code:    protocol.CodeBadRequest,
		Message: msg,
	})
}

func writeAPIError(w http.ResponseWriter, status int, apiErr *protocol.APIError) {
	writeJSON(w, status, protocol.ErrorEnvelope{Error: *apiErr})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}
