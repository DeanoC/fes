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

func TestMGLROMPathUsesCurrentAtari7800CoreHomeForAtari2600(t *testing.T) {
	const coreHome = "/media/fat/games/ATARI7800"
	if got := mglROMPath(coreHome, "/media/fat/games/Atari2600/Adventure.a26", "Adventure.a26"); got != "../Atari2600/Adventure.a26" {
		t.Fatalf("direct Atari 2600 MGL path = %q", got)
	}
	if got := mglROMPath(coreHome, "/media/fat/fogcast/cache/a2600/test.a26", "../../fogcast/cache/a2600/test.a26"); got != "../../fogcast/cache/a2600/test.a26" {
		t.Fatalf("cached Atari 2600 MGL path = %q", got)
	}
}

func TestMGLROMPathFallsBackForNonMiSTerPaths(t *testing.T) {
	if got := mglROMPath("", "/tmp/test.md", "RPGs/test.md"); got != "RPGs/test.md" {
		t.Fatalf("MGL path = %q", got)
	}
}
