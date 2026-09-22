package httpapi

import (
	"context"

	"io"
	"net/http"
	"net/url"

	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/cast"

	"github.com/DeanoC/FogCast/internal/flightdiag"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/protocol"
)

const (
	maxCachedLaunchJSONBytes  = 64 << 10
	statusClientClosedRequest = 499
)

type ContentController interface {
	ProbeContent(context.Context, protocol.System, protocol.ContentKey) (protocol.CacheProbeResponse, *protocol.APIError)
	PutContent(context.Context, protocol.System, protocol.ContentIdentity, io.Reader) (protocol.CacheUploadResponse, *protocol.APIError)
}

// CacheIndexController is the optional lease-free GET /v2/cache inventory.
type CacheIndexController interface {
	CacheIndex() (protocol.CacheIndex, *protocol.APIError)
}

type serverOptions struct {
	targetID    string
	kitLease    *kitlease.Manager
	content     ContentController
	input       InputController
	cast        CastController
	development DevelopmentController
	diagnostics DiagnosticController
	update      *applianceupdate.Service
}

type Option func(*serverOptions)

func WithTargetID(targetID string) Option {
	return func(options *serverOptions) { options.targetID = targetID }
}

func WithContent(controller ContentController) Option {
	return func(options *serverOptions) {
		options.content = controller
	}
}

// DiagnosticController is the read-mostly target debug surface. The
// pre-reboot snapshot is separately admitted by the current kit lease.
type DiagnosticController interface {
	Events(limit int) []flightdiag.Event
	SnapshotBeforeReboot(flightdiag.SnapshotRequest) (flightdiag.SnapshotResult, error)
}

func WithDiagnostics(controller DiagnosticController) Option {
	return func(options *serverOptions) { options.diagnostics = controller }
}

func WithInput(controller InputController) Option {
	return func(options *serverOptions) {
		options.input = controller
	}
}

type CastController interface {
	Start(context.Context, string, string, uint64) error
	Stop(context.Context, string, uint64) error
	Status(context.Context) cast.Status
}

// MediaCastController is an optional extension. Legacy cast controllers keep
// receiving video-only requests through CastController.Start.
type MediaCastController interface {
	StartWithMedia(context.Context, string, string, uint64, protocol.CastMediaSet) error
	CastMediaCapabilities(context.Context) protocol.CastMediaCapabilities
}

func WithCast(controller CastController) Option {
	return func(options *serverOptions) { options.cast = controller }
}

type CachedIdentityController interface {
	LookupCachedIdentity(context.Context, string) (protocol.CachedIdentityResponse, *protocol.APIError)
}

func registerContentRoutes(mux *http.ServeMux, token string, controller ContentController) {
	mux.Handle("/v2/cache/{system}/{sha256}", authenticate(token, cacheContentHandler(controller)))
	if indexer, ok := controller.(CacheIndexController); ok {
		mux.Handle("/v2/cache", authenticate(token, cacheIndexHandler(indexer)))
	}
	if lookup, ok := controller.(CachedIdentityController); ok {
		mux.Handle("GET /v2/hostless/identity/{game_id}", authenticate(token, cachedIdentityHandler(lookup)))
	}
}

func cacheIndexHandler(controller CacheIndexController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" {
			writeContentError(w, r, badContentRequest("cache index does not accept a query"))
			return
		}
		index, apiErr := controller.CacheIndex()
		if apiErr != nil {
			writeContentError(w, r, apiErr)
			return
		}
		if !validCacheIndex(index) {
			writeContentError(w, r, internalContentResponseError())
			return
		}
		writeJSON(w, http.StatusOK, index)
	})
}

func validCacheIndex(index protocol.CacheIndex) bool {
	if index.UsedBytes < 0 || index.MaxBytes < 0 || index.FreeBytes < 0 {
		return false
	}
	if index.Entries == nil {
		return false
	}
	for _, entry := range index.Entries {
		if protocol.ValidateSystem(entry.System) != nil {
			return false
		}
		if protocol.ValidateContentKey(protocol.ContentKey{SHA256: entry.SHA256, Extension: entry.Extension}) != nil {
			return false
		}
		if entry.Size < 0 {
			return false
		}
	}
	return true
}

