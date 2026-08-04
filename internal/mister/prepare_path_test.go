package mister

import "testing"

func TestMGLROMPathIsRelativeToCoreGamesRoot(t *testing.T) {
	if got := mglROMPath("/media/fat/games/MegaDrive", "/media/fat/games/MegaDrive/test.md", "test.md"); got != "test.md" {
		t.Fatalf("MGL path = %q", got)
	}
}

func TestMGLROMPathFallsBackForCacheOutsideCoreGamesRoot(t *testing.T) {
	if got := mglROMPath("/media/fat/games/MegaDrive", "/media/fat/fogcast/cache/megadrive/test.md", "../../fogcast/cache/megadrive/test.md"); got != "../../fogcast/cache/megadrive/test.md" {
		t.Fatalf("MGL path = %q", got)
	}
}

func TestMGLROMPathFallsBackForNonMiSTerPaths(t *testing.T) {
	if got := mglROMPath("", "/tmp/test.md", "RPGs/test.md"); got != "RPGs/test.md" {
		t.Fatalf("MGL path = %q", got)
	}
}
