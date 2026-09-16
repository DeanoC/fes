package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	internallease "github.com/DeanoC/FogCast/internal/kitlease"
	contractlease "github.com/DeanoC/FogCast/kitlease"
)

const KitLeaseHeader = "X-FogCast-Kit-Lease"

func WithKitLease(manager *internallease.Manager) Option {
	return func(options *serverOptions) { options.kitLease = manager }
}

func leaseError(w http.ResponseWriter, err error) {
	status, code := http.StatusConflict, "KIT_LEASE_BUSY"
	switch {
	case errors.Is(err, internallease.ErrInvalid):
		status, code = http.StatusBadRequest, "KIT_LEASE_INVALID"
	case errors.Is(err, internallease.ErrLease):
		status, code = http.StatusForbidden, "KIT_LEASE_REQUIRED"
	case errors.Is(err, internallease.ErrBlocked):
		status, code = http.StatusServiceUnavailable, "KIT_LEASE_BLOCKED"
	}
	writeError(w, status, code, code)
}

func registerKitLeaseRoutes(mux *http.ServeMux, token string, manager *internallease.Manager) {
	mux.Handle("GET /v1/kit/lease", authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, manager.Status()) })))
	for _, action := range []string{"claim", "renew", "release", "takeover"} {
		mux.Handle("POST /v1/kit/"+action, authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 8192)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			var result any
			var err error
			switch action {
			case "claim":
				var request contractlease.ClaimRequest
				if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
					leaseError(w, internallease.ErrInvalid)
					return
				}
				result, err = manager.Claim(request)
			case "takeover":
				var request contractlease.TakeoverRequest
				if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
					leaseError(w, internallease.ErrInvalid)
					return
				}
				result, err = manager.Takeover(request)
			default:
				body, readErr := io.ReadAll(r.Body)
				if readErr != nil || len(body) != 0 {
					leaseError(w, internallease.ErrInvalid)
					return
				}
				if action == "renew" {
					result, err = manager.Renew(r.Header.Get(KitLeaseHeader))
				} else {
					result, err = manager.Release(r.Header.Get(KitLeaseHeader))
				}
			}
			if err != nil {
				leaseError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
		})))
	}
}

func hostlessMutationDenied(status contractlease.Status, path string) bool {
	if status.Owner != contractlease.HostlessOwner {
		return false
	}
	if !contractlease.HostlessSession(status) {
		return true
	}
	switch path {
	case "/v2/launch", "/v1/stop", "/v1/input/attach", "/v1/input/detach", "/v1/input/stream":
		return false
	default:
		return true
	}
}

func guardKitLease(next http.Handler, token string, manager *internallease.Manager) http.Handler {
	guarded := authenticate(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaseContext, done, err := manager.Begin(r.Header.Get(KitLeaseHeader))
		if err != nil {
			leaseError(w, err)
			return
		}
		defer done()
		if hostlessMutationDenied(manager.Status(), r.URL.Path) {
			writeError(w, http.StatusForbidden, "KIT_LEASE_DENIED", "hostless owner cannot mutate this path")
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		// Cancellation must interrupt a stalled upload, including net/http body reads.
		cancelled := make(chan struct{})
		stop := context.AfterFunc(leaseContext, func() {
			defer close(cancelled)
			cancel()
			_ = http.NewResponseController(w).SetReadDeadline(time.Now())
		})
		defer func() {
			if !stop() {
				<-cancelled
			}
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/development/media", "/v1/library/core/load", "/v1/library/core/settings", "/v1/launch", "/v1/stop", "/v2/launch", "/v1/development/rbf", "/v1/development/core", "/v1/development/reboot", "/v1/kit/debug/snapshot-before-reboot", "/v1/input/attach", "/v1/input/detach", "/v1/input/stream", "/v1/cast/start", "/v1/cast/stop", "/v1/update/stage", "/v1/update/activate", "/v1/update/rollback", "/v1/update/confirm":
			guarded.ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}
