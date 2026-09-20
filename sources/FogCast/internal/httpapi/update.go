package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"syscall"
	"time"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/applianceupdate"
	"github.com/DeanoC/FogCast/protocol"
)

const ReleaseManifestHeader = "X-FogCast-Release-Manifest"

func WithUpdate(service *applianceupdate.Service) Option {
	return func(o *serverOptions) { o.update = service }
}

func updateError(w http.ResponseWriter, err error) {
	status, code := http.StatusUnprocessableEntity, "UPDATE_INVALID"
	switch {
	case errors.Is(err, applianceupdate.ErrBlocked), errors.Is(err, appliance.ErrBusy), errors.Is(err, applianceupdate.ErrIdentity):
		status, code = http.StatusConflict, "UPDATE_CONFLICT"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code = statusClientClosedRequest, "UPDATE_CANCELLED"
	case errors.Is(err, syscall.ENOSPC):
		status, code = http.StatusInsufficientStorage, "UPDATE_NO_SPACE"
	}
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		writeAPIError(w, statusForError(apiErr.Code), apiErr)
		return
	}
	writeError(w, status, code, code)
}

func registerUpdateRoutes(mux *http.ServeMux, token string, s *applianceupdate.Service, leased bool) {
	mux.Handle("GET /v1/update", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		status, err := s.Status(ctx)
		if err != nil {
			updateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})))
	mutation := func(h http.HandlerFunc) http.Handler {
		return authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !leased {
				writeError(w, http.StatusServiceUnavailable, "KIT_LEASE_REQUIRED", "update mutations require kit lease support")
				return
			}
			h(w, r)
		}))
	}
	mux.Handle("POST /v1/update/stage", mutation(func(w http.ResponseWriter, r *http.Request) {
		headers := r.Header.Values(ReleaseManifestHeader)
		if !exactContentType(r, "application/octet-stream") || len(headers) != 1 || len(headers[0]) > 16<<10 || len(r.TransferEncoding) != 0 || r.ContentLength < 1 || r.ContentLength > release.MaxImageSize {
			writeBadRequest(w, r, "upload requires a bounded raw image and one release manifest header")
			return
		}
		data, err := base64.StdEncoding.Strict().DecodeString(headers[0])
		if err != nil {
			writeBadRequest(w, r, "invalid encoded release manifest")
			return
		}
		m, err := release.DecodeManifest(bytes.NewReader(data))
		if err != nil || m.ImageSize != r.ContentLength {
			writeBadRequest(w, r, "release manifest or upload length is invalid")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, m.ImageSize+1)
		if err = s.Stage(r.Context(), m, r.ContentLength, r.Body); err != nil {
			updateError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, m)
	}))
	mux.Handle("POST /v1/update/activate", mutation(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ImageSHA256 string `json:"image_sha256"`
		}
		if !decodeUpdateJSON(w, r, &req) {
			return
		}
		finish, err := s.Activate(r.Context(), req.ImageSHA256)
		if err != nil {
			updateError(w, err)
			return
		}
		finishUpdateResponse(w, r, finish)
	}))
	mux.Handle("POST /v1/update/rollback", mutation(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1)
		b, err := io.ReadAll(r.Body)
		if err != nil || len(b) != 0 {
			writeBadRequest(w, r, "rollback body must be empty")
			return
		}
		finish, err := s.Rollback(r.Context())
		if err != nil {
			updateError(w, err)
			return
		}
		finishUpdateResponse(w, r, finish)
	}))
	mux.Handle("POST /v1/update/confirm", mutation(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			BootID      string `json:"boot_id"`
			ImageSHA256 string `json:"image_sha256"`
		}
		if !decodeUpdateJSON(w, r, &req) {
			return
		}
		if err := s.Confirm(r.Context(), req.BootID, req.ImageSHA256); err != nil {
			updateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Confirmed bool `json:"confirmed"`
		}{true})
	}))
}

func decodeUpdateJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !exactContentType(r, "application/json") {
		writeBadRequest(w, r, "update request requires application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil || d.Decode(&struct{}{}) != io.EOF {
		writeBadRequest(w, r, "update body must contain one valid object")
		return false
	}
	return true
}

func finishUpdateResponse(w http.ResponseWriter, r *http.Request, finish func(context.Context) error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, err := io.WriteString(w, "{\"rebooting\":true}\n")
	if err == nil {
		err = http.NewResponseController(w).Flush()
	}
	if err != nil {
		ctx, cancel := context.WithCancel(r.Context())
		cancel()
		_ = finish(ctx)
		return
	}
	// The response is already committed. A lost reply or reboot failure is
	// resolved by inspecting pending selection and the actual boot identity.
	_ = finish(r.Context())
}

func guardUpdateAdmission(next http.Handler, token string, s *applianceupdate.Service) http.Handler {
	guarded := authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, done, err := s.BeginOperation(r.Context())
		if err != nil {
			updateError(w, err)
			return
		}
		defer done()
		cancelled := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { defer close(cancelled); _ = http.NewResponseController(w).SetReadDeadline(time.Now()) })
		defer func() {
			if !stop() {
				<-cancelled
			}
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/development/media-stream", "/v1/development/media", "/v1/development/firmware", "/v1/library/core/load", "/v1/library/core/settings", "/v1/launch", "/v2/launch", "/v1/development/rbf", "/v1/development/core", "/v1/development/reboot", "/v1/input/attach", "/v1/input/detach", "/v1/input/stream", "/v1/cast/start", "/v1/cast/stop":
			guarded.ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}
