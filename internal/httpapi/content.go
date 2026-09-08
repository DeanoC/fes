package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/internal/cast"
	"github.com/DeanoC/FogCast/internal/core"
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
	LaunchContent(context.Context, protocol.CachedLaunchRequest) (protocol.CachedLaunchResponse, *protocol.APIError)
}

type serverOptions struct {
	targetID    string
	kitLease    *kitlease.Manager
	content     ContentController
	input       InputController
	cast        CastController
	development DevelopmentController
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

func registerContentRoutes(mux *http.ServeMux, token string, controller ContentController) {
	mux.Handle("/v2/cache/{system}/{sha256}", authenticate(token, cacheContentHandler(controller)))
	mux.Handle("/v2/launch", authenticate(token, exactMethod(http.MethodPost, launchContentHandler(controller))))
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

func launchContentHandler(controller ContentController) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !exactContentType(r, "application/json") {
			writeContentError(w, r, badContentRequest("content launch requires application/json"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCachedLaunchJSONBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request protocol.CachedLaunchRequest
		if err := decoder.Decode(&request); err != nil {
			writeContentError(w, r, badContentRequest("request body must contain one valid content launch object"))
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeContentError(w, r, badContentRequest("request body must contain exactly one JSON object"))
			return
		}
		if err := protocol.ValidateGameID(request.GameID); err != nil {
			writeContentError(w, r, badContentRequest("game ID is invalid"))
			return
		}
		if err := protocol.ValidateSystem(request.System); err != nil {
			writeContentError(w, r, unsupportedContentSystem())
			return
		}
		if err := protocol.ValidateContentIdentity(request.Content); err != nil {
			writeContentError(w, r, badContentRequest("content identity is invalid"))
			return
		}
		setRequestLaunchContent(r, request)
		response, apiErr := controller.LaunchContent(r.Context(), request)
		if apiErr != nil {
			writeContentError(w, r, apiErr)
			return
		}
		if !validLaunchResponse(response, request) {
			writeContentError(w, r, internalContentResponseError())
			return
		}
		setRequestState(r, protocol.Status{State: protocol.StateActive})
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

func validLaunchResponse(response protocol.CachedLaunchResponse, request protocol.CachedLaunchRequest) bool {
	spec, ok := core.DefaultRegistry().Lookup(request.System)
	if !ok {
		return false
	}
	return response.Content == request.Content &&
		response.Status.State == protocol.StateActive &&
		response.Status.GameID != nil && *response.Status.GameID == request.GameID &&
		response.Status.System != nil && *response.Status.System == request.System &&
		response.Status.ExpectedCore != nil && *response.Status.ExpectedCore == spec.ExpectedCore &&
		response.Status.ObservedCore != nil && *response.Status.ObservedCore == spec.ExpectedCore &&
		response.Status.LastError == nil
}

func setRequestLaunchContent(r *http.Request, request protocol.CachedLaunchRequest) {
	setRequestLaunch(r, protocol.LaunchRequest{GameID: request.GameID, System: request.System})
	setRequestContent(r, request.System, request.Content.SHA256, request.Content.Size, true)
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
