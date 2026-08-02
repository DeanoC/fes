package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast-POC/protocol"
	"github.com/pelletier/go-toml/v2"
)

type Game struct {
	ID      string          `toml:"id" json:"id"`
	Title   string          `toml:"title" json:"title"`
	System  protocol.System `toml:"system" json:"system"`
	ROMPath string          `toml:"rom_path" json:"rom_path"`
}

type Manifest struct {
	Games []Game `toml:"games" json:"games"`
}

func LoadManifest(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	var manifest Manifest
	decoder := toml.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode game manifest: %w", err)
	}
	seen := make(map[string]struct{}, len(manifest.Games))
	for index, game := range manifest.Games {
		if err := protocol.ValidateGameID(game.ID); err != nil {
			return Manifest{}, fmt.Errorf("game %d ID: %w", index, err)
		}
		if err := protocol.ValidateSystem(game.System); err != nil {
			return Manifest{}, fmt.Errorf("game %q system: %w", game.ID, err)
		}
		if strings.TrimSpace(game.Title) == "" {
			return Manifest{}, fmt.Errorf("game %q title must not be blank", game.ID)
		}
		if !filepath.IsAbs(game.ROMPath) {
			return Manifest{}, fmt.Errorf("game %q ROM path must be absolute", game.ID)
		}
		if _, ok := seen[game.ID]; ok {
			return Manifest{}, fmt.Errorf("duplicate game ID %q", game.ID)
		}
		seen[game.ID] = struct{}{}
	}
	return manifest, nil
}
