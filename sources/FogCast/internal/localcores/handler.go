package localcores

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Handler is the no-bearer local-control API.
func Handler(service *Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/local/cores", func(w http.ResponseWriter, r *http.Request) {
		cores := service.List()
		if cores == nil {
			cores = []Core{}
		}
		writeJSON(w, http.StatusOK, cores)
	})
	mux.HandleFunc("POST /v1/local/cores/{package_id}/launch", func(w http.ResponseWriter, r *http.Request) {
		core, err := service.Launch(r.Context(), r.PathValue("package_id"))
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

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		writeLocalError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, errBlocked):
		writeLocalError(w, http.StatusConflict, "blocked")
	case errors.Is(err, errInUse):
		writeLocalError(w, http.StatusConflict, "in_use")
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
