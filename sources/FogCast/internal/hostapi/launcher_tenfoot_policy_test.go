package hostapi

import "testing"

// Keep this inventory aligned with production requests made by tenfoot's
// paired host client. Settings remains intentionally unsupported because its
// response contains host filesystem roots; lease polling uses launcher/target.
func TestTenfootKitRequestPathsMatchLauncherPolicy(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"GET", "/api/v1/games", true},
		{"GET", "/api/v1/games/fpga-zx81", true},
		{"GET", "/api/v1/games/../settings", false},
		{"GET", "/api/v1/platforms", true},
		{"GET", "/api/v1/health", true},
		{"GET", "/api/v1/status", true},
		{"GET", "/api/v1/session", true},
		{"GET", "/api/v1/session/input", true},
		{"GET", "/api/v1/sessions", false},
		{"GET", "/api/v1/session/events", false},
		{"GET", "/api/v1/session/preview", false},
		{"GET", "/api/v1/library/attract", true},
		{"GET", "/api/v1/library/cache", true},
		{"GET", "/api/v1/library/collections", true},
		{"GET", "/api/v1/library/facets", true},
		{"GET", "/api/v1/library/settings", false},
		{"GET", "/api/v1/library/edition-preferences", false},
		{"GET", "/api/v1/library/core-entries", false},
		{"GET", "/api/v1/core-packages", false},
		{"GET", "/api/v1/core-catalog", false},
		{"GET", "/api/v1/core-catalog/fes.zx81/setup", false},
		{"GET", "/api/v1/library/hardware", false},
		{"GET", "/api/v1/library/firmware", false},
		{"GET", "/api/v1/library/core-entries/fpga-zx81/expansion", false},
		{"GET", "/api/v1/core-expansions/expansion-id/presentation", false},
		{"GET", "/api/v1/launcher/target", true},
		{"GET", "/api/v1/presentation/games/fpga-zx81", true},
		{"GET", "/api/v1/presentation/artwork/" + tenfootTestArtworkID, true},
		{"POST", "/api/v1/session/launch", true},
		{"POST", "/api/v1/session/stop", true},
		{"POST", "/api/v1/session/input/attach", false},
		{"POST", "/api/v1/session/input/detach", false},
		{"POST", "/api/v1/session/input/event", false},
		{"POST", "/api/v1/session/development-rbf", false},
		{"POST", "/api/v1/session/live-media", false},
		{"POST", "/api/v1/session/live-media/clear", false},
		{"POST", "/api/v1/core-media", false},
		{"POST", "/api/v1/core-catalog/install", false},
		{"POST", "/api/v1/core-catalog/entries", false},
		{"PUT", "/api/v1/library/firmware", false},
		{"POST", "/api/v1/debug/ui-events", false},
		{"PATCH", "/api/v1/library/settings", false},
		{"PUT", "/api/v1/library/edition-preferences", false},
		{"PUT", "/api/v1/library/collections/collection-id/game-id", false},
		{"DELETE", "/api/v1/library/favorites/game-id", false},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			if got := launcherOperation(tc.method, tc.path); got != tc.allowed {
				t.Fatalf("launcherOperation = %t, want %t", got, tc.allowed)
			}
			if got := launcherPairedRead(tc.method, tc.path); got && !tc.allowed {
				t.Fatal("paired read admitted a request outside the documented allowlist")
			}
		})
	}
}

const tenfootTestArtworkID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
