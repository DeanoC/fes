package hostapi

import (
	"strings"
	"testing"
)

func TestPairedStoragePathsAreExplicit(t *testing.T) {
	id := strings.Repeat("a", 24)
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/api/v1/storage/drives", true}, {"PUT", "/api/v1/storage/drives", true},
		{"GET", "/api/v1/storage/drives/" + id + "/entries", true}, {"GET", "/api/v1/storage/drives/" + id + "/file", true},
		{"DELETE", "/api/v1/storage/drives/" + id, true}, {"GET", "/api/v1/storage/drives/../../etc/file", false},
		{"POST", "/api/v1/storage/drives/" + id + "/file", false}, {"GET", "/api/v1/storage/drives/" + id + "/entries/extra", false},
	} {
		if got := launcherOperation(tc.method, tc.path); got != tc.allowed {
			t.Errorf("%s %s: %v", tc.method, tc.path, got)
		}
	}
}
