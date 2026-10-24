package hostapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DeanoC/FogCast/storage"
)

func WithStorage(manager *storage.Manager) ServerOption {
	return func(options *serverOptions) { options.storage = manager }
}

// storageOperation is also the paired-launcher allowlist. Registered drive IDs
// and relative paths are the only filesystem authority carried over the wire.
func storageOperation(method, path string) bool {
	if path == "/api/v1/storage/drives" {
		return method == http.MethodGet || method == http.MethodPut
	}
	const prefix = "/api/v1/storage/drives/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) < 1 || len(parts[0]) != 24 || strings.Trim(parts[0], "0123456789abcdef") != "" {
		return false
	}
	if len(parts) == 1 {
		return method == http.MethodDelete
	}
	return len(parts) == 2 && method == http.MethodGet && (parts[1] == "entries" || parts[1] == "file")
}

func registerStorage(mux *http.ServeMux, m *storage.Manager) {
	if m == nil {
		return
	}
	run := func(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			if err := fn(w, r.WithContext(ctx)); err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, os.ErrNotExist) {
					status = http.StatusNotFound
				}
				// OS and helper errors can contain private mount paths. Keep those local.
				writeError(w, status, "STORAGE_UNAVAILABLE", "Drive unavailable. Check the drive, share, credentials and selected folder.")
			}
		}
	}
	mux.HandleFunc("GET /api/v1/storage/drives", run(func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.RawQuery != "" && r.URL.RawQuery != "discover=true" {
			return errors.New("invalid query")
		}
		drives, err := m.List(r.Context(), r.URL.Query().Get("discover") == "true")
		// Saved shares remain usable when lsblk is unavailable on a desktop host.
		if drives == nil && err != nil {
			return err
		}
		result := map[string]any{"drives": drives}
		if err != nil {
			result["notice"] = "USB discovery unavailable on this host"
		}
		writeJSON(w, http.StatusOK, result)
		return nil
	}))
	mux.HandleFunc("PUT /api/v1/storage/drives", run(func(w http.ResponseWriter, r *http.Request) error {
		var drive storage.Drive
		if err := decodeSingleJSON(w, r, &drive); err != nil {
			return nil // decodeSingleJSON already wrote the response.
		}
		saved, err := m.Save(drive)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, saved)
		return nil
	}))
	mux.HandleFunc("DELETE /api/v1/storage/drives/{id}", run(func(w http.ResponseWriter, r *http.Request) error {
		if err := m.Forget(r.PathValue("id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	mux.HandleFunc("GET /api/v1/storage/drives/{id}/entries", run(func(w http.ResponseWriter, r *http.Request) error {
		entries, err := m.Entries(r.Context(), r.PathValue("id"), r.URL.Query().Get("path"))
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
		return nil
	}))
	mux.HandleFunc("GET /api/v1/storage/drives/{id}/file", run(func(w http.ResponseWriter, r *http.Request) error {
		query := r.URL.Query()
		if len(query["path"]) > 1 || len(query["max_bytes"]) > 1 {
			return errors.New("invalid file query")
		}
		limit := storage.MaxFileBytes
		if value, present := query["max_bytes"]; present {
			requested, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil || requested < 1 || requested > storage.MaxExtendedFileBytes {
				return errors.New("invalid file limit")
			}
			limit = requested
		}
		file, size, err := m.OpenWithLimit(r.Context(), r.PathValue("id"), query.Get("path"), limit)
		if err != nil {
			return err
		}
		defer file.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		// The descriptor and its size are bound before sending any bytes.
		_, _ = io.CopyN(w, file, size)
		return nil
	}))
}
