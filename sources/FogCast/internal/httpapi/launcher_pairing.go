package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/DeanoC/FogCast/internal/atomicwrite"
	"github.com/DeanoC/FogCast/launcherconfig"
	"github.com/DeanoC/FogCast/protocol"
)

// WithLauncherPairing enables idle, lease-owned setup at the kit's fixed config
// path. The existing launcher consumes the new pairing on its next startup.
func WithLauncherPairing(path string) Option {
	return func(options *serverOptions) { options.launcherPairingPath = path }
}

func launcherPairingHandler(controller Controller, agentToken string, settings serverOptions) http.Handler {
	var lock sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var config launcherconfig.Config
		if r.URL.RawQuery != "" || decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeBadRequest(w, r, "pairing requires one launcher configuration")
			return
		}
		config, err := launcherconfig.Normalize(config)
		if err != nil || config.TargetID != settings.targetID || config.Token == agentToken {
			writeBadRequest(w, r, "launcher pairing is invalid for this target")
			return
		}
		lock.Lock()
		defer lock.Unlock()
		status := controller.Status()
		if settings.kitLocal != nil {
			status = applyKitLocalRun(status, settings.kitLocal())
		}
		if r.Context().Err() != nil {
			writeError(w, statusClientClosedRequest, "PAIRING_CANCELLED", "pairing request cancelled")
			return
		}
		if !controller.Health("").Ready || status.State != protocol.StateIdle {
			writeError(w, http.StatusConflict, "PAIRING_NOT_IDLE", "launcher pairing requires a ready idle kit")
			return
		}
		path := settings.launcherPairingPath
		info, err := os.Lstat(path)
		if err == nil {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
				writeError(w, http.StatusConflict, "PAIRING_UNAVAILABLE", "existing pairing is not a private regular file")
				return
			}
			previous, loadErr := launcherconfig.Load(path)
			if loadErr != nil || previous.TargetID != settings.targetID {
				writeError(w, http.StatusConflict, "PAIRING_UNAVAILABLE", "existing pairing identity could not be verified")
				return
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusInternalServerError, "PAIRING_UNAVAILABLE", "launcher pairing is unavailable")
			return
		}
		data, err := json.Marshal(config)
		if err == nil {
			data = append(data, '\n')
			err = r.Context().Err()
		}
		if err == nil {
			err = atomicwrite.WriteFile(path, data, 0o600)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PAIRING_UNAVAILABLE", "launcher pairing could not be saved")
			return
		}
		digest := sha256.Sum256(data)
		writeJSON(w, http.StatusOK, struct {
			API             string `json:"api"`
			TargetID        string `json:"target_id"`
			SHA256          string `json:"sha256"`
			RestartRequired bool   `json:"restart_required"`
		}{config.API, config.TargetID, hex.EncodeToString(digest[:]), true})
	})
}
