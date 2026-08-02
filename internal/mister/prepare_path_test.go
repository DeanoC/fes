package mister

import "testing"

func TestMGLROMPathIsRelativeToMiSTerFat(t *testing.T) {
	if got := mglROMPath("/media/fat/games/MegaDrive/test.md", "test.md"); got != "games/MegaDrive/test.md" {
		t.Fatalf("MGL path = %q", got)
	}
}

func TestMGLROMPathFallsBackForNonMiSTerPaths(t *testing.T) {
	if got := mglROMPath("/tmp/test.md", "RPGs/test.md"); got != "RPGs/test.md" {
		t.Fatalf("MGL path = %q", got)
	}
}
