package localcores

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// Handler is the no-bearer local-control API.
func Handler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/local/cores", func(w http.ResponseWriter, r *http.Request) {
		// An unreadable durable library is HTTP 200 and an empty list.
		// Bearer /v1/core-packages uses HTTP 503 for that failure.
		// A catalog warm still in progress is HTTP 503 warming. The client
		// retries that; it is not an empty catalog.
		// A disconnected client cancels this wait. The shared catalog scan
		// is not started again by the retry.
		cores, err := service.list(r.Context())
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			if errors.Is(err, errWarming) {
				// The catalog warm is still reading package bytes. The client
				// retries; an empty 200 would be cached as a finished list.
				writeLocalError(w, http.StatusServiceUnavailable, "warming")
				return
			}
			writeLocalError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		if cores == nil {
			cores = []Core{}
		}
		writeJSON(w, http.StatusOK, cores)
	})
	return mux
}

func writeLocalError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: code})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
