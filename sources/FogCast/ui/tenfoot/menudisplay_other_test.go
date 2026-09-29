//go:build !linux

package tenfoot

import (
	"path/filepath"
	"testing"
)

func TestRunMenuDisplayRequiresLinux(t *testing.T) {
	err := Run(t.Context(), Options{
		GFX:        "menu-display",
		MenuSocket: filepath.Join(t.TempDir(), "missing.sock"),
		Input:      "none",
		PrefsPath:  filepath.Join(t.TempDir(), "prefs.json"),
	})
	if err == nil || err.Error() != "menu-display requires Linux" {
		t.Fatalf("err = %v", err)
	}
}
