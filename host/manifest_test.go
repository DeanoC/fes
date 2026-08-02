package host_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestLoadManifest(t *testing.T) {
	t.Parallel()
	content := `[[games]]
id = "megadrive-test"
title = "Mega Drive test game"
system = "megadrive"
rom_path = "/media/fat/games/MegaDrive/test.md"

[[games]]
id = "snes-test"
title = "SNES test game"
system = "snes"
rom_path = "/media/fat/games/SNES/test.sfc"
`
	path := filepath.Join(t.TempDir(), "games.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := host.LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Games) != 2 || manifest.Games[1].System != protocol.SystemSNES {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestLoadManifestRejectsInvalidGames(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"duplicate":     "[[games]]\nid=\"same\"\ntitle=\"One\"\nsystem=\"snes\"\nrom_path=\"/media/fat/games/SNES/one.sfc\"\n[[games]]\nid=\"same\"\ntitle=\"Two\"\nsystem=\"snes\"\nrom_path=\"/media/fat/games/SNES/two.sfc\"\n",
		"unsupported":   "[[games]]\nid=\"nes-test\"\ntitle=\"NES\"\nsystem=\"nes\"\nrom_path=\"/media/fat/games/NES/test.nes\"\n",
		"relative path": "[[games]]\nid=\"snes-test\"\ntitle=\"SNES\"\nsystem=\"snes\"\nrom_path=\"relative.sfc\"\n",
		"blank title":   "[[games]]\nid=\"snes-test\"\ntitle=\" \"\nsystem=\"snes\"\nrom_path=\"/media/fat/games/SNES/test.sfc\"\n",
		"bad ID":        "[[games]]\nid=\"Bad ID\"\ntitle=\"SNES\"\nsystem=\"snes\"\nrom_path=\"/media/fat/games/SNES/test.sfc\"\n",
		"unknown field": "[[games]]\nid=\"snes-test\"\ntitle=\"SNES\"\nsystem=\"snes\"\nrom_path=\"/media/fat/games/SNES/test.sfc\"\ncore=\"SNES\"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "games.toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := host.LoadManifest(path); err == nil {
				t.Fatal("invalid manifest loaded")
			}
		})
	}
}
