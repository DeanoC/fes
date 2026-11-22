package hostapi

import (
	"net/http"
	"testing"
)

func TestLauncherCoreEntryMediaSelectionPath(t *testing.T) {
	bareMediaName := "/api/v1/library/core-entries/media-name"
	if launcherCoreEntrySubPath(bareMediaName, "/media-name") {
		t.Fatal("bare media-name path was admitted as a named media selection")
	}
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/media", true},
		{http.MethodPut, "/api/v1/library/core-entries/Nope/media", false},
		{http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/media/extra", false},
		{http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/media/", false},
		{http.MethodGet, "/api/v1/library/core-entries/fpga-zx81/media", false},
		{http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/media-name", true},
		{http.MethodPut, bareMediaName, true},
		{http.MethodPut, "/api/v1/library/core-entries/Nope/media-name", false},
		{http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/media-name/extra", false},
		{http.MethodGet, "/api/v1/library/core-entries/fpga-zx81/media-name", false},
		{http.MethodPut, "/api/v1/library/core-entries/fpga-atari-st-xenon/media-set", true},
		{http.MethodPut, "/api/v1/library/core-entries/Nope/media-set", false},
		{http.MethodPut, "/api/v1/library/core-entries/fpga-zx81/media-set/extra", false},
		{http.MethodGet, "/api/v1/library/core-entries/fpga-zx81/media-set", false},
	} {
		if got := launcherOperation(tc.method, tc.path); got != tc.allowed {
			t.Errorf("%s %s: got %v, want %v", tc.method, tc.path, got, tc.allowed)
		}
	}
}
