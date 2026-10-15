package gfx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSoftwareReadinessTracksSuccessfulDisplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher-ready.json")
	t.Setenv("FES_SOFTWARE_READY_FILE", path)
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", "release")
	reportSoftwareReady(false)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	reportSoftwareReady(true)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ready struct {
		BundleID string `json:"bundle_id"`
		PID      int    `json:"pid"`
	}
	if err = json.Unmarshal(data, &ready); err != nil || ready.BundleID != "release" || ready.PID != os.Getpid() {
		t.Fatalf("%s %v", data, err)
	}
	reportSoftwareReady(false)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
