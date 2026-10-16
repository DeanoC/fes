package gfx

import (
	"encoding/json"
	"os"
)

// reportSoftwareReady is used only by the managed kit menu launcher. Readiness
// follows a successful runtime frame acknowledgement, not process creation.
func reportSoftwareReady(ready bool) {
	path, id := os.Getenv("FES_SOFTWARE_READY_FILE"), os.Getenv("FES_SOFTWARE_BUNDLE_ID")
	if path == "" || id == "" {
		return
	}
	if !ready {
		_ = os.Remove(path)
		return
	}
	data, err := json.Marshal(struct {
		BundleID string `json:"bundle_id"`
		PID      int    `json:"pid"`
	}{id, os.Getpid()})
	if err != nil {
		return
	}
	temp := path + ".tmp"
	if os.WriteFile(temp, data, 0600) == nil {
		_ = os.Rename(temp, path)
	}
}