func cachedIdentityHandler(controller CachedIdentityController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gameID := r.PathValue("game_id")
		if err := protocol.ValidateGameID(gameID); err != nil {
			writeContentError(w, r, badContentRequest("game ID is invalid"))
			return
		}
		response, apiErr := controller.LookupCachedIdentity(r.Context(), gameID)
		if apiErr != nil {
			writeContentError(w, r, apiErr)
			return
		}
		if !validIdentityResponse(response, gameID) {
			writeContentError(w, r, internalContentResponseError())
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func validIdentityResponse(response protocol.CachedIdentityResponse, gameID string) bool {
	if !response.Present {
		return response.GameID == "" && response.System == nil && response.Content == nil
	}
	return response.GameID == gameID && response.System != nil && response.Content != nil &&
		protocol.ValidateSystem(*response.System) == nil && protocol.ValidateContentIdentity(*response.Content) == nil
}

func cacheContentHandler(controller ContentController) http.Handler {
	probe := probeContentHandler(controller)
	put := putContentHandler(controller)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			probe.ServeHTTP(w, r)
		case http.MethodPut:
			put.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, PUT")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func exactMethod(method string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func probeContentHandler(controller ContentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		system, key, apiErr := contentKeyFromRequest(r)
		if apiErr != nil {
			writeContentError(w, r, apiErr)
			return
		}
		setRequestContent(r, system, key.SHA256, 0, false)
		response, apiErr := controller.ProbeContent(r.Context(), system, key)
		if apiErr != nil {
			writeContentError(w, r, apiErr)
			return
		}
		if !validProbeResponse(response, system, key) {
			writeContentError(w, r, internalContentResponseError())
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func putContentHandler(controller ContentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		system, key, apiErr := contentKeyFromRequest(r)
		if apiErr != nil {
			writeContentError(w, r, apiErr)
			return
		}
		if !exactContentType(r, "application/octet-stream") {
			writeContentError(w, r, badContentRequest("content upload requires application/octet-stream"))
			return
		}
		if len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > protocol.MaxContentBytes {
			writeContentError(w, r, badContentRequest("content upload requires a valid Content-Length"))
			return
		}
		identity := protocol.ContentIdentity{SHA256: key.SHA256, Size: r.ContentLength, Extension: key.Extension}
		if err := protocol.ValidateContentIdentity(identity); err != nil {
			writeContentError(w, r, badContentRequest("content upload identity is invalid"))
			return
		}
		setRequestContent(r, system, identity.SHA256, identity.Size, true)
		r.Body = http.MaxBytesReader(w, r.Body, protocol.MaxContentBytes+1)
		response, apiErr := controller.PutContent(r.Context(), system, identity, r.Body)
		if apiErr != nil {
			writeContentError(w, r, apiErr)
			return
		}
		if !validUploadResponse(response, system, identity) {
			writeContentError(w, r, internalContentResponseError())
			return
		}
		writeJSON(w, http.StatusOK, response)
	})
}

func contentKeyFromRequest(r *http.Request) (protocol.System, protocol.ContentKey, *protocol.APIError) {
	system := protocol.System(r.PathValue("system"))
	if err := protocol.ValidateSystem(system); err != nil {
		return "", protocol.ContentKey{}, unsupportedContentSystem()
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) != 1 {
		return "", protocol.ContentKey{}, badContentRequest("content query must contain exactly one extension")
	}
	extensions, ok := values["extension"]
	if !ok || len(extensions) != 1 {
		return "", protocol.ContentKey{}, badContentRequest("content query must contain exactly one extension")
	}
	key := protocol.ContentKey{SHA256: r.PathValue("sha256"), Extension: extensions[0]}
	if err := protocol.ValidateContentKey(key); err != nil {
		return "", protocol.ContentKey{}, badContentRequest("content key is invalid")
	}
	return system, key, nil
}

func exactContentType(r *http.Request, expected string) bool {
	values := r.Header.Values("Content-Type")
	return len(values) == 1 && values[0] == expected
}

func validProbeResponse(response protocol.CacheProbeResponse, system protocol.System, key protocol.ContentKey) bool {
	if !response.Present {
		return response.System == nil && response.Content == nil
	}
	return response.System != nil && *response.System == system &&
		response.Content != nil && response.Content.Key() == key &&
		protocol.ValidateContentIdentity(*response.Content) == nil
}

func validUploadResponse(response protocol.CacheUploadResponse, system protocol.System, identity protocol.ContentIdentity) bool {
	if response.Result != protocol.CacheUploadPresent && response.Result != protocol.CacheUploadCreated {
		return false
	}
	return response.System == system && response.Content == identity && protocol.ValidateContentIdentity(response.Content) == nil
}

func writeContentError(w http.ResponseWriter, r *http.Request, apiErr *protocol.APIError) {
	safe := sanitizedContentError(apiErr.Code)
	setRequestError(r, safe.Code)
	status := statusForError(safe.Code)
	if safe.Code == protocol.CodeTransferFailed && r.Context().Err() != nil {
		status = statusClientClosedRequest
	}
	writeAPIError(w, status, safe)
}

func sanitizedContentError(code protocol.ErrorCode) *protocol.APIError {
	var message string
	switch code {
	case protocol.CodeBadRequest:
		message = "content request is invalid"
	case protocol.CodeUnauthorized:
		message = "missing or incorrect bearer token"
	case protocol.CodeROMNotFound:
		message = "requested ROM was not found"
	case protocol.CodeBusy:
		message = "another launch or stop transition is running"
	case protocol.CodeUnsupportedSystem:
		message = "system or content extension is unsupported"
	case protocol.CodeInvalidROMPath:
		message = "ROM path is invalid"
	case protocol.CodeMiSTerUnavailable:
		message = "MiSTer is unavailable"
	case protocol.CodeCoreTimeout:
		message = "core transition timed out"
	case protocol.CodeInternal:
		message = "content operation failed internally"
	case protocol.CodeUnrecognizedCore:
		message = "active core is unrecognized"
	case protocol.CodeSourceUnavailable:
		message = "content source is unavailable"
	case protocol.CodeInvalidArchive:
		message = "content archive is invalid"
	case protocol.CodeTransferFailed:
		message = "content transfer failed"
	case protocol.CodeDigestMismatch:
		message = "content digest does not match its identity"
	case protocol.CodeContentNotCached:
		message = "requested content is not present in the verified cache"
	case protocol.CodeCacheFull:
		message = "target cache has insufficient safe capacity"
	default:
		return &protocol.APIError{Code: protocol.CodeInternal, Message: "content operation failed internally"}
	}
	return &protocol.APIError{Code: code, Message: message}
}

func badContentRequest(message string) *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeBadRequest, Message: message}
}

func unsupportedContentSystem() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeUnsupportedSystem, Message: "system is not Mega Drive or SNES"}
}

func internalContentResponseError() *protocol.APIError {
	return &protocol.APIError{Code: protocol.CodeInternal, Message: "content controller returned an invalid response"}
}
