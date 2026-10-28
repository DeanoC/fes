package hostapi

import "testing"

// Keep this inventory aligned with production requests made by tenfoot's
// paired host client. Settings remains intentionally unsupported because its
// response contains host filesystem roots; lease polling uses the paired kit lease projection.
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
		{"GET", "/api/v1/launcher/kit-lease", true},
		{"GET", "/v1/kit/lease", false},
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
		{"GET", "/api/v1/library/core-entries", true},
		{"POST", "/api/v1/library/core-entries", true},
		{"PUT", "/api/v1/library/core-entries", false},
		{"PATCH", "/api/v1/library/core-entries", false},
		{"DELETE", "/api/v1/library/core-entries", false},
		{"POST", "/api/v1/library/core-entries/", false},
		{"GET", "/api/v1/library/core-entries/fpga-zx81", true},
		{"GET", "/api/v1/library/core-entries/Nope", false},
		{"GET", "/api/v1/library/core-entries/fpga-zx81/rom", true},
		{"GET", "/api/v1/library/core-entries/Nope/rom", false},
		{"GET", "/api/v1/library/core-entries/fpga-zx81/rom/extra", false},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81", true},
		{"PUT", "/api/v1/library/core-entries/Nope", false},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81/presentation", false},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81/extra", false},
		{"POST", "/api/v1/library/core-entries/fpga-zx81", false},
		{"GET", "/api/v1/core-packages", true},
		{"GET", "/api/v1/core-packages/" + tenfootTestArtworkID, true},
		{"GET", "/api/v1/core-packages/" + tenfootTestArtworkID + "/media-capabilities", true},
		{"GET", "/api/v1/core-packages/" + tenfootTestArtworkID + "/compatibility", false},
		{"GET", "/api/v1/core-catalog", true},
		{"GET", "/api/v1/core-catalog/fes.zx81/setup", true},
		{"GET", "/api/v1/core-catalog/fes.spectrum-plus3/setup", true},
		{"GET", "/api/v1/core-catalog/fes.zx81/setup/extra", false},
		{"GET", "/api/v1/core-catalog/fes.zx81/setup/", false},
		{"GET", "/api/v1/core-catalog/Nope/setup", false},
		{"POST", "/api/v1/core-catalog/install/extra", false},
		{"POST", "/api/v1/core-catalog/entries/extra", false},
		{"POST", "/api/v1/library/atari-st-disks", true},
		{"POST", "/api/v1/library/atari-st-disks/extra", false},
		{"GET", "/api/v1/library/hardware", true},
		{"GET", "/api/v1/core-expansions", true},
		{"POST", "/api/v1/core-expansions", true},
		{"GET", "/api/v1/library/core-entries/fpga-c64/expansions", true},
		{"GET", "/api/v1/library/core-entries/fpga-c64/expansions/1", false},
		{"PUT", "/api/v1/library/core-entries/fpga-c64/expansions/7", true},
		{"PUT", "/api/v1/library/core-entries/fpga-c64/expansions/0", false},
		{"PUT", "/api/v1/library/core-entries/fpga-c64/expansions/8", false},
		{"PUT", "/api/v1/library/core-entries/fpga-c64/expansions/01", false},
		{"PUT", "/api/v1/library/core-entries/Nope/expansions/1", false},
		{"PUT", "/api/v1/library/core-entries/fpga-c64/expansions/1/extra", false},
		{"GET", "/api/v1/library/titles", true},
		{"GET", "/api/v1/library/zx81-tapes", true},
		{"POST", "/api/v1/library/zx81-tapes/guess-number/import", true},
		{"POST", "/api/v1/library/zx81-tapes/unknown/import", false},
		{"POST", "/api/v1/library/zx81-tapes/../settings/import", false},
		{"POST", "/api/v1/session/display", true},
		{"GET", "/api/v1/library/firmware", true},
		{"GET", "/api/v1/library/core-entries/fpga-zx81/expansion", true},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81/expansion", true},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81/expansion/extra", false},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81/rom", true},
		{"PUT", "/api/v1/library/core-entries/Nope/rom", false},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81/rom/extra", false},
		{"POST", "/api/v1/library/core-entries/fpga-zx81/rom", false},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81/expansions/1", true},
		{"PUT", "/api/v1/library/core-entries/Nope/expansion", false},
		{"PUT", "/api/v1/library/core-entries/fpga-zx81/../expansion", false},
		{"PUT", "/api/v1/core-expansions/" + tenfootTestArtworkID + "/presentation", false},
		{"GET", "/api/v1/core-expansions/expansion-id/presentation", false},
		{"GET", "/api/v1/presentation/games/fpga-zx81", true},
		{"GET", "/api/v1/presentation/artwork/" + tenfootTestArtworkID, true},
		{"POST", "/api/v1/session/launch", true},
		{"POST", "/api/v1/session/stop", true},
		{"POST", "/api/v1/session/input/attach", false},
		{"POST", "/api/v1/session/input/detach", false},
		{"POST", "/api/v1/session/input/event", false},
		{"POST", "/api/v1/session/development-rbf", false},
		{"POST", "/api/v1/session/live-media", true},
		{"POST", "/api/v1/session/live-media/clear", true},
		{"POST", "/api/v1/core-media", true},
		{"POST", "/api/v1/core-catalog/install", true},
		{"POST", "/api/v1/core-catalog/entries", true},
		{"PUT", "/api/v1/library/firmware", true},
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

func TestLibraryTitlesIsPairedReadNotMeshContent(t *testing.T) {
	if launcherMeshContentRead("GET", "/api/v1/library/titles") {
		t.Fatal("library titles admitted as a mesh content read")
	}
	if !launcherPairedRead("GET", "/api/v1/library/titles") || !launcherOperation("GET", "/api/v1/library/titles") {
		t.Fatal("library titles is not a paired launcher read")
	}
}

const tenfootTestArtworkID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
