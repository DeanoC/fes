package localcores

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Handler is the no-bearer local-control API.
func Handler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/local/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, service.RunStatus())
	})
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
	mux.HandleFunc("POST /v1/local/cores/{package_id}/launch", func(w http.ResponseWriter, r *http.Request) {
		core, err := launchFromRequest(service, r)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			OK        bool   `json:"ok"`
			PackageID string `json:"package_id"`
			CoreID    string `json:"core_id"`
		}{true, core.PackageID, core.CoreID})
	})
	mux.HandleFunc("POST /v1/local/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := service.Stop(r.Context()); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			OK bool `json:"ok"`
		}{true})
	})
	return mux
}

// launchFromRequest keeps an empty body on the package launch. A JSON
// rom_path reads that local file and launches the cartridge. It does not
// open a network connection.
func launchFromRequest(service *Service, r *http.Request) (Core, error) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return Core{}, errUnavailable
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return service.Launch(r.Context(), r.PathValue("package_id"))
	}
	var body struct {
		ROMPath string `json:"rom_path"`
	}
	if json.Unmarshal(data, &body) != nil {
		return Core{}, errUnavailable
	}
	if strings.TrimSpace(body.ROMPath) == "" {
		return service.Launch(r.Context(), r.PathValue("package_id"))
	}
	rom, err := readCartridgeFile(body.ROMPath)
	if err != nil {
		return Core{}, err
	}
	return service.LaunchCartridge(r.Context(), r.PathValue("package_id"), rom)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		writeLocalError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, errBlocked):
		writeLocalError(w, http.StatusConflict, "blocked")
	case errors.Is(err, errInUse):
		writeLocalError(w, http.StatusConflict, "in_use")
	case errors.Is(err, errWarming):
		writeLocalError(w, http.StatusServiceUnavailable, "warming")
	default:
		writeLocalError(w, http.StatusServiceUnavailable, "unavailable")
	}
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
